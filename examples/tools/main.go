// Command tools demonstrates strongly-typed tool registration: the model's
// tool input is unmarshalled directly into a plain Go struct — no
// map[string]any, no manual JSON Schema.
//
//	export ANTHROPIC_API_KEY=sk-ant-...
//	go run ./examples/tools
package main

import (
	"context"
	"fmt"
	"log"

	agent "github.com/prasenjit-net/go-agent"
	"github.com/prasenjit-net/go-agent/provider/claude"
)

// WeatherInput is the tool's input schema, described entirely by struct
// tags: `jsonschema:"required,description=..."` drives the JSON Schema the
// model sees, and `json:"..."` drives field naming — the same tags
// encoding/json already uses.
type WeatherInput struct {
	City  string `json:"city" jsonschema:"required,description=City name, e.g. Paris"`
	Units string `json:"units,omitempty" jsonschema:"enum=celsius;fahrenheit,description=Temperature unit"`
}

func getWeather(_ context.Context, in WeatherInput) (agent.ToolResult, error) {
	units := in.Units
	if units == "" {
		units = "celsius"
	}
	// A real implementation would call a weather API here.
	return agent.TextResult(fmt.Sprintf("It's 22°%s and sunny in %s.", unitSymbol(units), in.City)), nil
}

func unitSymbol(units string) string {
	if units == "fahrenheit" {
		return "F"
	}
	return "C"
}

func main() {
	ctx := context.Background()
	provider := claude.NewFromEnv()

	weatherTool := agent.NewTool(
		"get_weather",
		"Get the current weather for a city. Call this when the user asks about current conditions.",
		getWeather,
	)

	a := agent.New(
		agent.WithProvider(provider),
		agent.WithModel("claude-opus-4-8"),
		agent.WithTools(weatherTool),
		agent.WithMaxTokens(1024),
		agent.WithHooks(agent.Hooks{
			AfterToolCall: func(_ context.Context, call agent.ToolCall, _ agent.ToolResult) {
				fmt.Printf("[tool call] %s(%s)\n", call.Name, string(call.Input))
			},
		}),
	)

	result, err := a.Run(ctx, "What's the weather in Paris? Answer in Fahrenheit.")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(result.FinalResponse.Message.Text())
}
