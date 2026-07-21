package filestore

import (
	"encoding/json"
	"fmt"

	agent "github.com/prasenjit-net/go-agent"
)

// wireMessage and wireBlock are a JSON-serializable mirror of agent.Message
// / agent.ContentBlock. ContentBlock is a closed interface (its one method
// is unexported to the agent package), so plain encoding/json can marshal a
// []ContentBlock field but cannot unmarshal one back — there is no
// discriminator telling json.Unmarshal which concrete type to instantiate.
// wireBlock supplies that discriminator (Type) and a flat superset of every
// variant's fields; toWireBlock/fromWireBlock do the two-way conversion.
type wireMessage struct {
	Role    agent.Role  `json:"role"`
	Content []wireBlock `json:"content"`
}

type wireBlock struct {
	Type string `json:"type"`

	// text, thinking
	Text      string `json:"text,omitempty"`
	Signature string `json:"signature,omitempty"` // thinking only

	// image, document
	SourceKind string `json:"source_kind,omitempty"`
	MediaType  string `json:"media_type,omitempty"`
	Data       string `json:"data,omitempty"`
	Title      string `json:"title,omitempty"` // document only

	// tool_use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`

	// tool_result
	ToolUseID string      `json:"tool_use_id,omitempty"`
	Content   []wireBlock `json:"content,omitempty"`
	IsError   bool        `json:"is_error,omitempty"`
}

func toWireMessages(msgs []agent.Message) ([]wireMessage, error) {
	out := make([]wireMessage, len(msgs))
	for i, m := range msgs {
		blocks, err := toWireBlocks(m.Content)
		if err != nil {
			return nil, err
		}
		out[i] = wireMessage{Role: m.Role, Content: blocks}
	}
	return out, nil
}

func toWireBlocks(blocks []agent.ContentBlock) ([]wireBlock, error) {
	out := make([]wireBlock, len(blocks))
	for i, b := range blocks {
		wb, err := toWireBlock(b)
		if err != nil {
			return nil, err
		}
		out[i] = wb
	}
	return out, nil
}

func toWireBlock(b agent.ContentBlock) (wireBlock, error) {
	switch v := b.(type) {
	case agent.TextBlock:
		return wireBlock{Type: "text", Text: v.Text}, nil

	case agent.ImageBlock:
		return wireBlock{
			Type: "image", SourceKind: string(v.Source.Kind),
			MediaType: v.Source.MediaType, Data: v.Source.Data,
		}, nil

	case agent.DocumentBlock:
		return wireBlock{
			Type: "document", SourceKind: string(v.Source.Kind),
			MediaType: v.Source.MediaType, Data: v.Source.Data, Title: v.Title,
		}, nil

	case agent.ToolUseBlock:
		return wireBlock{Type: "tool_use", ID: v.ID, Name: v.Name, Input: json.RawMessage(v.Input)}, nil

	case agent.ToolResultBlock:
		content, err := toWireBlocks(v.Content)
		if err != nil {
			return wireBlock{}, err
		}
		return wireBlock{Type: "tool_result", ToolUseID: v.ToolUseID, Content: content, IsError: v.IsError}, nil

	case agent.ThinkingBlock:
		return wireBlock{Type: "thinking", Text: v.Text, Signature: v.Signature}, nil

	default:
		return wireBlock{}, fmt.Errorf("filestore: unsupported content block type %T", b)
	}
}

func fromWireMessages(wms []wireMessage) ([]agent.Message, error) {
	out := make([]agent.Message, len(wms))
	for i, wm := range wms {
		blocks, err := fromWireBlocks(wm.Content)
		if err != nil {
			return nil, err
		}
		out[i] = agent.Message{Role: wm.Role, Content: blocks}
	}
	return out, nil
}

func fromWireBlocks(wbs []wireBlock) ([]agent.ContentBlock, error) {
	out := make([]agent.ContentBlock, len(wbs))
	for i, wb := range wbs {
		b, err := fromWireBlock(wb)
		if err != nil {
			return nil, err
		}
		out[i] = b
	}
	return out, nil
}

func fromWireBlock(wb wireBlock) (agent.ContentBlock, error) {
	switch wb.Type {
	case "text":
		return agent.TextBlock{Text: wb.Text}, nil

	case "image":
		return agent.ImageBlock{Source: agent.ImageSource{
			Kind: agent.SourceKind(wb.SourceKind), MediaType: wb.MediaType, Data: wb.Data,
		}}, nil

	case "document":
		return agent.DocumentBlock{
			Source: agent.ImageSource{Kind: agent.SourceKind(wb.SourceKind), MediaType: wb.MediaType, Data: wb.Data},
			Title:  wb.Title,
		}, nil

	case "tool_use":
		return agent.ToolUseBlock{ID: wb.ID, Name: wb.Name, Input: []byte(wb.Input)}, nil

	case "tool_result":
		content, err := fromWireBlocks(wb.Content)
		if err != nil {
			return nil, err
		}
		return agent.ToolResultBlock{ToolUseID: wb.ToolUseID, Content: content, IsError: wb.IsError}, nil

	case "thinking":
		return agent.ThinkingBlock{Text: wb.Text, Signature: wb.Signature}, nil

	default:
		return nil, fmt.Errorf("filestore: unknown content block type %q in stored session", wb.Type)
	}
}
