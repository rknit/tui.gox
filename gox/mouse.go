package gox

import tea "github.com/charmbracelet/bubbletea"

// MouseEvent is a mouse event delivered to an element's OnMouse handler.
// X and Y are relative to the element's top-left corner.
type MouseEvent struct {
	X, Y   int
	Button tea.MouseButton
	Action tea.MouseAction
	Alt    bool
	Ctrl   bool
	Shift  bool
}

// Clicked reports a left button press.
func (e MouseEvent) Clicked() bool {
	return e.Action == tea.MouseActionPress && e.Button == tea.MouseButtonLeft
}

// Wheel returns -1 for wheel up, 1 for wheel down and 0 otherwise.
func (e MouseEvent) Wheel() int {
	if e.Action != tea.MouseActionPress {
		return 0
	}
	switch e.Button {
	case tea.MouseButtonWheelUp:
		return -1
	case tea.MouseButtonWheelDown:
		return 1
	}
	return 0
}

// handleMouse delivers m to the innermost element under the pointer, then
// bubbles outwards until a handler returns true.
func (a *App) handleMouse(m tea.MouseMsg) {
	for i := len(a.regions) - 1; i >= 0; i-- {
		r := a.regions[i]
		if m.X < r.x || m.X >= r.x+r.w || m.Y < r.y || m.Y >= r.y+r.h {
			continue
		}
		ev := MouseEvent{X: m.X - r.x, Y: m.Y - r.y, Button: m.Button, Action: m.Action, Alt: m.Alt, Ctrl: m.Ctrl, Shift: m.Shift}
		if r.fn(ev) {
			return
		}
	}
}
