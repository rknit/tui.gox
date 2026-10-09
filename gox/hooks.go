package gox

import (
	"fmt"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// slot returns the hook state at the current position, creating it with
// init on first render.
func slot[T any](hook string, init func(r *renderer) *T) (*T, *renderer) {
	r := cur(hook)
	in := r.inst
	i := in.idx
	in.idx++
	if i < len(in.hooks) {
		h, ok := in.hooks[i].(*T)
		if !ok {
			panic(fmt.Sprintf("gox: hook order changed in %s (%s at position %d)", in.name, hook, i))
		}
		return h, r
	}
	h := init(r)
	in.hooks = append(in.hooks, h)
	return h, r
}

type stateHook[T any] struct {
	v   T
	set func(T)
}

// UseState returns a state value and a setter. The setter is stable across
// renders and safe to call from any goroutine; it schedules a re-render.
func UseState[T any](initial T) (T, func(T)) {
	h, _ := slot("UseState", func(r *renderer) *stateHook[T] {
		h := &stateHook[T]{v: initial}
		app := r.app
		h.set = func(v T) { app.enqueue(func() { h.v = v }) }
		return h
	})
	return h.v, h.set
}

type reducerHook[S, A any] struct {
	v        S
	reducer  func(S, A) S
	dispatch func(A)
}

// UseReducer manages state through a reducer. dispatch is stable and safe to
// call from any goroutine; actions are applied in order on the update loop.
func UseReducer[S, A any](reducer func(state S, action A) S, initial S) (S, func(A)) {
	h, _ := slot("UseReducer", func(r *renderer) *reducerHook[S, A] {
		h := &reducerHook[S, A]{v: initial}
		app := r.app
		h.dispatch = func(a A) { app.enqueue(func() { h.v = h.reducer(h.v, a) }) }
		return h
	})
	h.reducer = reducer
	return h.v, h.dispatch
}

// UseRef returns a pointer that persists across renders. Writing through it
// does not trigger a re-render.
func UseRef[T any](initial T) *T {
	h, _ := slot("UseRef", func(*renderer) *T { v := initial; return &v })
	return h
}

type memoHook[T any] struct {
	v    T
	deps []any
}

// UseMemo caches fn's result until deps change.
func UseMemo[T any](fn func() T, deps ...any) T {
	fresh := false
	h, _ := slot("UseMemo", func(*renderer) *memoHook[T] {
		fresh = true
		return &memoHook[T]{v: fn(), deps: deps}
	})
	if !fresh && !depsEqual(h.deps, deps) {
		h.v, h.deps = fn(), deps
	}
	return h.v
}

type effectHook struct {
	deps    []any
	ran     bool
	cleanup func()
	gone    bool
}

func (e *effectHook) unmount() {
	e.gone = true
	if e.cleanup != nil {
		e.cleanup()
		e.cleanup = nil
	}
}

// UseEffect runs fn after the frame is rendered, and again whenever deps
// change. With no deps it runs once, on mount. fn may return a cleanup
// function that runs before the next invocation and on unmount.
func UseEffect(fn func() func(), deps ...any) {
	h, r := slot("UseEffect", func(*renderer) *effectHook { return &effectHook{} })
	if h.ran && depsEqual(h.deps, deps) {
		return
	}
	h.ran, h.deps = true, deps
	r.app.effects = append(r.app.effects, func() {
		if h.gone {
			return
		}
		if h.cleanup != nil {
			h.cleanup()
		}
		h.cleanup = fn()
	})
}

// UseRenderEffect runs fn after every render, like a React effect without a
// dependency list. The cleanup returned by the previous run is called first.
func UseRenderEffect(fn func() func()) {
	h, r := slot("UseRenderEffect", func(*renderer) *effectHook { return &effectHook{} })
	r.app.effects = append(r.app.effects, func() {
		if h.gone {
			return
		}
		if h.cleanup != nil {
			h.cleanup()
		}
		h.cleanup = fn()
	})
}

// UseApp returns the App rendering the current component.
func UseApp() *App { return cur("UseApp").app }

// UseWindowSize returns the terminal width and height.
func UseWindowSize() (width, height int) { return cur("UseWindowSize").app.Size() }

// UseInput registers a keyboard handler that receives keys not consumed by
// the focused component. Return true to stop propagation. Handlers of
// components rendered later (deeper) run first.
func UseInput(fn func(k Key) bool) {
	r := cur("UseInput")
	r.inputs = append(r.inputs, fn)
}

// UseMsg registers a handler for every Bubble Tea message the app receives,
// including custom messages produced by commands.
func UseMsg(fn func(msg tea.Msg)) {
	r := cur("UseMsg")
	r.msgs = append(r.msgs, fn)
}

// FocusOptions configure UseFocus.
type FocusOptions struct {
	// AutoFocus focuses this component when nothing else is focused.
	AutoFocus bool
	// Disabled removes the component from the focus order.
	Disabled bool
	// OnKey receives keys while focused. Return true to consume the key.
	OnKey func(k Key) bool
}

// Focus reports and controls a component's focus.
type Focus struct {
	Focused bool
	id      string
	app     *App
}

// Focus moves focus to this component.
func (f Focus) Focus() {
	app, id := f.app, f.id
	app.enqueue(func() { app.setFocus(id) })
}

// ID returns the focus id of the component.
func (f Focus) ID() string { return f.id }

type focusHook struct{ id string }

// UseFocus makes the component focusable. Tab and shift+tab cycle focus in
// render order.
func UseFocus(opts FocusOptions) Focus {
	h, r := slot("UseFocus", func(r *renderer) *focusHook {
		return &focusHook{id: r.inst.path + "@" + strconv.Itoa(r.inst.idx-1)}
	})
	if !opts.Disabled {
		r.focusables = append(r.focusables, focusEntry{id: h.id, onKey: opts.OnKey, autoFocus: opts.AutoFocus})
	}
	return Focus{Focused: !opts.Disabled && r.app.focused == h.id, id: h.id, app: r.app}
}

type intervalHook struct {
	fn   func()
	stop chan struct{}
	d    time.Duration
}

func (h *intervalHook) unmount() { h.halt() }

func (h *intervalHook) halt() {
	if h.stop != nil {
		close(h.stop)
		h.stop = nil
	}
}

// UseInterval calls fn every d on the update loop while the component is
// mounted. A zero or negative d pauses the interval.
func UseInterval(d time.Duration, fn func()) {
	h, r := slot("UseInterval", func(*renderer) *intervalHook { return &intervalHook{} })
	h.fn = fn
	if h.d == d && (h.stop != nil || d <= 0) {
		return
	}
	h.halt()
	h.d = d
	if d <= 0 {
		return
	}
	stop := make(chan struct{})
	h.stop = stop
	app := r.app
	go func() {
		t := time.NewTicker(d)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				app.enqueueFresh(func() {
					select {
					case <-stop:
					default:
						h.fn()
					}
				})
			}
		}
	}()
}
