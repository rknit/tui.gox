package lsp

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rknit/tui.gox/transpile"
)

// syntheticName prefixes the scratch files goxls opens in gopls to compute
// attribute completions. They exist only as overlays.
const syntheticName = "goxls_completion_"

const (
	kindField   = 5
	kindClass   = 7
	kindKeyword = 14
)

type completionKind int

const (
	noCompletion completionKind = iota
	tagCompletion
	attrCompletion
)

type completionCtx struct {
	kind    completionKind
	tag     string
	prefix  string
	present map[string]bool
}

// complete answers completion requests that gopls cannot: tag names after
// '<' and attribute names inside an opening tag. ok=false defers to gopls.
func (s *server) complete(d *doc, params any) (any, bool) {
	p, _ := params.(map[string]any)
	pos, _ := p["position"].(map[string]any)
	line, _ := pos["line"].(float64)
	char, _ := pos["character"].(float64)
	s.mu.Lock()
	src := d.src.s
	off := d.src.offset(int(line), int(char))
	s.mu.Unlock()

	ctx := completionContext(src, off)
	switch ctx.kind {
	case tagCompletion:
		return s.completeTags(d), true
	case attrCompletion:
		return s.completeAttrs(d, src, ctx), true
	}
	return nil, false
}

var exprKeywords = map[string]bool{"return": true, "case": true, "go": true, "defer": true, "else": true}

func isWordByte(c byte) bool {
	return c == '_' || c == '.' || c == '-' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

func completionContext(s string, off int) completionCtx {
	ws := off
	for ws > 0 && isWordByte(s[ws-1]) {
		ws--
	}
	prefix := s[ws:off]

	// Tag name: "<" directly before the word, in element position.
	if ws > 0 && s[ws-1] == '<' && !strings.Contains(prefix, "-") {
		i := ws - 2
		for i >= 0 && (s[i] == ' ' || s[i] == '\t') {
			i--
		}
		element := i < 0 || !(isWordByte(s[i]) || s[i] == ')' || s[i] == ']') || s[i] == '-'
		if !element && isWordByte(s[i]) {
			// A keyword such as return or case starts an expression.
			k := i
			for k >= 0 && isWordByte(s[k]) {
				k--
			}
			element = exprKeywords[s[k+1:i+1]]
		}
		if element {
			return completionCtx{kind: tagCompletion, prefix: prefix}
		}
		return completionCtx{}
	}

	// Attribute name: whitespace before the word, inside an open tag.
	if ws == 0 || !unicode.IsSpace(rune(s[ws-1])) {
		return completionCtx{}
	}
	lo := max(ws-4000, 0)
	for i := ws - 1; i >= lo; i-- {
		if s[i] != '<' || i+1 >= len(s) {
			continue
		}
		r, _ := utf8.DecodeRuneInString(s[i+1:])
		if r != '_' && !unicode.IsLetter(r) {
			continue
		}
		j := i + 1
		for j < len(s) && (isWordByte(s[j]) || s[j] == '[' || s[j] == ']') {
			j++
		}
		tag := s[i+1 : j]
		if present, open := scanAttrs(s[j:ws]); open {
			return completionCtx{kind: attrCompletion, tag: tag, prefix: prefix, present: present}
		}
	}
	return completionCtx{}
}

// scanAttrs scans the attribute area of an opening tag and reports whether
// it is still open at the end, along with the attribute names seen.
func scanAttrs(s string) (map[string]bool, bool) {
	present := map[string]bool{}
	depth := 0
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote && (depth == 0 || s[i-1] != '\\') {
				quote = 0
			}
		case c == '"' || c == '\'' || c == '`':
			quote = c
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth < 0 {
				return nil, false
			}
		case depth == 0 && (c == '>' || c == '<'):
			return nil, false
		case depth == 0 && isWordByte(c) && (i == 0 || unicode.IsSpace(rune(s[i-1]))):
			j := i
			for j < len(s) && isWordByte(s[j]) {
				j++
			}
			present[s[i:j]] = true
			i = j - 1
		}
	}
	return present, depth == 0 && quote == 0
}

