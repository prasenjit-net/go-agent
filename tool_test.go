package agent

import (
	"context"
	"encoding/json"
	"testing"
)

type greetInput struct {
	Name string `json:"name" jsonschema:"required,description=Person to greet"`
}

func TestTool_InvokeUnmarshalsTypedInput(t *testing.T) {
	tool := NewTool("greet", "Greets a person by name.", func(_ context.Context, in greetInput) (ToolResult, error) {
		return TextResult("hello, " + in.Name), nil
	})

	result, err := tool.Invoke(context.Background(), json.RawMessage(`{"name":"Ada"}`))
	if err != nil {
		t.Fatalf("Invoke returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %+v", result)
	}
	tb, ok := result.Content[0].(TextBlock)
	if !ok || tb.Text != "hello, Ada" {
		t.Fatalf("result content = %+v, want TextBlock{hello, Ada}", result.Content)
	}
}

func TestTool_InvokeMalformedInputIsRecoverableNotFatal(t *testing.T) {
	tool := NewTool("greet", "Greets a person by name.", func(_ context.Context, in greetInput) (ToolResult, error) {
		return TextResult("hello, " + in.Name), nil
	})

	result, err := tool.Invoke(context.Background(), json.RawMessage(`{not valid json`))
	if err != nil {
		t.Fatalf("Invoke should never return a Go error for malformed model input, got: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected an error ToolResult for malformed input, got %+v", result)
	}
}

func TestTool_SchemaReflectsInputStruct(t *testing.T) {
	tool := NewTool("greet", "Greets a person by name.", func(_ context.Context, in greetInput) (ToolResult, error) {
		return TextResult("hi"), nil
	})

	sch := tool.Schema()
	if len(sch.Required) != 1 || sch.Required[0] != "name" {
		t.Errorf("Required = %v, want [name]", sch.Required)
	}
	if _, ok := sch.Properties["name"]; !ok {
		t.Errorf("Properties missing %q", "name")
	}
}

func TestToolSet_AddGetList(t *testing.T) {
	a := NewTool("a", "tool a", func(context.Context, greetInput) (ToolResult, error) { return TextResult(""), nil })
	b := NewTool("b", "tool b", func(context.Context, greetInput) (ToolResult, error) { return TextResult(""), nil })

	ts := NewToolSet(a, b)
	if got, ok := ts.Get("a"); !ok || got.Name() != "a" {
		t.Fatalf("Get(a) = %v, %v", got, ok)
	}
	if len(ts.List()) != 2 {
		t.Fatalf("List() len = %d, want 2", len(ts.List()))
	}

	// Re-adding under the same name replaces the tool but keeps its
	// original position.
	a2 := NewTool("a", "tool a v2", func(context.Context, greetInput) (ToolResult, error) { return TextResult(""), nil })
	ts.Add(a2)
	list := ts.List()
	if len(list) != 2 {
		t.Fatalf("List() len after replace = %d, want 2", len(list))
	}
	if list[0].Description() != "tool a v2" {
		t.Errorf("list[0].Description() = %q, want updated description at original position", list[0].Description())
	}
}

func TestErrorResultf_IsError(t *testing.T) {
	r := ErrorResultf("lookup failed: %v", "boom")
	if !r.IsError {
		t.Error("ErrorResultf result should have IsError = true")
	}
	tb, ok := r.Content[0].(TextBlock)
	if !ok || tb.Text != "lookup failed: boom" {
		t.Errorf("content = %+v", r.Content)
	}
}

func TestJSONResult_MarshalsValue(t *testing.T) {
	r := JSONResult(map[string]any{"temp": 72})
	tb, ok := r.Content[0].(TextBlock)
	if !ok {
		t.Fatalf("content = %+v", r.Content)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(tb.Text), &got); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	if got["temp"] != float64(72) {
		t.Errorf("temp = %v, want 72", got["temp"])
	}
}
