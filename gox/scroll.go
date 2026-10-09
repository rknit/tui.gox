package gox

// ScrollProps configure <scroll>, a vertically scrollable viewport.
type ScrollProps struct {
	// Height of the viewport. Zero uses the height given by the parent
	// (e.g. grow inside a fixed-height column); without either, the
	// content is shown in full.
	Height int
	// FollowBottom keeps the view pinned to the end while new content
	// arrives, as long as the user hasn't scrolled up (logs, chats).
	FollowBottom bool
	// NoScrollbar hides the scrollbar column.
	NoScrollbar bool
	// WheelStep is the number of lines per mouse wheel notch (default 3).
	WheelStep  int
	AutoFocus  bool
	Disabled   bool
	ThumbColor string
	Children   Node
}

type scrollState struct {
	offset, max, view int
	atBottom          bool
}

type scrollNode struct {
	props   ScrollProps
	kids    []host
	state   *scrollState
	onMouse func(MouseEvent) bool
}

// Scroll is a viewport scrolled with up/down, k/j, pgup/pgdown, home/end
// while focused, or with the mouse wheel.
func Scroll(p ScrollProps) Node {
	app := UseApp()
	st := UseRef(scrollState{atBottom: true})
	by := func(d int) {
		st.offset = min(max(st.offset+d, 0), st.max)
		st.atBottom = st.offset >= st.max
		app.Invalidate()
	}
	focus := UseFocus(FocusOptions{AutoFocus: p.AutoFocus, Disabled: p.Disabled, OnKey: func(k Key) bool {
		switch k.String() {
		case "up", "k":
			by(-1)
		case "down", "j":
			by(1)
		case "pgup", "ctrl+u":
			by(-max(st.view-1, 1))
		case "pgdown", "ctrl+d", " ":
			by(max(st.view-1, 1))
		case "home", "g":
			by(-st.offset)
		case "end", "G":
			by(st.max)
		default:
			return false
		}
		return true
	}})
	step := p.WheelStep
	if step <= 0 {
		step = 3
	}
	return &scrollNode{props: p, state: st, onMouse: func(e MouseEvent) bool {
		if p.Disabled {
			return false
		}
		if d := e.Wheel(); d != 0 {
			by(d * step)
			return true
		}
		if e.Clicked() && !focus.Focused {
			focus.Focus()
			return true
		}
		return false
	}}
}

func layoutScroll(s *scrollNode, c constraint, st style) block {
	view := s.props.Height
	if view <= 0 {
		view = c.h
	}
	col := BoxProps{}
	content := layoutColumn(s.kids, col, c.w, c.stretch, -1, st)
	bar := !s.props.NoScrollbar && view > 0 && content.h() > view && c.w > 1
	if bar {
		content = layoutColumn(s.kids, col, c.w-1, c.stretch, -1, st)
	}
	if view <= 0 {
		view = content.h()
	}

	state := s.state
	maxOff := max(content.h()-view, 0)
	off := state.offset
	if s.props.FollowBottom && state.atBottom {
		off = maxOff
	}
	off = min(max(off, 0), maxOff)
	state.offset, state.max, state.view, state.atBottom = off, maxOff, view, off >= maxOff

	lines := content.lines[min(off, len(content.lines)):min(off+view, len(content.lines))]
	out := block{lines: append([]string(nil), lines...), w: content.w}
	out = out.withHeight(view, st)
	out.regions = append([]region{{0, 0, content.w, view, s.onMouse}}, clip(shift(content.regions, 0, -off), content.w, view)...)

	if bar {
		thumb := max(view*view/max(content.h(), 1), 1)
		pos := 0
		if maxOff > 0 {
			pos = (off*(view-thumb) + maxOff/2) / maxOff
		}
		ts := st
		ts.fg = s.props.ThumbColor
		track := st
		track.faint = true
		for i := range out.lines {
			if i >= pos && i < pos+thumb {
				out.lines[i] += ts.render("┃")
			} else {
				out.lines[i] += track.render("│")
			}
		}
		out.w++
		out.regions[0].w++
	}
	if c.stretch && out.w < c.w {
		for i, l := range out.lines {
			out.lines[i] = l + st.fill(c.w-out.w)
		}
		out.w = c.w
	}
	return out
}
