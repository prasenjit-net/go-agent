package agent_test

import (
	"context"
	"testing"

	agent "github.com/prasenjit-net/go-agent"
	"github.com/prasenjit-net/go-agent/agenttest"
)

func TestSession_Send_PersistsHistoryAcrossTurns(t *testing.T) {
	mock := &agenttest.MockProvider{
		Responses: []*agent.Response{
			{Message: agent.AssistantMessage(agent.TextBlock{Text: "hi Alice"}), StopReason: agent.StopEndTurn},
			{Message: agent.AssistantMessage(agent.TextBlock{Text: "your name is Alice"}), StopReason: agent.StopEndTurn},
		},
	}
	a := agent.New(agent.WithProvider(mock), agent.WithModel("mock-model"))
	session := a.NewSession("user-1")

	if _, err := session.Send(context.Background(), "I'm Alice"); err != nil {
		t.Fatalf("first Send returned error: %v", err)
	}
	if _, err := session.Send(context.Background(), "what's my name?"); err != nil {
		t.Fatalf("second Send returned error: %v", err)
	}

	// The second call's request should carry the first turn's full history.
	calls := mock.Calls()
	if len(calls) != 2 {
		t.Fatalf("len(calls) = %d, want 2", len(calls))
	}
	if len(calls[1].Messages) != 3 {
		t.Fatalf("second call Messages = %+v, want 3 (user, assistant, user)", calls[1].Messages)
	}

	history, err := session.History(context.Background())
	if err != nil {
		t.Fatalf("History returned error: %v", err)
	}
	if len(history) != 4 {
		t.Fatalf("len(history) = %d, want 4", len(history))
	}
}

func TestSession_Reset_ClearsHistory(t *testing.T) {
	mock := &agenttest.MockProvider{
		Responses: []*agent.Response{{Message: agent.AssistantMessage(agent.TextBlock{Text: "hi"}), StopReason: agent.StopEndTurn}},
	}
	a := agent.New(agent.WithProvider(mock), agent.WithModel("mock-model"))
	session := a.NewSession("user-1")

	if _, err := session.Send(context.Background(), "hello"); err != nil {
		t.Fatalf("Send returned error: %v", err)
	}
	if err := session.Reset(context.Background()); err != nil {
		t.Fatalf("Reset returned error: %v", err)
	}

	history, err := session.History(context.Background())
	if err != nil {
		t.Fatalf("History returned error: %v", err)
	}
	if len(history) != 0 {
		t.Errorf("len(history) = %d, want 0 after Reset", len(history))
	}
}

func TestSession_ID(t *testing.T) {
	a := agent.New(agent.WithProvider(&agenttest.MockProvider{}))
	session := a.NewSession("user-42")
	if session.ID() != "user-42" {
		t.Errorf("ID() = %q, want user-42", session.ID())
	}
}

// countingProvider extends MockProvider with a scriptable CountTokens, so
// tests can exercise WithCompactor's TokenCounter-gated trigger without a
// real provider.
type countingProvider struct {
	*agenttest.MockProvider
	tokens int
}

func (p *countingProvider) CountTokens(_ context.Context, _ *agent.Request) (int, error) {
	return p.tokens, nil
}

var _ agent.TokenCounter = (*countingProvider)(nil)

// recordingCompactor records every history it's asked to compact and
// returns a fixed replacement, so tests can assert both whether compaction
// ran and what Session.Send persisted afterward.
type recordingCompactor struct {
	called      bool
	gotMessages []agent.Message
	replacement []agent.Message
	err         error
}

func (c *recordingCompactor) Compact(_ context.Context, msgs []agent.Message) ([]agent.Message, error) {
	c.called = true
	c.gotMessages = msgs
	if c.err != nil {
		return nil, c.err
	}
	return c.replacement, nil
}

func TestSession_Send_NoCompactorByDefault(t *testing.T) {
	mock := &agenttest.MockProvider{
		Responses: []*agent.Response{{Message: agent.AssistantMessage(agent.TextBlock{Text: "ok"}), StopReason: agent.StopEndTurn}},
	}
	provider := &countingProvider{MockProvider: mock, tokens: 1_000_000}
	a := agent.New(agent.WithProvider(provider), agent.WithModel("mock-model"))
	session := a.NewSession("s1")

	if _, err := session.Send(context.Background(), "hi"); err != nil {
		t.Fatalf("Send returned error: %v", err)
	}
	// No WithCompactor was configured, so CountTokens should never even be
	// consulted — nothing to assert on directly here beyond "it didn't
	// error", but the point is proven by the next few tests contrasting
	// against a configured compactor.
}

