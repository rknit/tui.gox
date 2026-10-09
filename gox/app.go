package gox

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
)

// Key is a key press message.
type Key = tea.KeyMsg

// App hosts a component tree. It implements tea.Model, so it can be run
// directly or embedded in another Bubble Tea model (forward every message to
// Update and render View).
type App struct {
	root Node
	opts options

	insts map[string]*instance
	frame uint64

	width, height int
	view          string

	focused    string
	focusables []focusEntry
	inputs     []func(Key) bool
	msgs       []func(tea.Msg)
	effects    []func()

	cmds []tea.Cmd // pending commands, owned by the update loop

	mu      sync.Mutex
	queue   []func()
	wake    chan struct{}
	dirty   bool
	started bool
}

type focusEntry struct {
	id        string
	onKey     func(Key) bool
	autoFocus bool
}

type options struct {
	altScreen  bool
	fullHeight bool
	noCtrlC    bool
	width      int
	height     int
	program    []tea.ProgramOption
}

// Option configures an App.
type Option func(*options)

// WithAltScreen runs the program in the alternate screen buffer and lets the
// root fill the terminal height.
func WithAltScreen() Option {
	return func(o *options) { o.altScreen, o.fullHeight = true, true }
}

// WithFullHeight makes the root box fill the terminal height.
func WithFullHeight() Option { return func(o *options) { o.fullHeight = true } }

// WithoutCtrlC disables quitting on ctrl+c.
func WithoutCtrlC() Option { return func(o *options) { o.noCtrlC = true } }

// WithSize sets the initial size used before the terminal reports its own.
func WithSize(w, h int) Option { return func(o *options) { o.width, o.height = w, h } }

// WithProgramOptions passes options to tea.NewProgram (used by Run).
func WithProgramOptions(opts ...tea.ProgramOption) Option {
	return func(o *options) { o.program = append(o.program, opts...) }
}

// New creates an App rendering root.
func New(root Node, opts ...Option) *App {
	a := &App{root: root, insts: map[string]*instance{}, wake: make(chan struct{}, 1)}
	a.opts.width, a.opts.height = 80, 24
	for _, o := range opts {
		o(&a.opts)
	}
	a.width, a.height = a.opts.width, a.opts.height
	return a
}

// Run renders root in a new Bubble Tea program and blocks until it exits.
func Run(root Node, opts ...Option) error {
	a := New(root, opts...)
	popts := a.opts.program
	if a.opts.altScreen {
		popts = append(popts, tea.WithAltScreen())
	}
	_, err := tea.NewProgram(a, popts...).Run()
	a.Unmount()
	return err
}

type wakeMsg struct{ app *App }

type sendMsg struct {
	app *App
	msg tea.Msg
}

func (a *App) listen() tea.Cmd {
	return func() tea.Msg {
		<-a.wake
		return wakeMsg{a}
	}
}

// Init implements tea.Model.
func (a *App) Init() tea.Cmd {
	a.started = true
	a.dirty = true
	a.flush()
	return tea.Batch(append(a.takeCmds(), a.listen())...)
}

// Update implements tea.Model.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if !a.started {
		a.started = true
		a.dirty = true
		a.cmds = append(a.cmds, a.listen())
	}
	switch m := msg.(type) {
	case wakeMsg:
		if m.app == a {
			a.cmds = append(a.cmds, a.listen())
		}
	case sendMsg:
		if m.app == a {
			a.dispatch(m.msg)
		}
	default:
		a.dispatch(msg)
	}
	a.flush()
	return a, tea.Batch(a.takeCmds()...)
}

// View implements tea.Model.
func (a *App) View() string { return a.view }

func (a *App) dispatch(msg tea.Msg) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = m.Width, m.Height
		a.dirty = true
	case Key:
		a.handleKey(m)
	}
	for _, h := range a.msgs {
		h(msg)
	}
}

func (a *App) handleKey(k Key) {
	if k.Type == tea.KeyCtrlC && !a.opts.noCtrlC {
		a.Quit()
		return
	}
	for _, f := range a.focusables {
		if f.id == a.focused && f.onKey != nil {
			if f.onKey(k) {
				return
			}
			break
		}
	}
	for i := len(a.inputs) - 1; i >= 0; i-- {
		if a.inputs[i](k) {
			return
		}
	}
	switch k.String() {
	case "tab":
		a.FocusNext()
	case "shift+tab":
		a.FocusPrev()
	}
}

func (a *App) takeCmds() []tea.Cmd {
	c := a.cmds
	a.cmds = nil
	return c
}

