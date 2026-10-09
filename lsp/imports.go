package lsp

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"

	"github.com/rknit/tui.gox/transpile"
)

// gopls edits to the import section of a generated file (auto-import
// completions, organize imports) cannot be mapped position by position: the
// generated header and the injected runtime import have no .gox counterpart,
// and gopls freely restructures the import block around them. Instead, the
// edits are applied to the generated code, the resulting change in imports is
// computed, and equivalent edits are made to the .gox import section.

type importSpec struct{ name, path string }

func (i importSpec) String() string {
	if i.name != "" {
		return i.name + " " + strconv.Quote(i.path)
	}
	return strconv.Quote(i.path)
}

type textEdit struct {
	start, end int
	text       string
}

// isTextEdits reports whether v is a non-empty list of LSP TextEdits.
func isTextEdits(v []any) bool {
	if len(v) == 0 {
		return false
	}
	for _, e := range v {
		m, ok := e.(map[string]any)
		if !ok {
			return false
		}
		if _, ok := m["newText"].(string); !ok {
			return false
		}
		if _, ok := m["range"].(map[string]any); !ok {
			return false
		}
	}
	return true
}

func editRange(t *text, m map[string]any) (int, int, bool) {
	r := m["range"].(map[string]any)
	s, ok1 := r["start"].(map[string]any)
	e, ok2 := r["end"].(map[string]any)
	if !ok1 || !ok2 || !isPosition(s) || !isPosition(e) {
		return 0, 0, false
	}
	a := t.offset(int(s["line"].(float64)), int(s["character"].(float64)))
	b := t.offset(int(e["line"].(float64)), int(e["character"].(float64)))
	return a, b, true
}

// importEdits translates TextEdits on a generated file that touch its import
// section. It returns ok=false when the edits do not touch imports and should
// be mapped normally.
func (w *walker) importEdits(edits []any, ctx string) ([]any, bool) {
	goxPath := transpile.SourceName(uriToPath(ctx))
	if goxPath == "" || !w.s.goxExists(goxPath) {
		return nil, false
	}
	src, gen, sm := w.s.mapping(goxPath)
	if sm == nil {
		return nil, false
	}
	end := importsEnd(gen.s)
	if end < 0 {
		return nil, false
	}
	var imp []textEdit
	var rest []any
	for _, e := range edits {
		m := e.(map[string]any)
		a, b, ok := editRange(gen, m)
		if !ok {
			return nil, false
		}
		if a <= end {
			imp = append(imp, textEdit{a, b, m["newText"].(string)})
		} else {
			rest = append(rest, e)
		}
	}
	if len(imp) == 0 {
		return nil, false
	}
	before := parseImports(gen.s)
	after := parseImports(applyEdits(gen.s, imp))
	added, removed := diffImports(before, after)
	out := make([]any, 0, len(edits))
	for _, e := range goxImportEdits(src.s, added, removed) {
		l1, c1 := src.position(e.start)
		l2, c2 := src.position(e.end)
		out = append(out, map[string]any{"range": rng(l1, c1, l2, c2), "newText": e.text})
	}
	for _, e := range rest {
		out = append(out, w.walk(e, ctx))
	}
	return out, true
}

// importsEnd returns the offset just past the import declarations of Go
// source s (or its package clause when it has none), or -1.
func importsEnd(s string) int {
	fset := token.NewFileSet()
	f, _ := parser.ParseFile(fset, "", s, parser.ImportsOnly)
	if f == nil || f.Name == nil {
		return -1
	}
	end := fset.Position(f.Name.End()).Offset
	for _, d := range f.Decls {
		if g, ok := d.(*ast.GenDecl); ok && g.Tok == token.IMPORT {
			end = max(end, fset.Position(g.End()).Offset)
		}
	}
	return end
}

func parseImports(s string) []importSpec {
	f, _ := parser.ParseFile(token.NewFileSet(), "", s, parser.ImportsOnly)
	if f == nil {
		return nil
	}
	var out []importSpec
	for _, imp := range f.Imports {
		out = append(out, specOf(imp))
	}
	return out
}

func specOf(imp *ast.ImportSpec) importSpec {
	p, _ := strconv.Unquote(imp.Path.Value)
	sp := importSpec{path: p}
	if imp.Name != nil {
		sp.name = imp.Name.Name
	}
	return sp
}

