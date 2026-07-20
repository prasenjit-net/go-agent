package gemini

import (
	"encoding/json"
	"testing"

	"google.golang.org/genai"

	agent "github.com/prasenjit-net/go-agent"
)

func TestToContents_ToolResultRecoversNameFromEarlierToolUse(t *testing.T) {
	// Gemini matches a function response to its call by *name*, not a call
	// ID (FunctionCall.ID is often unpopulated) — toContents must recover
	// the name from the ToolUseBlock earlier in history using ToolUseID,
	// since ToolResultBlock itself only carries the ID.
	msgs := []agent.Message{
		agent.UserMessage("what's the weather in Paris?"),
		agent.AssistantMessage(agent.ToolUseBlock{ID: "get_weather_0", Name: "get_weather", Input: json.RawMessage(`{"city":"Paris"}`)}),
		{Role: agent.RoleUser, Content: []agent.ContentBlock{
			agent.ToolResultBlock{ToolUseID: "get_weather_0", Content: []agent.ContentBlock{agent.TextBlock{Text: "72F and sunny"}}},
		}},
	}

	contents, err := toContents(msgs)
	if err != nil {
		t.Fatalf("toContents returned error: %v", err)
	}
	if len(contents) != 3 {
		t.Fatalf("len(contents) = %d, want 3", len(contents))
	}

	resultContent := contents[2]
	if len(resultContent.Parts) != 1 || resultContent.Parts[0].FunctionResponse == nil {
		t.Fatalf("expected a single FunctionResponse part, got %+v", resultContent.Parts)
	}
	fr := resultContent.Parts[0].FunctionResponse
	if fr.Name != "get_weather" {
		t.Errorf("FunctionResponse.Name = %q, want get_weather (recovered from the earlier tool_use, not the raw ID)", fr.Name)
	}
	if fr.Response["output"] != "72F and sunny" {
		t.Errorf("FunctionResponse.Response = %+v", fr.Response)
	}
}

func TestToContents_ErrorToolResultUsesErrorKey(t *testing.T) {
	msgs := []agent.Message{
		agent.AssistantMessage(agent.ToolUseBlock{ID: "x", Name: "risky", Input: json.RawMessage(`{}`)}),
		{Role: agent.RoleUser, Content: []agent.ContentBlock{
			agent.ToolResultBlock{ToolUseID: "x", IsError: true, Content: []agent.ContentBlock{agent.TextBlock{Text: "boom"}}},
		}},
	}
	contents, err := toContents(msgs)
	if err != nil {
		t.Fatal(err)
	}
	fr := contents[1].Parts[0].FunctionResponse
	if fr.Response["error"] != "boom" {
		t.Errorf("Response = %+v, want error=boom", fr.Response)
	}
}

func TestFromResponse_FunctionCallForcesStopToolUse(t *testing.T) {
	// Gemini signals a function-call turn via the presence of FunctionCall
	// parts, not a dedicated finish reason: real responses report
	// finish_reason STOP even when the model called a tool.
	resp := &genai.GenerateContentResponse{
		ResponseID:   "resp_1",
		ModelVersion: "gemini-test",
		Candidates: []*genai.Candidate{{
			FinishReason: genai.FinishReasonStop,
			Content: &genai.Content{
				Role: "model",
				Parts: []*genai.Part{
					genai.NewPartFromFunctionCall("get_weather", map[string]any{"city": "Paris"}),
				},
			},
		}},
	}

	got, err := fromResponse(resp)
	if err != nil {
		t.Fatalf("fromResponse returned error: %v", err)
	}
	if got.StopReason != agent.StopToolUse {
		t.Errorf("StopReason = %v, want tool_use (finish_reason alone was STOP)", got.StopReason)
	}
	if len(got.Message.Content) != 1 {
		t.Fatalf("Content = %+v", got.Message.Content)
	}
	tu, ok := got.Message.Content[0].(agent.ToolUseBlock)
	if !ok || tu.Name != "get_weather" {
		t.Errorf("Content[0] = %+v", got.Message.Content[0])
	}
	if tu.ID == "" {
		t.Error("expected a synthesized ID since FunctionCall.ID was empty")
	}
}

func TestToToolChoice_OneForcesAllowedFunctionNames(t *testing.T) {
	cfg := toToolConfig(agent.ToolChoice{Mode: agent.ToolChoiceOne, Name: "get_weather"})
	if cfg.FunctionCallingConfig.Mode != genai.FunctionCallingConfigModeAny {
		t.Errorf("Mode = %v, want ANY (Gemini has no single-tool-force mode; ANY + AllowedFunctionNames is the closest equivalent)", cfg.FunctionCallingConfig.Mode)
	}
	if len(cfg.FunctionCallingConfig.AllowedFunctionNames) != 1 || cfg.FunctionCallingConfig.AllowedFunctionNames[0] != "get_weather" {
		t.Errorf("AllowedFunctionNames = %v", cfg.FunctionCallingConfig.AllowedFunctionNames)
	}
}
