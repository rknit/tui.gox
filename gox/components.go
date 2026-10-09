package gox

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
)

// ---------------------------------------------------------------------------
// Layout and text primitives

// BoxProps configure <box>, a flexbox-like container.
type BoxProps struct {
	// Direction is "column" (default) or "row".
	Direction string
	// Gap is the number of blank cells/lines between children.
	Gap int

	Padding, PaddingX, PaddingY                          int
	PaddingTop, PaddingRight, PaddingBottom, PaddingLeft int
	Margin, MarginX, MarginY                             int
	MarginTop, MarginRight, MarginBottom, MarginLeft     int

	// Width and Height are outer sizes including border and padding.
	// Zero means automatic.
	Width, Height int
	MinWidth      int
	MaxWidth      int
	// Grow distributes free space along the parent's direction.
	Grow int

	// Border is "", "single", "rounded", "double", "thick", "hidden",
	// "block" or "ascii".
	Border      string
	BorderColor string
	// Title is drawn into the top border.
	Title string

	// Align positions children horizontally: "left", "center", "right".
	Align string
	// Justify positions children vertically in a column with a fixed
	// height: "start", "center", "end".
	Justify string
	// AlignItems is "stretch" (default) or "start" for rows.
	AlignItems string

	// Color, Background and Bold are inherited by descendant text.
	Color, Background string
	Bold              bool

	Hidden   bool
	Children Node
}

// Box is the <box> container.
func Box(p BoxProps) Node { return &boxNode{props: p} }

// TextProps configure <text> and <span>.
type TextProps struct {
	Color, Background string
	Bold, Italic      bool
	Underline         bool
	Strikethrough     bool
	Faint, Reverse    bool
	Blink             bool
	// Wrap is "wrap" (default), "truncate" or "none".
	Wrap string
	// Align is "left", "center" or "right" within the text block.
	Align    string
	Children Node
}

// Text renders styled, wrapped text. Nested <text>/<span> elements are inline.
func Text(p TextProps) Node { return &textNode{props: p} }

// SpanProps configure <span>.
type SpanProps = TextProps

// Span is an alias of Text, conventionally used for inline styling.
func Span(p SpanProps) Node { return &textNode{props: p} }

// BrProps configure <br>.
type BrProps struct{}

// Br is a line break inside text.
func Br(BrProps) Node { return "\n" }

// SpacerProps configure <spacer>.
type SpacerProps struct{ Grow int }

// Spacer fills free space in its parent's direction.
func Spacer(p SpacerProps) Node {
	return &boxNode{props: BoxProps{Grow: max(p.Grow, 1)}}
}

// DividerProps configure <divider> and <hr>.
type DividerProps struct {
	Char  string
	Color string
	Title string
}

// Divider draws a horizontal rule across the available width.
func Divider(p DividerProps) Node { return &ruleNode{props: p} }

// HrProps configure <hr>.
type HrProps = DividerProps

// Hr is an alias of Divider.
func Hr(p HrProps) Node { return &ruleNode{props: p} }

// ShowProps configure <show>.
type ShowProps struct {
	When     bool
	Fallback Node
	Children Node
}

// Show renders its children when When holds, else Fallback.
func Show(p ShowProps) Node {
	if p.When {
		return p.Children
	}
	return p.Fallback
}

// ---------------------------------------------------------------------------
// Interactive components

// useControlled returns the effective value of a prop that can be either
// controlled (onChange set) or internal.
func useControlled[T any](value T, controlled bool, onChange func(T)) (T, func(T)) {
	internal, setInternal := UseState(value)
	if controlled {
		return value, onChange
	}
	return internal, func(v T) {
		setInternal(v)
		if onChange != nil {
			onChange(v)
		}
	}
}

// InputProps configure <input>, a single-line text field. It is controlled
// when OnChange is set, otherwise it keeps its own value seeded by Value.
type InputProps struct {
	Value       string
	OnChange    func(string)
	OnSubmit    func(string)
	Placeholder string
	Prompt      string
	// Width limits the visible width; the text scrolls with the cursor.
	Width int
	// Mask replaces every character, e.g. "*" for passwords.
	Mask      string
	CharLimit int
	AutoFocus bool
	Disabled  bool
	Color     string
	// FocusColor colors the prompt while focused.
	FocusColor string
}

