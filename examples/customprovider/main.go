// Command customprovider shows the minimum needed to bring up a new
// inference backend: implement agent.Provider's single required method,
// Generate. No streaming, capability declaration, or token counting is
// required to get a working Agent — this is the whole adapter.
package main

import (
	"context"
	"fmt"
	"log"

	agent "github.com/prasenjit-net/go-agent"
)

// EchoProvider is the smallest possible agent.Provider: it just echoes the
// last user message back. Swap this for a real HTTP client against any
// inference backend and the rest of this file — the Agent, tools, hooks —
// needs no changes.
type EchoProvider struct{}

func (EchoProvider) Name() string { return "echo" }

func (EchoProvider) Generate(_ context.Context, req *agent.Request) (*agent.Response, error) {
	last := req.Messages[len(req.Messages)-1]
	return &agent.Response{
		Message:    agent.AssistantMessage(agent.TextBlock{Text: "echo: " + last.Text()}),
		StopReason: agent.StopEndTurn,
	}, nil
}

var _ agent.Provider = EchoProvider{}

func main() {
	a := agent.New(agent.WithProvider(EchoProvider{}))

	result, err := a.Run(context.Background(), "hello, custom provider")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(result.FinalResponse.Message.Text())
}
