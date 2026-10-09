package gox_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/rknit/tui.gox/gox"
	"github.com/rknit/tui.gox/gox/goxtest"
)

func box(p gox.BoxProps, children ...gox.Node) gox.Node {
	p.Children = gox.F(children...)
	return gox.C(gox.Box, p)
}

func text(s ...gox.Node) gox.Node {
	return gox.C(gox.Text, gox.TextProps{Children: gox.F(s...)})
}

func render(n gox.Node, w int) string {
	lines := strings.Split(ansi.Strip(gox.RenderString(n, w)), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	return strings.Join(lines, "\n")
}

func expect(t *testing.T, got, want string) {
	t.Helper()
	want = strings.TrimPrefix(want, "\n")
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestBorderPaddingTitle(t *testing.T) {
	got := render(box(gox.BoxProps{Border: "single", PaddingX: 1, Title: "T", Width: 12}, text("hi")), 40)
	expect(t, got, `
┌─ T ──────┐
│ hi       │
└──────────┘`)
}

func TestRowGrowAndGap(t *testing.T) {
	got := render(box(gox.BoxProps{Direction: "row", Gap: 1},
		text("L"),
		gox.C(gox.Spacer, gox.SpacerProps{}),
		box(gox.BoxProps{Width: 3}, text("R")),
	), 10)
	expect(t, got, "L      R")
}

func TestRowStretchesHeights(t *testing.T) {
	got := render(box(gox.BoxProps{Direction: "row"},
		box(gox.BoxProps{Border: "single"}, text("a"), text("b")),
		box(gox.BoxProps{Border: "single"}, text("c")),
	), 20)
	expect(t, got, `
┌─┐┌─┐
│a││c│
│b││ │
└─┘└─┘`)
}

func TestTextWrapAndAlign(t *testing.T) {
	got := render(box(gox.BoxProps{Width: 9, Align: "right"},
		gox.C(gox.Text, gox.TextProps{Children: "aaa bbb ccc"}),
		text("x"),
	), 40)
	expect(t, got, `
  aaa bbb
  ccc
        x`)
}

func TestColumnGrowFillsHeight(t *testing.T) {
	got := render(box(gox.BoxProps{Height: 4},
		text("top"),
		gox.C(gox.Spacer, gox.SpacerProps{}),
		text("bottom"),
	), 10)
	expect(t, got, "top\n\n\nbottom")
}

func TestNodeKinds(t *testing.T) {
	got := render(text("n=", 3, " ", true, nil, fmt.Errorf("e"), []string{"a", "b"}, 1.5), 40)
	expect(t, got, "n=3 eab1.5")
}

type counterProps struct{ Start int }

func counter(p counterProps) gox.Node {
	n, set := gox.UseState(p.Start)
	gox.UseFocus(gox.FocusOptions{OnKey: func(k gox.Key) bool {
		if k.String() == "+" {
			set(n + 1)
			return true
		}
		return false
	}})
	return text("n=", n)
}

func TestStateAndFocus(t *testing.T) {
	d := goxtest.New(box(gox.BoxProps{},
		gox.C(counter, counterProps{Start: 1}),
		gox.C(counter, counterProps{Start: 10}),
	), 20, 5)
	defer d.Close()
	d.Press("+") // nothing focused yet
	expect(t, d.View(), "n=1\nn=10")
	d.Press("tab", "+", "+", "tab", "+")
	expect(t, d.View(), "n=3\nn=11")
	d.Press("shift+tab", "+")
	expect(t, d.View(), "n=4\nn=11")
}

type listProps struct{ Order []string }

func keyedList(p listProps) gox.Node {
	return gox.Map(p.Order, func(k string, _ int) gox.Node {
		return gox.K(k, gox.C(item, itemProps{Name: k}))
	})
}

type itemProps struct{ Name string }

var mounts, unmounts int

func item(p itemProps) gox.Node {
	id := gox.UseRef(0)
	gox.UseEffect(func() func() {
		mounts++
		*id = mounts
		return func() { unmounts++ }
	})
	return text(p.Name, *id)
}

func TestKeysPreserveStateAndUnmount(t *testing.T) {
	mounts, unmounts = 0, 0
	var setOrder func([]string)
	root := gox.C0(func() gox.Node {
		order, set := gox.UseState([]string{"a", "b", "c"})
		setOrder = set
		return gox.C(keyedList, listProps{Order: order})
	})
	d := goxtest.New(root, 20, 5)
	expect(t, d.View(), "a1\nb2\nc3")
	setOrder([]string{"c", "a"})
	d.Send(struct{}{})
	expect(t, d.View(), "c3\na1")
	if mounts != 3 || unmounts != 1 {
		t.Errorf("mounts=%d unmounts=%d", mounts, unmounts)
	}
	d.Close()
	if unmounts != 3 {
		t.Errorf("unmounts after close=%d", unmounts)
	}
}

func TestEffectDepsAndAsyncSetter(t *testing.T) {
	runs := 0
	root := gox.C0(func() gox.Node {
		v, set := gox.UseState(0)
		other, setOther := gox.UseState(0)
		gox.UseEffect(func() func() {
			runs++
			if v < 3 {
				go set(v + 1) // from another goroutine
			}
			return nil
		}, v)
		gox.UseInput(func(gox.Key) bool { setOther(other + 1); return true })
		return text(v, " ", other)
	})
	d := goxtest.New(root, 20, 5)
	defer d.Close()
	for i := 0; i < 50 && d.View() != "3 0"; i++ {
		d.Settle(5 * time.Millisecond)
	}
	expect(t, d.View(), "3 0")
	d.Press("x")
	expect(t, d.View(), "3 1")
	if runs != 4 {
		t.Errorf("effect runs=%d, want 4", runs)
	}
}

func TestReducerMemo(t *testing.T) {
	computed := 0
	root := gox.C0(func() gox.Node {
		n, dispatch := gox.UseReducer(func(s int, a string) int {
			if a == "inc" {
				return s + 1
			}
			return s
		}, 0)
		parity := gox.UseMemo(func() string { computed++; return []string{"even", "odd"}[n%2] }, n%2)
		gox.UseInput(func(k gox.Key) bool { dispatch(k.String()); return true })
		return text(n, parity)
	})
	d := goxtest.New(root, 20, 5)
	defer d.Close()
	d.Press("inc", "noop", "inc", "inc")
	expect(t, d.View(), "3odd")
	if computed != 4 {
		t.Errorf("memo computed %d times, want 4", computed)
	}
}

func TestInput(t *testing.T) {
	var submitted string
	root := gox.C0(func() gox.Node {
		v, set := gox.UseState("")
		return box(gox.BoxProps{},
			gox.C(gox.Input, gox.InputProps{Value: v, OnChange: set, OnSubmit: func(s string) { submitted = s }, Prompt: "> ", AutoFocus: true}),
			text("value=", v),
		)
	})
	d := goxtest.New(root, 30, 5)
	defer d.Close()
	d.Type("hello world").Press("ctrl+w").Type("there").Press("left", "left", "backspace", "home", "delete", "enter")
	expect(t, d.View(), "> ello thre\nvalue=ello thre")
	if submitted != "ello thre" {
		t.Errorf("submitted %q", submitted)
	}
}

func TestSelectCheckboxButton(t *testing.T) {
	var picked = -1
	presses := 0
	root := box(gox.BoxProps{},
		gox.C(gox.Select, gox.SelectProps{Options: []string{"a", "b", "c"}, OnSelect: func(i int) { picked = i }, AutoFocus: true}),
		gox.C(gox.Checkbox, gox.CheckboxProps{Children: "check"}),
		gox.C(gox.Button, gox.ButtonProps{OnPress: func() { presses++ }, Children: "ok"}),
	)
	d := goxtest.New(root, 20, 6)
	defer d.Close()
	d.Press("down", "down", "up", "enter", "tab", "space", "tab", "enter", "space")
	expect(t, d.View(), "  a\n❯ b\n  c\n[x] check\n[ ok ]")
	if picked != 1 || presses != 2 {
		t.Errorf("picked=%d presses=%d", picked, presses)
	}
}

type echo struct{ got []string }

func (e echo) Init() tea.Cmd { return nil }
func (e echo) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		e.got = append(append([]string{}, e.got...), k.String())
	}
	return e, nil
}
func (e echo) View() string { return "keys:" + strings.Join(e.got, ",") }

func TestModelInterop(t *testing.T) {
	d := goxtest.New(gox.C(gox.Model, gox.ModelProps{Model: echo{}, AutoFocus: true}), 20, 3)
	defer d.Close()
	d.Press("a", "b", "tab", "c")
	expect(t, d.View(), "keys:a,b,c")
}

func TestCtrlCQuits(t *testing.T) {
	d := goxtest.New(text("x"), 10, 2)
	defer d.Close()
	d.Press("ctrl+c")
	quit := false
	for _, c := range d.Cmds {
		if msgIsQuit(c()) {
			quit = true
		}
	}
	if !quit {
		t.Error("ctrl+c did not produce tea.Quit")
	}
}

func msgIsQuit(m tea.Msg) bool {
	switch v := m.(type) {
	case tea.QuitMsg:
		return true
	case tea.BatchMsg:
		for _, c := range v {
			if c != nil {
				// Skip blocking listeners.
				done := make(chan tea.Msg, 1)
				go func() { done <- c() }()
				select {
				case r := <-done:
					if msgIsQuit(r) {
						return true
					}
				case <-time.After(10 * time.Millisecond):
				}
			}
		}
	}
	return false
}
