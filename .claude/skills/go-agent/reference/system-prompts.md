# System Prompts

```go
sp := agent.NewSystemPrompt().
    Add("You are a customer support agent for Acme Corp.").
    AddCacheable(knowledgeBaseDump).
    AddFunc(func(ctx context.Context) (string, error) {
        return "Current user: " + userFromContext(ctx).Name, nil
    }).
    AddTemplate("Today's date is {{.Date}}.", map[string]string{"Date": time.Now().Format("2006-01-02")})

agent.WithSystemPrompt(sp)
```

Methods, all chainable, evaluated in the order added:

- `Add(text)` — static section.
- `AddCacheable(text)` — static section hinted for prompt caching. Only
  Claude currently acts on this hint (`cache_control: {type: "ephemeral"}`
  on that content block); OpenAI and Gemini adapters ignore it (no-op, not
  an error).
- `AddFunc(func(ctx) (string, error))` — evaluated fresh on **every**
  `Run`/`RunStream` call, not cached — use for per-request dynamic content
  (current date, authenticated user, feature flags).
- `AddTemplate(tmplString, data)` — `text/template` convenience wrapper
  over `AddFunc`; the template is parsed once and cached, `data` is
  whatever the template needs.

## Ordering matters for prompt caching

Put static/cacheable sections **first**, dynamic sections **last**.
Caching is a prefix match: if a dynamic section (e.g. the current
timestamp) appears before a cacheable section, it invalidates the cache on
every single request regardless of the `AddCacheable` hint. `SystemPrompt`
preserves call order exactly — it doesn't reorder for you.

## Rendering

`SystemPrompt.Render(ctx)` runs on every `Run`/`RunStream`/`RunMessages`
call internally — application code doesn't need to call it directly except
in tests (a nil `*SystemPrompt` is safe and renders to `nil, nil`).
