package mcp

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"
	"sync"

	canonical "github.com/Juanqu261/conn/contracts"
)

var canonicalSchemas = sync.OnceValues(loadCanonicalSchemas)

// The MCP schemas compose Conn's canonical protocol vocabulary. The only
// deliberate relaxations are nested intent_id fields that the adapter assigns
// or defaults before decoding through protocol.Decode.
func objectiveToolSchema() map[string]any {
	return objectSchema(nil, map[string]any{
		"project_id":   idSchema("Project ID. Defaults to CONN_PROJECT."),
		"objective_id": idSchema("Objective ID. Optional when the Project has one Objective."),
		"revision":     revisionSchema("Objective revision. Defaults to the latest."),
	})
}

func blueprintToolSchema() map[string]any {
	return objectSchema(nil, map[string]any{
		"project_id": idSchema("Project ID. Defaults to CONN_PROJECT."),
		"blueprint_id": idSchema("Blueprint ID. Optional when the Project has one Blueprint, or when the signed-in Human " +
			"has Charters in only one Blueprint."),
		"revision": revisionSchema("Blueprint revision. Defaults to the active revision, or the latest when none is active."),
	})
}

func updatesToolSchema() map[string]any {
	after := canonicalDefinition("urn:conn:schema:v1:common", "eventSequence")
	after["minimum"] = 0
	after["description"] = "The as_of_event value from the last Conn read, or 0 for the full history."
	return objectSchema([]string{"after"}, map[string]any{
		"project_id": idSchema("Project ID. Defaults to CONN_PROJECT."),
		"after":      after,
		"limit": map[string]any{
			"type":        "integer",
			"minimum":     1,
			"maximum":     500,
			"description": "Maximum events to return. Defaults to 100.",
		},
	})
}

func publishIntentToolSchema() map[string]any {
	intent := canonicalRecord("urn:conn:schema:v1:intent-input")
	removeRequired(intent, "intent_id")
	intent["description"] = "Participant-authored Intent content. Omit intent_id to let Conn assign a UUIDv7. Conn derives Human and Participant authority."
	return objectSchema([]string{"intent"}, map[string]any{
		"project_id": idSchema("Project ID. Defaults to CONN_PROJECT."),
		"intent":     intent,
	})
}

func reviseIntentToolSchema() map[string]any {
	intent := canonicalRecord("urn:conn:schema:v1:intent-input")
	removeRequired(intent, "intent_id")
	intent["description"] = "Complete replacement Intent content. Omitted intent_id defaults to the top-level intent_id; a supplied value must match it."
	return objectSchema([]string{"intent_id", "expected_revision", "intent"}, map[string]any{
		"project_id":        idSchema("Project ID. Defaults to CONN_PROJECT."),
		"intent_id":         idSchema("Owned active Intent UUID to revise."),
		"expected_revision": revisionSchema("Current Intent revision required for optimistic concurrency."),
		"intent":            intent,
	})
}

func intentIDToolSchema(description string) map[string]any {
	return objectSchema([]string{"intent_id"}, map[string]any{
		"project_id": idSchema("Project ID. Defaults to CONN_PROJECT."),
		"intent_id":  idSchema(description),
	})
}

func collisionsToolSchema() map[string]any {
	return objectSchema(nil, map[string]any{
		"project_id": idSchema("Project ID. Defaults to CONN_PROJECT."),
		"intent_id":  idSchema("Optional Intent UUID. When present, return only its open collision assessments."),
	})
}

func projectToolSchema() map[string]any {
	return objectSchema(nil, map[string]any{
		"project_id": idSchema("Project ID. Defaults to CONN_PROJECT."),
	})
}

func collisionIDToolSchema() map[string]any {
	return objectSchema([]string{"collision_id"}, map[string]any{
		"project_id":   idSchema("Project ID. Defaults to CONN_PROJECT."),
		"collision_id": idSchema("Collision assessment UUID."),
	})
}

func proposalToolSchema() map[string]any {
	proposal := canonicalRecord("urn:conn:schema:v1:resolution-proposal-input")
	proposal["description"] = "Non-binding participant resolution proposal content. It cannot carry decision authority."
	return objectSchema([]string{"collision_id", "proposal"}, map[string]any{
		"project_id":   idSchema("Project ID. Defaults to CONN_PROJECT."),
		"collision_id": idSchema("Collision assessment UUID."),
		"proposal":     proposal,
	})
}

func evidenceToolSchema() map[string]any {
	evidence := canonicalRecord("urn:conn:schema:v1:evidence-attachment-input")
	removeRequired(evidence, "intent_id")
	evidence["description"] = "Bounded reference-only evidence. Omitted intent_id defaults to the top-level intent_id; a supplied value must match it."
	return objectSchema([]string{"intent_id", "evidence"}, map[string]any{
		"project_id": idSchema("Project ID. Defaults to CONN_PROJECT."),
		"intent_id":  idSchema("Owned active Intent UUID."),
		"evidence":   evidence,
	})
}

