// Package analyze watches ingested spans: schema learning, contract drift,
// improvise detection. All passive; findings are recorded, never enforced.
package analyze

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// maxDepth bounds recursion into nested payloads.
const maxDepth = 6

// InferSchema returns the JSON Schema subset capybara would learn from one
// recorded output body, falling back to a string shape for non-JSON output.
func InferSchema(body string) json.RawMessage {
	var v any
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		v = body
	}
	raw, _ := json.Marshal(infer(v, 0))
	return raw
}

// SchemaViolation describes how a recorded output breaks a schema, in the same
// terms as a drift finding. Empty when the output still fits.
func SchemaViolation(schema json.RawMessage, body string) string {
	var want jsonSchema
	if json.Unmarshal(schema, &want) != nil {
		return ""
	}
	var v any
	if json.Unmarshal([]byte(body), &v) != nil {
		v = body
	}
	d := diffSchemas(&want, infer(v, 0), "")
	if !d.breaking() {
		return ""
	}
	var parts []string
	if len(d.Missing) > 0 {
		parts = append(parts, "missing "+strings.Join(d.Missing, ", "))
	}
	for _, r := range d.Retyped {
		parts = append(parts, fmt.Sprintf("%s: want %s, got %s", r.Field, r.Want, r.Got))
	}
	return strings.Join(parts, "; ")
}

// jsonSchema is the learned shape of a value: a JSON Schema subset covering
// field set, types and nullability. Declared Pydantic schemas parse into the
// same subset.
type jsonSchema struct {
	Types      typeList               `json:"type,omitempty"`
	Properties map[string]*jsonSchema `json:"properties,omitempty"`
	Required   []string               `json:"required,omitempty"`
	Items      *jsonSchema            `json:"items,omitempty"`
}

// typeList accepts JSON Schema's string-or-array type field.
type typeList []string

func (t *typeList) UnmarshalJSON(data []byte) error {
	var one string
	if err := json.Unmarshal(data, &one); err == nil {
		*t = typeList{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(data, &many); err != nil {
		return fmt.Errorf("type field: %w", err)
	}
	*t = many
	return nil
}

func (t typeList) has(typ string) bool {
	return slices.Contains(t, typ)
}

func jsonType(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	default:
		return "object"
	}
}

// infer builds the schema of one observed value.
func infer(v any, depth int) *jsonSchema {
	s := &jsonSchema{Types: typeList{jsonType(v)}}
	if depth >= maxDepth {
		return s
	}
	switch t := v.(type) {
	case map[string]any:
		s.Properties = make(map[string]*jsonSchema, len(t))
		for k, val := range t {
			s.Properties[k] = infer(val, depth+1)
			s.Required = append(s.Required, k)
		}
		sort.Strings(s.Required)
	case []any:
		for _, el := range t {
			s.Items = mergeSchemas(s.Items, infer(el, depth+1))
		}
	}
	return s
}

// mergeSchemas widens a to also accept b: union of types, intersection of
// required fields, recursive merge of properties and items.
func mergeSchemas(a, b *jsonSchema) *jsonSchema {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	out := &jsonSchema{Types: slices.Clone(a.Types)}
	for _, t := range b.Types {
		if !out.Types.has(t) {
			out.Types = append(out.Types, t)
		}
	}
	sort.Strings(out.Types)
	if a.Properties != nil || b.Properties != nil {
		out.Properties = make(map[string]*jsonSchema)
		for k, v := range a.Properties {
			out.Properties[k] = mergeSchemas(v, b.Properties[k])
		}
		for k, v := range b.Properties {
			if _, ok := a.Properties[k]; !ok {
				out.Properties[k] = v
			}
		}
	}
	for _, k := range a.Required {
		if slices.Contains(b.Required, k) {
			out.Required = append(out.Required, k)
		}
	}
	out.Items = mergeSchemas(a.Items, b.Items)
	return out
}

// schemaDiff is the contract check of one observation against the current schema.
type schemaDiff struct {
	Missing []string  `json:"missing,omitempty"`
	Retyped []retyped `json:"retyped,omitempty"`
	widened bool
}

type retyped struct {
	Field string `json:"field"`
	Want  string `json:"want"`
	Got   string `json:"got"`
}

func (d schemaDiff) breaking() bool {
	return len(d.Missing) > 0 || len(d.Retyped) > 0
}

// diffSchemas validates observation obs against schema at path. New fields
// only widen; removed or retyped fields are breaking.
func diffSchemas(schema, obs *jsonSchema, path string) schemaDiff {
	var d schemaDiff
	if schema == nil || obs == nil {
		return d
	}
	obsType := ""
	if len(obs.Types) > 0 {
		obsType = obs.Types[0]
	}
	if obsType != "" && !schema.Types.has(obsType) {
		field := path
		if field == "" {
			field = "$"
		}
		d.Retyped = append(d.Retyped, retyped{
			Field: field, Want: joinTypes(schema.Types), Got: obsType,
		})
		return d
	}
	for _, k := range schema.Required {
		if _, ok := obs.Properties[k]; !ok {
			d.Missing = append(d.Missing, joinPath(path, k))
		}
	}
	for k, sub := range obs.Properties {
		want, ok := schema.Properties[k]
		if !ok {
			d.widened = true
			continue
		}
		sub := diffSchemas(want, sub, joinPath(path, k))
		d.Missing = append(d.Missing, sub.Missing...)
		d.Retyped = append(d.Retyped, sub.Retyped...)
		d.widened = d.widened || sub.widened
	}
	if schema.Items != nil && obs.Items != nil {
		sub := diffSchemas(schema.Items, obs.Items, path+"[]")
		d.Missing = append(d.Missing, sub.Missing...)
		d.Retyped = append(d.Retyped, sub.Retyped...)
		d.widened = d.widened || sub.widened
	}
	// Deterministic order: finding dedupe compares detail_json byte for byte.
	sort.Strings(d.Missing)
	sort.Slice(d.Retyped, func(i, j int) bool { return d.Retyped[i].Field < d.Retyped[j].Field })
	return d
}

func hasContainer(t typeList) bool {
	return t.has("object") || t.has("array")
}

// rootEncodingFlip reports whether the whole break is the top-level value
// crossing between free text and JSON. A shell tool that usually prints text
// and once prints a line of JSON never promised either shape: calling that
// drift replaces the contract with the accident, and every later plain-text
// call is then filed as malformed. Field-level retypes are untouched, which is
// where real drift shows up.
func rootEncodingFlip(current, obs *jsonSchema, d schemaDiff) bool {
	if len(d.Missing) > 0 || len(d.Retyped) != 1 || d.Retyped[0].Field != "$" {
		return false
	}
	return hasContainer(current.Types) != hasContainer(obs.Types)
}

func joinPath(path, field string) string {
	if path == "" {
		return field
	}
	return path + "." + field
}

func joinTypes(t typeList) string {
	if len(t) == 1 {
		return t[0]
	}
	out := ""
	for i, s := range t {
		if i > 0 {
			out += "|"
		}
		out += s
	}
	return out
}
