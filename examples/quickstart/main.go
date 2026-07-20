// Command quickstart is the smallest possible go-agent program: construct a
// provider, build an Agent, and run a single turn.
//
//	export ANTHROPIC_API_KEY=sk-ant-...
//	go run ./examples/quickstart
package main

import (
	"context"
	"fmt"
	"log"

	agent "github.com/prasenjit-net/go-agent"
	"github.com/prasenjit-net/go-agent/provider/claude"
)

func main() {
	ctx := context.Background()

	// NewFromEnv reads ANTHROPIC_API_KEY (or ANTHROPIC_AUTH_TOKEN) the same
	// way the underlying Anthropic SDK does.
	provider := claude.NewFromEnv()

	a := agent.New(
		agent.WithProvider(provider),
		agent.WithModel("claude-opus-4-8"),
		agent.WithSystemPrompt(agent.NewSystemPrompt().Add("You are a concise, helpful assistant.")),
		agent.WithMaxTokens(1024),
	)

	result, err := a.Run(ctx, "Explain the CAP theorem in two sentences.")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(result.FinalResponse.Message.Text())
}
