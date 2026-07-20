// Package openai adapts the official OpenAI Go SDK
// (github.com/openai/openai-go/v3) to the agent.Provider interface family,
// giving it first-class status alongside the claude and gemini adapters.
// It targets the Chat Completions API rather than the Responses API: the
// Agent run loop resends the full message history on every call (it never
// relies on server-side conversation state), which maps directly onto Chat
// Completions' stateless request shape.
package openai

import (
	"context"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"

	agent "github.com/prasenjit-net/go-agent"
)

// defaultMaxTokens is used when a Request doesn't specify MaxTokens.
const defaultMaxTokens = 4096

// Client wraps an OpenAI SDK client. Construct one with New or NewFromEnv;
// both return a *Client implementing agent.Provider, agent.StreamingProvider,
// and agent.Capable.
type Client struct {
	sdk openai.Client
}

// Option configures a Client.
type Option func(*options)

type options struct {
	requestOptions []option.RequestOption
}

// WithBaseURL overrides the API base URL — useful for Azure OpenAI, a
// proxy, or any OpenAI-compatible endpoint.
func WithBaseURL(url string) Option {
	return func(o *options) { o.requestOptions = append(o.requestOptions, option.WithBaseURL(url)) }
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(c option.HTTPClient) Option {
	return func(o *options) { o.requestOptions = append(o.requestOptions, option.WithHTTPClient(c)) }
}

// WithOrganization sets the OpenAI-Organization header.
func WithOrganization(id string) Option {
	return func(o *options) { o.requestOptions = append(o.requestOptions, option.WithOrganization(id)) }
}

// WithMaxRetries overrides the SDK's own low-level HTTP retry count
// (distinct from agent.RetryPolicy, which retries at the Agent level).
func WithMaxRetries(n int) Option {
	return func(o *options) { o.requestOptions = append(o.requestOptions, option.WithMaxRetries(n)) }
}

func buildOptions(opts []Option) options {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// New returns a Client authenticated with apiKey.
func New(apiKey string, opts ...Option) *Client {
	o := buildOptions(opts)
	reqOpts := append([]option.RequestOption{option.WithAPIKey(apiKey)}, o.requestOptions...)
	return &Client{sdk: openai.NewClient(reqOpts...)}
}

// NewFromEnv returns a Client that resolves credentials the same way the
// underlying SDK does natively (OPENAI_API_KEY, OPENAI_ORG_ID,
// OPENAI_PROJECT_ID, OPENAI_BASE_URL).
func NewFromEnv(opts ...Option) *Client {
	o := buildOptions(opts)
	return &Client{sdk: openai.NewClient(o.requestOptions...)}
}

func (c *Client) Name() string { return "openai" }

// Generate implements agent.Provider.
func (c *Client) Generate(ctx context.Context, req *agent.Request) (*agent.Response, error) {
	params, err := toParams(req)
	if err != nil {
		return nil, err
	}
	resp, err := c.sdk.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, translateError(err)
	}
	return fromChatCompletion(resp)
}

// Capabilities implements agent.Capable.
func (c *Client) Capabilities() agent.Capabilities {
	return agent.Capabilities{
		Streaming:             true,
		Tools:                 true,
		ParallelToolCalls:     true,
		Vision:                true,
		Documents:             true,
		Thinking:              true, // via reasoning_effort; see toReasoningEffort in translate.go
		SystemCaching:         false,
		MidConversationSystem: false,
		MaxContextTokens:      0, // varies by model; query the OpenAI models API if needed
		MaxOutputTokens:       0,
	}
}

var (
	_ agent.Provider          = (*Client)(nil)
	_ agent.StreamingProvider = (*Client)(nil)
	_ agent.Capable           = (*Client)(nil)
)