func (s *server) packageFor(path string) *transpile.Package {
	dir := filepath.Dir(path)
	s.mu.Lock()
	overlay := map[string][]byte{}
	for p, d := range s.docs {
		if filepath.Dir(p) == dir {
			overlay[p] = []byte(d.src.s)
		}
	}
	s.mu.Unlock()
	pkg, err := transpile.LoadPackage(dir, overlay)
	if err != nil {
		return &transpile.Package{Sigs: map[string]transpile.Signature{}}
	}
	return pkg
}

func (s *server) completeTags(d *doc) any {
	var items []any
	var names []string
	for n := range transpile.Intrinsics {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		items = append(items, map[string]any{"label": n, "kind": kindKeyword, "detail": "gox built-in"})
	}
	pkg := s.packageFor(d.path)
	names = names[:0]
	for n := range pkg.Sigs {
		if !transpile.Intrinsics[n] {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	for _, n := range names {
		sig := pkg.Sigs[n]
		detail := "func() Node"
		if !sig.NoProps {
			detail = "func(" + sig.PropsType + ") Node"
		}
		label := n
		if len(sig.TypeParams) > 0 {
			label = n + "[" + strings.Join(sig.TypeParams, ", ") + "]"
		}
		items = append(items, map[string]any{"label": label, "insertText": n, "filterText": n, "kind": kindClass, "detail": detail})
	}
	return map[string]any{"isIncomplete": false, "items": nonNil(items)}
}

func (s *server) completeAttrs(d *doc, src string, ctx completionCtx) any {
	empty := map[string]any{"isIncomplete": false, "items": []any{}}
	fset := token.NewFileSet()
	f, _ := parser.ParseFile(fset, "", src, parser.ImportsOnly)
	if f == nil || f.Name == nil {
		return empty
	}
	rt := "gox"
	var imports []string
	hasRT := false
	for _, imp := range f.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		name := ""
		if imp.Name != nil {
			name = imp.Name.Name
		}
		if path == transpile.RuntimeImport {
			hasRT = true
			if name != "" {
				rt = name
			}
		}
		imports = append(imports, name+" "+imp.Path.Value)
	}
	if !hasRT {
		imports = append(imports, rt+" "+strconv.Quote(transpile.RuntimeImport))
	}
	pkg := s.packageFor(d.path)
	props, ok := transpile.PropsType(ctx.tag, pkg.Sigs, rt)
	if !ok {
		return empty
	}
	props = strings.TrimPrefix(props, "*")

	var b strings.Builder
	b.WriteString("package " + f.Name.Name + "\n\nimport (\n")
	for _, imp := range imports {
		b.WriteString("\t" + imp + "\n")
	}
	b.WriteString(")\n\nvar _ = " + props + "{\n")
	text := b.String()
	lastLine := strings.Count(text, "\n")
	text += ctx.prefix

	s.mu.Lock()
	s.ownSeq++
	uri := pathToURI(filepath.Join(filepath.Dir(d.path), syntheticName+strconv.Itoa(s.ownSeq)+".go"))
	s.mu.Unlock()
	_ = s.notifyGopls("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "go", "version": 1, "text": text}})
	defer func() {
		_ = s.notifyGopls("textDocument/didClose", map[string]any{"textDocument": map[string]any{"uri": uri}})
	}()
	resp, err := s.requestGopls("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": lastLine, "character": utf16Len(ctx.prefix)},
	})
	if err != nil || resp.Result == nil {
		return empty
	}
	var list struct {
		Items []map[string]any `json:"items"`
	}
	if json.Unmarshal(resp.Result, &list) != nil || list.Items == nil {
		_ = json.Unmarshal(resp.Result, &list.Items)
	}
	present := map[string]bool{}
	for a := range ctx.present {
		present[transpile.FieldName(a)] = true
	}
	var items []any
	for _, it := range list.Items {
		label, _ := it["label"].(string)
		kind, _ := it["kind"].(float64)
		if int(kind) != kindField || label == "Children" || present[label] {
			continue
		}
		attr := lowerFirst(label)
		item := map[string]any{"label": attr, "kind": kindField, "insertText": attr, "filterText": attr}
		for _, k := range []string{"detail", "documentation"} {
			if v, ok := it[k]; ok {
				item[k] = v
			}
		}
		items = append(items, item)
	}
	return map[string]any{"isIncomplete": false, "items": nonNil(items)}
}

func lowerFirst(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToLower(r)) + s[n:]
}

func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}
