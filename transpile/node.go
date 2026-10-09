package transpile

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"strings"
)

// Component declarations with parameters as props:
//
//	node Card[T](title string, item T, children gox.Node) {
//
// becomes, on the same source line,
//
//	func Card[T any](gox_props_ CardProps[T]) gox.Node { return gox_Card[T](gox_props_.Title, gox_props_.Item, gox_props_.Children) }; func gox_Card[T any](title string, item T, children gox.Node) gox.Node {
//
// and, at the end of the file, with a //line directive pointing back at the
// parameters,
//
//	type CardProps[T any] struct { Title string; Item T; Children gox.Node }
//
// The parameter list is copied verbatim to the body func, so it keeps its
// lines, and parameters are never reported as unused.
//
// Type parameters without constraints default to any. A declaration without
// parameters becomes func Name() gox.Node.

const (
	nodeProps = "gox_props_"
	nodeBody  = "gox_" // prefix of the func holding the body of a node
)

// nodeDecl is the header of a node declaration.
type nodeDecl struct {
	start   int // offset of "node"
	name    string
	namePos int
	tparams int // offset of '[', or -1
	params  int // offset of '('
	rparen  int // offset of ')'
	lbrace  int // offset of the body's '{'
}

func (d *nodeDecl) typeParams(src []byte) string {
	if d.tparams < 0 {
		return ""
	}
	return string(src[d.tparams+1 : d.params-1])
}

// nodeDecl parses a node declaration header at pos (the word "node"). It
// returns nil when the text there does not have the shape `node Name(` or
// `node Name[`, so that node can still be used as an ordinary identifier.
func (t *transpiler) nodeDecl(pos int) *nodeDecl {
	src := t.src
	p := pos + len("node")
	if p >= len(src) || (src[p] != ' ' && src[p] != '\t') {
		return nil
	}
	p = skipBlank(src, p)
	if p >= len(src) || !isIdentStart(src, p) {
		return nil
	}
	d := &nodeDecl{start: pos, namePos: p, tparams: -1}
	p = identEnd(src, p)
	d.name = string(src[d.namePos:p])
	if goKeywords[d.name] {
		return nil
	}
	p = skipBlank(src, p)
	if p >= len(src) || (src[p] != '[' && src[p] != '(') {
		return nil
	}
	if src[p] == '[' {
		d.tparams = p
		p = skipBlank(src, t.matchClose(p, "type parameters")+1)
		if p >= len(src) || src[p] != '(' {
			t.fail(p, "expected '(' after type parameters of node %s", d.name)
		}
	}
	d.params = p
	d.rparen = t.matchClose(p, "parameters")
	p = skipBlank(src, d.rparen+1)
	switch {
	case p < len(src) && src[p] == '{':
		d.lbrace = p
	case p >= len(src) || src[p] == '\n' || src[p] == '\r' || src[p] == '/':
		t.fail(d.rparen, "expected '{' after parameters of node %s", d.name)
	default:
		t.fail(p, "node %s cannot declare results: it returns gox.Node", d.name)
	}
	return d
}

func skipBlank(src []byte, p int) int {
	for p < len(src) && (src[p] == ' ' || src[p] == '\t') {
		p++
	}
	return p
}

// matchClose returns the offset of the bracket closing the one at open.
func (t *transpiler) matchClose(open int, what string) int {
	src := t.src[open:]
	fset := token.NewFileSet()
	file := fset.AddFile("", -1, len(src))
	var s scanner.Scanner
	s.Init(file, src, nil, 0)
	depth := 0
	for {
		p, tok, _ := s.Scan()
		switch tok {
		case token.LPAREN, token.LBRACK, token.LBRACE:
			depth++
		case token.RPAREN, token.RBRACK, token.RBRACE:
			if depth--; depth == 0 {
				return open + file.Offset(p)
			}
		case token.EOF:
			t.fail(open, "unterminated %s", what)
		}
	}
}

type nodeParam struct {
	names   []string
	namePos []int
	typ     string
	typPos  int
}

// nodeParams parses the parameter list of d.
func (t *transpiler) nodeParams(d *nodeDecl) []nodeParam {
	const prefix = "package p;func _("
	text := string(t.src[d.params+1 : d.rparen])
	ft := t.parseFunc(prefix, text, "){}", d.params+1)
	var out []nodeParam
	for _, f := range ft.Params.List {
		at := func(p token.Pos) int { return int(p) - 1 - len(prefix) + d.params + 1 }
		if len(f.Names) == 0 {
			t.fail(at(f.Pos()), "node parameters must be named")
		}
		if _, ok := f.Type.(*ast.Ellipsis); ok {
			t.fail(at(f.Type.Pos()), "node parameters cannot be variadic")
		}
		var p nodeParam
		for _, n := range f.Names {
			if n.Name == "_" {
				t.fail(at(n.Pos()), "node parameters cannot be named _")
			}
			p.names = append(p.names, n.Name)
			p.namePos = append(p.namePos, at(n.Pos()))
		}
		p.typPos = at(f.Type.Pos())
		p.typ = string(t.src[p.typPos:at(f.Type.End())])
		out = append(out, p)
	}
	return out
}

