package gemini

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"google.golang.org/genai"

	agent "github.com/prasenjit-net/go-agent"
)

const (
	roleUser  genai.Role = "user"
	roleModel genai.Role = "model"
)

func toConfig(req *agent.Request) (*genai.GenerateContentConfig, error) {
	cfg := &genai.GenerateContentConfig{}

	if len(req.System) > 0 {
		cfg.SystemInstruction = toSystemInstruction(req.System)
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}
	cfg.MaxOutputTokens = int32(maxTokens)

	if len(req.Tools) > 0 {
		cfg.Tools = toTools(req.Tools)
		cfg.ToolConfig = toToolConfig(req.ToolChoice)
	}
	if req.Thinking != nil {
		cfg.ThinkingConfig = toThinkingConfig(*req.Thinking)
	}
	return cfg, nil
}

func toSystemInstruction(blocks []agent.SystemBlock) *genai.Content {
	var sb strings.Builder
	for i, b := range blocks {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(b.Text)
	}
	// Role is intentionally left unset: systemInstruction content has no
	// meaningful role, and the constructor helpers all require one.
	return &genai.Content{Parts: []*genai.Part{genai.NewPartFromText(sb.String())}}
}

func toTools(tools []agent.RegisteredTool) []*genai.Tool {
	decls := make([]*genai.FunctionDeclaration, 0, len(tools))
	for _, t := range tools {
		decls = append(decls, &genai.FunctionDeclaration{
			Name:                 t.Name(),
			Description:          t.Description(),
			ParametersJsonSchema: t.Schema(),
		})
	}
	return []*genai.Tool{{FunctionDeclarations: decls}}
}

func toToolConfig(tc agent.ToolChoice) *genai.ToolConfig {
	switch tc.Mode {
	case agent.ToolChoiceAny:
		return &genai.ToolConfig{FunctionCallingConfig: &genai.FunctionCallingConfig{Mode: genai.FunctionCallingConfigModeAny}}
	case agent.ToolChoiceOne:
		return &genai.ToolConfig{FunctionCallingConfig: &genai.FunctionCallingConfig{
			Mode:                 genai.FunctionCallingConfigModeAny,
			AllowedFunctionNames: []string{tc.Name},
		}}
	case agent.ToolChoiceNone:
		return &genai.ToolConfig{FunctionCallingConfig: &genai.FunctionCallingConfig{Mode: genai.FunctionCallingConfigModeNone}}
	default:
		return &genai.ToolConfig{FunctionCallingConfig: &genai.FunctionCallingConfig{Mode: genai.FunctionCallingConfigModeAuto}}
	}
}

// toThinkingConfig maps our provider-agnostic ThinkingConfig onto Gemini's
// thinking configuration. ThinkingBudgeted maps directly (Gemini is the
// only first-class provider with a literal token-budget knob);
// ThinkingAdaptive requests thoughts without a fixed budget, letting the
// model decide; ThinkingOff sets an explicit zero budget to disable
// thinking on models that support toggling it.
func toThinkingConfig(cfg agent.ThinkingConfig) *genai.ThinkingConfig {
	switch cfg.Mode {
	case agent.ThinkingAdaptive:
		return &genai.ThinkingConfig{IncludeThoughts: true}
	case agent.ThinkingBudgeted:
		budget := int32(cfg.Budget)
		return &genai.ThinkingConfig{IncludeThoughts: true, ThinkingBudget: &budget}
	default:
		zero := int32(0)
		return &genai.ThinkingConfig{ThinkingBudget: &zero}
	}
}

// toContents translates the unified message history into Gemini's Content
// list. Gemini identifies a function response by the function's *name*, not
// a call ID the way Claude/OpenAI do (FunctionCall.ID is optional and often
// unpopulated) — so toolNamesByID first scans the whole history for every
// ToolUseBlock's ID -> Name mapping, and toolResultBlock below recovers the
// name from that map when building the outgoing FunctionResponse.
func toContents(msgs []agent.Message) ([]*genai.Content, error) {
	idToName := toolNamesByID(msgs)

	contents := make([]*genai.Content, 0, len(msgs))
	for _, m := range msgs {
		role := roleUser
		if m.Role == agent.RoleAssistant {
			role = roleModel
		}

		var parts []*genai.Part
		for _, b := range m.Content {
			part, err := toPart(b, idToName)
			if err != nil {
				return nil, err
			}
			if part != nil {
				parts = append(parts, part)
			}
		}
		if len(parts) == 0 {
			continue
		}
		contents = append(contents, genai.NewContentFromParts(parts, role))
	}
	return contents, nil
}

func toolNamesByID(msgs []agent.Message) map[string]string {
	out := map[string]string{}
	for _, m := range msgs {
		for _, b := range m.Content {
			if tu, ok := b.(agent.ToolUseBlock); ok {
				out[tu.ID] = tu.Name
			}
		}
	}
	return out
}

func toPart(b agent.ContentBlock, idToName map[string]string) (*genai.Part, error) {
	switch v := b.(type) {
	case agent.TextBlock:
		return genai.NewPartFromText(v.Text), nil

	case agent.ThinkingBlock:
		// Gemini's "thought" parts are model-generated and read back via
		// Part.Thought; there is no supported way to echo an opaque
		// signature back as client-authored history, so thinking content
		// is dropped from outgoing requests.
		return nil, nil

	case agent.ImageBlock:
		return toImagePart(v)

	case agent.DocumentBlock:
		return toDocumentPart(v)

	case agent.ToolUseBlock:
		var args map[string]any
		if len(v.Input) > 0 {
			if err := json.Unmarshal(v.Input, &args); err != nil {
				return nil, fmt.Errorf("gemini: unmarshalling tool_use input for echo: %w", err)
			}
		}
		return genai.NewPartFromFunctionCall(v.Name, args), nil

	case agent.ToolResultBlock:
		name := idToName[v.ToolUseID]
		if name == "" {
			name = v.ToolUseID
		}
		return genai.NewPartFromFunctionResponse(name, toolResponseMap(v)), nil

	default:
		return nil, fmt.Errorf("gemini: unsupported content block type %T", b)
	}
}

