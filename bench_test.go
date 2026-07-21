package agent_test

import (
	"context"
	"encoding/json"
	"testing"

	agent "github.com/prasenjit-net/go-agent"
	"github.com/prasenjit-net/go-agent/agenttest"
)

// BenchmarkTool_Invoke isolates the tool-dispatch overhead the plan calls
// out specifically: json.Unmarshal into the typed input plus the handler
// call, with no Agent/provider machinery around it. cityInput is defined in
// agent_test.go.
func BenchmarkTool_Invoke(b *testing.B) {
	tool := agent.NewTool("get_weather", "Get current weather for a city.",
		func(_ context.Context, in cityInput) (agent.ToolResult, error) {
			return agent.TextResult("72F and sunny in " + in.City), nil
		})
	raw := json.RawMessage(`{"city":"Paris"}`)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := tool.Invoke(ctx, raw); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkAgent_Run_TextOnly measures a single-iteration Run (no tool
// calls): buildRequest, the nil-hooks fast path, and generateWithRetry's
// happy path. The mock's single scripted response repeats every call
// (agenttest.MockProvider's documented behavior), so building it once
// outside the loop is correct here.
func BenchmarkAgent_Run_TextOnly(b *testing.B) {
	mock := &agenttest.MockProvider{
		Responses: []*agent.Response{
			{Message: agent.AssistantMessage(agent.TextBlock{Text: "hello there"}), StopReason: agent.StopEndTurn},
		},
	}
	a := agent.New(agent.WithProvider(mock), agent.WithModel("mock-model"))
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := a.Run(ctx, "hi"); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkAgent_Run_WithToolCall measures a full 2-iteration turn: a
// tool_use response, executeTools' dispatch (json.Unmarshal + handler call
// through invokeOne), then a final text response. Unlike the text-only
// benchmark, the mock and Agent are rebuilt each iteration (outside the
// timed section): MockProvider's scripted Responses don't cycle — once
// exhausted, every further call repeats the last entry — so reusing one
// mock across b.N iterations would silently degrade into the trivial
// single-response case after the first.
func BenchmarkAgent_Run_WithToolCall(b *testing.B) {
	weatherTool := agent.NewTool("get_weather", "Get current weather for a city.",
		func(_ context.Context, in cityInput) (agent.ToolResult, error) {
			return agent.TextResult("72F and sunny in " + in.City), nil
		})
	ctx := context.Background()

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		mock := &agenttest.MockProvider{
			Responses: []*agent.Response{
				{
					Message: agent.AssistantMessage(agent.ToolUseBlock{
						ID: "call_1", Name: "get_weather", Input: json.RawMessage(`{"city":"Paris"}`),
					}),
					StopReason: agent.StopToolUse,
				},
				{Message: agent.AssistantMessage(agent.TextBlock{Text: "72F and sunny in Paris"}), StopReason: agent.StopEndTurn},
			},
		}
		a := agent.New(agent.WithProvider(mock), agent.WithModel("mock-model"), agent.WithTools(weatherTool))
		b.StartTimer()

		if _, err := a.Run(ctx, "weather in Paris?"); err != nil {
			b.Fatal(err)
		}
	}
}
