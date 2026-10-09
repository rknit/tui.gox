# tui.gox

TSX-style XML syntax and a React-style component runtime for
[Bubble Tea](https://github.com/charmbracelet/bubbletea).

```go
func Counter(p CounterProps) gox.Node {
	count, setCount := gox.UseState(0)
	return (
		<box direction="row" gap={2} border="rounded" paddingX={1} title={p.Label}>
			<text>Count: <span bold color="10">{count}</span></text>
			<spacer />
			<button onPress={func() { setCount(count + 1) }}>+1</button>
		</box>
	)
}

func main() { gox.Run(<Counter label="Clicks" />) }
```

There are three parts:

| Part | Path | Purpose |
|---|---|---|
| Compiler | `cmd/goxc`, `transpile` | Turns `foo.gox` into `foo_gox.go` |
| Runtime | `gox` | Components, hooks, focus, layout, Bubble Tea `tea.Model` |
| Test driver | `gox/goxtest` | Renders a tree and sends keys without a terminal |

## Getting started

```sh
go get github.com/rknit/tui.gox
go install github.com/rknit/tui.gox/cmd/goxc@latest
```

Write `.gox` files (ordinary Go plus XML expressions), then compile them:

```sh
goxc ./...          # writes foo_gox.go next to every foo.gox
goxc run .          # generate, then go run the package
goxc -check ./...   # fail if generated files are stale (CI)
```

Without installing: `go run github.com/rknit/tui.gox/cmd/goxc ./...`.

To use `go generate`, put the directive in a regular `.go` file, because `go generate` only
reads `.go` files and a package that contains only `.gox` files isn't a Go package yet:

```go
// gen.go
package main

//go:generate go run github.com/rknit/tui.gox/cmd/goxc .
```

Generated files keep the `.gox` line numbers and start with a `//line` directive, so compiler
errors point at the `.gox` source:

```
main.gox:8: cannot use "oops" (untyped string constant) as int value in struct literal
```

## Syntax

| `.gox` | Generated Go |
|---|---|
| `<box padding={1} />` | `gox.C(gox.Box, gox.BoxProps{Padding: 1})` |
| `<Card title="x">hi</Card>` | `gox.C(Card, CardProps{Title: "x", Children: "hi"})` |
| `<ui.Card />` | `gox.C(ui.Card, ui.CardProps{})` |
| `<App />` where `func App() gox.Node` | `gox.C0(App)` |
| `<Item key={id} />` | `gox.K(id, gox.C(Item, ItemProps{}))` |
| `<>a{b}</>` | `gox.F("a", b)` |

- **Lowercase tags** are built-in components from the `gox` package (`box` maps to `gox.Box`/`gox.BoxProps`).
- **Capitalized tags** are your own components. The props type is the declared parameter type
  when goxc finds the function in the same package. Otherwise it follows the `<Name>Props`
  convention. Pointer props (`func C(p *Opts)`) and zero-argument components are supported.
- **Attributes** become struct fields with the first letter capitalized (`onPress` → `OnPress`,
  `on-press` → `OnPress`). Values can be `"strings"`, `{expressions}` or `<elements>`. A bare
  attribute means `true`.
  Props are type-checked by the Go compiler.
- **Children** go into the props' `Children gox.Node` field. Text follows JSX whitespace rules,
  and HTML entities such as `&amp;` are decoded.
- **`{expr}`** children can be any renderable value: strings, numbers, `fmt.Stringer`, `error`,
  slices, nodes, or `nil`/`bool` for nothing. Expressions can contain more XML, for example:
  `{gox.Map(items, func(it Item, i int) gox.Node { return <Row key={it.ID} item={it}/> })}`.
- **Conditionals** use `gox.If(cond, node)`, `gox.IfElse(cond, a, b)` or
  `<show when={cond} fallback={...}>`.
- A JSX-style `return (\n <box>…</box>\n)` works, because goxc avoids Go's
  semicolon-insertion problem.
- The runtime import is added automatically. If you import the runtime under another alias,
  goxc uses that alias.

## Components

A component is a function `func(Props) gox.Node` (or `func() gox.Node`). Every Bubble Tea
concept can also be used directly from Go without the XML syntax.

### Hooks

| Hook | Notes |
|---|---|
| `UseState(init) (T, func(T))` | The setter is stable and safe to call from any goroutine. |
| `UseReducer(reducer, init) (S, func(A))` | Actions are applied in order. |
| `UseRef(init) *T` | Persistent value that doesn't cause a re-render. |
| `UseMemo(fn, deps...) T` | Recomputes `fn` when deps change. |
| `UseEffect(fn, deps...)` | Runs after render when deps change. With no deps it runs once on mount. `fn` may return a cleanup. |
| `UseFocus(FocusOptions) Focus` | Makes the component focusable. `OnKey` receives keys while it has focus. |
| `UseInput(func(Key) bool)` | Global key handler for keys the focused component didn't consume. |
| `UseMsg(func(tea.Msg))` | Receives every Bubble Tea message, including custom ones. |
| `UseInterval(d, fn)` | Ticker that runs `fn` on the update loop. |
| `UseWindowSize() (w, h)` | Terminal size. |
| `UseApp() *App` | `Quit()`, `Cmd(tea.Cmd)`, `Send(msg)`, `FocusNext()`, `FocusPrev()`, `Invalidate()`. |

Hooks follow React's rules: call them unconditionally, in the same order on every render. State
is keyed by tree position, plus `key` in lists.

Focus moves with `tab`/`shift+tab` in render order. `ctrl+c` quits unless you pass
`gox.WithoutCtrlC()`.

### Built-in elements

| Tag | Purpose / main props |
|---|---|
| `box` | Flexbox-like container. Props: `direction` (`column`/`row`), `gap`, `padding*`, `margin*`, `width`, `height`, `minWidth`, `maxWidth`, `grow`, `border` (`single`, `rounded`, `double`, `thick`, `hidden`, `ascii`), `borderColor`, `title`, `align`, `justify`, `color`, `background`, `bold`, `hidden` |
| `text`, `span` | Styled, wrapped text. Nested `span`s are inline. Props: `color`, `background`, `bold`, `italic`, `underline`, `strikethrough`, `faint`, `reverse`, `wrap` (`wrap`/`truncate`/`none`), `align` |
| `br`, `spacer`, `divider`/`hr` | Line break, flexible space, horizontal rule (`title`, `color`, `char`) |
| `show` | `when`, `fallback` |
| `input` | Single-line field with readline-style keys. Props: `value`, `onChange`, `onSubmit`, `placeholder`, `prompt`, `mask`, `width`, `charLimit` |
| `button` | `onPress`; activated with enter or space |
| `checkbox` | `checked`, `onChange` |
| `select` | `options`, `index`, `onChange`, `onSelect`, `height` (scrolls) |
| `spinner`, `progress` | Animated indicator; bar with `value` from 0 to 1, `width`, `showPercent` |
| `model` | Embeds any `tea.Model`. Bubbles components can be wrapped with `gox.Bubble(m)` and read back with `gox.Unwrap[T](m)` |

Interactive elements accept `autoFocus`, `disabled` and `focusColor`. They're controlled when
you pass `onChange`. Otherwise they keep their own state.

Colors are lipgloss colors (`"12"`, `"#ff8800"`). Box `color` and `background` are inherited by
the text inside the box.

### Interop with Bubble Tea

- **gox inside Bubble Tea:** `gox.New(root)` returns an `*App` that implements `tea.Model`.
  Forward messages to it and render its `View()`.
- **Bubble Tea inside gox:** use `<model model={m} onChange={...} />`.
- **Commands:** `gox.UseApp().Cmd(cmd)` runs a command. Its result message reaches
  `UseMsg` handlers.

## Testing

```go
d := goxtest.New(<App />, 80, 24)
d.Type("buy milk").Press("enter", "tab", "space")
if !strings.Contains(d.View(), "[x] buy milk") { t.Fatal(d.View()) }
```

`gox.RenderString(node, width)` renders a tree once, which is useful for static output and
snapshot tests.

## Examples

```sh
go run ./examples/counter    # state, buttons, focus
go run ./examples/todo       # reducer, input, keyed lists, checkbox, show
go run ./examples/showcase   # layout, select, async effects, spinner/progress, bubbles textarea
```

The generated `*_gox.go` files are committed, so `go build ./...` works without running goxc.
