package agent

import (
	"context"
	"fmt"
	"sync"
)

// ConversationStore persists a session's message history between calls.
// Implement this to back Session with Redis, Postgres, a file, etc.; the
// zero-config default is NewInMemoryStore.
type ConversationStore interface {
	Load(ctx context.Context, sessionID string) ([]Message, error)
	Save(ctx context.Context, sessionID string, msgs []Message) error
}

type inMemoryStore struct {
	mu   sync.Mutex
	data map[string][]Message
}

// NewInMemoryStore returns a ConversationStore backed by a process-local
// map. Fine for CLIs, tests, and single-process use; not shared across
// processes or persisted across restarts.
func NewInMemoryStore() ConversationStore {
	return &inMemoryStore{data: make(map[string][]Message)}
}

func (s *inMemoryStore) Load(_ context.Context, sessionID string) ([]Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	msgs := s.data[sessionID]
	out := make([]Message, len(msgs))
	copy(out, msgs)
	return out, nil
}

func (s *inMemoryStore) Save(_ context.Context, sessionID string, msgs []Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored := make([]Message, len(msgs))
	copy(stored, msgs)
	s.data[sessionID] = stored
	return nil
}

// Session binds an Agent to a persistent conversation identified by id,
// loading and saving history via the Agent's ConversationStore around every
// Send/SendStream call. Session is not safe for concurrent Send calls on
// the same session ID — conversation history has an inherent sequential
// dependency; synchronize at the application layer (e.g. a per-session
// mutex or single-writer queue) if concurrent turns on one session are
// possible.
type Session struct {
	agent *Agent
	id    string
}

// NewSession returns a Session bound to id, using a.store for persistence.
func (a *Agent) NewSession(id string) *Session {
	return &Session{agent: a, id: id}
}

// ID returns the session identifier.
func (s *Session) ID() string { return s.id }

// History returns the session's current stored messages.
func (s *Session) History(ctx context.Context) ([]Message, error) {
	return s.agent.store.Load(ctx, s.id)
}

// Reset clears the session's stored history.
func (s *Session) Reset(ctx context.Context) error {
	return s.agent.store.Save(ctx, s.id, nil)
}

// Send appends input as a user turn to the session's history, runs the
// agent, persists the updated history (including tool round-trips), and
// returns the result. If a Compactor is configured (see WithCompactor), the
// loaded history is compacted first whenever it crosses the configured
// token threshold, and the compacted (not the original) history is what
// gets persisted.
func (s *Session) Send(ctx context.Context, input string) (*Result, error) {
	history, err := s.agent.store.Load(ctx, s.id)
	if err != nil {
		return nil, err
	}
	history, err = s.agent.maybeCompact(ctx, history)
	if err != nil {
		return nil, err
	}
	history = append(history, UserMessage(input))

	result, err := s.agent.RunMessages(ctx, history...)
	if err != nil {
		return nil, err
	}
	if err := s.agent.store.Save(ctx, s.id, result.Messages); err != nil {
		return nil, err
	}
	return result, nil
}

// Compactor reduces a conversation's message history, e.g. to stay under a
// token budget. Implementations are free to summarize, truncate, or drop
// messages; the returned slice replaces history for the upcoming turn and
// is what Session.Send persists afterward. Left pluggable deliberately:
// some providers have native server-side compaction (out of scope to
// reimplement here), others need a client-side summarization pass — the
// interface accommodates either without Session knowing which. See
// NewWindowCompactor for a dependency-free reference implementation.
type Compactor interface {
	Compact(ctx context.Context, msgs []Message) ([]Message, error)
}

// maybeCompact runs the configured Compactor over history if the provider
// implements TokenCounter and the estimated token count is at or above the
// configured threshold. A nil Compactor or non-positive threshold disables
// this entirely (the default) — compaction is lossy, so callers opt in
// deliberately via WithCompactor rather than getting it for free. If the
// provider doesn't implement TokenCounter, there is no cheap way to
// estimate size, so compaction never triggers; Session.Send still works,
// simply without it.
func (a *Agent) maybeCompact(ctx context.Context, history []Message) ([]Message, error) {
	if a.compactor == nil || a.compactTokens <= 0 || len(history) == 0 {
		return history, nil
	}
	counter, ok := a.provider.(TokenCounter)
	if !ok {
		return history, nil
	}
	count, err := counter.CountTokens(ctx, &Request{Model: a.model, Messages: history})
	if err != nil {
		return nil, fmt.Errorf("agent: estimating token count for compaction: %w", err)
	}
	if count < a.compactTokens {
		return history, nil
	}
	compacted, err := a.compactor.Compact(ctx, history)
	if err != nil {
		return nil, fmt.Errorf("agent: compacting history: %w", err)
	}
	return compacted, nil
}