// Input is the <input> component.
func Input(p InputProps) Node {
	value, setValue := useControlled(p.Value, p.OnChange != nil, p.OnChange)
	cursor, setCursor := UseState(len([]rune(p.Value)))
	runes := []rune(value)
	cursor = min(max(cursor, 0), len(runes))

	edit := func(r []rune, c int) {
		setValue(string(r))
		setCursor(c)
	}
	focus := UseFocus(FocusOptions{AutoFocus: p.AutoFocus, Disabled: p.Disabled, OnKey: func(k Key) bool {
		switch k.Type {
		case tea.KeyRunes, tea.KeySpace:
			ins := k.Runes
			if p.CharLimit > 0 && len(runes)+len(ins) > p.CharLimit {
				ins = ins[:max(p.CharLimit-len(runes), 0)]
			}
			out := append(append(append([]rune{}, runes[:cursor]...), ins...), runes[cursor:]...)
			edit(out, cursor+len(ins))
		case tea.KeyBackspace:
			if cursor > 0 {
				edit(append(append([]rune{}, runes[:cursor-1]...), runes[cursor:]...), cursor-1)
			}
		case tea.KeyDelete:
			if cursor < len(runes) {
				edit(append(append([]rune{}, runes[:cursor]...), runes[cursor+1:]...), cursor)
			}
		case tea.KeyLeft, tea.KeyCtrlB:
			setCursor(max(cursor-1, 0))
		case tea.KeyRight, tea.KeyCtrlF:
			setCursor(min(cursor+1, len(runes)))
		case tea.KeyHome, tea.KeyCtrlA:
			setCursor(0)
		case tea.KeyEnd, tea.KeyCtrlE:
			setCursor(len(runes))
		case tea.KeyCtrlU:
			edit(append([]rune{}, runes[cursor:]...), 0)
		case tea.KeyCtrlK:
			edit(append([]rune{}, runes[:cursor]...), cursor)
		case tea.KeyCtrlW:
			i := cursor
			for i > 0 && unicode.IsSpace(runes[i-1]) {
				i--
			}
			for i > 0 && !unicode.IsSpace(runes[i-1]) {
				i--
			}
			edit(append(append([]rune{}, runes[:i]...), runes[cursor:]...), i)
		case tea.KeyEnter:
			if p.OnSubmit != nil {
				p.OnSubmit(value)
			}
		default:
			return false
		}
		return true
	}})

	shown := runes
	if p.Mask != "" {
		shown = []rune(strings.Repeat(p.Mask, len(runes)))
	}
	start := 0
	if p.Width > 0 && cursor >= p.Width {
		start = cursor - p.Width + 1
	}
	end := len(shown)
	if p.Width > 0 {
		end = min(end, start+p.Width)
	}

	promptColor := p.Color
	if focus.Focused && p.FocusColor != "" {
		promptColor = p.FocusColor
	}
	var parts []Node
	if p.Prompt != "" {
		parts = append(parts, Span(TextProps{Color: promptColor, Children: p.Prompt}))
	}
	switch {
	case len(runes) == 0 && p.Placeholder != "":
		ph := []rune(p.Placeholder)
		if focus.Focused {
			parts = append(parts, Span(TextProps{Reverse: true, Children: string(ph[:1])}),
				Span(TextProps{Faint: true, Children: string(ph[1:])}))
		} else {
			parts = append(parts, Span(TextProps{Faint: true, Children: p.Placeholder}))
		}
	case focus.Focused:
		at := " "
		if cursor < end {
			at = string(shown[cursor])
		}
		parts = append(parts,
			Span(TextProps{Color: p.Color, Children: string(shown[start:cursor])}),
			Span(TextProps{Reverse: true, Children: at}))
		if cursor < end {
			parts = append(parts, Span(TextProps{Color: p.Color, Children: string(shown[cursor+1 : end])}))
		}
	default:
		parts = append(parts, Span(TextProps{Color: p.Color, Faint: p.Disabled, Children: string(shown[start:end])}))
	}
	return Text(TextProps{Wrap: "none", Children: Fragment(parts)})
}

// ButtonProps configure <button>.
type ButtonProps struct {
	OnPress    func()
	AutoFocus  bool
	Disabled   bool
	Color      string
	FocusColor string
	Children   Node
}