// enqueue schedules f to run on the update loop. Safe for concurrent use.
func (a *App) enqueue(f func()) {
	a.mu.Lock()
	a.queue = append(a.queue, f)
	a.mu.Unlock()
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

// flush applies queued updates and re-renders until the tree is stable.
func (a *App) flush() {
	for i := 0; i < 100; i++ {
		a.mu.Lock()
		q := a.queue
		a.queue = nil
		a.mu.Unlock()
		for _, f := range q {
			f()
		}
		if len(q) > 0 {
			a.dirty = true
		}
		if !a.dirty {
			return
		}
		a.dirty = false
		a.render()
		effects := a.effects
		a.effects = nil
		for _, e := range effects {
			e()
		}
	}
}

// --- public controls -------------------------------------------------------

// Quit stops the program.
func (a *App) Quit() { a.Cmd(tea.Quit) }

// Cmd schedules a Bubble Tea command. Safe for concurrent use.
func (a *App) Cmd(c tea.Cmd) {
	if c == nil {
		return
	}
	a.enqueue(func() { a.cmds = append(a.cmds, c) })
}

// Send delivers msg to the app's handlers as if it came from Bubble Tea.
// Safe for concurrent use.
func (a *App) Send(msg tea.Msg) {
	a.Cmd(func() tea.Msg { return sendMsg{a, msg} })
}

// Invalidate forces a re-render.
func (a *App) Invalidate() { a.enqueue(func() {}) }

// Size returns the terminal size.
func (a *App) Size() (int, int) { return a.width, a.height }

// FocusNext moves focus to the next focusable component.
func (a *App) FocusNext() { a.moveFocus(1) }

// FocusPrev moves focus to the previous focusable component.
func (a *App) FocusPrev() { a.moveFocus(-1) }

func (a *App) moveFocus(d int) {
	n := len(a.focusables)
	if n == 0 {
		return
	}
	cur := -1
	for i, f := range a.focusables {
		if f.id == a.focused {
			cur = i
		}
	}
	next := 0
	if cur >= 0 {
		next = ((cur+d)%n + n) % n
	} else if d < 0 {
		next = n - 1
	}
	a.setFocus(a.focusables[next].id)
}

func (a *App) setFocus(id string) {
	if a.focused != id {
		a.focused = id
		a.dirty = true
	}
}

// --- rendering -------------------------------------------------------------

// instance holds the state of a mounted component.
type instance struct {
	path  string
	name  string
	hooks []any
	idx   int
	frame uint64
}

type unmounter interface{ unmount() }

type renderer struct {
	app        *App
	inst       *instance
	focusables []focusEntry
	inputs     []func(Key) bool
	msgs       []func(tea.Msg)
}

var (
	renderMu sync.Mutex
	current  *renderer
)

func cur(hook string) *renderer {
	if current == nil || current.inst == nil {
		panic(fmt.Sprintf("gox: %s called outside of a component render", hook))
	}
	return current
}

func (a *App) render() {
	a.frame++
	r := &renderer{app: a}

	renderMu.Lock()
	prev := current
	current = r
	hosts := func() []host {
		defer func() { current = prev; renderMu.Unlock() }()
		return r.expandList(a.root, "")
	}()

	// Unmount components that were not rendered this frame.
	var gone []string
	for p, in := range a.insts {
		if in.frame != a.frame {
			gone = append(gone, p)
		}
	}
	sort.Strings(gone)
	for _, p := range gone {
		for _, h := range a.insts[p].hooks {
			if u, ok := h.(unmounter); ok {
				u.unmount()
			}
		}
		delete(a.insts, p)
	}

	prevIdx := -1
	for i, f := range a.focusables {
		if f.id == a.focused {
			prevIdx = i
		}
	}
	a.focusables, a.inputs, a.msgs = r.focusables, r.inputs, r.msgs
	found := false
	for _, f := range a.focusables {
		if f.id == a.focused {
			found = true
		}
	}
	if !found && prevIdx >= 0 && len(a.focusables) > 0 {
		// The focused component went away: focus its successor.
		a.focused = a.focusables[min(prevIdx, len(a.focusables)-1)].id
		a.dirty = true
	} else if !found {
		a.focused = ""
		for _, f := range a.focusables {
			if f.autoFocus {
				a.focused = f.id
				a.dirty = true // re-render so components see their focus
				break
			}
		}
	}

	c := constraint{w: a.width, stretch: true}
	if a.opts.fullHeight {
		c.h = a.height
	}
	root := &boxNode{kids: hosts}
	if a.opts.fullHeight {
		root.props.Height = a.height
	}
	a.view = strings.Join(layout(root, c, style{}).lines, "\n")
}

func (r *renderer) expandList(n Node, path string) []host {
	var out []host
	for i, c := range Children(n) {
		p := path + "/" + strconv.Itoa(i)
		if e, ok := c.(*element); ok && e.hasKey {
			p = path + "/k=" + fmt.Sprint(e.key)
		}
		out = append(out, r.expand(c, p)...)
	}
	return out
}

func (r *renderer) expand(n Node, path string) []host {
	switch v := n.(type) {
	case *element:
		id := path + ":" + strconv.FormatUint(uint64(v.id), 36)
		a := r.app
		in := a.insts[id]
		if in == nil {
			in = &instance{path: id, name: v.name}
			a.insts[id] = in
		}
		if in.frame == a.frame {
			panic(fmt.Sprintf("gox: duplicate key at %s (%s)", path, v.name))
		}
		in.frame = a.frame
		in.idx = 0
		parent := r.inst
		r.inst = in
		out := v.render()
		r.inst = parent
		return r.expandList(out, id)
	case *boxNode:
		c := *v
		c.kids = r.expandList(v.props.Children, path)
		return []host{&c}
	case *textNode:
		c := *v
		c.kids = r.expandList(v.props.Children, path)
		return []host{&c}
	case *rawNode, *ruleNode:
		return []host{v}
	case string:
		if v == "" {
			return nil
		}
		return []host{v}
	case fmt.Stringer:
		return []host{v.String()}
	case error:
		return []host{v.Error()}
	default:
		return []host{fmt.Sprint(v)}
	}
}

// RenderString renders a tree once at the given width and returns the frame.
// Effects run once; useful for tests and static output.
func RenderString(root Node, width int) string {
	a := New(root, WithSize(width, 0))
	a.dirty = true
	a.flush()
	a.Unmount()
	return a.view
}

// Unmount runs cleanups of every mounted component (effects, intervals).
func (a *App) Unmount() {
	for p, in := range a.insts {
		for _, h := range in.hooks {
			if u, ok := h.(unmounter); ok {
				u.unmount()
			}
		}
		delete(a.insts, p)
	}
}
