package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	canonical "github.com/Juanqu261/conn/contracts"
	"github.com/Juanqu261/conn/internal/contracts"
)

// Document is a protocol type with a canonical schema.
type Document interface {
	SchemaID() string
}

var schemas = sync.OnceValues(func() (*contracts.Registry, error) {
	return contracts.Load(canonical.V1, "v1/*.schema.json")
})

// Decode validates data against T's schema and then decodes it into T.
// Structural failures are returned as *contracts.ValidationError.
func Decode[T Document](data []byte) (T, error) {
	var doc T
	registry, err := schemas()
	if err != nil {
		return doc, err
	}
	if err := registry.ValidateJSON(doc.SchemaID(), data); err != nil {
		return doc, err
	}
	if err := strictUnmarshal(data, &doc); err != nil {
		return doc, fmt.Errorf("decode %s: %w", doc.SchemaID(), err)
	}
	return doc, nil
}

// Encode encodes doc and validates the result against its schema, so no
// non-conforming document leaves the process.
func Encode[T Document](doc T) ([]byte, error) {
	registry, err := schemas()
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", doc.SchemaID(), err)
	}
	if err := registry.ValidateJSON(doc.SchemaID(), data); err != nil {
		return nil, fmt.Errorf("encoded %s does not match its schema: %w", doc.SchemaID(), err)
	}
	return data, nil
}

func strictUnmarshal(data []byte, v any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("unexpected data after the JSON document")
	}
	return nil
}
