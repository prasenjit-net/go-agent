package schema

import (
	"encoding/json"
	"testing"
)

type weatherInput struct {
	City  string `json:"city" jsonschema:"required,description=City name, e.g. Paris"`
	Units string `json:"units,omitempty" jsonschema:"enum=celsius;fahrenheit,description=Temperature unit"`
}

func TestFromStruct_RequiredAndOptional(t *testing.T) {
	s := FromStruct[weatherInput]()

	if s.Type != TypeObject {
		t.Fatalf("Type = %q, want object", s.Type)
	}
	if len(s.Required) != 1 || s.Required[0] != "city" {
		t.Fatalf("Required = %v, want [city]", s.Required)
	}
	city, ok := s.Properties["city"]
	if !ok {
		t.Fatal("missing city property")
	}
	if city.Type != TypeString {
		t.Errorf("city.Type = %q, want string", city.Type)
	}
	if city.Description != "City name, e.g. Paris" {
		t.Errorf("city.Description = %q, want the comma-containing description preserved verbatim", city.Description)
	}

	units, ok := s.Properties["units"]
	if !ok {
		t.Fatal("missing units property")
	}
	if len(units.Enum) != 2 || units.Enum[0] != "celsius" || units.Enum[1] != "fahrenheit" {
		t.Errorf("units.Enum = %v, want [celsius fahrenheit]", units.Enum)
	}
}

type nestedInput struct {
	Name    string   `json:"name" jsonschema:"required"`
	Tags    []string `json:"tags,omitempty"`
	Address struct {
		City string `json:"city" jsonschema:"required"`
	} `json:"address"`
	Optional *string `json:"optional,omitempty"`
}

func TestFromStruct_NestedAndSlices(t *testing.T) {
	s := FromStruct[nestedInput]()

	tags, ok := s.Properties["tags"]
	if !ok || tags.Type != TypeArray || tags.Items == nil || tags.Items.Type != TypeString {
		t.Fatalf("tags schema wrong: %+v", tags)
	}

	addr, ok := s.Properties["address"]
	if !ok || addr.Type != TypeObject {
		t.Fatalf("address schema wrong: %+v", addr)
	}
	if len(addr.Required) != 1 || addr.Required[0] != "city" {
		t.Errorf("address.Required = %v, want [city]", addr.Required)
	}

	// name is required (no omitempty, not a pointer); optional and tags are
	// not (pointer / omitempty respectively). address has no omitempty and
	// isn't a pointer, so it's required too.
	wantRequired := map[string]bool{"name": true, "address": true}
	for _, r := range s.Required {
		if !wantRequired[r] {
			t.Errorf("unexpected required field %q", r)
		}
		delete(wantRequired, r)
	}
	if len(wantRequired) != 0 {
		t.Errorf("missing required fields: %v", wantRequired)
	}
	for _, r := range s.Required {
		if r == "tags" || r == "optional" {
			t.Errorf("field %q should not be required", r)
		}
	}
}

func TestSchema_MarshalJSON_StablePropertyOrder(t *testing.T) {
	s := FromStruct[weatherInput]()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}

	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got["type"] != "object" {
		t.Errorf("type = %v, want object", got["type"])
	}
	if got["additionalProperties"] != false {
		t.Errorf("additionalProperties = %v, want false", got["additionalProperties"])
	}

	// PropertyOrder should place "city" before "units" in the raw bytes,
	// matching struct field declaration order.
	cityIdx := indexOf(string(b), `"city"`)
	unitsIdx := indexOf(string(b), `"units"`)
	if cityIdx < 0 || unitsIdx < 0 || cityIdx > unitsIdx {
		t.Errorf("expected city before units in marshaled output, got %s", b)
	}
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

type ptrRequiredInput struct {
	// A pointer field explicitly tagged required should still end up in
	// Required, even though pointers are optional by default.
	Count *int `json:"count" jsonschema:"required"`
}

func TestFromStruct_ExplicitRequiredOnPointer(t *testing.T) {
	s := FromStruct[ptrRequiredInput]()
	if len(s.Required) != 1 || s.Required[0] != "count" {
		t.Errorf("Required = %v, want [count]", s.Required)
	}
	count := s.Properties["count"]
	if count.Type != TypeInteger {
		t.Errorf("count.Type = %q, want integer", count.Type)
	}
}
