package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// DefaultMaxIterations bounds how many model round-trips a single Run may
// take before it gives up with ErrMaxIterations. It exists specifically so
// a misbehaving tool or an unexpectedly chatty model can't loop forever;
// combine with a context deadline for a wall-clock bound as well.
const DefaultMaxIterations = 25

// Agent binds a Provider, model, system prompt, and tool set into a bounded
// tool-use loop. Agent is safe for concurrent use by multiple goroutines
// once constructed (no field is mutated after New returns) — the common
// case of one *Agent shared across many HTTP requests works without
// external locking.
type Agent struct {
	provider       Provider
	model          string
	system         *SystemPrompt
	tools          *ToolSet
	toolChoice     ToolChoice
	maxTokens      int
	thinking       *ThinkingConfig
	maxIterations  int
	maxParallel    int
	hooks          Hooks
	store          ConversationStore
	retry          RetryPolicy
	streamFallback StreamingFallbackMode
}

// New builds an Agent from the given options. WithProvider is effectively
// required — an Agent with no provider returns an error from every Run
// call rather than panicking.
func New(opts ...Option) *Agent {
	a := &Agent{
		maxIterations:  DefaultMaxIterations,
		toolChoice:     ToolChoice{Mode: ToolChoiceAuto},
		system:         NewSystemPrompt(),
		tools:          NewToolSet(),
		store:          NewInMemoryStore(),
		retry:          DefaultRetryPolicy(),
		streamFallback: FallbackSingleShot,
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// Result is what Run/RunMessages return: the final response plus the full
// transcript (including any tool round-trips) appended during this run.
type Result struct {
	FinalResponse *Response
	Messages      []Message
	Usage         Usage
	Iterations    int
}

// Run sends input as a single user turn (with no prior history) and runs
// the tool-use loop until the model produces a final answer.
func (a *Agent) Run(ctx context.Context, input string) (*Result, error) {
	return a.RunMessages(ctx, UserMessage(input))
}

// RunMessages runs the tool-use loop starting from an explicit message
// history (e.g. a prior conversation loaded from a Session/ConversationStore).
func (a *Agent) RunMessages(ctx context.Context, msgs ...Message) (*Result, error) {
	if a.provider == nil {
		return nil, &Error{Code: ErrInvalidRequest, Message: "agent: no Provider configured (use agent.WithProvider)"}
	}
	if err := a.validateConfig(); err != nil {
		return nil, err
	}

	history := make([]Message, len(msgs))
	copy(history, msgs)

	usage := Usage{}
	for iter := 0; iter < a.maxIterations; iter++ {
		req, err := a.buildRequest(ctx, history)
		if err != nil {
			return nil, err
		}

		if a.hooks.BeforeGenerate != nil {
			if err := a.hooks.BeforeGenerate(ctx, req); err != nil {
				a.reportError(ctx, err)
				return nil, err
			}
		}

		resp, err := a.generateWithRetry(ctx, req)
		if err != nil {
			a.reportError(ctx, err)
			return nil, err
		}
		usage.Add(resp.Usage)
		if a.hooks.AfterGenerate != nil {
			a.hooks.AfterGenerate(ctx, resp)
		}

		history = append(history, resp.Message)

		if resp.StopReason != StopToolUse {
			return &Result{FinalResponse: resp, Messages: history, Usage: usage, Iterations: iter + 1}, nil
		}

		toolResults := a.executeTools(ctx, resp.Message.Content)
		history = append(history, Message{Role: RoleUser, Content: toolResults})
	}

	err := &Error{Code: ErrMaxIterations, Provider: a.provider.Name(),
		Message: fmt.Sprintf("exceeded max iterations (%d) without a final answer", a.maxIterations)}
	a.reportError(ctx, err)
	return nil, err
}

func (a *Agent) validateConfig() error {
	caps := CapabilitiesOf(a.provider)
	if a.thinking != nil && a.thinking.Mode != ThinkingOff && !caps.Thinking {
		// Not fatal: some providers implement Capable conservatively (all
		// false) without actually lacking the feature, so this is only
		// enforced when the provider positively declares support and it's
		// missing. Providers that don't implement Capable at all (zero
		// value Capabilities{}) are given the benefit of the doubt here and
		// simply forward Thinking as requested.
		if _, ok := a.provider.(Capable); ok && !caps.Thinking {
			return &Error{Code: ErrInvalidRequest, Provider: a.provider.Name(),
				Message: "thinking requested but provider does not declare Thinking support"}
		}
	}
	return nil
}

func (a *Agent) buildRequest(ctx context.Context, history []Message) (*Request, error) {
	system, err := a.system.Render(ctx)
	if err != nil {
		return nil, fmt.Errorf("agent: rendering system prompt: %w", err)
	}
	return &Request{
		Model:      a.model,
		System:     system,
		Messages:   history,
		Tools:      a.tools.List(),
		ToolChoice: a.toolChoice,
		MaxTokens:  a.maxTokens,
		Thinking:   a.thinking,
	}, nil
}

func (a *Agent) generateWithRetry(ctx context.Context, req *Request) (*Response, error) {
	var lastErr error
	for attempt := 0; attempt <= a.retry.MaxRetries; attempt++ {
		resp, err := a.provider.Generate(ctx, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if attempt == a.retry.MaxRetries || !IsRetryable(err) {
			return nil, err
		}

		var agentErr *Error
		delay := a.retry.delay(attempt)
		if errors.As(err, &agentErr) && agentErr.RetryAfter > 0 {
			delay = agentErr.RetryAfter
		}
		if err := sleepCtx(ctx, delay); err != nil {
			return nil, err
		}
	}
	return nil, lastErr
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// executeTools runs every ToolUseBlock found in blocks concurrently
// (matching how providers expect all tool_results for a turn to come back
// together in the next message) and returns the corresponding
// ToolResultBlocks in the same order.
func (a *Agent) executeTools(ctx context.Context, blocks []ContentBlock) []ContentBlock {
	var toolUses []ToolUseBlock
	for _, b := range blocks {
		if tu, ok := b.(ToolUseBlock); ok {
			toolUses = append(toolUses, tu)
		}
	}
	if len(toolUses) == 0 {
		return nil
	}

	results := make([]ContentBlock, len(toolUses))
	sem := a.parallelSemaphore(len(toolUses))
	var wg sync.WaitGroup
	for i, tu := range toolUses {
		wg.Add(1)
		go func(i int, tu ToolUseBlock) {
			defer wg.Done()
			if sem != nil {
				sem <- struct{}{}
				defer func() { <-sem }()
			}
			results[i] = a.invokeOne(ctx, tu)
		}(i, tu)
	}
	wg.Wait()
	return results
}

func (a *Agent) parallelSemaphore(n int) chan struct{} {
	if a.maxParallel <= 0 || a.maxParallel >= n {
		return nil
	}
	return make(chan struct{}, a.maxParallel)
}

func (a *Agent) invokeOne(ctx context.Context, tu ToolUseBlock) ContentBlock {
	call := ToolCall{ID: tu.ID, Name: tu.Name, Input: tu.Input}

	if a.hooks.BeforeToolCall != nil {
		allow, override := a.hooks.BeforeToolCall(ctx, call)
		if !allow {
			result := ToolResult{Content: []ContentBlock{TextBlock{Text: "tool call denied by policy"}}, IsError: true}
			if override != nil {
				result = *override
			}
			if a.hooks.AfterToolCall != nil {
				a.hooks.AfterToolCall(ctx, call, result)
			}
			return ToolResultBlock{ToolUseID: tu.ID, Content: result.Content, IsError: result.IsError}
		}
	}

	tool, ok := a.tools.Get(tu.Name)
	var result ToolResult
	if !ok {
		result = ErrorResultf("unknown tool %q", tu.Name)
	} else {
		r, err := tool.Invoke(ctx, tu.Input)
		if err != nil {
			// A Go error from a tool handler (as opposed to a
			// model-recoverable ToolResult{IsError:true}) signals a
			// programming error, not something the model can route around.
			// It's still surfaced to the model as an error result so the
			// run can terminate gracefully, and reported via OnError.
			a.reportError(ctx, fmt.Errorf("tool %q: %w", tu.Name, err))
			result = ErrorResultf("tool %q failed: %v", tu.Name, err)
		} else {
			result = r
		}
	}

	if a.hooks.AfterToolCall != nil {
		a.hooks.AfterToolCall(ctx, call, result)
	}
	return ToolResultBlock{ToolUseID: tu.ID, Content: result.Content, IsError: result.IsError}
}

func (a *Agent) reportError(ctx context.Context, err error) {
	if a.hooks.OnError != nil {
		a.hooks.OnError(ctx, err)
	}
}

// Note delivers an operator instruction mid-conversation via Session.Send's
// next call. If the provider implements SystemUpdater, the instruction uses
// the provider's native mechanism (preserving any cached prefix); otherwise
// it is queued as a synthetic reminder block prepended to the next user
// turn, so the same call works across every provider with best-available
// fidelity.
//
// Note is a placeholder for the mid-conversation-system-message feature
// described in the design document (Phase 4); the current implementation
// covers the SystemUpdater-aware path and is expected to grow the
// synthetic-fallback queuing in a follow-up change.
func (a *Agent) Note(_ context.Context, note string) (Message, error) {
	if su, ok := a.provider.(SystemUpdater); ok {
		return su.SystemUpdateMessage(note)
	}
	return Message{Role: RoleUser, Content: []ContentBlock{TextBlock{Text: "<system-reminder>" + note + "</system-reminder>"}}}, nil
}
