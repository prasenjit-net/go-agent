package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"

	agent "github.com/prasenjit-net/go-agent"
	"github.com/prasenjit-net/go-agent/agenttest"
)

func drainStream(t *testing.T, stream agent.EventStream) []agent.Event {
	t.Helper()
	var events []agent.Event
	for {
		ev, err := stream.Next(context.Background())
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("stream.Next returned error: %v", err)
		}
		events = append(events, ev)
	}
	return events
}

func TestAgent_RunStream_NativeStreamingProvider(t *testing.T) {
	mock := &agenttest.MockStreamingProvider{
		MockProvider: agenttest.MockProvider{
			Responses: []*agent.Response{
				{Message: agent.AssistantMessage(agent.TextBlock{Text: "hi there"}), StopReason: agent.StopEndTurn},
			},
		},
	}
	a := agent.New(agent.WithProvider(mock), agent.WithModel("mock-model"))

	stream, err := a.RunStream(context.Background(), "hello")
	if err != nil {
		t.Fatalf("RunStream returned error: %v", err)
	}
	defer func() { _ = stream.Close() }()

	events := drainStream(t, stream)

	var sawText, sawDone bool
	for _, ev := range events {
		switch ev.Type {
		case agent.EventTextDelta:
			sawText = true
			if ev.TextDelta != "hi there" {
				t.Errorf("TextDelta = %q", ev.TextDelta)
			}
		case agent.EventRunDone:
			sawDone = true
			if ev.Result.FinalResponse.Message.Text() != "hi there" {
				t.Errorf("final text = %q", ev.Result.FinalResponse.Message.Text())
			}
			if ev.Result.Iterations != 1 {
				t.Errorf("Iterations = %d, want 1", ev.Result.Iterations)
			}
		}
	}
	if !sawText || !sawDone {
		t.Errorf("events = %+v, want at least one text_delta and one run_done", events)
	}
}

func TestAgent_RunStream_ToolRoundTrip(t *testing.T) {
	toolCalled := false
	tool := agent.NewTool("noop", "does nothing", func(context.Context, cityInput) (agent.ToolResult, error) {
		toolCalled = true
		return agent.TextResult("done"), nil
	})

	mock := &agenttest.MockStreamingProvider{
		MockProvider: agenttest.MockProvider{
			Responses: []*agent.Response{
				{Message: agent.AssistantMessage(agent.ToolUseBlock{ID: "1", Name: "noop", Input: json.RawMessage(`{"city":"x"}`)}), StopReason: agent.StopToolUse},
				{Message: agent.AssistantMessage(agent.TextBlock{Text: "all done"}), StopReason: agent.StopEndTurn},
			},
		},
	}
	a := agent.New(agent.WithProvider(mock), agent.WithModel("mock-model"), agent.WithTools(tool))

	stream, err := a.RunStream(context.Background(), "go")
	if err != nil {
		t.Fatalf("RunStream returned error: %v", err)
	}
	defer func() { _ = stream.Close() }()

	events := drainStream(t, stream)
	if !toolCalled {
		t.Error("tool was never invoked during RunStream")
	}

	var sawToolResult, sawDone bool
	for _, ev := range events {
		if ev.Type == agent.EventToolResult {
			sawToolResult = true
		}
		if ev.Type == agent.EventRunDone {
			sawDone = true
			if ev.Result.Iterations != 2 {
				t.Errorf("Iterations = %d, want 2", ev.Result.Iterations)
			}
		}
	}
	if !sawToolResult || !sawDone {
		t.Errorf("events = %+v, want a tool_result and a run_done", events)
	}
}

func TestAgent_RunStream_FallbackSingleShotForNonStreamingProvider(t *testing.T) {
	// Plain MockProvider has no Stream method, so this exercises Agent's
	// documented fallback: one blocking Generate call synthesized into a
	// single-burst stream.
	mock := &agenttest.MockProvider{
		Responses: []*agent.Response{
			{Message: agent.AssistantMessage(agent.TextBlock{Text: "fallback text"}), StopReason: agent.StopEndTurn},
		},
	}
	a := agent.New(agent.WithProvider(mock), agent.WithModel("mock-model"))

	stream, err := a.RunStream(context.Background(), "hello")
	if err != nil {
		t.Fatalf("RunStream returned error: %v", err)
	}
	defer func() { _ = stream.Close() }()

	events := drainStream(t, stream)
	var gotText string
	for _, ev := range events {
		if ev.Type == agent.EventTextDelta {
			gotText += ev.TextDelta
		}
	}
	if gotText != "fallback text" {
		t.Errorf("gotText = %q, want %q", gotText, "fallback text")
	}
}

func TestAgent_RunStream_FallbackErrorMode(t *testing.T) {
	mock := &agenttest.MockProvider{}
	a := agent.New(agent.WithProvider(mock), agent.WithModel("mock-model"), agent.WithStreamingFallback(agent.FallbackError))

	stream, err := a.RunStream(context.Background(), "hello")
	if err != nil {
		t.Fatalf("RunStream should only fail on the first Next() call in this design, got immediate error: %v", err)
	}
	_, err = stream.Next(context.Background())
	if err == nil {
		t.Fatal("expected an error from Next() when the provider doesn't support streaming and FallbackError is set")
	}
	if agent.CodeOf(err) != agent.ErrStreamUnsupported {
		t.Errorf("CodeOf(err) = %v, want ErrStreamUnsupported", agent.CodeOf(err))
	}
}
