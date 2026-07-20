package agent

// ThinkingMode selects how a model should use extended reasoning.
type ThinkingMode string

const (
	// ThinkingOff disables extended thinking (the default when Thinking is nil).
	ThinkingOff ThinkingMode = "off"
	// ThinkingAdaptive lets the provider decide when and how much to think.
	ThinkingAdaptive ThinkingMode = "adaptive"
	// ThinkingBudgeted requests a fixed thinking-token budget, for providers
	// that only support the legacy fixed-budget form.
	ThinkingBudgeted ThinkingMode = "budgeted"
)

// ThinkingConfig configures extended/deep reasoning for a request. Providers
// that don't support thinking simply ignore it; providers whose only mode is
// adaptive treat ThinkingBudgeted as ThinkingAdaptive.
type ThinkingConfig struct {
	Mode   ThinkingMode
	Budget int // consulted only when Mode == ThinkingBudgeted
}

// ToolChoiceMode controls whether/how the model must use tools.
type ToolChoiceMode string

const (
	// ToolChoiceAuto lets the model decide whether to use a tool (default).
	ToolChoiceAuto ToolChoiceMode = "auto"
	// ToolChoiceAny forces the model to use some tool, any tool.
	ToolChoiceAny ToolChoiceMode = "any"
	// ToolChoiceOne forces the model to use the tool named by ToolChoice.Name.
	ToolChoiceOne ToolChoiceMode = "tool"
	// ToolChoiceNone disables tool use for this request even if tools are
	// attached (useful for a final "summarize" turn).
	ToolChoiceNone ToolChoiceMode = "none"
)

// ToolChoice controls tool-use behavior for a single request.
type ToolChoice struct {
	Mode ToolChoiceMode
	Name string // required when Mode == ToolChoiceOne
}

// SystemBlock is one section of a rendered system prompt. See SystemPrompt
// in system.go for the composable builder that produces these.
type SystemBlock struct {
	Text string
	// Cacheable hints that a provider with prompt-caching support should
	// cache this block. Providers without caching support ignore it.
	Cacheable bool
}

// Request is the provider-agnostic shape of a single inference call. Every
// Provider.Generate/Stream implementation translates a Request into that
// vendor's wire format and back.
type Request struct {
	Model      string
	System     []SystemBlock
	Messages   []Message
	Tools      []RegisteredTool
	ToolChoice ToolChoice
	MaxTokens  int
	Thinking   *ThinkingConfig
	// Metadata is free-form and provider-specific; adapters may ignore keys
	// they don't understand.
	Metadata map[string]string
}