// Button is a focusable button activated with enter or space.
func Button(p ButtonProps) Node {
	focus := UseFocus(FocusOptions{AutoFocus: p.AutoFocus, Disabled: p.Disabled, OnKey: func(k Key) bool {
		switch k.String() {
		case "enter", " ":
			if p.OnPress != nil {
				p.OnPress()
			}
			return true
		}
		return false
	}})
	color := p.Color
	if focus.Focused && p.FocusColor != "" {
		color = p.FocusColor
	}
	return Text(TextProps{
		Color:    color,
		Reverse:  focus.Focused,
		Faint:    p.Disabled,
		Wrap:     "none",
		Children: F("[ ", p.Children, " ]"),
	})
}

// CheckboxProps configure <checkbox>. It is controlled when OnChange is set.
type CheckboxProps struct {
	Checked    bool
	OnChange   func(bool)
	AutoFocus  bool
	Disabled   bool
	FocusColor string
	Children   Node
}

// Checkbox is a focusable toggle.
func Checkbox(p CheckboxProps) Node {
	checked, set := useControlled(p.Checked, p.OnChange != nil, p.OnChange)
	focus := UseFocus(FocusOptions{AutoFocus: p.AutoFocus, Disabled: p.Disabled, OnKey: func(k Key) bool {
		switch k.String() {
		case "enter", " ":
			set(!checked)
			return true
		}
		return false
	}})
	mark := "[ ]"
	if checked {
		mark = "[x]"
	}
	color := ""
	if focus.Focused {
		color = p.FocusColor
		if color == "" {
			color = "12"
		}
	}
	return Text(TextProps{
		Faint:    p.Disabled,
		Children: F(Span(TextProps{Color: color, Bold: focus.Focused, Children: mark}), " ", p.Children),
	})
}

// SelectProps configure <select>, a vertical list with a highlighted item.
// The highlight is controlled when OnChange is set.
type SelectProps struct {
	Options []string
	// Index is the highlighted option.
	Index    int
	OnChange func(index int)
	// OnSelect is called with enter.
	OnSelect func(index int)
	// Height limits the visible rows; the list scrolls with the highlight.
	Height     int
	Indicator  string
	Color      string
	FocusColor string
	AutoFocus  bool
	Disabled   bool
}

// Select is a keyboard-navigable list (up/down, k/j, home/end, enter).
func Select(p SelectProps) Node {
	index, setIndex := useControlled(p.Index, p.OnChange != nil, p.OnChange)
	n := len(p.Options)
	index = min(max(index, 0), max(n-1, 0))
	focus := UseFocus(FocusOptions{AutoFocus: p.AutoFocus, Disabled: p.Disabled, OnKey: func(k Key) bool {
		if n == 0 {
			return false
		}
		switch k.String() {
		case "up", "k":
			setIndex((index - 1 + n) % n)
		case "down", "j":
			setIndex((index + 1) % n)
		case "home", "g":
			setIndex(0)
		case "end", "G":
			setIndex(n - 1)
		case "enter":
			if p.OnSelect != nil {
				p.OnSelect(index)
			}
		default:
			return false
		}
		return true
	}})
	ind := p.Indicator
	if ind == "" {
		ind = "❯"
	}
	pad := strings.Repeat(" ", len([]rune(ind)))
	hi := p.FocusColor
	if hi == "" {
		hi = "12"
	}
	start, end := 0, n
	if p.Height > 0 && n > p.Height {
		start = min(max(index-p.Height/2, 0), n-p.Height)
		end = start + p.Height
	}
	rows := make(Fragment, 0, end-start)
	for i := start; i < end; i++ {
		if i == index {
			color := p.Color
			if focus.Focused {
				color = hi
			}
			rows = append(rows, Text(TextProps{Color: color, Bold: focus.Focused, Wrap: "truncate", Children: ind + " " + p.Options[i]}))
		} else {
			rows = append(rows, Text(TextProps{Color: p.Color, Faint: p.Disabled, Wrap: "truncate", Children: pad + " " + p.Options[i]}))
		}
	}
	return Box(BoxProps{Children: rows})
}

// SpinnerProps configure <spinner>.
type SpinnerProps struct {
	// Frames defaults to a braille dots animation.
	Frames   []string
	Interval time.Duration
	Color    string
	Children Node
}