// nodeTypeParams returns the type parameter names of d, and a function
// producing the type parameter list as Go source (with brackets) using mark.
func (t *transpiler) nodeTypeParams(d *nodeDecl) (func(mark func(pos, n int, s string) string) string, []string) {
	if d.tparams < 0 {
		return func(func(int, int, string) string) string { return "" }, nil
	}
	text := d.typeParams(t.src)
	constraint := ""
	if bareTypeParams(text) {
		constraint = " any"
	}
	const prefix = "package p;func _["
	ft := t.parseFunc(prefix, text+constraint, "](){}", d.tparams+1)
	var names []string
	for _, f := range ft.TypeParams.List {
		for _, n := range f.Names {
			names = append(names, n.Name)
		}
	}
	return func(mark func(pos, n int, s string) string) string {
		return "[" + mark(d.tparams+1, len(text), text) + constraint + "]"
	}, names
}

// bareTypeParams reports whether a type parameter list has no constraints,
// as in [T] or [K, V].
func bareTypeParams(text string) bool {
	for _, p := range splitTopLevel(text) {
		if p == "" || identEnd([]byte(p), 0) != len(p) || !isIdentStart([]byte(p), 0) {
			return false
		}
	}
	return true
}

// parseFunc parses prefix+text+suffix, a Go file holding a single func
// declaration, and returns its type. text starts at src offset base.
func (t *transpiler) parseFunc(prefix, text, suffix string, base int) *ast.FuncType {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", prefix+text+suffix, parser.SkipObjectResolution)
	if err != nil {
		var list scanner.ErrorList
		if errors.As(err, &list) && len(list) > 0 {
			off := min(max(list[0].Pos.Offset-len(prefix), 0), len(text))
			t.fail(base+off, "%s", list[0].Msg)
		}
		t.fail(base, "%v", err)
	}
	return f.Decls[0].(*ast.FuncDecl).Type
}

// nodeCode generates the Go code replacing the header of d, up to and
// including the body's '{', on as many lines as the header. The props type
// is declared at the end of the file (see t.trailer) so that a doc comment
// above the node stays on the function.
func (t *transpiler) nodeCode(d *nodeDecl) string {
	t.used = true
	params := t.nodeParams(d)
	tparamList, tnames := t.nodeTypeParams(d)
	tparams := tparamList(t.mark)
	name := t.mark(d.namePos, len(d.name), d.name)
	result := ") " + t.rt + ".Node {"
	if len(params) == 0 {
		startLine, _ := t.lineCol(d.start)
		endLine, _ := t.lineCol(d.lbrace)
		pad := strings.Repeat("\n", endLine-startLine)
		return t.mark(d.start, d.lbrace+1-d.start, "func "+name+tparams+"("+pad+result)
	}

	props := d.name + "Props"
	var b strings.Builder
	startLine, _ := t.lineCol(d.start)
	fmt.Fprintf(&b, "\n//line %s:%d\ntype %s%s struct {", baseName(t.opts.Filename), startLine, props, tparamList(t.markWeak))
	var args []string
	for _, p := range params {
		line, _ := t.lineCol(p.namePos[0])
		for startLine+strings.Count(b.String(), "\n")-2 < line {
			b.WriteByte('\n')
		}
		b.WriteString(" ")
		for i, n := range p.names {
			if i > 0 {
				b.WriteString(", ")
			}
			// Weak: a parameter name maps to the parameter of the body func.
			b.WriteString(t.markWeak(p.namePos[i], len(n), fieldName(n)))
			args = append(args, nodeProps+"."+fieldName(n))
		}
		b.WriteString(" " + t.markWeak(p.typPos, len(p.typ), p.typ) + ";")
	}
	b.WriteString(" }\n")
	t.trailer = append(t.trailer, t.markWeak(d.params, d.rparen+1-d.params, b.String()))

	targs := ""
	if len(tnames) > 0 {
		targs = "[" + strings.Join(tnames, ", ") + "]"
	}
	body := nodeBody + d.name
	// The parameter list, verbatim, with names and types marked so that they
	// win over the props fields when mapping source offsets.
	var list strings.Builder
	at := d.params + 1
	piece := func(pos, n int) {
		list.Write(t.src[at:pos])
		list.WriteString(t.mark(pos, n, string(t.src[pos:pos+n])))
		at = pos + n
	}
	for _, p := range params {
		for i, n := range p.names {
			piece(p.namePos[i], len(n))
		}
		piece(p.typPos, len(p.typ))
	}
	list.Write(t.src[at:d.rparen])
	var h strings.Builder
	h.WriteString("func " + name + tparams + "(" + nodeProps + " " + props + targs + result + " ")
	h.WriteString("return " + body + targs + "(" + strings.Join(args, ", ") + ") }; ")
	h.WriteString("func " + body + tparamList(func(_, _ int, s string) string { return s }) + "(")
	h.WriteString(t.mark(d.params+1, d.rparen-d.params-1, list.String()) + result)
	return t.mark(d.start, d.lbrace+1-d.start, h.String())
}
