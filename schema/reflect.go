package schema

import (
	"reflect"
	"strings"
)

// FromStruct derives a Schema for T via reflection. T is expected to be a
// struct type (or a pointer to one); every other kind is reflected as a
// best-effort leaf schema so FromStruct never panics.
//
// Two struct tags drive generation:
//
//   - `json:"name,omitempty"` — the standard encoding/json tag. The field
//     name becomes the schema property name; a `-` name excludes the field
//     entirely; `omitempty` marks the field optional (see the required rule
//     below).
//   - `jsonschema:"required,description=...,enum=a;b;c"` — recognized keys:
//     "required" forces the field into the schema's required list;
//     "description=" must be the last key present and consumes the rest of
//     the tag verbatim (including any embedded commas), so a description
//     can itself contain commas; "enum=" takes a semicolon-separated list
//     (semicolon, not comma, so it survives the outer comma-split).
//
// A field is required in the generated schema if it is explicitly tagged
// `required`, or if it is neither a pointer nor tagged `omitempty` — i.e.
// the same "zero value is meaningful vs. this is optional" convention Go
// developers already use for JSON marshaling.
func FromStruct[T any]() *Schema {
	var zero T
	return fromType(reflect.TypeOf(zero))
}

func fromType(t reflect.Type) *Schema {
	if t == nil {
		return &Schema{Type: TypeString}
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		return structSchema(t)
	case reflect.String:
		return &Schema{Type: TypeString}
	case reflect.Bool:
		return &Schema{Type: TypeBoolean}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return &Schema{Type: TypeInteger}
	case reflect.Float32, reflect.Float64:
		return &Schema{Type: TypeNumber}
	case reflect.Slice, reflect.Array:
		return &Schema{Type: TypeArray, Items: fromType(t.Elem())}
	case reflect.Map:
		// Represented as a permissive object; JSON map keys are always
		// strings, so only the value type would be reflectable, and most
		// tool inputs are better modeled as an explicit nested struct.
		return &Schema{Type: TypeObject, Properties: map[string]*Schema{}}
	default:
		return &Schema{Type: TypeString}
	}
}

func structSchema(t reflect.Type) *Schema {
	s := &Schema{Type: TypeObject, Properties: map[string]*Schema{}}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}

		name, omitEmpty, skip := jsonFieldName(f)
		if skip {
			continue
		}

		fieldSchema := fromType(f.Type)
		required, description, enum := parseJSONSchemaTag(f.Tag.Get("jsonschema"))
		if description != "" {
			fieldSchema.Description = description
		}
		if len(enum) > 0 && fieldSchema.Type == TypeString {
			fieldSchema.Enum = enum
		}

		isPointer := f.Type.Kind() == reflect.Pointer
		if required || (!isPointer && !omitEmpty) {
			s.Required = append(s.Required, name)
		}

		s.Properties[name] = fieldSchema
		s.PropertyOrder = append(s.PropertyOrder, name)
	}
	return s
}

func jsonFieldName(f reflect.StructField) (name string, omitEmpty bool, skip bool) {
	tag := f.Tag.Get("json")
	if tag == "-" {
		return "", false, true
	}
	parts := strings.Split(tag, ",")
	name = f.Name
	if parts[0] != "" {
		name = parts[0]
	}
	for _, p := range parts[1:] {
		if p == "omitempty" {
			omitEmpty = true
		}
	}
	return name, omitEmpty, false
}

func parseJSONSchemaTag(tag string) (required bool, description string, enum []string) {
	if tag == "" {
		return
	}
	parts := strings.Split(tag, ",")
	for _, raw := range parts {
		p := strings.TrimSpace(raw)
		switch {
		case p == "required":
			required = true
		case strings.HasPrefix(p, "description="):
			idx := strings.Index(tag, "description=")
			description = tag[idx+len("description="):]
			return required, description, enum
		case strings.HasPrefix(p, "enum="):
			enum = strings.Split(strings.TrimPrefix(p, "enum="), ";")
		}
	}
	return required, description, enum
}