func objectSchema(required []string, properties map[string]any) map[string]any {
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func idSchema(description string) map[string]any {
	schema := canonicalDefinition("urn:conn:schema:v1:common", "uuidv7")
	schema["description"] = description + " It must be a canonical lowercase UUIDv7."
	return schema
}

func revisionSchema(description string) map[string]any {
	schema := canonicalDefinition("urn:conn:schema:v1:common", "revision")
	schema["description"] = description
	return schema
}

func canonicalRecord(id string) map[string]any {
	documents, err := canonicalSchemas()
	if err != nil {
		panic(fmt.Sprintf("load canonical MCP schemas: %v", err))
	}
	document, ok := documents[id]
	if !ok {
		panic(fmt.Sprintf("canonical MCP schema %q is missing", id))
	}
	resolved, err := inlineSchema(document, id, documents, map[string]bool{})
	if err != nil {
		panic(fmt.Sprintf("resolve canonical MCP schema %q: %v", id, err))
	}
	result, ok := resolved.(map[string]any)
	if !ok {
		panic(fmt.Sprintf("canonical MCP schema %q is not an object", id))
	}
	return result
}

func canonicalDefinition(id, name string) map[string]any {
	documents, err := canonicalSchemas()
	if err != nil {
		panic(fmt.Sprintf("load canonical MCP schemas: %v", err))
	}
	value, err := resolveJSONPointer(documents[id], "/$defs/"+name)
	if err != nil {
		panic(fmt.Sprintf("resolve canonical definition %s#/$defs/%s: %v", id, name, err))
	}
	resolved, err := inlineSchema(value, id, documents, map[string]bool{})
	if err != nil {
		panic(fmt.Sprintf("inline canonical definition %s#/$defs/%s: %v", id, name, err))
	}
	result, ok := resolved.(map[string]any)
	if !ok {
		panic(fmt.Sprintf("canonical definition %s#/$defs/%s is not an object", id, name))
	}
	return result
}

func loadCanonicalSchemas() (map[string]any, error) {
	paths, err := fs.Glob(canonical.V1, "v1/*.schema.json")
	if err != nil {
		return nil, err
	}
	documents := make(map[string]any, len(paths))
	for _, path := range paths {
		data, err := fs.ReadFile(canonical.V1, path)
		if err != nil {
			return nil, err
		}
		var document map[string]any
		if err := json.Unmarshal(data, &document); err != nil {
			return nil, fmt.Errorf("decode %s: %w", path, err)
		}
		id, ok := document["$id"].(string)
		if !ok || id == "" {
			return nil, fmt.Errorf("schema %s has no $id", path)
		}
		documents[id] = document
	}
	return documents, nil
}

func inlineSchema(value any, baseID string, documents map[string]any, resolving map[string]bool) (any, error) {
	switch value := value.(type) {
	case []any:
		result := make([]any, len(value))
		for i, item := range value {
			resolved, err := inlineSchema(item, baseID, documents, resolving)
			if err != nil {
				return nil, err
			}
			result[i] = resolved
		}
		return result, nil
	case map[string]any:
		if ref, ok := value["$ref"].(string); ok {
			targetID, pointer := splitSchemaRef(baseID, ref)
			key := targetID + "#" + pointer
			if resolving[key] {
				return nil, fmt.Errorf("cyclic schema reference %s", key)
			}
			target, ok := documents[targetID]
			if !ok {
				return nil, fmt.Errorf("unknown schema reference %s", targetID)
			}
			if pointer != "" {
				var err error
				target, err = resolveJSONPointer(target, pointer)
				if err != nil {
					return nil, fmt.Errorf("resolve %s: %w", key, err)
				}
			}
			resolving[key] = true
			resolved, err := inlineSchema(target, targetID, documents, resolving)
			delete(resolving, key)
			if err != nil {
				return nil, err
			}
			result, ok := resolved.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("schema reference %s is not an object", key)
			}
			for sibling, siblingValue := range value {
				if sibling == "$ref" {
					continue
				}
				resolvedSibling, err := inlineSchema(siblingValue, baseID, documents, resolving)
				if err != nil {
					return nil, err
				}
				result[sibling] = resolvedSibling
			}
			return result, nil
		}

		result := make(map[string]any, len(value))
		for key, item := range value {
			if key == "$schema" || key == "$id" || key == "$defs" {
				continue
			}
			resolved, err := inlineSchema(item, baseID, documents, resolving)
			if err != nil {
				return nil, err
			}
			result[key] = resolved
		}
		return result, nil
	default:
		return value, nil
	}
}

func splitSchemaRef(baseID, ref string) (string, string) {
	if strings.HasPrefix(ref, "#") {
		return baseID, strings.TrimPrefix(ref, "#")
	}
	id, fragment, found := strings.Cut(ref, "#")
	if !found {
		return id, ""
	}
	return id, fragment
}

func resolveJSONPointer(document any, pointer string) (any, error) {
	if pointer == "" {
		return document, nil
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, fmt.Errorf("unsupported JSON Pointer %q", pointer)
	}
	current := document
	for _, raw := range strings.Split(strings.TrimPrefix(pointer, "/"), "/") {
		part := strings.ReplaceAll(strings.ReplaceAll(raw, "~1", "/"), "~0", "~")
		object, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%q does not identify an object property", part)
		}
		current, ok = object[part]
		if !ok {
			return nil, fmt.Errorf("property %q is missing", part)
		}
	}
	return current, nil
}

func removeRequired(schema map[string]any, names ...string) {
	forbidden := make(map[string]bool, len(names))
	for _, name := range names {
		forbidden[name] = true
	}
	required, ok := schema["required"].([]any)
	if !ok {
		return
	}
	filtered := make([]any, 0, len(required))
	for _, value := range required {
		name, _ := value.(string)
		if !forbidden[name] {
			filtered = append(filtered, value)
		}
	}
	schema["required"] = filtered
}
