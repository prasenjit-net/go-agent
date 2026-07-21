package gemini

import (
	"context"
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

func TestToConfig_DefaultsMaxOutputTokensWhenUnset(t *testing.T) {
	req := &agent.Request{Model: "gemini-test", Messages: []agent.Message{agent.UserMessage("hi")}}
	cfg, err := toConfig(req)
	if err != nil {
		t.Fatalf("toConfig returned error: %v", err)
	}
	if cfg.MaxOutputTokens != int32(defaultMaxTokens) {
		t.Errorf("MaxOutputTokens = %d, want default %d", cfg.MaxOutputTokens, defaultMaxTokens)
	}
}

func TestToConfig_WiresSystemToolsThinking(t *testing.T) {
	type input struct {
		City string `json:"city" jsonschema:"required"`
	}
	tool := agent.NewTool("get_weather", "Get current weather.",
		func(ctx context.Context, in input) (agent.ToolResult, error) { return agent.TextResult(""), nil })

	req := &agent.Request{
		Model:      "gemini-test",
		System:     []agent.SystemBlock{{Text: "be terse"}},
		Messages:   []agent.Message{agent.UserMessage("hi")},
		Tools:      []agent.RegisteredTool{tool},
		ToolChoice: agent.ToolChoice{Mode: agent.ToolChoiceAny},
		Thinking:   &agent.ThinkingConfig{Mode: agent.ThinkingBudgeted, Budget: 2048},
	}
	cfg, err := toConfig(req)
	if err != nil {
		t.Fatalf("toConfig returned error: %v", err)
	}
	if cfg.SystemInstruction == nil || len(cfg.SystemInstruction.Parts) != 1 || cfg.SystemInstruction.Parts[0].Text != "be terse" {
		t.Errorf("SystemInstruction = %+v", cfg.SystemInstruction)
	}
	if len(cfg.Tools) != 1 || len(cfg.Tools[0].FunctionDeclarations) != 1 {
		t.Errorf("Tools = %+v", cfg.Tools)
	}
	if cfg.ToolConfig.FunctionCallingConfig.Mode != genai.FunctionCallingConfigModeAny {
		t.Errorf("ToolConfig = %+v", cfg.ToolConfig)
	}
	if cfg.ThinkingConfig == nil || cfg.ThinkingConfig.ThinkingBudget == nil || *cfg.ThinkingConfig.ThinkingBudget != 2048 {
		t.Errorf("ThinkingConfig = %+v", cfg.ThinkingConfig)
	}
}

func TestToThinkingConfig(t *testing.T) {
	cases := []struct {
		name string
		cfg  agent.ThinkingConfig
		want func(*genai.ThinkingConfig) bool
	}{
		{"adaptive", agent.ThinkingConfig{Mode: agent.ThinkingAdaptive}, func(tc *genai.ThinkingConfig) bool {
			return tc.IncludeThoughts && tc.ThinkingBudget == nil
		}},
		{"budgeted", agent.ThinkingConfig{Mode: agent.ThinkingBudgeted, Budget: 1024}, func(tc *genai.ThinkingConfig) bool {
			return tc.IncludeThoughts && tc.ThinkingBudget != nil && *tc.ThinkingBudget == 1024
		}},
		{"off (default)", agent.ThinkingConfig{Mode: agent.ThinkingOff}, func(tc *genai.ThinkingConfig) bool {
			return !tc.IncludeThoughts && tc.ThinkingBudget != nil && *tc.ThinkingBudget == 0
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := toThinkingConfig(tc.cfg)
			if !tc.want(got) {
				t.Errorf("toThinkingConfig(%+v) = %+v, unexpected shape", tc.cfg, got)
			}
		})
	}
}

func TestToPart(t *testing.T) {
	cases := []struct {
		name    string
		block   agent.ContentBlock
		wantNil bool
		wantErr bool
	}{
		{"thinking is dropped", agent.ThinkingBlock{Text: "reasoning"}, true, false},
		{"image base64", agent.ImageBlock{Source: agent.ImageSource{Kind: agent.SourceBase64, MediaType: "image/png", Data: "aGVsbG8="}}, false, false},
		{"image base64 invalid", agent.ImageBlock{Source: agent.ImageSource{Kind: agent.SourceBase64, Data: "not-base64!!"}}, false, true},
		{"image url", agent.ImageBlock{Source: agent.ImageSource{Kind: agent.SourceURL, Data: "gs://bucket/x.png"}}, false, false},
		{"image unsupported kind", agent.ImageBlock{Source: agent.ImageSource{Kind: "ftp"}}, false, true},
		{"document base64 default mediatype", agent.DocumentBlock{Source: agent.ImageSource{Kind: agent.SourceBase64, Data: "aGVsbG8="}}, false, false},
		{"document base64 invalid", agent.DocumentBlock{Source: agent.ImageSource{Kind: agent.SourceBase64, Data: "not-base64!!"}}, false, true},
		{"document url", agent.DocumentBlock{Source: agent.ImageSource{Kind: agent.SourceURL, Data: "gs://bucket/x.pdf"}}, false, false},
		{"document unsupported kind", agent.DocumentBlock{Source: agent.ImageSource{Kind: "ftp"}}, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			part, err := toPart(tc.block, nil)
			if (err != nil) != tc.wantErr {
				t.Fatalf("toPart(%s) error = %v, wantErr %v", tc.name, err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if (part == nil) != tc.wantNil {
				t.Errorf("toPart(%s) part = %v, wantNil %v", tc.name, part, tc.wantNil)
			}
		})
	}
}

func TestFromPart(t *testing.T) {
	tu, ok := fromPart(genai.NewPartFromFunctionCall("get_weather", map[string]any{"city": "Paris"}), 0)
	if !ok {
		t.Fatal("expected ok=true for a function call part")
	}
	toolUse, ok := tu.(agent.ToolUseBlock)
	if !ok || toolUse.Name != "get_weather" {
		t.Errorf("fromPart(function call) = %+v", tu)
	}
	if toolUse.ID != "get_weather_0" {
		t.Errorf("ID = %q, want a synthesized ID when FunctionCall.ID is empty", toolUse.ID)
	}

	withID, ok := fromPart(&genai.Part{FunctionCall: &genai.FunctionCall{ID: "explicit_id", Name: "f"}}, 3)
	if !ok || withID.(agent.ToolUseBlock).ID != "explicit_id" {
		t.Errorf("fromPart should preserve an explicit FunctionCall.ID: %+v", withID)
	}

	thought, ok := fromPart(&genai.Part{Thought: true, Text: "thinking..."}, 0)
	if !ok {
		t.Fatal("expected ok=true for a thought part")
	}
	if tb, ok := thought.(agent.ThinkingBlock); !ok || tb.Text != "thinking..." {
		t.Errorf("fromPart(thought) = %+v", thought)
	}

	text, ok := fromPart(&genai.Part{Text: "hello"}, 0)
	if !ok {
		t.Fatal("expected ok=true for a text part")
	}
	if tb, ok := text.(agent.TextBlock); !ok || tb.Text != "hello" {
		t.Errorf("fromPart(text) = %+v", text)
	}

	_, ok = fromPart(&genai.Part{}, 0)
	if ok {
		t.Error("expected ok=false for an empty part with no recognized content")
	}
}

func TestFromFinishReason(t *testing.T) {
	cases := map[genai.FinishReason]agent.StopReason{
		genai.FinishReasonStop:       agent.StopEndTurn,
		"":                           agent.StopEndTurn,
		genai.FinishReasonMaxTokens:  agent.StopMaxTokens,
		genai.FinishReasonSafety:     agent.StopContentFilter,
		genai.FinishReasonRecitation: agent.StopContentFilter,
		genai.FinishReasonLanguage:   agent.StopContentFilter,
		"OTHER":                      agent.StopUnknown,
	}
	for in, want := range cases {
		if got := fromFinishReason(in); got != want {
			t.Errorf("fromFinishReason(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFromUsage(t *testing.T) {
	if got := fromUsage(nil); got != (agent.Usage{}) {
		t.Errorf("fromUsage(nil) = %+v, want zero value", got)
	}
	got := fromUsage(&genai.GenerateContentResponseUsageMetadata{
		PromptTokenCount:        10,
		CandidatesTokenCount:    5,
		ThoughtsTokenCount:      2,
		CachedContentTokenCount: 3,
	})
	want := agent.Usage{InputTokens: 10, OutputTokens: 7, CacheReadTokens: 3}
	if got != want {
		t.Errorf("fromUsage = %+v, want %+v", got, want)
	}
}

func TestToContents_MessageWithOnlyDroppedContentIsSkipped(t *testing.T) {
	msgs := []agent.Message{
		agent.AssistantMessage(agent.ThinkingBlock{Text: "internal reasoning, not echoable"}),
		agent.UserMessage("hi"),
	}
	contents, err := toContents(msgs)
	if err != nil {
		t.Fatalf("toContents returned error: %v", err)
	}
	if len(contents) != 1 {
		t.Fatalf("len(contents) = %d, want 1 (the thinking-only message should be skipped)", len(contents))
	}
}

func TestFromResponse_NoCandidatesErrors(t *testing.T) {
	if _, err := fromResponse(&genai.GenerateContentResponse{}); err == nil {
		t.Error("expected an error for a response with no candidates, got nil")
	}
}

func TestToToolConfig(t *testing.T) {
	cases := []struct {
		name string
		mode agent.ToolChoiceMode
		want genai.FunctionCallingConfigMode
	}{
		{"any", agent.ToolChoiceAny, genai.FunctionCallingConfigModeAny},
		{"none", agent.ToolChoiceNone, genai.FunctionCallingConfigModeNone},
		{"auto (default)", agent.ToolChoiceAuto, genai.FunctionCallingConfigModeAuto},
		{"unrecognized falls back to auto", "bogus", genai.FunctionCallingConfigModeAuto},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := toToolConfig(agent.ToolChoice{Mode: tc.mode})
			if got.FunctionCallingConfig.Mode != tc.want {
				t.Errorf("toToolConfig(%q) = %v, want %v", tc.mode, got.FunctionCallingConfig.Mode, tc.want)
			}
		})
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
