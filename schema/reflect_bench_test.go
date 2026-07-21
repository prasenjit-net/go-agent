package schema

import "testing"

// manyFieldsInput exercises FromStruct against a wider struct than
// weatherInput/nestedInput (defined in reflect_test.go), to show how cost
// scales with field count — the shape most likely to regress silently if a
// change adds an accidental extra reflection pass per field.
type manyFieldsInput struct {
	A string   `json:"a" jsonschema:"required"`
	B string   `json:"b,omitempty"`
	C int      `json:"c" jsonschema:"required"`
	D int64    `json:"d,omitempty"`
	E float64  `json:"e,omitempty"`
	F bool     `json:"f,omitempty"`
	G []string `json:"g,omitempty"`
	H *string  `json:"h,omitempty"`
	I string   `json:"i,omitempty" jsonschema:"enum=x;y;z"`
	J string   `json:"j,omitempty" jsonschema:"description=A field with a description"`
}

func BenchmarkFromStruct_Flat(b *testing.B) {
	for i := 0; i < b.N; i++ {
		FromStruct[weatherInput]()
	}
}

func BenchmarkFromStruct_Nested(b *testing.B) {
	for i := 0; i < b.N; i++ {
		FromStruct[nestedInput]()
	}
}

func BenchmarkFromStruct_ManyFields(b *testing.B) {
	for i := 0; i < b.N; i++ {
		FromStruct[manyFieldsInput]()
	}
}
