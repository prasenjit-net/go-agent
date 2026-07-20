// Command streaming shows Agent.RunStream: a single logical event stream
// for a whole run, including any tool round-trips.
//
//	export ANTHROPIC_API_KEY=sk-ant-...
//	go run ./examples/streaming
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"

	agent "github.com/prasenjit-net/go-agent"
	"github.com/prasenjit-net/go-agent/provider/claude"
)

func main() {
	ctx := context.Background()
	provider := claude.NewFromEnv()

	a := agent.New(
		agent.WithProvider(provider),
		agent.WithModel("claude-opus-4-8"),
		agent.WithMaxTokens(1024),
	)

	stream, err := a.RunStream(ctx, "Write a three-line haiku about Go generics.")
	if err != nil {
		log.Fatal(err)
	}
	defer stream.Close()

	for {
		event, err := stream.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			log.Fatal(err)
		}

		switch event.Type {
		case agent.EventTextDelta:
			fmt.Print(event.TextDelta)
		case agent.EventToolCallStart:
			fmt.Printf("\n[calling %s]\n", event.ToolCall.Name)
		case agent.EventRunDone:
			fmt.Printf("\n\n(used %d output tokens across %d turn(s))\n",
				event.Result.Usage.OutputTokens, event.Result.Iterations)
		}
	}
}
