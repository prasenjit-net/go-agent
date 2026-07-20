// Package gemini adapts the official Google GenAI Go SDK
// (google.golang.org/genai) to the agent.Provider interface family, giving
// it first-class status alongside the claude and openai adapters. It
// supports both the Gemini API and Vertex AI backends (see WithVertexAI).
package gemini

import (
	"context"
	"net/http"

	"google.golang.org/genai"

	agent "github.com/prasenjit-net/go-agent"
)

// defaultMaxTokens is used when a Request doesn't specify MaxTokens.
const defaultMaxTokens = 4096

// Client wraps a Google GenAI SDK client. Construct one with New or
// NewFromEnv; both return a *Client implementing agent.Provider,
// agent.StreamingProvider, and agent.Capable.
type Client struct {
	sdk *genai.Client
}

// Option configures the underlying genai.ClientConfig.
type Option func(*genai.ClientConfig)

// WithVertexAI switches the client to the Vertex AI backend, targeting the
// given GCP project and location, instead of the default Gemini API
// backend.
func WithVertexAI(project, location string) Option {
	return func(c *genai.ClientConfig) {
		c.Backend = genai.BackendVertexAI
		c.Project = project
		c.Location = location
	}
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *genai.ClientConfig) { c.HTTPClient = hc }
}

// New returns a Client authenticated with apiKey against the Gemini API
// backend. Use WithVertexAI to target Vertex AI instead (which
// authenticates via Application Default Credentials rather than an API
// key).
func New(ctx context.Context, apiKey string, opts ...Option) (*Client, error) {
	cfg := &genai.ClientConfig{APIKey: apiKey, Backend: genai.BackendGeminiAPI}
	for _, opt := range opts {
		opt(cfg)
	}
	sdk, err := genai.NewClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &Client{sdk: sdk}, nil
}

// NewFromEnv returns a Client that resolves credentials the same way the
// underlying SDK does natively: GOOGLE_API_KEY / GEMINI_API_KEY for the
// Gemini API backend, or Application Default Credentials plus
// GOOGLE_CLOUD_PROJECT / GOOGLE_CLOUD_LOCATION when GOOGLE_GENAI_USE_VERTEXAI
// selects the Vertex AI backend.
func NewFromEnv(ctx context.Context, opts ...Option) (*Client, error) {
	cfg := &genai.ClientConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	sdk, err := genai.NewClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &Client{sdk: sdk}, nil
}

func (c *Client) Name() string { return "gemini" }

// Generate implements agent.Provider.
func (c *Client) Generate(ctx context.Context, req *agent.Request) (*agent.Response, error) {
	contents, err := toContents(req.Messages)
	if err != nil {
		return nil, err
	}
	cfg, err := toConfig(req)
	if err != nil {
		return nil, err
	}
	resp, err := c.sdk.Models.GenerateContent(ctx, req.Model, contents, cfg)
	if err != nil {
		return nil, translateError(err)
	}
	return fromResponse(resp)
}

// Capabilities implements agent.Capable.
func (c *Client) Capabilities() agent.Capabilities {
	return agent.Capabilities{
		Streaming:             true,
		Tools:                 true,
		ParallelToolCalls:     true,
		Vision:                true,
		Documents:             true,
		Thinking:              true,
		SystemCaching:         false,
		MidConversationSystem: false,
		MaxContextTokens:      1_000_000,
		MaxOutputTokens:       8192,
	}
}

var (
	_ agent.Provider          = (*Client)(nil)
	_ agent.StreamingProvider = (*Client)(nil)
	_ agent.Capable           = (*Client)(nil)
)
