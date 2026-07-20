// Command multiprovider shows that Agent code is identical regardless of
// which first-class provider backs it — only construction differs.
//
//	export ANTHROPIC_API_KEY=sk-ant-...
//	go run ./examples/multiprovider -provider claude -model claude-opus-4-8
//
//	export OPENAI_API_KEY=sk-...
//	go run ./examples/multiprovider -provider openai -model gpt-4.1
//
//	export GEMINI_API_KEY=...
//	go run ./examples/multiprovider -provider gemini -model gemini-2.5-pro
package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	agent "github.com/prasenjit-net/go-agent"
	"github.com/prasenjit-net/go-agent/provider/claude"
	"github.com/prasenjit-net/go-agent/provider/gemini"
	"github.com/prasenjit-net/go-agent/provider/openai"
)

func newProvider(ctx context.Context, name string) (agent.Provider, error) {
	switch name {
	case "claude":
		return claude.NewFromEnv(), nil
	case "openai":
		return openai.NewFromEnv(), nil
	case "gemini":
		return gemini.NewFromEnv(ctx)
	default:
		return nil, fmt.Errorf("unknown provider %q (want claude, openai, or gemini)", name)
	}
}

func main() {
	providerName := flag.String("provider", "claude", "provider to use: claude, openai, or gemini")
	model := flag.String("model", "claude-opus-4-8", "model ID for the selected provider")
	flag.Parse()

	ctx := context.Background()

	provider, err := newProvider(ctx, *providerName)
	if err != nil {
		log.Fatal(err)
	}

	// Everything below this line is provider-agnostic.
	a := agent.New(
		agent.WithProvider(provider),
		agent.WithModel(*model),
		agent.WithSystemPrompt(agent.NewSystemPrompt().Add("You are a concise, helpful assistant.")),
		agent.WithMaxTokens(1024),
	)

	result, err := a.Run(ctx, "In one sentence, what makes Go's concurrency model distinctive?")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("[%s/%s] %s\n", *providerName, *model, result.FinalResponse.Message.Text())
}
