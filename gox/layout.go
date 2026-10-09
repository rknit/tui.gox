package gox

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Host nodes are the primitives the layout engine understands. Components
// eventually expand into these.

type boxNode struct {
	props BoxProps
	kids  []host
}

type textNode struct {
	props TextProps
	kids  []host
}

type rawNode struct{ s string }

type ruleNode struct{ props DividerProps }

type host = any // string | *boxNode | *textNode | *rawNode | *ruleNode | *scrollNode

// Raw renders a preformatted (possibly ANSI-styled) string verbatim, without
// wrapping. Lines wider than the available space are truncated.
func Raw(s string) Node { return &rawNode{s: s} }

// style is the inherited text style.
type style struct {
	fg, bg                                 string
	bold, italic, underline, strike, faint bool
	reverse, blink                         bool
}

func (s style) merge(p TextProps) style {
	if p.Color != "" {
		s.fg = p.Color
	}
	if p.Background != "" {
		s.bg = p.Background
	}
	s.bold = s.bold || p.Bold
	s.italic = s.italic || p.Italic
	s.underline = s.underline || p.Underline
	s.strike = s.strike || p.Strikethrough
	s.faint = s.faint || p.Faint
	s.reverse = s.reverse || p.Reverse
	s.blink = s.blink || p.Blink
	return s
}

func (s style) lip() lipgloss.Style {
	ls := lipgloss.NewStyle()
	if s.fg != "" {
		ls = ls.Foreground(lipgloss.Color(s.fg))
	}
	if s.bg != "" {
		ls = ls.Background(lipgloss.Color(s.bg))
	}
	return ls.Bold(s.bold).Italic(s.italic).Underline(s.underline).
		Strikethrough(s.strike).Faint(s.faint).Reverse(s.reverse).Blink(s.blink)
}

func (s style) plain() bool { return s == style{} }

func (s style) render(str string) string {
	if s.plain() || str == "" {
		return str
	}
	return s.lip().Render(str)
}

// fill returns n spaces with the background of s.
func (s style) fill(n int) string {
	if n <= 0 {
		return ""
	}
	sp := strings.Repeat(" ", n)
	if s.bg == "" {
		return sp
	}
	return lipgloss.NewStyle().Background(lipgloss.Color(s.bg)).Render(sp)
}

// block is a rendered rectangle: every line has display width w. regions
// are mouse targets relative to the block's top-left corner, outermost first.
type block struct {
	lines   []string
	w       int
	regions []region
}

type region struct {
	x, y, w, h int
	fn         func(MouseEvent) bool
}

func shift(rs []region, dx, dy int) []region {
	out := make([]region, len(rs))
	for i, r := range rs {
		r.x += dx
		r.y += dy
		out[i] = r
	}
	return out
}

// clip intersects regions with the rectangle [0,w)x[0,h).
func clip(rs []region, w, h int) []region {
	var out []region
	for _, r := range rs {
		x0, y0 := max(r.x, 0), max(r.y, 0)
		x1, y1 := min(r.x+r.w, w), min(r.y+r.h, h)
		if x1 > x0 && y1 > y0 {
			out = append(out, region{x0, y0, x1 - x0, y1 - y0, r.fn})
		}
	}
	return out
}

// alignShift is the offset of a w-wide item aligned within total cells.
func alignShift(total, w int, align string) int {
	if w >= total {
		return 0
	}
	switch align {
	case "center":
		return (total - w) / 2
	case "right", "end":
		return total - w
	}
	return 0
}

func (b block) h() int { return len(b.lines) }

type constraint struct {
	w       int  // available outer width
	stretch bool // fill w instead of shrinking to content
	h       int  // forced outer height, 0 for auto
}

func newBlock(lines []string, w int, st style) block {
	if w < 0 {
		w = 0
		for _, l := range lines {
			w = max(w, ansi.StringWidth(l))
		}
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = fit(l, w, "left", st)
	}
	return block{lines: out, w: w}
}

