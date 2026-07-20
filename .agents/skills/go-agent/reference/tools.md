# Tools

## Defining a tool

```go
type WeatherInput struct {
    City  string `json:"city" jsonschema:"required,description=City name, e.g. Paris"`
    Units string `json:"units,omitempty" jsonschema:"enum=celsius;fahrenheit,description=Temperature unit"`
}

var GetWeather = agent.NewTool(
    "get_weather",
    "Get the current weather for a city. Call this when the user asks about current conditions.",
    func(ctx context.Context, in WeatherInput) (agent.ToolResult, error) {
        return agent.TextResult(fmt.Sprintf("72°F and sunny in %s", in.City)), nil
    },
)
```

`agent.NewTool[TIn](name, description, handler)` — `TIn` is a plain struct.
Its JSON Schema is derived once via reflection from struct tags and cached;
the handler always receives a fully-typed, already-unmarshalled `TIn`.
There is no `map[string]any` anywhere in a correct tool implementation.

Write the `description` for the model to decide *when* to call the tool,
not just what it does — "Call this when the user asks about current
conditions" is more useful to the model than a bare restatement of the
function name.

## Struct tag reference

- `json:"name"` / `json:"name,omitempty"` — same tag `encoding/json`
  already uses. The field name becomes the schema property name;
  `omitempty` makes the field optional in the generated schema (a pointer
  field is also treated as optional even without `omitempty`).
- `jsonschema:"required"` — forces the field into the schema's required
  list even if it's a pointer or has `omitempty`.
- `jsonschema:"description=..."` — must be the **last** key in the tag if
  present; it consumes the rest of the tag verbatim, including any commas,
  so a description can itself contain commas (`description=City name, e.g.
  Paris` works correctly).
- `jsonschema:"enum=a;b;c"` — semicolon-separated (not comma — comma is the
  outer tag separator). Only applies to string fields.
- Nested structs, slices, and pointers are handled automatically: a nested
  struct becomes a nested `object` schema, a slice becomes an `array` with
  `items`, and a pointer is dereferenced for its element schema.

Both tags combine on one field: `` `json:"units,omitempty" jsonschema:"enum=celsius;fahrenheit,description=Temperature unit"` ``.

## Tool results

```go
agent.TextResult("72°F and sunny")                 // success, plain text
agent.JSONResult(map[string]any{"temp": 72})        // success, marshals v to a text block
agent.ErrorResultf("lookup failed: %v", err)        // model-recoverable error (IsError: true)
```

A `ToolResult{IsError: true}` from `ErrorResultf` is shown to the model,
which can react to it (retry differently, apologize, ask for
clarification) — this is different from a tool handler returning a Go
`error`, which the Agent run loop treats as **fatal to the run**. Use
`ErrorResultf` (with a `nil` Go error) for anything the model should be
able to see and route around; reserve a real `error` return for genuine
bugs that should abort.

## Registering tools

```go
agent.WithTools(GetWeather, SearchDocs)          // most common case: pass tools directly

tools := agent.NewToolSet(GetWeather, SearchDocs) // or build a ToolSet first
agent.WithTools(tools.List()...)
```

`agent.WithTools(...)` can be passed more than once — later calls append,
they don't replace. `ToolSet.Add` re-adding a name replaces that tool but
keeps its original position in `List()`.

## Tool choice

```go
agent.WithToolChoice(agent.ToolChoice{Mode: agent.ToolChoiceAuto})  // default: model decides
agent.WithToolChoice(agent.ToolChoice{Mode: agent.ToolChoiceAny})   // must use some tool
agent.WithToolChoice(agent.ToolChoice{Mode: agent.ToolChoiceOne, Name: "get_weather"}) // must use this one
agent.WithToolChoice(agent.ToolChoice{Mode: agent.ToolChoiceNone})  // disable tools for this call
```

## Bridging a non-struct-backed tool (e.g. MCP)

`agent.RegisteredTool` is the type-erased interface `Tool[TIn]` implements
(`Name() string`, `Description() string`, `Schema() *schema.Schema`,
`Invoke(ctx, json.RawMessage) (ToolResult, error)`). Anything satisfying
that interface can be passed to `agent.WithTools`, so a tool whose schema
is discovered at runtime (rather than known at compile time as a Go
struct) can be wrapped directly instead of going through `NewTool`. The
`github.com/prasenjit-net/go-agent/schema` package's `*schema.Schema` type
is exported for building such a schema by hand.
