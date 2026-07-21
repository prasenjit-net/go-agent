package agent

import "context"

// windowCompactor keeps only the most recent maxMessages, implementing
// Compactor.
type windowCompactor struct {
	maxMessages int
}

// NewWindowCompactor returns a Compactor that keeps only the most recent
// maxMessages messages, dropping the rest. It is tool-pairing-aware: if the
// naive cut point would keep a ToolResultBlock whose originating
// ToolUseBlock falls outside the window, that message (and any others like
// it at the front of the kept slice) is dropped too — every first-class
// provider rejects a tool result with no matching call in the same
// request, so keeping an orphaned one would break the next turn rather
// than merely lose context.
//
// This is a blunt strategy: dropped messages are gone, not summarized. It
// needs no extra model call and no dependencies, making it a reasonable
// default and a template for a smarter (e.g. summarizing) Compactor.
func NewWindowCompactor(maxMessages int) Compactor {
	return &windowCompactor{maxMessages: maxMessages}
}

func (c *windowCompactor) Compact(_ context.Context, msgs []Message) ([]Message, error) {
	if c.maxMessages <= 0 || len(msgs) <= c.maxMessages {
		return msgs, nil
	}
	kept := msgs[len(msgs)-c.maxMessages:]
	return dropOrphanedToolResults(kept), nil
}

// dropOrphanedToolResults trims leading messages from msgs whose
// ToolResultBlock(s) reference a ToolUseBlock ID not present anywhere in
// msgs, repeating until the invariant holds (a single truncation can orphan
// more than one leading tool-result message when several tool calls were
// answered in sequence).
func dropOrphanedToolResults(msgs []Message) []Message {
	toolUseIDs := map[string]bool{}
	for _, m := range msgs {
		for _, tu := range m.ToolUses() {
			toolUseIDs[tu.ID] = true
		}
	}
	start := 0
	for start < len(msgs) && hasOrphanedToolResult(msgs[start], toolUseIDs) {
		start++
	}
	return msgs[start:]
}

func hasOrphanedToolResult(m Message, toolUseIDs map[string]bool) bool {
	for _, b := range m.Content {
		if tr, ok := b.(ToolResultBlock); ok && !toolUseIDs[tr.ToolUseID] {
			return true
		}
	}
	return false
}
