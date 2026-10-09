package transpile

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/scanner"
	"go/token"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Format formats .gox source: Go code is formatted by gofmt and elements are
// re-indented with normalized spacing. Like gofmt, line breaks chosen by the
// author are kept, which also preserves the meaning of element text (JSX
// whitespace rules depend on line breaks). Format refuses to return output
// that would compile differently from the input.
func Format(src []byte) (out []byte, err error) {
	f := &fmtParser{src: src}
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(*Error); ok {
				out, err = nil, e
				return
			}
			if e, ok := r.(fmtErr); ok {
				out, err = nil, e.err
				return
			}
			panic(r)
		}
	}()
	frag := f.goCode(0, false)
	lines, err := renderGo(frag, true)
	if err != nil {
		return nil, err
	}
	out = []byte(strings.Join(lines, "\n"))
	if !bytes.HasSuffix(out, []byte("\n")) {
		out = append(out, '\n')
	}
	if err := sameProgram(src, out); err != nil {
		return nil, err
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Parsing into a layout tree. Go code is kept as text in which elements are
// replaced by placeholder identifiers.

type goFrag struct {
	code  string
	elems []*xElem
}

type xElem struct {
	name        string // "" for fragments
	fragment    bool
	selfClosing bool
	attrs       []xAttr
	attrsML     bool // attributes were written over several lines
	children    []xItem
}

type xAttr struct {
	name   string
	spread bool
	str    string // raw quoted string value
	expr   *goFrag
	elem   *xElem
}

type xItemKind int

const (
	itemText xItemKind = iota
	itemExpr
	itemElem
)

type xItem struct {
	kind xItemKind
	text string // raw text
	expr *goFrag
	elem *xElem
}

type fmtParser struct {
	src []byte
	end int
}

func (f *fmtParser) fail(pos int, format string, args ...any) {
	t := &transpiler{src: f.src, opts: Options{Filename: "input.gox"}}
	t.fail(pos, format, args...)
}

func placeholder(i int) string { return "_gox" + strconv.Itoa(i) + "_" }

var placeholderRE = regexp.MustCompile(`_gox(\d+)_`)

// goCode mirrors transpiler.goCode, producing Go text with placeholders.
func (f *fmtParser) goCode(pos int, inBrace bool) *goFrag {
	t := &transpiler{src: f.src, opts: Options{Filename: "input.gox"}}
	frag := &goFrag{}
	var out strings.Builder
	src := f.src
	depth := 0
	prev := tkNone
	start := pos
	for pos < len(src) {
		c := src[pos]
		switch {
		case c == '\n':
			out.WriteByte(c)
			pos++
			if prev == tkOperand {
				prev = tkNone
			}
		case c == ' ' || c == '\t' || c == '\r':
			out.WriteByte(c)
			pos++
		case c == '/' && pos+1 < len(src) && src[pos+1] == '/':
			end := bytes.IndexByte(src[pos:], '\n')
			if end < 0 {
				end = len(src) - pos
			}
			out.Write(src[pos : pos+end])
			pos += end
		case c == '/' && pos+1 < len(src) && src[pos+1] == '*':
			end := bytes.Index(src[pos+2:], []byte("*/"))
			if end < 0 {
				f.fail(pos, "unterminated comment")
			}
			out.Write(src[pos : pos+end+4])
			pos += end + 4
		case c == '"' || c == '\'' || c == '`':
			end := t.skipQuoted(pos)
			out.Write(src[pos:end])
			pos = end
			prev = tkOperand
		case c == '{' || c == '(' || c == '[':
			if c == '{' {
				depth++
			}
			out.WriteByte(c)
			pos++
			prev = tkOther
		case c == '}':
			if inBrace && depth == 0 {
				f.end = pos + 1
				frag.code = out.String()
				return frag
			}
			depth--
			out.WriteByte(c)
			pos++
			prev = tkOperand
		case c == ')' || c == ']':
			out.WriteByte(c)
			pos++
			prev = tkOperand
		case c == '<' && prev != tkOperand && t.startsElement(pos):
			e := f.element(pos)
			out.WriteString(placeholder(len(frag.elems)))
			frag.elems = append(frag.elems, e)
			pos = f.end
			prev = tkOperand
		case isIdentStart(src, pos):
			end := identEnd(src, pos)
			word := string(src[pos:end])
			out.WriteString(word)
			pos = end
			if goKeywords[word] && !operandKeywords[word] {
				prev = tkOther
			} else {
				prev = tkOperand
			}
		case c >= '0' && c <= '9' || (c == '.' && pos+1 < len(src) && src[pos+1] >= '0' && src[pos+1] <= '9'):
			end := pos
			for end < len(src) && (isAlnum(src[end]) || src[end] == '.' ||
				((src[end] == '+' || src[end] == '-') && end > pos && strings.ContainsRune("eEpP", rune(src[end-1])))) {
				end++
			}
			out.Write(src[pos:end])
			pos = end
			prev = tkOperand
		default:
			if (c == '+' || c == '-') && pos+1 < len(src) && src[pos+1] == c {
				out.Write(src[pos : pos+2])
				pos += 2
				prev = tkOperand
				continue
			}
			out.WriteByte(c)
			pos++
			if c == '<' {
				for pos < len(src) && (src[pos] == '<' || src[pos] == '=' || src[pos] == '-') {
					out.WriteByte(src[pos])
					pos++
				}
			}
			prev = tkOther
		}
	}
	if inBrace {
		f.fail(start, "unterminated expression: missing '}'")
	}
	f.end = pos
	frag.code = out.String()
	return frag
}

// tagSpace skips whitespace inside a tag, reporting whether it contained a
// newline. Comments inside tags are not supported by the formatter.
func (f *fmtParser) tagSpace(pos int) (int, bool) {
	nl := false
	for pos < len(f.src) {
		switch c := f.src[pos]; {
		case c == '\n':
			nl = true
			pos++
		case c == ' ' || c == '\t' || c == '\r':
			pos++
		case c == '/' && pos+1 < len(f.src) && (f.src[pos+1] == '/' || f.src[pos+1] == '*'):
			f.fail(pos, "cannot format comments inside tags")
		default:
			return pos, nl
		}
	}
	return pos, nl
}

func (f *fmtParser) element(pos int) *xElem {
	t := &transpiler{src: f.src, opts: Options{Filename: "input.gox"}}
	start := pos
	pos++
	if pos < len(f.src) && f.src[pos] == '>' {
		e := &xElem{fragment: true}
		e.children, f.end = f.children(pos+1, "", t)
		return e
	}
	name, pos := t.tagName(pos)
	e := &xElem{name: name}
	for {
		var nl bool
		pos, nl = f.tagSpace(pos)
		e.attrsML = e.attrsML || nl
		if pos >= len(f.src) {
			f.fail(start, "unterminated element <%s>", name)
		}
		c := f.src[pos]
		if c == '/' {
			if pos+1 >= len(f.src) || f.src[pos+1] != '>' {
				f.fail(pos, "expected '/>'")
			}
			pos += 2
			e.selfClosing = true
			break
		}
		if c == '>' {
			pos++
			break
		}
		if c == '{' {
			p, _ := f.tagSpace(pos + 1)
			if !bytes.HasPrefix(f.src[p:], []byte("...")) {
				f.fail(pos, "expected spread attribute {...expr}")
			}
			e.attrs = append(e.attrs, xAttr{spread: true, expr: f.goCode(p+3, true)})
			pos = f.end
			continue
		}
		if !isIdentStart(f.src, pos) {
			f.fail(pos, "unexpected %q in element <%s>", c, name)
		}
		aEnd := identEnd(f.src, pos)
		for aEnd < len(f.src) && f.src[aEnd] == '-' {
			aEnd = identEnd(f.src, aEnd+1)
		}
		a := xAttr{name: string(f.src[pos:aEnd])}
		pos, _ = f.tagSpace(aEnd)
		if pos < len(f.src) && f.src[pos] == '=' {
			pos, _ = f.tagSpace(pos + 1)
			if pos >= len(f.src) {
				f.fail(pos, "missing attribute value")
			}
			switch f.src[pos] {
			case '"', '\'':
				q := f.src[pos]
				end := bytes.IndexByte(f.src[pos+1:], q)
				if end < 0 {
					f.fail(pos, "unterminated attribute string")
				}
				a.str = string(f.src[pos : pos+end+2])
				pos += end + 2
			case '{':
				a.expr = f.goCode(pos+1, true)
				pos = f.end
			case '<':
				a.elem = f.element(pos)
				pos = f.end
			default:
				f.fail(pos, "attribute value must be a string, {expression} or element")
			}
		}
		e.attrs = append(e.attrs, a)
	}
	if !e.selfClosing {
		e.children, pos = f.children(pos, name, t)
	}
	f.end = pos
	return e
}

func (f *fmtParser) children(pos int, name string, t *transpiler) ([]xItem, int) {
	var out []xItem
	open := pos
	for {
		if pos >= len(f.src) {
			f.fail(open, "missing closing tag </%s>", name)
		}
		switch f.src[pos] {
		case '<':
			if pos+1 < len(f.src) && f.src[pos+1] == '/' {
				p, _ := f.tagSpace(pos + 2)
				closing := ""
				if p < len(f.src) && f.src[p] != '>' {
					closing, p = t.tagName(p)
				}
				p, _ = f.tagSpace(p)
				if p >= len(f.src) || f.src[p] != '>' {
					f.fail(pos, "expected '>'")
				}
				if cb, _ := splitTag(closing); closing != name && (cb != closing || cb != baseOf(name)) {
					f.fail(pos, "expected </%s>, got </%s>", name, closing)
				}
				return out, p + 1
			}
			out = append(out, xItem{kind: itemElem, elem: f.element(pos)})
			pos = f.end
		case '{':
			out = append(out, xItem{kind: itemExpr, expr: f.goCode(pos+1, true)})
			pos = f.end
		case '}':
			f.fail(pos, "unexpected '}' in element content")
		default:
			end := pos
			for end < len(f.src) && f.src[end] != '<' && f.src[end] != '{' && f.src[end] != '}' {
				end++
			}
			out = append(out, xItem{kind: itemText, text: string(f.src[pos:end])})
			pos = end
		}
	}
}

// ---------------------------------------------------------------------------
// Rendering. Renderers return lines whose indentation is relative to the
// first line; callers prefix their own indentation to the following lines.

var parenPlaceholderRE = regexp.MustCompile(`\(\s*(_gox\d+_)\s*\)`)

// renderGo formats Go text (a whole file, or an expression) and substitutes
// rendered elements for placeholders.
func renderGo(frag *goFrag, file bool) ([]string, error) {
	code := frag.code
	if !file && strings.TrimSpace(stripComments(code)) == "" {
		return []string{strings.TrimSpace(code)}, nil
	}
	// `(\n <x/>\n)` is layout only; let the element renderer decide.
	code = parenPlaceholderRE.ReplaceAllString(code, "($1)")

	var formatted string
	if file {
		b, err := format.Source([]byte(code))
		if err != nil {
			return nil, goError(err)
		}
		formatted = strings.TrimRight(string(b), "\n")
	} else {
		const prefix = "package p\n\nvar _ = "
		b, err := format.Source([]byte(prefix + code + "\n"))
		if err != nil {
			return nil, goError(err)
		}
		s := string(b)
		i := strings.Index(s, "var _ = ")
		if i < 0 {
			return nil, errors.New("goxfmt: cannot format expression")
		}
		formatted = strings.TrimRight(s[i+len("var _ = "):], "\n")
	}

	var out []string
	for _, line := range strings.Split(formatted, "\n") {
		out = append(out, substituteLine(line, frag)...)
	}
	return out, nil
}

func goError(err error) error {
	var list scanner.ErrorList
	if errors.As(err, &list) && len(list) > 0 {
		return fmt.Errorf("goxfmt: %s", list[0].Msg)
	}
	return fmt.Errorf("goxfmt: %v", err)
}

// substituteLine replaces placeholders in one formatted line, possibly producing
// several lines.
func substituteLine(line string, frag *goFrag) []string {
	m := placeholderRE.FindStringSubmatchIndex(line)
	if m == nil {
		return []string{line}
	}
	indent := leadingTabs(line)
	n, _ := strconv.Atoi(line[m[2]:m[3]])
	el := renderElem(frag.elems[n])
	before, after := line[:m[0]], line[m[1]:]

	var out []string
	if len(el) > 1 && strings.HasSuffix(before, "(") && strings.HasPrefix(after, ")") {
		// return (
		//     <box>
		//     </box>
		// )
		out = append(out, before)
		out = append(out, indentLines(el, indent+"\t")...)
		rest := substituteLine(indent+after, frag)
		return append(out, rest...)
	}
	out = append(out, before+el[0])
	out = append(out, indentLines(el[1:], indent)...)
	last := len(out) - 1
	rest := substituteLine(out[last]+after, frag)
	return append(out[:last], rest...)
}

func leadingTabs(s string) string {
	i := 0
	for i < len(s) && s[i] == '\t' {
		i++
	}
	return s[:i]
}

// appendLines appends b to the last line of a (continuing it).
func appendLines(a []string, b []string) []string {
	if len(a) == 0 {
		return append(a, b...)
	}
	a[len(a)-1] += b[0]
	return append(a, b[1:]...)
}

func indentLines(lines []string, prefix string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		if l == "" {
			out[i] = l
		} else {
			out[i] = prefix + l
		}
	}
	return out
}