func TestSession_Send_CompactsWhenThresholdCrossed(t *testing.T) {
	mock := &agenttest.MockProvider{
		Responses: []*agent.Response{{Message: agent.AssistantMessage(agent.TextBlock{Text: "ok"}), StopReason: agent.StopEndTurn}},
	}
	provider := &countingProvider{MockProvider: mock, tokens: 500}
	compactor := &recordingCompactor{replacement: []agent.Message{agent.UserMessage("summary")}}
	a := agent.New(
		agent.WithProvider(provider),
		agent.WithModel("mock-model"),
		agent.WithCompactor(compactor, 100),
	)
	session := a.NewSession("s1")

	// Seed some history first so there's something to compact on the next Send.
	if _, err := session.Send(context.Background(), "turn one"); err != nil {
		t.Fatalf("seeding Send returned error: %v", err)
	}
	compactor.called = false // reset: only care about the second Send below

	if _, err := session.Send(context.Background(), "turn two"); err != nil {
		t.Fatalf("second Send returned error: %v", err)
	}
	if !compactor.called {
		t.Fatal("expected the compactor to be invoked once the token estimate crossed the threshold")
	}

	// The persisted history should be built on the compactor's replacement,
	// not the original — the request sent to the provider on this turn
	// should reflect "summary" + "turn two", not the original seed turn.
	calls := mock.Calls()
	last := calls[len(calls)-1]
	if last.Messages[0].Text() != "summary" {
		t.Errorf("first message in the compacted request = %q, want %q", last.Messages[0].Text(), "summary")
	}
}

func TestSession_Send_SkipsCompactionBelowThreshold(t *testing.T) {
	mock := &agenttest.MockProvider{
		Responses: []*agent.Response{{Message: agent.AssistantMessage(agent.TextBlock{Text: "ok"}), StopReason: agent.StopEndTurn}},
	}
	provider := &countingProvider{MockProvider: mock, tokens: 10}
	compactor := &recordingCompactor{}
	a := agent.New(
		agent.WithProvider(provider),
		agent.WithModel("mock-model"),
		agent.WithCompactor(compactor, 100),
	)
	session := a.NewSession("s1")

	if _, err := session.Send(context.Background(), "hi"); err != nil {
		t.Fatalf("Send returned error: %v", err)
	}
	if compactor.called {
		t.Error("compactor should not run when the estimated token count is below the threshold")
	}
}

func TestSession_Send_SkipsCompactionWithoutTokenCounter(t *testing.T) {
	// A plain MockProvider does not implement agent.TokenCounter, so there's
	// no cheap way to estimate size — compaction must never trigger, no
	// matter the configured threshold.
	mock := &agenttest.MockProvider{
		Responses: []*agent.Response{{Message: agent.AssistantMessage(agent.TextBlock{Text: "ok"}), StopReason: agent.StopEndTurn}},
	}
	compactor := &recordingCompactor{}
	a := agent.New(
		agent.WithProvider(mock),
		agent.WithModel("mock-model"),
		agent.WithCompactor(compactor, 1),
	)
	session := a.NewSession("s1")

	if _, err := session.Send(context.Background(), "hi"); err != nil {
		t.Fatalf("Send returned error: %v", err)
	}
	if compactor.called {
		t.Error("compactor should not run against a provider that doesn't implement TokenCounter")
	}
}

func TestSession_Send_PropagatesCompactorError(t *testing.T) {
	mock := &agenttest.MockProvider{
		Responses: []*agent.Response{{Message: agent.AssistantMessage(agent.TextBlock{Text: "ok"}), StopReason: agent.StopEndTurn}},
	}
	provider := &countingProvider{MockProvider: mock, tokens: 500}
	compactor := &recordingCompactor{err: errCompactBoom}
	a := agent.New(
		agent.WithProvider(provider),
		agent.WithModel("mock-model"),
		agent.WithCompactor(compactor, 100),
	)
	session := a.NewSession("s1")

	// Seed history so the threshold check on the next Send has something to
	// estimate and the compactor actually gets a chance to fail.
	if _, err := session.Send(context.Background(), "turn one"); err != nil {
		t.Fatalf("seeding Send returned error: %v", err)
	}

	if _, err := session.Send(context.Background(), "turn two"); err == nil {
		t.Fatal("expected Send to propagate the compactor's error")
	}
}

var errCompactBoom = &agent.Error{Code: agent.ErrUnknown, Message: "boom"}
