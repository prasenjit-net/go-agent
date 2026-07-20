package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	agent "github.com/prasenjit-net/go-agent"
	"github.com/prasenjit-net/go-agent/agenttest"
)

func TestAgent_Run_SimpleTextResponse(t *testing.T) {
	mock := &agenttest.MockProvider{
		Responses: []*agent.Response{
			{Message: agent.AssistantMessage(agent.TextBlock{Text: "hello there"}), StopReason: agent.StopEndTurn},
		},
	}
	a := agent.New(agent.WithProvider(mock), agent.WithModel("mock-model"))

	result, err := a.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.FinalResponse.Message.Text() != "hello there" {
		t.Errorf("final text = %q", result.FinalResponse.Message.Text())
	}
	if result.Iterations != 1 {
		t.Errorf("Iterations = %d, want 1", result.Iterations)
	}
	if len(result.Messages) != 2 {
		t.Errorf("len(Messages) = %d, want 2 (user + assistant)", len(result.Messages))
	}
}

type cityInput struct {
	City string `json:"city" jsonschema:"required"`
}

func TestAgent_Run_ExecutesToolAndContinues(t *testing.T) {
	var gotCity string
	weatherTool := agent.NewTool("get_weather", "Get current weather for a city.",
		func(_ context.Context, in cityInput) (agent.ToolResult, error) {
			gotCity = in.City
			return agent.TextResult("72F and sunny"), nil
		})

	mock := &agenttest.MockProvider{
		Responses: []*agent.Response{
			{
				Message: agent.AssistantMessage(agent.ToolUseBlock{
					ID: "call_1", Name: "get_weather", Input: json.RawMessage(`{"city":"Paris"}`),
				}),
				StopReason: agent.StopToolUse,
			},
			{
				Message:    agent.AssistantMessage(agent.TextBlock{Text: "It's 72F and sunny in Paris."}),
				StopReason: agent.StopEndTurn,
			},
		},
	}

	a := agent.New(agent.WithProvider(mock), agent.WithModel("mock-model"), agent.WithTools(weatherTool))

	result, err := a.Run(context.Background(), "weather in Paris?")
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if gotCity != "Paris" {
		t.Errorf("tool received city = %q, want Paris", gotCity)
	}
	if result.Iterations != 2 {
		t.Errorf("Iterations = %d, want 2", result.Iterations)
	}
	if result.FinalResponse.Message.Text() != "It's 72F and sunny in Paris." {
		t.Errorf("final text = %q", result.FinalResponse.Message.Text())
	}

	// history: user, assistant(tool_use), user(tool_result), assistant(text)
	if len(result.Messages) != 4 {
		t.Fatalf("len(Messages) = %d, want 4", len(result.Messages))
	}
	toolResultMsg := result.Messages[2]
	trb, ok := toolResultMsg.Content[0].(agent.ToolResultBlock)
	if !ok {
		t.Fatalf("Messages[2].Content[0] = %T, want ToolResultBlock", toolResultMsg.Content[0])
	}
	if trb.ToolUseID != "call_1" {
		t.Errorf("ToolUseID = %q, want call_1", trb.ToolUseID)
	}
}

func TestAgent_Run_UnknownToolProducesErrorResultAndContinues(t *testing.T) {
	mock := &agenttest.MockProvider{
		Responses: []*agent.Response{
			{
				Message: agent.AssistantMessage(agent.ToolUseBlock{
					ID: "call_1", Name: "does_not_exist", Input: json.RawMessage(`{}`),
				}),
				StopReason: agent.StopToolUse,
			},
			{Message: agent.AssistantMessage(agent.TextBlock{Text: "sorry, I can't do that"}), StopReason: agent.StopEndTurn},
		},
	}
	a := agent.New(agent.WithProvider(mock), agent.WithModel("mock-model"))

	result, err := a.Run(context.Background(), "do the thing")
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	trb := result.Messages[2].Content[0].(agent.ToolResultBlock)
	if !trb.IsError {
		t.Error("expected an error ToolResultBlock for an unknown tool")
	}
}

func TestAgent_Run_MaxIterationsExceeded(t *testing.T) {
	mock := &agenttest.MockProvider{
		OnGenerate: func(req *agent.Request) (*agent.Response, error) {
			// Always ask for another tool call — never terminates on its own.
			return &agent.Response{
				Message:    agent.AssistantMessage(agent.ToolUseBlock{ID: "x", Name: "noop", Input: json.RawMessage(`{}`)}),
				StopReason: agent.StopToolUse,
			}, nil
		},
	}
	a := agent.New(agent.WithProvider(mock), agent.WithModel("mock-model"), agent.WithMaxIterations(3))

	_, err := a.Run(context.Background(), "loop forever")
	if err == nil {
		t.Fatal("expected an error when max iterations is exceeded")
	}
	if agent.CodeOf(err) != agent.ErrMaxIterations {
		t.Errorf("CodeOf(err) = %v, want ErrMaxIterations", agent.CodeOf(err))
	}
}