func toolResponseMap(v agent.ToolResultBlock) map[string]any {
	text := flattenText(v.Content)
	if v.IsError {
		return map[string]any{"error": text}
	}
	return map[string]any{"output": text}
}

// flattenText concatenates every TextBlock in blocks. Gemini's function
// response is a JSON object rather than a plain string; every ToolResult
// built via agent.TextResult/JSONResult/ErrorResultf produces a single
// TextBlock, which toolResponseMap wraps as {"output": text} / {"error": text}.
func flattenText(blocks []agent.ContentBlock) string {
	var sb strings.Builder
	for _, b := range blocks {
		if tb, ok := b.(agent.TextBlock); ok {
			sb.WriteString(tb.Text)
		}
	}
	return sb.String()
}

func toImagePart(v agent.ImageBlock) (*genai.Part, error) {
	switch v.Source.Kind {
	case agent.SourceBase64:
		data, err := base64.StdEncoding.DecodeString(v.Source.Data)
		if err != nil {
			return nil, fmt.Errorf("gemini: decoding base64 image data: %w", err)
		}
		return genai.NewPartFromBytes(data, v.Source.MediaType), nil
	case agent.SourceURL:
		// Best effort: Gemini fetches file URIs from its own Files API (or,
		// on Vertex AI, gs:// URIs) — not arbitrary public HTTP(S) URLs the
		// way Claude/OpenAI do.
		return genai.NewPartFromURI(v.Source.Data, v.Source.MediaType), nil
	default:
		return nil, fmt.Errorf("gemini: unsupported image source kind %q", v.Source.Kind)
	}
}

func toDocumentPart(v agent.DocumentBlock) (*genai.Part, error) {
	mediaType := v.Source.MediaType
	if mediaType == "" {
		mediaType = "application/pdf"
	}
	switch v.Source.Kind {
	case agent.SourceBase64:
		data, err := base64.StdEncoding.DecodeString(v.Source.Data)
		if err != nil {
			return nil, fmt.Errorf("gemini: decoding base64 document data: %w", err)
		}
		return genai.NewPartFromBytes(data, mediaType), nil
	case agent.SourceURL:
		return genai.NewPartFromURI(v.Source.Data, mediaType), nil
	default:
		return nil, fmt.Errorf("gemini: unsupported document source kind %q", v.Source.Kind)
	}
}

func fromResponse(resp *genai.GenerateContentResponse) (*agent.Response, error) {
	if len(resp.Candidates) == 0 {
		return nil, fmt.Errorf("gemini: response contained no candidates")
	}
	cand := resp.Candidates[0]

	var blocks []agent.ContentBlock
	hasToolUse := false
	if cand.Content != nil {
		for i, p := range cand.Content.Parts {
			block, ok := fromPart(p, i)
			if !ok {
				continue
			}
			if _, isToolUse := block.(agent.ToolUseBlock); isToolUse {
				hasToolUse = true
			}
			blocks = append(blocks, block)
		}
	}

	stopReason := fromFinishReason(cand.FinishReason)
	if hasToolUse {
		// Gemini signals a function-call turn via the presence of
		// FunctionCall parts, not a dedicated finish reason — FinishReason
		// is typically "STOP" (or empty) even when the model called a tool.
		stopReason = agent.StopToolUse
	}

	return &agent.Response{
		ID:         resp.ResponseID,
		Model:      resp.ModelVersion,
		Message:    agent.Message{Role: agent.RoleAssistant, Content: blocks},
		StopReason: stopReason,
		Usage:      fromUsage(resp.UsageMetadata),
		Raw:        resp,
	}, nil
}

func fromPart(p *genai.Part, index int) (agent.ContentBlock, bool) {
	switch {
	case p.FunctionCall != nil:
		fc := p.FunctionCall
		id := fc.ID
		if id == "" {
			id = fmt.Sprintf("%s_%d", fc.Name, index)
		}
		input, err := json.Marshal(fc.Args)
		if err != nil {
			input = []byte("{}")
		}
		return agent.ToolUseBlock{ID: id, Name: fc.Name, Input: input}, true
	case p.Thought:
		return agent.ThinkingBlock{Text: p.Text}, true
	case p.Text != "":
		return agent.TextBlock{Text: p.Text}, true
	default:
		return nil, false
	}
}

func fromFinishReason(fr genai.FinishReason) agent.StopReason {
	switch fr {
	case genai.FinishReasonStop, "":
		return agent.StopEndTurn
	case genai.FinishReasonMaxTokens:
		return agent.StopMaxTokens
	case genai.FinishReasonSafety, genai.FinishReasonRecitation, genai.FinishReasonLanguage:
		return agent.StopContentFilter
	default:
		return agent.StopUnknown
	}
}

func fromUsage(u *genai.GenerateContentResponseUsageMetadata) agent.Usage {
	if u == nil {
		return agent.Usage{}
	}
	return agent.Usage{
		InputTokens:     int(u.PromptTokenCount),
		OutputTokens:    int(u.CandidatesTokenCount) + int(u.ThoughtsTokenCount),
		CacheReadTokens: int(u.CachedContentTokenCount),
	}
}
