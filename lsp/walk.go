package lsp

import (
	"strings"

	"github.com/rknit/tui.gox/transpile"
)

type direction int

const (
	toGen direction = iota // client (.gox) -> gopls (_gox.go)
	toSrc                  // gopls (_gox.go) -> client (.gox)
)

// walker rewrites URIs and positions in arbitrary LSP JSON values. The
// document a position belongs to is taken from the nearest enclosing "uri",
// "targetUri" or "textDocument.uri"; WorkspaceEdit.changes keys are handled
// too.
type walker struct {
	s      *server
	dir    direction
	failed bool // a .gox position could not be mapped (toGen only)
}

func (w *walker) walk(v any, ctx string) any {
	switch x := v.(type) {
	case []any:
		if w.dir == toSrc && isTextEdits(x) {
			if out, ok := w.importEdits(x, ctx); ok {
				return out
			}
		}
		for i := range x {
			x[i] = w.walk(x[i], ctx)
		}
		return x
	case map[string]any:
		if isPosition(x) {
			w.position(x, ctx)
			return x
		}
		local := ctx
		if u, ok := x["uri"].(string); ok {
			local = u
		}
		if td, ok := x["textDocument"].(map[string]any); ok {
			if u, ok := td["uri"].(string); ok {
				local = u
			}
		}
		target := local
		if u, ok := x["targetUri"].(string); ok {
			target = u
		}
		for k, val := range x {
			switch k {
			case "uri", "targetUri":
				continue
			case "changes":
				if m, ok := val.(map[string]any); ok {
					out := make(map[string]any, len(m))
					for u, edits := range m {
						out[w.uri(u)] = w.walk(edits, u)
					}
					x[k] = out
					continue
				}
			case "targetRange", "targetSelectionRange":
				x[k] = w.walk(val, target)
				continue
			case "originSelectionRange":
				x[k] = w.walk(val, ctx)
				continue
			}
			x[k] = w.walk(val, local)
		}
		for _, k := range []string{"uri", "targetUri"} {
			if u, ok := x[k].(string); ok {
				x[k] = w.uri(u)
				if k == "uri" && w.dir == toSrc && x[k] != u {
					if _, ok := x["version"]; ok {
						x["version"] = w.s.goxVersion(uriToPath(x[k].(string)))
					}
				}
			}
		}
		return x
	}
	return v
}

func isPosition(m map[string]any) bool {
	if len(m) != 2 {
		return false
	}
	_, l := m["line"].(float64)
	_, c := m["character"].(float64)
	return l && c
}

// uri translates a document URI between .gox and generated files.
func (w *walker) uri(u string) string {
	path := uriToPath(u)
	switch w.dir {
	case toGen:
		if strings.HasSuffix(path, ".gox") {
			return pathToURI(transpile.OutputName(path))
		}
	case toSrc:
		if src := transpile.SourceName(path); src != "" && w.s.goxExists(src) {
			return w.s.goxURI(src)
		}
	}
	return u
}

func (w *walker) position(p map[string]any, ctx string) {
	path := uriToPath(ctx)
	line, char := int(p["line"].(float64)), int(p["character"].(float64))
	switch w.dir {
	case toGen:
		if !strings.HasSuffix(path, ".gox") {
			return
		}
		src, gen, sm := w.s.mapping(path)
		if sm == nil {
			w.failed = true
			return
		}
		off, ok := sm.ToGenerated(src.offset(line, char))
		if !ok {
			w.failed = true
			return
		}
		l, c := gen.position(off)
		p["line"], p["character"] = float64(l), float64(c)
	case toSrc:
		goxPath := transpile.SourceName(path)
		if goxPath == "" {
			return
		}
		src, gen, sm := w.s.mapping(goxPath)
		if sm == nil {
			return
		}
		if off, ok := sm.ToSource(gen.offset(line, char)); ok {
			l, c := src.position(off)
			p["line"], p["character"] = float64(l), float64(c)
			return
		}
		// Generated lines are source lines shifted by the header.
		l := max(line-headerLines, 0)
		p["line"], p["character"] = float64(l), float64(0)
	}
}

// headerLines is the number of lines goxc prepends to generated files.
const headerLines = 3

func (s *server) goxURI(path string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d := s.docs[path]; d != nil {
		return d.uri
	}
	return pathToURI(path)
}

func (s *server) goxVersion(path string) any {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d := s.docs[path]; d != nil {
		return d.version
	}
	return nil
}