func TestAgent_Run_HooksAreCalled(t *testing.T) {
	var beforeGen, afterGen, beforeTool, afterTool int

	tool := agent.NewTool("noop", "does nothing", func(context.Context, cityInput) (agent.ToolResult, error) {
		return agent.TextResult("ok"), nil
	})
	mock := &agenttest.MockProvider{
		Responses: []*agent.Response{
			{Message: agent.AssistantMessage(agent.ToolUseBlock{ID: "1", Name: "noop", Input: json.RawMessage(`{"city":"x"}`)}), StopReason: agent.StopToolUse},
			{Message: agent.AssistantMessage(agent.TextBlock{Text: "done"}), StopReason: agent.StopEndTurn},
		},
	}
	a := agent.New(
		agent.WithProvider(mock),
		agent.WithModel("mock-model"),
		agent.WithTools(tool),
		agent.WithHooks(agent.Hooks{
			BeforeGenerate: func(context.Context, *agent.Request) error { beforeGen++; return nil },
			AfterGenerate:  func(context.Context, *agent.Response) { afterGen++ },
			BeforeToolCall: func(context.Context, agent.ToolCall) (bool, *agent.ToolResult) { beforeTool++; return true, nil },
			AfterToolCall:  func(context.Context, agent.ToolCall, agent.ToolResult) { afterTool++ },
		}),
	)

	if _, err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if beforeGen != 2 || afterGen != 2 {
		t.Errorf("BeforeGenerate/AfterGenerate calls = %d/%d, want 2/2", beforeGen, afterGen)
	}
	if beforeTool != 1 || afterTool != 1 {
		t.Errorf("BeforeToolCall/AfterToolCall calls = %d/%d, want 1/1", beforeTool, afterTool)
	}
}

func TestAgent_Run_ToolCallDeniedByHook(t *testing.T) {
	tool := agent.NewTool("dangerous", "deletes everything", func(context.Context, cityInput) (agent.ToolResult, error) {
		t.Fatal("tool handler should not run when BeforeToolCall denies the call")
		return agent.ToolResult{}, nil
	})
	mock := &agenttest.MockProvider{
		Responses: []*agent.Response{
			{Message: agent.AssistantMessage(agent.ToolUseBlock{ID: "1", Name: "dangerous", Input: json.RawMessage(`{}`)}), StopReason: agent.StopToolUse},
			{Message: agent.AssistantMessage(agent.TextBlock{Text: "ok, not doing that"}), StopReason: agent.StopEndTurn},
		},
	}
	a := agent.New(
		agent.WithProvider(mock),
		agent.WithModel("mock-model"),
		agent.WithTools(tool),
		agent.WithHooks(agent.Hooks{
			BeforeToolCall: func(context.Context, agent.ToolCall) (bool, *agent.ToolResult) { return false, nil },
		}),
	)

	result, err := a.Run(context.Background(), "delete everything")
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	trb := result.Messages[2].Content[0].(agent.ToolResultBlock)
	if !trb.IsError {
		t.Error("denied tool call should produce an error ToolResultBlock")
	}
}

func TestAgent_Run_NoProviderConfigured(t *testing.T) {
	a := agent.New()
	_, err := a.Run(context.Background(), "hi")
	if err == nil {
		t.Fatal("expected an error when no provider is configured")
	}
	if agent.CodeOf(err) != agent.ErrInvalidRequest {
		t.Errorf("CodeOf(err) = %v, want ErrInvalidRequest", agent.CodeOf(err))
	}
}

func TestAgent_Run_RetriesRetryableErrors(t *testing.T) {
	attempts := 0
	mock := &agenttest.MockProvider{
		OnGenerate: func(*agent.Request) (*agent.Response, error) {
			attempts++
			if attempts < 3 {
				return nil, &agent.Error{Code: agent.ErrRateLimited, Retryable: true, Message: "slow down"}
			}
			return &agent.Response{Message: agent.AssistantMessage(agent.TextBlock{Text: "ok"}), StopReason: agent.StopEndTurn}, nil
		},
	}
	a := agent.New(
		agent.WithProvider(mock),
		agent.WithModel("mock-model"),
		agent.WithRetryPolicy(agent.RetryPolicy{MaxRetries: 3, BaseDelay: 0, MaxDelay: 0}),
	)

	result, err := a.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
	if result.FinalResponse.Message.Text() != "ok" {
		t.Errorf("final text = %q", result.FinalResponse.Message.Text())
	}
}

func TestAgent_Run_DoesNotRetryNonRetryableErrors(t *testing.T) {
	attempts := 0
	mock := &agenttest.MockProvider{
		OnGenerate: func(*agent.Request) (*agent.Response, error) {
			attempts++
			return nil, &agent.Error{Code: agent.ErrInvalidRequest, Retryable: false, Message: "bad request"}
		},
	}
	a := agent.New(agent.WithProvider(mock), agent.WithModel("mock-model"))

	_, err := a.Run(context.Background(), "hi")
	if err == nil {
		t.Fatal("expected an error")
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1 (non-retryable errors must not be retried)", attempts)
	}
}

func TestAgent_Run_ContextCancellation(t *testing.T) {
	mock := &agenttest.MockProvider{
		OnGenerate: func(*agent.Request) (*agent.Response, error) {
			return nil, context.Canceled
		},
	}
	a := agent.New(agent.WithProvider(mock), agent.WithModel("mock-model"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := a.Run(ctx, "hi")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}