func mustRenderGo(frag *goFrag) []string {
	lines, err := renderGo(frag, false)
	if err != nil {
		panic(fmtErr{err})
	}
	return lines
}

type fmtErr struct{ err error }

// braced renders {expr}, keeping the closing brace off a line comment.
func braced(prefix string, frag *goFrag) []string {
	lines := mustRenderGo(frag)
	out := appendLines([]string{"{" + prefix}, lines)
	if strings.Contains(out[len(out)-1], "//") {
		return append(out, "}")
	}
	out[len(out)-1] += "}"
	return out
}

func renderAttr(a xAttr) []string {
	switch {
	case a.spread:
		return braced("...", a.expr)
	case a.str != "":
		return []string{a.name + "=" + a.str}
	case a.expr != nil:
		return appendLines([]string{a.name + "="}, braced("", a.expr))
	case a.elem != nil:
		return appendLines([]string{a.name + "="}, renderElem(a.elem))
	}
	return []string{a.name}
}

func renderElem(e *xElem) []string {
	runs, blankBefore, multiline := layoutChildren(e.children)
	empty := len(runs) == 0

	// Opening tag.
	var open []string
	if e.fragment {
		open = []string{"<>"}
	} else if e.attrsML && len(e.attrs) > 0 {
		open = []string{"<" + e.name}
		for _, a := range e.attrs {
			open = append(open, indentLines(renderAttr(a), "\t")...)
		}
		if empty && !e.fragment {
			open = append(open, "/>")
			return open
		}
		open = append(open, ">")
	} else {
		open = []string{"<" + e.name}
		for _, a := range e.attrs {
			al := renderAttr(a)
			al[0] = " " + al[0]
			open = appendLines(open, al)
		}
		if empty {
			open[len(open)-1] += " />"
			return open
		}
		open[len(open)-1] += ">"
	}
	closeTag := "</" + baseOf(e.name) + ">"
	if e.fragment {
		closeTag = "</>"
		if empty {
			open[len(open)-1] += closeTag
			return open
		}
	}

	rendered := make([][]string, len(runs))
	for i, r := range runs {
		rendered[i] = renderRun(r)
		if len(rendered[i]) > 1 {
			multiline = true
		}
	}
	if !multiline && len(runs) == 1 && (len(open) == 1 || !e.attrsML) {
		open[len(open)-1] += rendered[0][0] + closeTag
		return open
	}
	out := open
	last := len(rendered) - 1
	for i, r := range rendered {
		// Whitespace at the very start or end of the content is significant
		// when it shares a line with the tag; keep such runs on that line.
		if i == 0 && leadingSpace(r[0]) != "" {
			out = appendLines(out, r)
			if i == last && trailingSpace(r[len(r)-1]) != "" {
				out[len(out)-1] += closeTag
				return out
			}
			continue
		}
		if blankBefore[i] {
			out = append(out, "")
		}
		out = append(out, indentLines(r, "\t")...)
		if i == last && trailingSpace(r[len(r)-1]) != "" {
			out[len(out)-1] += closeTag
			return out
		}
	}
	return append(out, closeTag)
}

