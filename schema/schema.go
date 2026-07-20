// Package schema implements the small JSON Schema subset used to describe
// tool inputs to Claude, OpenAI, and Gemini. Most callers never construct a
// Schema by hand — agent.NewTool derives one automatically from a Go struct
// via FromStruct (see reflect.go). The type is exported so it can also be
// built or edited directly, e.g. to bridge in a tool whose schema is
// discovered at runtime (an MCP server) rather than known at compile time.
package schema

import (
	"bytes"
	"encoding/json"
)

// Type is a JSON Schema primitive type.
type Type string

const (
	TypeObject  Type = "object"
	TypeString  Type = "string"
	TypeNumber  Type = "number"
	TypeInteger Type = "integer"
	TypeBoolean Type = "boolean"
	TypeArray   Type = "array"
)

// Schema is a JSON Schema document restricted to the subset every
// first-class provider's tool/function-declaration format understands:
// object/string/number/integer/boolean/array, nested properties, required
// lists, array items, and string enums.
type Schema struct {
	Type        Type
	Description string

	// Object
	Properties    map[string]*Schema
	PropertyOrder []string // declaration order; used to keep marshaling stable
	Required      []string

	// Array
	Items *Schema

	// String
	Enum []string
}

// MarshalJSON renders the schema as standard JSON Schema. Object properties
// are emitted in PropertyOrder so output is stable and diff-friendly rather
// than reordered on every call (Go map iteration order is randomized).
func (s *Schema) MarshalJSON() ([]byte, error) {
	if s == nil {
		return []byte("null"), nil
	}

	var buf bytes.Buffer
	buf.WriteByte('{')
	first := true

	field := func(key string, val any) error {
		if !first {
			buf.WriteByte(',')
		}
		first = false
		kb, err := json.Marshal(key)
		if err != nil {
			return err
		}
		buf.Write(kb)
		buf.WriteByte(':')
		vb, err := json.Marshal(val)
		if err != nil {
			return err
		}
		buf.Write(vb)
		return nil
	}

	if s.Type != "" {
		if err := field("type", s.Type); err != nil {
			return nil, err
		}
	}
	if s.Description != "" {
		if err := field("description", s.Description); err != nil {
			return nil, err
		}
	}
	if len(s.Enum) > 0 {
		if err := field("enum", s.Enum); err != nil {
			return nil, err
		}
	}
	if s.Type == TypeArray && s.Items != nil {
		if err := field("items", s.Items); err != nil {
			return nil, err
		}
	}
	if s.Type == TypeObject {
		if !first {
			buf.WriteByte(',')
		}
		first = false
		buf.WriteString(`"properties":{`)

		written := make(map[string]bool, len(s.Properties))
		propFirst := true
		writeProp := func(name string) error {
			prop, ok := s.Properties[name]
			if !ok || written[name] {
				return nil
			}
			written[name] = true
			if !propFirst {
				buf.WriteByte(',')
			}
			propFirst = false
			kb, err := json.Marshal(name)
			if err != nil {
				return err
			}
			buf.Write(kb)
			buf.WriteByte(':')
			vb, err := json.Marshal(prop)
			if err != nil {
				return err
			}
			buf.Write(vb)
			return nil
		}
		for _, name := range s.PropertyOrder {
			if err := writeProp(name); err != nil {
				return nil, err
			}
		}
		// Properties present on a hand-built Schema but not listed in
		// PropertyOrder still get emitted, just without a guaranteed order.
		for name := range s.Properties {
			if err := writeProp(name); err != nil {
				return nil, err
			}
		}
		buf.WriteByte('}')

		if len(s.Required) > 0 {
			if err := field("required", s.Required); err != nil {
				return nil, err
			}
		}
		if err := field("additionalProperties", false); err != nil {
			return nil, err
		}
	}

	buf.WriteByte('}')
	return buf.Bytes(), nil
}
