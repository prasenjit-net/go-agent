package agent_test

import (
	"context"
	"testing"

	agent "github.com/prasenjit-net/go-agent"
)

func TestSystemPrompt_RenderOrderAndCacheable(t *testing.T) {
	sp := agent.NewSystemPrompt().
		Add("static section").
		AddCacheable("cacheable section").
		AddFunc(func(context.Context) (string, error) { return "dynamic section", nil })

	blocks, err := sp.Render(context.Background())
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	if len(blocks) != 3 {
		t.Fatalf("len(blocks) = %d, want 3", len(blocks))
	}
	if blocks[0].Text != "static section" || blocks[0].Cacheable {
		t.Errorf("blocks[0] = %+v", blocks[0])
	}
	if blocks[1].Text != "cacheable section" || !blocks[1].Cacheable {
		t.Errorf("blocks[1] = %+v", blocks[1])
	}
	if blocks[2].Text != "dynamic section" || blocks[2].Cacheable {
		t.Errorf("blocks[2] = %+v", blocks[2])
	}
}

func TestSystemPrompt_AddFuncEvaluatesFreshEveryRender(t *testing.T) {
	n := 0
	sp := agent.NewSystemPrompt().AddFunc(func(context.Context) (string, error) {
		n++
		if n == 1 {
			return "first", nil
		}
		return "second", nil
	})

	first, err := sp.Render(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := sp.Render(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first[0].Text != "first" || second[0].Text != "second" {
		t.Errorf("first=%q second=%q, want fresh evaluation on each Render call", first[0].Text, second[0].Text)
	}
}

func TestSystemPrompt_AddTemplate(t *testing.T) {
	sp := agent.NewSystemPrompt().AddTemplate("Hello, {{.Name}}!", struct{ Name string }{Name: "Ada"})
	blocks, err := sp.Render(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if blocks[0].Text != "Hello, Ada!" {
		t.Errorf("blocks[0].Text = %q, want %q", blocks[0].Text, "Hello, Ada!")
	}
}

func TestSystemPrompt_NilIsSafe(t *testing.T) {
	var sp *agent.SystemPrompt
	blocks, err := sp.Render(context.Background())
	if err != nil || blocks != nil {
		t.Errorf("nil SystemPrompt.Render() = %v, %v, want nil, nil", blocks, err)
	}
}
