package agent

import (
	"context"
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
// returns the result.
func (s *Session) Send(ctx context.Context, input string) (*Result, error) {
	history, err := s.agent.store.Load(ctx, s.id)
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
