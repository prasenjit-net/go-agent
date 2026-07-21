package filestore_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	agent "github.com/prasenjit-net/go-agent"
	"github.com/prasenjit-net/go-agent/filestore"
)

func TestStore_LoadUnknownSessionReturnsNilNotError(t *testing.T) {
	s, err := filestore.New(t.TempDir())
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	msgs, err := s.Load(context.Background(), "does-not-exist")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if msgs != nil {
		t.Errorf("Load = %+v, want nil for an unknown session", msgs)
	}
}

func TestStore_SaveThenLoadRoundTripsEveryBlockType(t *testing.T) {
	s, err := filestore.New(t.TempDir())
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	want := []agent.Message{
		agent.UserMessage("what's the weather in Paris?"),
		agent.AssistantMessage(
			agent.ToolUseBlock{ID: "call_1", Name: "get_weather", Input: json.RawMessage(`{"city":"Paris"}`)},
		),
		{Role: agent.RoleUser, Content: []agent.ContentBlock{
			agent.ToolResultBlock{
				ToolUseID: "call_1",
				Content:   []agent.ContentBlock{agent.TextBlock{Text: "72F and sunny"}},
			},
		}},
		agent.AssistantMessage(
			agent.TextBlock{Text: "It's 72F and sunny."},
			agent.ThinkingBlock{Text: "the tool said 72F", Signature: "sig-abc"},
		),
		agent.UserMessage("here's a photo"),
		{Role: agent.RoleUser, Content: []agent.ContentBlock{
			agent.ImageBlock{Source: agent.ImageSource{Kind: agent.SourceBase64, MediaType: "image/png", Data: "abc123"}},
			agent.DocumentBlock{
				Source: agent.ImageSource{Kind: agent.SourceURL, Data: "https://example.com/report.pdf"},
				Title:  "report",
			},
		}},
	}

	ctx := context.Background()
	if err := s.Save(ctx, "session-1", want); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	got, err := s.Load(ctx, "session-1")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if !messagesEqual(t, want[i], got[i]) {
			t.Errorf("message %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestStore_SaveOverwritesPreviousContent(t *testing.T) {
	s, err := filestore.New(t.TempDir())
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	ctx := context.Background()

	if err := s.Save(ctx, "s1", []agent.Message{agent.UserMessage("first")}); err != nil {
		t.Fatalf("first Save returned error: %v", err)
	}
	if err := s.Save(ctx, "s1", []agent.Message{agent.UserMessage("second")}); err != nil {
		t.Fatalf("second Save returned error: %v", err)
	}

	got, err := s.Load(ctx, "s1")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(got) != 1 || got[0].Text() != "second" {
		t.Errorf("got = %+v, want only the second Save's content", got)
	}
}

func TestStore_SessionIDsAreFilesystemSafe(t *testing.T) {
	dir := t.TempDir()
	s, err := filestore.New(dir)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	ctx := context.Background()

	// A session ID containing path-traversal-shaped characters must not
	// escape dir or collide with an unrelated session.
	dangerous := "../../etc/passwd"
	if err := s.Save(ctx, dangerous, []agent.Message{agent.UserMessage("hi")}); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	if err := s.Save(ctx, "normal-session", []agent.Message{agent.UserMessage("unrelated")}); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	got, err := s.Load(ctx, dangerous)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(got) != 1 || got[0].Text() != "hi" {
		t.Errorf("got = %+v, want the dangerous-session-ID's own content back", got)
	}

	other, err := s.Load(ctx, "normal-session")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(other) != 1 || other[0].Text() != "unrelated" {
		t.Errorf("normal-session content = %+v, should be unaffected", other)
	}

	// And nothing should have been written outside dir.
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(dir), "passwd"))
	if len(matches) != 0 {
		t.Errorf("found files outside the store directory: %v", matches)
	}
}

func TestStore_New_CreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "sessions")
	if _, err := filestore.New(dir); err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	s2, err := filestore.New(dir)
	if err != nil {
		t.Fatalf("New against an already-created dir returned error: %v", err)
	}
	if _, err := s2.Load(context.Background(), "x"); err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
}

func messagesEqual(t *testing.T, a, b agent.Message) bool {
	t.Helper()
	if a.Role != b.Role || len(a.Content) != len(b.Content) {
		return false
	}
	// Compare via JSON-ish text representation for simplicity: every block
	// type's exported fields are covered by the round-trip already, so a
	// deep field comparison via fmt is sufficient to catch drift without
	// hand-writing a comparator per block type.
	for i := range a.Content {
		if !blockEqual(a.Content[i], b.Content[i]) {
			return false
		}
	}
	return true
}

func blockEqual(a, b agent.ContentBlock) bool {
	switch av := a.(type) {
	case agent.TextBlock:
		bv, ok := b.(agent.TextBlock)
		return ok && av == bv
	case agent.ImageBlock:
		bv, ok := b.(agent.ImageBlock)
		return ok && av == bv
	case agent.DocumentBlock:
		bv, ok := b.(agent.DocumentBlock)
		return ok && av == bv
	case agent.ToolUseBlock:
		bv, ok := b.(agent.ToolUseBlock)
		return ok && av.ID == bv.ID && av.Name == bv.Name && string(av.Input) == string(bv.Input)
	case agent.ToolResultBlock:
		bv, ok := b.(agent.ToolResultBlock)
		if !ok || av.ToolUseID != bv.ToolUseID || av.IsError != bv.IsError || len(av.Content) != len(bv.Content) {
			return false
		}
		for i := range av.Content {
			if !blockEqual(av.Content[i], bv.Content[i]) {
				return false
			}
		}
		return true
	case agent.ThinkingBlock:
		bv, ok := b.(agent.ThinkingBlock)
		return ok && av == bv
	default:
		return false
	}
}
