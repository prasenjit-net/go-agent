package agent

// Role identifies who produced a Message.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// ContentBlock is a closed sum type covering every content shape a
// provider-agnostic Message can carry: text, images, documents, tool calls,
// tool results, and extended-thinking content. New concrete types are added
// only inside this package; application code consumes ContentBlock through a
// type switch.
type ContentBlock interface {
	// contentBlock is unexported so ContentBlock stays a closed set defined
	// entirely within this package.
	contentBlock()
}

// TextBlock is plain text content, the most common block type.
type TextBlock struct {
	Text string
}

func (TextBlock) contentBlock() {}

// SourceKind describes how ImageBlock/DocumentBlock data is encoded.
type SourceKind string

const (
	SourceBase64 SourceKind = "base64"
	SourceURL    SourceKind = "url"
)

// ImageSource describes where image or document bytes come from.
type ImageSource struct {
	Kind      SourceKind
	MediaType string // e.g. "image/png"; ignored when Kind == SourceURL
	Data      string // base64 payload, or the URL, depending on Kind
}

// ImageBlock carries an image for vision-capable models.
type ImageBlock struct {
	Source ImageSource
}

func (ImageBlock) contentBlock() {}

// DocumentBlock carries a document (e.g. a PDF) for models that support
// document understanding.
type DocumentBlock struct {
	Source ImageSource
	Title  string
}

func (DocumentBlock) contentBlock() {}

// ToolUseBlock is emitted by the assistant when it wants a tool invoked.
type ToolUseBlock struct {
	ID    string
	Name  string
	Input []byte // raw JSON object, as sent by the model
}

func (ToolUseBlock) contentBlock() {}

// ToolResultBlock carries a tool's output back to the model. It is sent in a
// user-role Message, addressed to a specific ToolUseBlock by ID.
type ToolResultBlock struct {
	ToolUseID string
	Content   []ContentBlock
	IsError   bool
}

func (ToolResultBlock) contentBlock() {}

// ThinkingBlock carries extended-reasoning content produced by the model.
// Signature is opaque and provider-specific; when a provider requires it
// echoed back unmodified on a later turn, the Agent run loop does so
// automatically and application code never needs to inspect it.
type ThinkingBlock struct {
	Text      string
	Signature string
}

func (ThinkingBlock) contentBlock() {}

// Message is one turn in a conversation.
type Message struct {
	Role    Role
	Content []ContentBlock
}

// UserMessage builds a single-block plain-text user Message.
func UserMessage(text string) Message {
	return Message{Role: RoleUser, Content: []ContentBlock{TextBlock{Text: text}}}
}

// UserMessageBlocks builds a user Message out of arbitrary content blocks
// (e.g. text plus an image).
func UserMessageBlocks(blocks ...ContentBlock) Message {
	return Message{Role: RoleUser, Content: blocks}
}

// AssistantMessage builds an assistant Message out of arbitrary content
// blocks. Mainly useful in tests and when hand-constructing few-shot
// examples in conversation history.
func AssistantMessage(blocks ...ContentBlock) Message {
	return Message{Role: RoleAssistant, Content: blocks}
}

// Text concatenates every TextBlock in the message, in order. Convenience
// for the common case of reading a plain-text response.
func (m Message) Text() string {
	var out string
	for _, b := range m.Content {
		if tb, ok := b.(TextBlock); ok {
			out += tb.Text
		}
	}
	return out
}

// ToolUses returns every ToolUseBlock in the message, in order.
func (m Message) ToolUses() []ToolUseBlock {
	var out []ToolUseBlock
	for _, b := range m.Content {
		if tu, ok := b.(ToolUseBlock); ok {
			out = append(out, tu)
		}
	}
	return out
}