func leadingSpace(s string) string  { return s[:len(s)-len(strings.TrimLeft(s, " \t"))] }
func trailingSpace(s string) string { return s[len(strings.TrimRight(s, " \t")):] }

type runAtom struct {
	item xItem
	text string // for text atoms
}

// layoutChildren splits children into runs: groups of items on the same
// line. Whitespace adjacent to a line break is insignificant (JSX rules) and
// dropped; whitespace inside a run is kept verbatim.
func layoutChildren(items []xItem) (runs [][]runAtom, blankBefore []bool, multiline bool) {
	var cur []runAtom
	breaks := 0
	pendingBlank := false
	flush := func() {
		// Trim text at run edges that touch a line break.
		if len(cur) > 0 {
			if len(runs) > 0 || breaks > 0 {
				if cur[0].item.kind == itemText {
					cur[0].text = strings.TrimLeft(cur[0].text, " \t\r")
				}
			}
		}
		var kept []runAtom
		for _, a := range cur {
			if a.item.kind == itemText && a.text == "" {
				continue
			}
			kept = append(kept, a)
		}
		if len(kept) > 0 {
			runs = append(runs, kept)
			blankBefore = append(blankBefore, pendingBlank && len(runs) > 1)
			pendingBlank = false
		}
		cur = nil
	}
	for _, it := range items {
		if it.kind != itemText {
			cur = append(cur, runAtom{item: it})
			continue
		}
		parts := strings.Split(it.text, "\n")
		for i, p := range parts {
			if i > 0 {
				multiline = true
				// Text before a break: trailing whitespace is insignificant.
				if n := len(cur); n > 0 && cur[n-1].item.kind == itemText {
					cur[n-1].text = strings.TrimRight(cur[n-1].text, " \t\r")
				}
				hadContent := false
				for _, a := range cur {
					if a.item.kind != itemText || strings.TrimSpace(a.text) != "" {
						hadContent = true
					}
				}
				flush()
				breaks++
				if !hadContent && i > 1 {
					pendingBlank = len(runs) > 0
				}
			}
			cur = append(cur, runAtom{item: xItem{kind: itemText}, text: p})
		}
	}
	flush()
	return runs, blankBefore, multiline
}