// applyEdits applies non-overlapping edits; edits at the same position are
// applied in order.
func applyEdits(s string, edits []textEdit) string {
	edits = append([]textEdit(nil), edits...)
	sort.SliceStable(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
	var b strings.Builder
	last := 0
	for _, e := range edits {
		if e.start < last || e.end < e.start || e.end > len(s) {
			continue
		}
		b.WriteString(s[last:e.start])
		b.WriteString(e.text)
		last = e.end
	}
	b.WriteString(s[last:])
	return b.String()
}

// diffImports returns the imports gained and lost from before to after,
// ignoring the runtime import goxc manages.
func diffImports(before, after []importSpec) (added, removed []importSpec) {
	has := func(list []importSpec, x importSpec) bool {
		for _, y := range list {
			if y == x {
				return true
			}
		}
		return false
	}
	for _, x := range after {
		if x.path != transpile.RuntimeImport && !has(before, x) && !has(added, x) {
			added = append(added, x)
		}
	}
	for _, x := range before {
		if x.path != transpile.RuntimeImport && !has(after, x) && !has(removed, x) {
			removed = append(removed, x)
		}
	}
	sort.Slice(added, func(i, j int) bool { return added[i].path < added[j].path })
	return added, removed
}

// goxImportEdits returns edits to the .gox source src adding and removing
// imports.
func goxImportEdits(src string, added, removed []importSpec) []textEdit {
	fset := token.NewFileSet()
	f, _ := parser.ParseFile(fset, "", src, parser.ImportsOnly|parser.ParseComments)
	if f == nil || f.Name == nil {
		return nil
	}
	off := func(p token.Pos) int { return fset.Position(p).Offset }
	lineStart := func(o int) int { return strings.LastIndexByte(src[:o], '\n') + 1 }
	lineEnd := func(o int) int { // past the newline
		if i := strings.IndexByte(src[o:], '\n'); i >= 0 {
			return o + i + 1
		}
		return len(src)
	}
	isRemoved := func(imp *ast.ImportSpec) bool {
		sp := specOf(imp)
		for _, r := range removed {
			if r == sp {
				return true
			}
		}
		return false
	}

	var edits []textEdit
	var groups, singles []*ast.GenDecl
	for _, d := range f.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.IMPORT {
			continue
		}
		var keep []ast.Spec
		for _, s := range g.Specs {
			if !isRemoved(s.(*ast.ImportSpec)) {
				keep = append(keep, s)
			}
		}
		switch {
		case len(keep) == 0:
			edits = append(edits, textEdit{lineStart(off(g.Pos())), lineEnd(off(g.End())), ""})
			continue
		case len(keep) < len(g.Specs):
			for _, s := range g.Specs {
				imp := s.(*ast.ImportSpec)
				if !isRemoved(imp) {
					continue
				}
				a := off(imp.Pos())
				if imp.Doc != nil {
					a = off(imp.Doc.Pos())
				}
				b := off(imp.End())
				if imp.Comment != nil {
					b = off(imp.Comment.End())
				}
				if strings.TrimSpace(src[lineStart(a):a]) == "" && strings.TrimSpace(src[b:lineEnd(b)]) == "" {
					a, b = lineStart(a), lineEnd(b)
				}
				edits = append(edits, textEdit{a, b, ""})
			}
		}
		if g.Lparen.IsValid() {
			groups = append(groups, g)
		} else {
			singles = append(singles, g)
		}
		g.Specs = keep
	}
	if len(added) == 0 {
		return edits
	}

	switch {
	case len(groups) > 0:
		// Insert into the last group, in path order.
		g := groups[len(groups)-1]
		for _, a := range added {
			at, indent := -1, "\t"
			for _, s := range g.Specs {
				imp := s.(*ast.ImportSpec)
				if specOf(imp).path > a.path {
					p := off(imp.Pos())
					if imp.Doc != nil {
						p = off(imp.Doc.Pos())
					}
					at = lineStart(p)
					indent = src[at:p]
					break
				}
			}
			if at >= 0 && strings.TrimSpace(indent) == "" {
				edits = append(edits, textEdit{at, at, indent + a.String() + "\n"})
				continue
			}
			rp := off(g.Rparen)
			if ls := lineStart(rp); strings.TrimSpace(src[ls:rp]) == "" {
				edits = append(edits, textEdit{ls, ls, "\t" + a.String() + "\n"})
			} else {
				edits = append(edits, textEdit{rp, rp, "\n\t" + a.String() + "\n"})
			}
		}
	case len(singles) == 1:
		// import "x" -> import ( "x"; "y" )
		g := singles[0]
		imp := g.Specs[0].(*ast.ImportSpec)
		cur := src[off(imp.Pos()):off(imp.End())]
		if imp.Comment != nil {
			cur += " " + src[off(imp.Comment.Pos()):off(imp.Comment.End())]
		}
		lines := []string{cur}
		for _, a := range added {
			lines = append(lines, a.String())
		}
		end := off(g.End())
		if imp.Comment != nil {
			end = max(end, off(imp.Comment.End()))
		}
		edits = append(edits, textEdit{off(g.Pos()), end, "import (\n\t" + strings.Join(lines, "\n\t") + "\n)"})
	case len(singles) > 1:
		at := lineEnd(off(singles[len(singles)-1].End()))
		var b strings.Builder
		for _, a := range added {
			b.WriteString("import " + a.String() + "\n")
		}
		edits = append(edits, textEdit{at, at, b.String()})
	default:
		at := off(f.Name.End())
		var text string
		if len(added) == 1 {
			text = "\n\nimport " + added[0].String()
		} else {
			lines := make([]string, len(added))
			for i, a := range added {
				lines[i] = a.String()
			}
			text = "\n\nimport (\n\t" + strings.Join(lines, "\n\t") + "\n)"
		}
		edits = append(edits, textEdit{at, at, text})
	}
	return edits
}
