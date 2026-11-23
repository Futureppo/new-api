package ir

import (
	"bytes"
	"encoding/json"
	"strings"
)

const maxSchemaNormalizeDepth = 64

// NormalizeJSONSchemaTypes recursively normalizes uppercase/mixed-case OpenAPI / Gemini
// JSON Schema type names (e.g. "OBJECT", "STRING", "BOOLEAN", "INTEGER", "NUMBER", "ARRAY",
// "NULL") to canonical lowercase JSON Schema types.
// Returns raw untouched when no normalization is needed.
func NormalizeJSONSchemaTypes(raw json.RawMessage) json.RawMessage {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || trimmed[0] != '{' {
		return raw
	}
	if !bytes.Contains(trimmed, []byte(`"type"`)) {
		return raw
	}
	var root map[string]any
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	if err := dec.Decode(&root); err != nil || root == nil {
		return raw
	}
	if !normalizeSchemaNodeTypes(root, 0) {
		return raw
	}
	out, err := json.Marshal(root)
	if err != nil {
		return raw
	}
	return json.RawMessage(out)
}

// NormalizeJSONSchemaTypesInPlace recursively normalizes schema type strings in an
// already-unmarshaled JSON Schema object map and reports whether any field changed.
func NormalizeJSONSchemaTypesInPlace(node any) bool {
	return normalizeSchemaNodeTypes(node, 0)
}

func canonicalJSONSchemaType(raw string) (string, bool) {
	lower := strings.ToLower(strings.TrimSpace(raw))
	switch lower {
	case "object", "string", "integer", "number", "boolean", "array", "null":
		return lower, lower != raw
	default:
		return raw, false
	}
}

func normalizeSchemaNodeTypes(node any, depth int) bool {
	if depth > maxSchemaNormalizeDepth {
		return false
	}
	changed := false
	switch v := node.(type) {
	case map[string]any:
		switch tv := v["type"].(type) {
		case string:
			if norm, ok := canonicalJSONSchemaType(tv); ok {
				v["type"] = norm
				changed = true
			}
		case []any:
			for i, item := range tv {
				if s, ok := item.(string); ok {
					if norm, mod := canonicalJSONSchemaType(s); mod {
						tv[i] = norm
						changed = true
					}
				}
			}
		}

		// Map-of-schemas keywords: recurse into values WITHOUT treating the map itself as a schema node
		// (so a user property literally named "type" inside "properties" is never mutated as a keyword).
		for _, mapKey := range []string{"properties", "patternProperties", "dependentSchemas", "$defs", "definitions"} {
			if subMap, ok := v[mapKey].(map[string]any); ok {
				for _, child := range subMap {
					if normalizeSchemaNodeTypes(child, depth+1) {
						changed = true
					}
				}
			}
		}

		if items, ok := v["items"]; ok {
			if normalizeSchemaNodeTypes(items, depth+1) {
				changed = true
			}
		}

		for _, listKey := range []string{"prefixItems", "anyOf", "oneOf", "allOf"} {
			if list, ok := v[listKey].([]any); ok {
				for _, child := range list {
					if normalizeSchemaNodeTypes(child, depth+1) {
						changed = true
					}
				}
			}
		}

		for _, subKey := range []string{"additionalProperties", "not", "if", "then", "else"} {
			if subObj, ok := v[subKey].(map[string]any); ok {
				if normalizeSchemaNodeTypes(subObj, depth+1) {
					changed = true
				}
			}
		}

	case []any:
		for _, child := range v {
			if normalizeSchemaNodeTypes(child, depth+1) {
				changed = true
			}
		}
	}
	return changed
}
