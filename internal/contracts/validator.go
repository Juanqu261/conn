package contracts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// Registry holds compiled Conn contracts keyed by schema $id.
type Registry struct {
	schemas map[string]*jsonschema.Schema
}

// Load compiles every document matching pattern in fsys as Draft 2020-12.
// References resolve only among the loaded documents; nothing is fetched
// over the network.
func Load(fsys fs.FS, pattern string) (*Registry, error) {
	files, err := fs.Glob(fsys, pattern)
	if err != nil {
		return nil, fmt.Errorf("list schemas: %w", err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no schemas match %q", pattern)
	}
	sort.Strings(files)

	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.UseLoader(denyExternalLoader{})

	ids := make([]string, 0, len(files))
	for _, file := range files {
		raw, err := fs.ReadFile(fsys, file)
		if err != nil {
			return nil, fmt.Errorf("read schema %s: %w", file, err)
		}
		document, err := decodeJSON(raw)
		if err != nil {
			return nil, fmt.Errorf("decode schema %s: %w", file, err)
		}
		id, err := schemaID(document)
		if err != nil {
			return nil, fmt.Errorf("schema %s: %w", path.Base(file), err)
		}
		if err := compiler.AddResource(id, document); err != nil {
			return nil, fmt.Errorf("register schema %s: %w", id, err)
		}
		ids = append(ids, id)
	}

	registry := &Registry{schemas: make(map[string]*jsonschema.Schema, len(ids))}
	for _, id := range ids {
		schema, err := compiler.Compile(id)
		if err != nil {
			return nil, fmt.Errorf("compile schema %s: %w", id, err)
		}
		registry.schemas[id] = schema
	}
	return registry, nil
}

// Violation is one leaf schema failure. InstancePath is a JSON Pointer into
// the validated document; the empty string denotes the document root.
type Violation struct {
	InstancePath string
	Message      string
}

// ValidationError reports a structurally invalid document. It carries no
// validator library types so callers can map it to their own error contract.
type ValidationError struct {
	Schema     string
	Violations []Violation
}

func (e *ValidationError) Error() string {
	if len(e.Violations) == 0 {
		return fmt.Sprintf("document does not match %s", e.Schema)
	}
	first := e.Violations[0]
	location := first.InstancePath
	if location == "" {
		location = "/"
	}
	return fmt.Sprintf("document does not match %s at %s: %s", e.Schema, location, first.Message)
}

// ValidateJSON validates exactly one JSON document against the schema id.
// Structural failures are returned as *ValidationError.
func (r *Registry) ValidateJSON(id string, document []byte) error {
	schema, ok := r.schemas[id]
	if !ok {
		return fmt.Errorf("unknown schema %q", id)
	}
	value, err := decodeJSON(document)
	if err != nil {
		return fmt.Errorf("decode document: %w", err)
	}
	err = schema.Validate(value)
	if err == nil {
		return nil
	}
	validationErr, ok := err.(*jsonschema.ValidationError)
	if !ok {
		return err
	}
	result := &ValidationError{Schema: id}
	collectViolations(validationErr, &result.Violations)
	return result
}

func collectViolations(err *jsonschema.ValidationError, out *[]Violation) {
	if len(err.Causes) == 0 {
		*out = append(*out, Violation{
			InstancePath: jsonPointer(err.InstanceLocation),
			Message:      err.ErrorKind.LocalizedString(englishPrinter),
		})
		return
	}
	for _, cause := range err.Causes {
		collectViolations(cause, out)
	}
}

func jsonPointer(tokens []string) string {
	var b strings.Builder
	for _, token := range tokens {
		b.WriteByte('/')
		b.WriteString(pointerEscaper.Replace(token))
	}
	return b.String()
}

var (
	pointerEscaper = strings.NewReplacer("~", "~0", "/", "~1")
	englishPrinter = message.NewPrinter(language.English)
)

func schemaID(document any) (string, error) {
	object, ok := document.(map[string]any)
	if !ok {
		return "", fmt.Errorf("schema is not a JSON object")
	}
	id, ok := object["$id"].(string)
	if !ok || id == "" {
		return "", fmt.Errorf("schema has no $id")
	}
	return id, nil
}

type denyExternalLoader struct{}

func (denyExternalLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("external schema reference is not registered: %s", url)
}

func decodeJSON(document []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.UseNumber()

	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}

	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("multiple JSON values are not allowed")
		}
		return nil, err
	}

	return value, nil
}
