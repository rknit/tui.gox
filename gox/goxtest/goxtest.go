// Package goxtest drives a gox tree headlessly for tests.
package goxtest

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/rknit/tui.gox/gox"
)

// Driver renders a tree and feeds it input without a terminal.
type Driver struct {
	App  *gox.App
	Cmds []tea.Cmd
}

// New mounts root at the given terminal size.
func New(root gox.Node, width, height int, opts ...gox.Option) *Driver {
	app := gox.New(root, append([]gox.Option{gox.WithSize(width, height)}, opts...)...)
	d := &Driver{App: app}
	d.collect(app.Init())
	d.Send(tea.WindowSizeMsg{Width: width, Height: height})
	return d
}

func (d *Driver) collect(c tea.Cmd) {
	if c != nil {
		d.Cmds = append(d.Cmds, c)
	}
}

// Send delivers a message.
func (d *Driver) Send(msg tea.Msg) *Driver {
	_, c := d.App.Update(msg)
	d.collect(c)
	return d
}

// Press sends key presses by name: "enter", "tab", "shift+tab", "up",
// "ctrl+c", "space", or a single character.
func (d *Driver) Press(keys ...string) *Driver {
	for _, k := range keys {
		d.Send(Key(k))
	}
	return d
}

// Type sends each rune of s as a key press.
func (d *Driver) Type(s string) *Driver {
	for _, r := range s {
		if r == ' ' {
			d.Send(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
			continue
		}
		d.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return d
}

// Click sends a left click at screen cell (x, y).
func (d *Driver) Click(x, y int) *Driver {
	return d.Send(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
}

// Wheel sends n wheel notches at (x, y); negative n scrolls up.
func (d *Driver) Wheel(x, y, n int) *Driver {
	b := tea.MouseButtonWheelDown
	if n < 0 {
		b, n = tea.MouseButtonWheelUp, -n
	}
	for i := 0; i < n; i++ {
		d.Send(tea.MouseMsg{X: x, Y: y, Button: b, Action: tea.MouseActionPress})
	}
	return d
}

// Find returns the cell position of the first occurrence of s in the view,
// or (-1, -1).
func (d *Driver) Find(s string) (x, y int) {
	for y, line := range strings.Split(d.View(), "\n") {
		if i := strings.Index(line, s); i >= 0 {
			return ansi.StringWidth(line[:i]), y
		}
	}
	return -1, -1
}

// ClickText clicks the first cell of the first occurrence of s.
func (d *Driver) ClickText(s string) *Driver {
	x, y := d.Find(s)
	if x < 0 {
		panic("goxtest: text not found: " + s + "\n" + d.View())
	}
	return d.Click(x, y)
}

// Settle waits up to timeout for asynchronous updates (goroutines calling
// setters, intervals) and applies them.
func (d *Driver) Settle(timeout time.Duration) *Driver {
	time.Sleep(timeout)
	return d.Send(struct{}{})
}

// View returns the current frame with ANSI styling removed and trailing
// spaces trimmed.
func (d *Driver) View() string {
	lines := strings.Split(ansi.Strip(d.App.View()), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

// Close unmounts the tree.
func (d *Driver) Close() { d.App.Unmount() }

var keyNames = map[string]tea.KeyType{}

func init() {
	for t, name := range map[tea.KeyType]string{
		tea.KeyEnter: "enter", tea.KeyTab: "tab", tea.KeyShiftTab: "shift+tab",
		tea.KeyUp: "up", tea.KeyDown: "down", tea.KeyLeft: "left", tea.KeyRight: "right",
		tea.KeyBackspace: "backspace", tea.KeyDelete: "delete", tea.KeyEsc: "esc",
		tea.KeyHome: "home", tea.KeyEnd: "end", tea.KeySpace: "space",
		tea.KeyCtrlA: "ctrl+a", tea.KeyCtrlC: "ctrl+c", tea.KeyCtrlE: "ctrl+e",
		tea.KeyCtrlK: "ctrl+k", tea.KeyCtrlU: "ctrl+u", tea.KeyCtrlW: "ctrl+w",
		tea.KeyPgUp: "pgup", tea.KeyPgDown: "pgdown",
	} {
		keyNames[name] = t
	}
}

// Key builds a key message from a name or single character.
func Key(name string) tea.KeyMsg {
	if t, ok := keyNames[name]; ok {
		if t == tea.KeySpace {
			return tea.KeyMsg{Type: t, Runes: []rune{' '}}
		}
		return tea.KeyMsg{Type: t}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)}
}