// fit pads or truncates a line to exactly w cells.
func fit(line string, w int, align string, st style) string {
	lw := ansi.StringWidth(line)
	if lw > w {
		return ansi.Truncate(line, w, "")
	}
	gap := w - lw
	switch align {
	case "center":
		return st.fill(gap/2) + line + st.fill(gap-gap/2)
	case "right", "end":
		return st.fill(gap) + line
	}
	return line + st.fill(gap)
}

func (b block) withHeight(h int, st style) block {
	if h < 0 {
		return b
	}
	if len(b.lines) > h {
		b.lines = b.lines[:h]
		b.regions = clip(b.regions, b.w, h)
	}
	for len(b.lines) < h {
		b.lines = append(b.lines, st.fill(b.w))
	}
	return b
}

func layout(n host, c constraint, st style) block {
	switch v := n.(type) {
	case string:
		return layoutText(&textNode{kids: []host{v}}, c, st)
	case *textNode:
		return layoutText(v, c, st)
	case *boxNode:
		return layoutBox(v, c, st)
	case *rawNode:
		lines := strings.Split(strings.TrimRight(v.s, "\n"), "\n")
		w := -1
		if c.stretch {
			w = c.w
		} else {
			w = 0
			for _, l := range lines {
				w = max(w, ansi.StringWidth(l))
			}
			w = min(w, c.w)
		}
		b := newBlock(lines, w, st)
		if c.h > 0 {
			b = b.withHeight(c.h, st)
		}
		return b
	case *ruleNode:
		return layoutRule(v, c, st)
	case *scrollNode:
		return layoutScroll(v, c, st)
	}
	return block{}
}

// inline flattens a text node into a single styled string.
func inline(t *textNode, st style) string {
	st = st.merge(t.props)
	var b strings.Builder
	for _, k := range t.kids {
		switch v := k.(type) {
		case string:
			b.WriteString(st.render(v))
		case *textNode:
			b.WriteString(inline(v, st))
		default:
			blk := layout(v, constraint{w: 1 << 16}, st)
			b.WriteString(strings.Join(blk.lines, "\n"))
		}
	}
	return b.String()
}

func layoutText(t *textNode, c constraint, st style) block {
	s := inline(t, st)
	limit := max(c.w, 1)
	switch t.props.Wrap {
	case "truncate", "truncate-end":
		lines := strings.Split(s, "\n")
		for i, l := range lines {
			if ansi.StringWidth(l) > limit {
				lines[i] = ansi.Truncate(l, limit, "…")
			}
		}
		s = strings.Join(lines, "\n")
	case "none":
	default:
		s = ansi.Wrap(s, limit, "")
	}
	lines := strings.Split(s, "\n")
	natural := 0
	for _, l := range lines {
		natural = max(natural, ansi.StringWidth(l))
	}
	w := c.w
	if !c.stretch {
		w = min(natural, c.w)
	}
	inner := st.merge(t.props)
	for i, l := range lines {
		lines[i] = fit(l, w, t.props.Align, inner)
	}
	b := block{lines: lines, w: w}
	if t.props.OnMouse != nil {
		nw := min(natural, w)
		b.regions = []region{{alignShift(w, nw, t.props.Align), 0, nw, len(lines), t.props.OnMouse}}
	}
	if c.h > 0 {
		b = b.withHeight(c.h, st)
	}
	return b
}

func layoutRule(r *ruleNode, c constraint, st style) block {
	w := 0
	if c.stretch {
		w = c.w
	}
	ch := r.props.Char
	if ch == "" {
		ch = "─"
	}
	rs := st
	if r.props.Color != "" {
		rs.fg = r.props.Color
	}
	line := ""
	if w > 0 {
		cw := max(ansi.StringWidth(ch), 1)
		if r.props.Title != "" {
			title := " " + r.props.Title + " "
			tw := ansi.StringWidth(title)
			left := 2
			rest := w - tw - left
			if rest < 0 {
				line = ansi.Truncate(rs.render(strings.Repeat(ch, left/cw))+title, w, "")
			} else {
				line = rs.render(strings.Repeat(ch, left/cw)) + st.render(title) + rs.render(strings.Repeat(ch, rest/cw))
			}
		} else {
			line = rs.render(strings.Repeat(ch, w/cw))
		}
	}
	return newBlock([]string{line}, w, st)
}