var spinnerDots = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Spinner is an animated activity indicator followed by its children.
func Spinner(p SpinnerProps) Node {
	frames := p.Frames
	if len(frames) == 0 {
		frames = spinnerDots
	}
	d := p.Interval
	if d == 0 {
		d = 80 * time.Millisecond
	}
	frame, setFrame := UseState(0)
	UseInterval(d, func() { setFrame((frame + 1) % len(frames)) })
	children := Children(p.Children)
	if len(children) == 0 {
		return Text(TextProps{Color: p.Color, Children: frames[frame%len(frames)]})
	}
	return Text(TextProps{Children: F(Span(TextProps{Color: p.Color, Children: frames[frame%len(frames)]}), " ", p.Children)})
}

// ProgressProps configure <progress>.
type ProgressProps struct {
	// Value is between 0 and 1.
	Value float64
	// Width of the bar in cells (default 30).
	Width       int
	Color       string
	TrackColor  string
	Full, Empty string
	ShowPercent bool
}

// Progress renders a horizontal progress bar.
func Progress(p ProgressProps) Node {
	w := p.Width
	if w <= 0 {
		w = 30
	}
	v := min(max(p.Value, 0), 1)
	full, empty := p.Full, p.Empty
	if full == "" {
		full = "█"
	}
	if empty == "" {
		empty = "░"
	}
	n := int(v*float64(w) + 0.5)
	parts := Fragment{
		Span(TextProps{Color: p.Color, Children: strings.Repeat(full, n)}),
		Span(TextProps{Color: p.TrackColor, Faint: p.TrackColor == "", Children: strings.Repeat(empty, w-n)}),
	}
	if p.ShowPercent {
		parts = append(parts, fmt.Sprintf(" %3.0f%%", v*100))
	}
	return Text(TextProps{Wrap: "none", Children: parts})
}

// ---------------------------------------------------------------------------
// Bubble Tea interop

// ModelProps configure <model>, which embeds an existing tea.Model (for
// example a component from charmbracelet/bubbles).
type ModelProps struct {
	// Model is the initial model; later values are ignored.
	Model tea.Model
	// OnChange receives the model after every update.
	OnChange func(tea.Model)
	// NoFocus removes the model from the focus order. Focused models
	// receive key messages; all other messages are always forwarded.
	NoFocus   bool
	AutoFocus bool
}

type modelRef struct{ m tea.Model }

// Model embeds a tea.Model: it calls Init on mount, forwards messages to
// Update and renders View.
func Model(p ModelProps) Node {
	app := UseApp()
	ref := UseRef(modelRef{m: p.Model})
	update := func(msg tea.Msg) {
		if ref.m == nil {
			return
		}
		m, cmd := ref.m.Update(msg)
		ref.m = m
		app.Cmd(cmd)
		app.Invalidate()
		if p.OnChange != nil {
			p.OnChange(m)
		}
	}
	UseEffect(func() func() {
		if ref.m != nil {
			app.Cmd(ref.m.Init())
		}
		return nil
	})
	UseFocus(FocusOptions{AutoFocus: p.AutoFocus, Disabled: p.NoFocus, OnKey: func(k Key) bool {
		switch k.String() {
		case "tab", "shift+tab":
			return false
		}
		update(k)
		return true
	}})
	UseMsg(func(msg tea.Msg) {
		if _, ok := msg.(Key); !ok {
			update(msg)
		}
	})
	if ref.m == nil {
		return nil
	}
	return Raw(ref.m.View())
}

// BubbleComponent is the shape of most charmbracelet/bubbles components,
// whose Update returns their own concrete type.
type BubbleComponent[M any] interface {
	Update(tea.Msg) (M, tea.Cmd)
	View() string
}

// BubbleModel adapts a BubbleComponent to tea.Model.
type BubbleModel[M BubbleComponent[M]] struct{ Inner M }

// Bubble wraps a bubbles-style component so it can be used with <model>.
func Bubble[M BubbleComponent[M]](m M) tea.Model { return BubbleModel[M]{Inner: m} }

// Unwrap returns the component inside a model created by Bubble.
func Unwrap[M BubbleComponent[M]](m tea.Model) M { return m.(BubbleModel[M]).Inner }

func (b BubbleModel[M]) Init() tea.Cmd { return nil }

func (b BubbleModel[M]) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m, cmd := b.Inner.Update(msg)
	return BubbleModel[M]{Inner: m}, cmd
}

func (b BubbleModel[M]) View() string { return b.Inner.View() }
