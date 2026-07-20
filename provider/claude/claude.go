// Package claude adapts the official Anthropic Go SDK
// (github.com/anthropics/anthropic-sdk-go) to the agent.Provider interface
// family, giving it first-class status alongside the openai and gemini
// adapters: Generate, Stream, CountTokens, and Capabilities are all
// implemented.
package claude

import (
	"context"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	agent "github.com/prasenjit-net/go-agent"
)

// defaultMaxTokens is used when a Request doesn't specify MaxTokens, since
// the Anthropic API requires a positive value.
const defaultMaxTokens = 4096

// Client wraps an Anthropic SDK client. Construct one with New or
// NewFromEnv; both return a *Client implementing agent.Provider,
// agent.StreamingProvider, agent.TokenCounter, and agent.Capable.
type Client struct {
	sdk anthropic.Client
}

// Option configures a Client.
type Option func(*options)

type options struct {
	requestOptions []option.RequestOption
}

// WithBaseURL overrides the API base URL — useful for a proxy or a
// self-hosted, wire-compatible endpoint.
func WithBaseURL(url string) Option {
	return func(o *options) { o.requestOptions = append(o.requestOptions, option.WithBaseURL(url)) }
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(c option.HTTPClient) Option {
	return func(o *options) { o.requestOptions = append(o.requestOptions, option.WithHTTPClient(c)) }
}

// WithBetaHeader adds an `anthropic-beta` header value (repeatable).
func WithBetaHeader(value string) Option {
	return func(o *options) {
		o.requestOptions = append(o.requestOptions, option.WithHeaderAdd("anthropic-beta", value))
	}
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
	return &Client{sdk: anthropic.NewClient(reqOpts...)}
}

// NewFromEnv returns a Client that resolves credentials the same way the
// underlying SDK does natively: the ANTHROPIC_API_KEY / ANTHROPIC_AUTH_TOKEN
// environment variables, or an `ant auth login` profile.
func NewFromEnv(opts ...Option) *Client {
	o := buildOptions(opts)
	return &Client{sdk: anthropic.NewClient(o.requestOptions...)}
}

func (c *Client) Name() string { return "claude" }

// Generate implements agent.Provider.
func (c *Client) Generate(ctx context.Context, req *agent.Request) (*agent.Response, error) {
	params, err := toMessageNewParams(req)
	if err != nil {
		return nil, err
	}
	msg, err := c.sdk.Messages.New(ctx, params)
	if err != nil {
		return nil, translateError(err)
	}
	return fromMessage(msg), nil
}

// CountTokens implements agent.TokenCounter.
func (c *Client) CountTokens(ctx context.Context, req *agent.Request) (int, error) {
	params, err := toMessageCountTokensParams(req)
	if err != nil {
		return 0, err
	}
	count, err := c.sdk.Messages.CountTokens(ctx, params)
	if err != nil {
		return 0, translateError(err)
	}
	return int(count.InputTokens), nil
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
		SystemCaching:         true,
		MidConversationSystem: false, // not yet implemented by this adapter; see SystemUpdater note in translate.go
		MaxContextTokens:      1_000_000,
		MaxOutputTokens:       128_000,
	}
}

var (
	_ agent.Provider          = (*Client)(nil)
	_ agent.StreamingProvider = (*Client)(nil)
	_ agent.TokenCounter      = (*Client)(nil)
	_ agent.Capable           = (*Client)(nil)
)