func borderOf(name string) (lipgloss.Border, bool) {
	switch name {
	case "":
		return lipgloss.Border{}, false
	case "rounded", "round":
		return lipgloss.RoundedBorder(), true
	case "double":
		return lipgloss.DoubleBorder(), true
	case "thick", "bold":
		return lipgloss.ThickBorder(), true
	case "hidden":
		return lipgloss.HiddenBorder(), true
	case "block":
		return lipgloss.BlockBorder(), true
	case "ascii", "classic":
		return lipgloss.Border{Top: "-", Bottom: "-", Left: "|", Right: "|", TopLeft: "+", TopRight: "+", BottomLeft: "+", BottomRight: "+"}, true
	default:
		return lipgloss.NormalBorder(), true
	}
}

type edges struct{ t, r, b, l int }

func pick(specific, axis, all int) int {
	if specific != 0 {
		return specific
	}
	if axis != 0 {
		return axis
	}
	return all
}

func layoutBox(bn *boxNode, c constraint, parent style) block {
	p := bn.props
	if p.Hidden {
		return block{}
	}
	st := parent.merge(TextProps{Color: p.Color, Background: p.Background, Bold: p.Bold})
	m := edges{pick(p.MarginTop, p.MarginY, p.Margin), pick(p.MarginRight, p.MarginX, p.Margin), pick(p.MarginBottom, p.MarginY, p.Margin), pick(p.MarginLeft, p.MarginX, p.Margin)}
	pd := edges{pick(p.PaddingTop, p.PaddingY, p.Padding), pick(p.PaddingRight, p.PaddingX, p.Padding), pick(p.PaddingBottom, p.PaddingY, p.Padding), pick(p.PaddingLeft, p.PaddingX, p.Padding)}
	border, hasBorder := borderOf(p.Border)
	bw := 0
	if hasBorder {
		bw = 1
	}
	frameW := 2*bw + pd.l + pd.r
	frameH := 2*bw + pd.t + pd.b

	avail := max(c.w-m.l-m.r, 0)
	outerW := -1
	switch {
	case p.Width > 0:
		outerW = min(p.Width, avail)
	case c.stretch:
		outerW = avail
	}
	maxContent := avail - frameW
	if outerW >= 0 {
		maxContent = outerW - frameW
	}
	maxContent = max(maxContent, 0)
	if p.MaxWidth > 0 {
		maxContent = min(maxContent, max(p.MaxWidth-frameW, 0))
	}

	outerH := 0
	switch {
	case p.Height > 0:
		outerH = p.Height
	case c.h > 0:
		outerH = max(c.h-m.t-m.b, 0)
	}
	contentH := -1
	if outerH > 0 {
		contentH = max(outerH-frameH, 0)
	}

	var content block
	if p.Direction == "row" || p.Direction == "horizontal" {
		content = layoutRow(bn.kids, p, maxContent, outerW >= 0, contentH, st)
	} else {
		content = layoutColumn(bn.kids, p, maxContent, outerW >= 0, contentH, st)
	}
	if contentH >= 0 {
		content = content.withHeight(contentH, st)
	}
	cw := content.w
	if outerW >= 0 {
		cw = maxContent
	}
	if p.MinWidth > 0 {
		cw = max(cw, min(p.MinWidth-frameW, maxContent))
	}

	content.regions = clip(content.regions, cw, content.h())
	var regions []region
	if p.OnMouse != nil {
		regions = append(regions, region{m.l, m.t, pd.l + cw + pd.r + 2*bw, content.h() + pd.t + pd.b + 2*bw, p.OnMouse})
	}
	regions = append(regions, shift(content.regions, m.l+bw+pd.l+alignShift(cw, content.w, p.Align), m.t+bw+pd.t)...)

	// Padding.
	var lines []string
	padLine := st.fill(pd.l + cw + pd.r)
	for i := 0; i < pd.t; i++ {
		lines = append(lines, padLine)
	}
	for _, l := range content.lines {
		lines = append(lines, st.fill(pd.l)+fit(l, cw, p.Align, st)+st.fill(pd.r))
	}
	for i := 0; i < pd.b; i++ {
		lines = append(lines, padLine)
	}
	innerW := pd.l + cw + pd.r

	// Border.
	if hasBorder {
		bs := style{fg: p.BorderColor, bg: parent.bg}
		if p.BorderColor == "" {
			bs.fg = st.fg
		}
		top := strings.Repeat(border.Top, innerW)
		if p.Title != "" && innerW > 2 {
			title := ansi.Truncate(" "+p.Title+" ", innerW-2, "…")
			top = bs.render(border.TopLeft+border.Top) + st.render(title) + bs.render(strings.Repeat(border.Top, max(innerW-1-ansi.StringWidth(title), 0))+border.TopRight)
		} else {
			top = bs.render(border.TopLeft + top + border.TopRight)
		}
		out := []string{top}
		for _, l := range lines {
			out = append(out, bs.render(border.Left)+l+bs.render(border.Right))
		}
		out = append(out, bs.render(border.BottomLeft+strings.Repeat(border.Bottom, innerW)+border.BottomRight))
		lines = out
		innerW += 2
	}

	// Margin (no background).
	ms := style{bg: parent.bg}
	if m.l > 0 || m.r > 0 {
		for i, l := range lines {
			lines[i] = ms.fill(m.l) + l + ms.fill(m.r)
		}
	}
	w := innerW + m.l + m.r
	if m.t > 0 || m.b > 0 {
		blank := ms.fill(w)
		out := make([]string, 0, len(lines)+m.t+m.b)
		for i := 0; i < m.t; i++ {
			out = append(out, blank)
		}
		out = append(out, lines...)
		for i := 0; i < m.b; i++ {
			out = append(out, blank)
		}
		lines = out
	}
	return block{lines: lines, w: w, regions: regions}
}