func renderRun(run []runAtom) []string {
	out := []string{""}
	for _, a := range run {
		switch a.item.kind {
		case itemText:
			out[len(out)-1] += a.text
		case itemExpr:
			out = appendLines(out, braced("", a.item.expr))
		case itemElem:
			out = appendLines(out, renderElem(a.item.elem))
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Safety net

// sameProgram checks that a and b transpile to the same Go tokens.
func sameProgram(a, b []byte) error {
	ga, err := Transpile(a, Options{})
	if err != nil {
		return err
	}
	gb, err := Transpile(b, Options{})
	if err != nil {
		return fmt.Errorf("goxfmt: formatted output does not compile: %v", err)
	}
	if !equalTokens(ga, gb) {
		return errors.New("goxfmt: formatting would change the program; please report this input")
	}
	return nil
}

type tok struct {
	t   token.Token
	lit string
}

func tokens(src []byte) []tok {
	var s scanner.Scanner
	fset := token.NewFileSet()
	s.Init(fset.AddFile("", -1, len(src)), src, nil, 0)
	var out []tok
	for {
		_, t, lit := s.Scan()
		if t == token.EOF {
			break
		}
		if t == token.SEMICOLON && lit == "\n" {
			continue
		}
		if t == token.STRING || t == token.CHAR {
			if v, err := strconv.Unquote(lit); err == nil {
				lit = strconv.Quote(v)
			}
		}
		out = append(out, tok{t, lit})
	}
	// Trailing commas are layout.
	var res []tok
	for i, x := range out {
		if x.t == token.COMMA && i+1 < len(out) && (out[i+1].t == token.RPAREN || out[i+1].t == token.RBRACE || out[i+1].t == token.RBRACK) {
			continue
		}
		res = append(res, x)
	}
	return res
}

// equalTokens reports whether Go sources a and b have the same tokens. Import
// declarations are compared as sets of imports since gofmt sorts them.
func equalTokens(a, b []byte) bool {
	ia, ra := splitImports(a)
	ib, rb := splitImports(b)
	if ia != ib {
		return false
	}
	ta, tb := tokens(ra), tokens(rb)
	if len(ta) != len(tb) {
		return false
	}
	for i := range ta {
		if ta[i] != tb[i] {
			return false
		}
	}
	return true
}

// splitImports returns the package name and sorted imports of Go source src
// as a key, and the source after the import declarations.
func splitImports(src []byte) (string, []byte) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ImportsOnly)
	if err != nil || f.Name == nil {
		return "", src
	}
	end := fset.Position(f.Name.End()).Offset
	var specs []string
	for _, d := range f.Decls {
		if g, ok := d.(*ast.GenDecl); ok && g.Tok == token.IMPORT {
			end = max(end, fset.Position(g.End()).Offset)
		}
	}
	for _, imp := range f.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		name := ""
		if imp.Name != nil {
			name = imp.Name.Name
		}
		specs = append(specs, name+" "+strconv.Quote(path))
	}
	sort.Strings(specs)
	return f.Name.Name + "\n" + strings.Join(specs, "\n"), src[end:]
}
