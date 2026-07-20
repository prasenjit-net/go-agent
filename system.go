package agent

import (
	"bytes"
	"context"
	"text/template"
)

// systemPart is one entry in a SystemPrompt, evaluated at Render time.
type systemPart interface {
	render(ctx context.Context) (SystemBlock, error)
}

// SystemPrompt is a composable, ordered builder for system instructions.
// Each section is added independently (and can be unit-tested
// independently) and rendered fresh on every Agent.Run/RunStream call, so
// AddFunc/AddTemplate sections always reflect current state.
//
// Ordering matters for providers with prompt caching: caching is a prefix
// match, so put static/cacheable sections first (via Add/AddCacheable) and
// per-request dynamic sections last (via AddFunc/AddTemplate) — that way a
// change in the dynamic tail never invalidates the cached prefix.
type SystemPrompt struct {
	parts []systemPart
}

// NewSystemPrompt returns an empty SystemPrompt ready for chaining.
func NewSystemPrompt() *SystemPrompt {
	return &SystemPrompt{}
}

type staticPart struct {
	text      string
	cacheable bool
}

func (p staticPart) render(context.Context) (SystemBlock, error) {
	return SystemBlock{Text: p.text, Cacheable: p.cacheable}, nil
}

// Add appends a static, non-cacheable instruction block.
func (s *SystemPrompt) Add(text string) *SystemPrompt {
	s.parts = append(s.parts, staticPart{text: text})
	return s
}

// AddCacheable appends a static instruction block hinted as cacheable — use
// this for large, stable content (few-shot examples, a knowledge-base dump,
// tool-usage policy) that doesn't change between requests. Providers
// without prompt-caching support simply ignore the hint.
func (s *SystemPrompt) AddCacheable(text string) *SystemPrompt {
	s.parts = append(s.parts, staticPart{text: text, cacheable: true})
	return s
}

type funcPart struct {
	fn func(ctx context.Context) (string, error)
}

func (p funcPart) render(ctx context.Context) (SystemBlock, error) {
	text, err := p.fn(ctx)
	if err != nil {
		return SystemBlock{}, err
	}
	return SystemBlock{Text: text}, nil
}

// AddFunc appends a block computed at render time — e.g. the current date,
// the authenticated user's name, feature flags. Evaluated fresh on every
// Run/RunStream call.
func (s *SystemPrompt) AddFunc(fn func(ctx context.Context) (string, error)) *SystemPrompt {
	s.parts = append(s.parts, funcPart{fn: fn})
	return s
}

// AddTemplate renders a text/template with data at render time. Convenience
// wrapper over AddFunc for the common "instructions with placeholders" case.
// The template is parsed once (at first render) and cached.
func (s *SystemPrompt) AddTemplate(tmpl string, data any) *SystemPrompt {
	var (
		compiled *template.Template
		compErr  error
		once     bool
	)
	s.parts = append(s.parts, funcPart{fn: func(context.Context) (string, error) {
		if !once {
			compiled, compErr = template.New("system").Parse(tmpl)
			once = true
		}
		if compErr != nil {
			return "", compErr
		}
		var buf bytes.Buffer
		if err := compiled.Execute(&buf, data); err != nil {
			return "", err
		}
		return buf.String(), nil
	}})
	return s
}

// Render evaluates every section, in order, into the final list of
// SystemBlock a Request carries.
func (s *SystemPrompt) Render(ctx context.Context) ([]SystemBlock, error) {
	if s == nil {
		return nil, nil
	}
	blocks := make([]SystemBlock, 0, len(s.parts))
	for _, p := range s.parts {
		b, err := p.render(ctx)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, b)
	}
	return blocks, nil
}