func growOf(h host) int {
	switch v := h.(type) {
	case *boxNode:
		return v.props.Grow
	case *scrollNode:
		if v.props.Height == 0 {
			return 1 // fill the remaining height of a sized column
		}
	}
	return 0
}

func fixedWidth(h host) int {
	if b, ok := h.(*boxNode); ok && b.props.Width > 0 {
		m := pick(b.props.MarginLeft, b.props.MarginX, b.props.Margin) + pick(b.props.MarginRight, b.props.MarginX, b.props.Margin)
		return b.props.Width + m
	}
	return -1
}

func fixedHeight(h host) bool {
	b, ok := h.(*boxNode)
	return ok && b.props.Height > 0
}

func visible(kids []host) []host {
	out := kids[:0:0]
	for _, k := range kids {
		if b, ok := k.(*boxNode); ok && b.props.Hidden {
			continue
		}
		out = append(out, k)
	}
	return out
}

func layoutColumn(kids []host, p BoxProps, w int, fixed bool, h int, st style) block {
	kids = visible(kids)
	blocks := make([]block, len(kids))
	heights := make([]int, len(kids))

	// Distribute height among growing children when the height is known.
	if h >= 0 {
		used, total := p.Gap*max(len(kids)-1, 0), 0
		for i, k := range kids {
			if g := growOf(k); g > 0 {
				total += g
				continue
			}
			blocks[i] = layout(k, constraint{w: w, stretch: true}, st)
			used += blocks[i].h()
		}
		if total > 0 {
			rem, left := max(h-used, 0), max(h-used, 0)
			last := -1
			for i, k := range kids {
				if g := growOf(k); g > 0 {
					heights[i] = rem * g / total
					left -= heights[i]
					last = i
				}
			}
			if last >= 0 {
				heights[last] += left
			}
		}
	}

	render := func(i int, k host, cw int, stretch bool) block {
		return layout(k, constraint{w: cw, stretch: stretch, h: heights[i]}, st)
	}

	cw := w
	if !fixed {
		// Shrink to the widest child, then stretch children to that width.
		cw = 0
		for i, k := range kids {
			cw = max(cw, render(i, k, w, false).w)
		}
	}
	// Children stretch across the column unless aligned (like CSS
	// align-items), in which case they shrink to their content.
	stretch := p.Align == "" || p.Align == "left" || p.Align == "stretch"
	for i, k := range kids {
		blocks[i] = render(i, k, cw, stretch)
	}

	var lines []string
	var regions []region
	for i, b := range blocks {
		if i > 0 {
			for g := 0; g < p.Gap; g++ {
				lines = append(lines, st.fill(cw))
			}
		}
		regions = append(regions, shift(b.regions, alignShift(cw, b.w, p.Align), len(lines))...)
		for _, l := range b.lines {
			lines = append(lines, fit(l, cw, p.Align, st))
		}
	}
	if h >= 0 && len(lines) < h && p.Justify != "" {
		gap := h - len(lines)
		top := 0
		switch p.Justify {
		case "center":
			top = gap / 2
		case "end", "bottom":
			top = gap
		}
		pad := make([]string, top)
		for i := range pad {
			pad[i] = st.fill(cw)
		}
		lines = append(pad, lines...)
		regions = shift(regions, 0, top)
	}
	return block{lines: lines, w: cw, regions: regions}
}

