// Package gox is a React-style component runtime for Bubble Tea. UIs are
// trees of Nodes produced by components; components hold state through
// hooks; the runtime renders the tree into a terminal frame and routes
// Bubble Tea messages to focused components and handlers.
//
// Trees are usually written in .gox files with XML syntax and compiled by
// goxc, but every construct is plain Go:
//
//	<box padding={1}><text bold>Hi</text></box>
//	gox.C(gox.Box, gox.BoxProps{Padding: 1, Children: gox.C(gox.Text, gox.TextProps{Bold: true, Children: "Hi"})})
package gox

import (
	"fmt"
	"reflect"
)

// Node is anything renderable: nil, bool (renders nothing), strings, numbers,
// fmt.Stringer, error, slices of renderable values, Fragment, elements
// created with C/C0 and host nodes.
type Node = any

// Fragment groups nodes without adding layout.
type Fragment []Node

// F creates a fragment from children.
func F(children ...Node) Node { return Fragment(children) }

// element is a lazily-rendered component invocation.
type element struct {
	id     uintptr
	name   string
	key    any
	hasKey bool
	render func() Node
}

// C creates an element for a component taking props.
func C[P any](fn func(P) Node, props P) Node {
	v := reflect.ValueOf(fn)
	return &element{
		id:     v.Pointer(),
		name:   funcName(v),
		render: func() Node { return fn(props) },
	}
}

// C0 creates an element for a component without props.
func C0(fn func() Node) Node {
	v := reflect.ValueOf(fn)
	return &element{id: v.Pointer(), name: funcName(v), render: fn}
}

// K assigns a reconciliation key to an element, keeping its state stable
// across reorders in lists.
func K(key any, n Node) Node {
	if e, ok := n.(*element); ok {
		c := *e
		c.key, c.hasKey = key, true
		return &c
	}
	return n
}

// If returns n when cond holds and nil otherwise.
func If(cond bool, n Node) Node {
	if cond {
		return n
	}
	return nil
}

// IfElse returns a when cond holds and b otherwise.
func IfElse(cond bool, a, b Node) Node {
	if cond {
		return a
	}
	return b
}

// Map renders each item of a slice.
func Map[T any](items []T, fn func(item T, index int) Node) Node {
	out := make(Fragment, len(items))
	for i, it := range items {
		out[i] = fn(it, i)
	}
	return out
}

// Children flattens a node into a list of non-empty child nodes.
func Children(n Node) []Node {
	var out []Node
	var walk func(Node)
	walk = func(n Node) {
		switch v := n.(type) {
		case nil, bool:
		case Fragment:
			for _, c := range v {
				walk(c)
			}
		case []Node:
			for _, c := range v {
				walk(c)
			}
		default:
			rv := reflect.ValueOf(n)
			if rv.Kind() == reflect.Slice && rv.Type().Elem().Kind() != reflect.Uint8 {
				for i := 0; i < rv.Len(); i++ {
					walk(rv.Index(i).Interface())
				}
				return
			}
			out = append(out, n)
		}
	}
	walk(n)
	return out
}

func funcName(v reflect.Value) string {
	if f := runtimePCName(v.Pointer()); f != "" {
		return f
	}
	return fmt.Sprintf("%v", v.Type())
}
