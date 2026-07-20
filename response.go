package agent

// StopReason explains why the model stopped generating.
type StopReason string

const (
	StopEndTurn       StopReason = "end_turn"
	StopMaxTokens     StopReason = "max_tokens"
	StopToolUse       StopReason = "tool_use"
	StopRefusal       StopReason = "refusal"
	StopContentFilter StopReason = "content_filter"
	StopUnknown       StopReason = "unknown"
)

// Usage reports token accounting for a single Generate/Stream call.
type Usage struct {
	InputTokens         int
	OutputTokens        int
	CacheReadTokens     int
	CacheCreationTokens int
}

// Add accumulates u2 into u in place and returns u for chaining.
func (u *Usage) Add(u2 Usage) *Usage {
	u.InputTokens += u2.InputTokens
	u.OutputTokens += u2.OutputTokens
	u.CacheReadTokens += u2.CacheReadTokens
	u.CacheCreationTokens += u2.CacheCreationTokens
	return u
}

// Response is the provider-agnostic shape of a single inference result.
type Response struct {
	ID         string
	Model      string
	Message    Message // Role is always RoleAssistant
	StopReason StopReason
	Usage      Usage

	// Raw is the provider-native response object (e.g. *anthropic.Message,
	// *openai.ChatCompletion, *genai.GenerateContentResponse). It is an
	// escape hatch for provider-specific fields not yet promoted into the
	// unified model; code that reads it is coupled to that provider by
	// definition, and the core Agent run loop never reads it.
	Raw any
}