func layoutRow(kids []host, p BoxProps, w int, fixed bool, h int, st style) block {
	kids = visible(kids)
	n := len(kids)
	widths := make([]int, n)
	blocks := make([]block, n)
	remaining := w - p.Gap*max(n-1, 0)
	totalGrow := 0
	for i, k := range kids {
		if fw := fixedWidth(k); fw >= 0 {
			widths[i] = min(fw, max(remaining, 0))
			remaining -= widths[i]
		}
	}
	for i, k := range kids {
		if fixedWidth(k) >= 0 {
			continue
		}
		if g := growOf(k); g > 0 && fixed {
			totalGrow += g
			continue
		}
		blocks[i] = layout(k, constraint{w: max(remaining, 0), h: max(h, 0)}, st)
		widths[i] = blocks[i].w
		remaining -= widths[i]
	}
	if totalGrow > 0 {
		left := max(remaining, 0)
		share := left
		last := -1
		for i, k := range kids {
			if g := growOf(k); g > 0 && fixedWidth(k) < 0 {
				widths[i] = share * g / totalGrow
				left -= widths[i]
				last = i
			}
		}
		if last >= 0 {
			widths[last] += left
		}
	}

	// First pass to find the row height, then stretch boxes to it.
	rowH := max(h, 0)
	for i, k := range kids {
		blocks[i] = layout(k, constraint{w: widths[i], stretch: true, h: max(h, 0)}, st)
		widths[i] = blocks[i].w
		if h < 0 {
			rowH = max(rowH, blocks[i].h())
		}
	}
	if h < 0 && p.AlignItems != "start" {
		for i, k := range kids {
			if _, ok := k.(*boxNode); ok && !fixedHeight(k) && blocks[i].h() < rowH {
				blocks[i] = layout(k, constraint{w: widths[i], stretch: true, h: rowH}, st)
			}
		}
	}

	total := p.Gap * max(n-1, 0)
	for _, b := range widths {
		total += b
	}
	var regions []region
	x := 0
	for i, b := range blocks {
		regions = append(regions, shift(b.regions, x, 0)...)
		x += widths[i] + p.Gap
	}
	lines := make([]string, rowH)
	for li := 0; li < rowH; li++ {
		var sb strings.Builder
		for i, b := range blocks {
			if i > 0 {
				sb.WriteString(st.fill(p.Gap))
			}
			if li < b.h() {
				sb.WriteString(b.lines[li])
			} else {
				sb.WriteString(st.fill(widths[i]))
			}
		}
		lines[li] = sb.String()
	}
	return block{lines: lines, w: total, regions: clip(regions, total, rowH)}
}
