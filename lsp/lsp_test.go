package lsp

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// client is a minimal LSP client for tests.
type client struct {
	t     *testing.T
	conn  *conn
	mu    sync.Mutex
	seq   int
	resp  map[string]chan *msg
	diags map[string][]any
	dch   chan string
}

func goplsPath(t *testing.T) string {
	if p, err := exec.LookPath("gopls"); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	p := filepath.Join(home, "go", "bin", "gopls")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	t.Skip("gopls not installed")
	return ""
}

func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Dir(filepath.Dir(file))
}

func startClient(t *testing.T, dir string) *client {
	t.Helper()
	cr, sw := io.Pipe() // server -> client
	sr, cw := io.Pipe() // client -> server
	go func() {
		err := Run(sr, sw, Options{Gopls: goplsPath(t)})
		if err != nil {
			t.Logf("server: %v", err)
		}
		sw.Close()
	}()
	c := &client{t: t, conn: newConn(cr, cw), resp: map[string]chan *msg{}, diags: map[string][]any{}, dch: make(chan string, 100)}
	go c.loop()
	c.call("initialize", map[string]any{
		"processId": nil, "rootUri": pathToURI(dir),
		"capabilities":          map[string]any{"textDocument": map[string]any{"hover": map[string]any{"contentFormat": []string{"plaintext"}}}},
		"workspaceFolders":      []any{map[string]any{"uri": pathToURI(dir), "name": "app"}},
		"initializationOptions": map[string]any{"goxls": map[string]any{"generateOnSave": true}},
	})
	c.notify("initialized", map[string]any{})
	t.Cleanup(func() {
		c.call("shutdown", nil)
		c.notify("exit", nil)
	})
	return c
}

func (c *client) loop() {
	for {
		m, err := c.conn.read()
		if err != nil {
			return
		}
		switch {
		case m.isResponse():
			c.mu.Lock()
			ch := c.resp[string(m.ID)]
			c.mu.Unlock()
			if ch != nil {
				ch <- m
			}
		case m.isRequest(): // e.g. workspace/configuration, window/workDoneProgress/create
			var res any
			if m.Method == "workspace/configuration" {
				res = []any{map[string]any{}}
			}
			_ = c.conn.write(&msg{ID: m.ID, Result: mustJSON(res)})
		case m.Method == "textDocument/publishDiagnostics":
			var p struct {
				URI         string `json:"uri"`
				Diagnostics []any  `json:"diagnostics"`
			}
			_ = json.Unmarshal(m.Params, &p)
			c.mu.Lock()
			c.diags[p.URI] = p.Diagnostics
			c.mu.Unlock()
			c.dch <- p.URI
		}
	}
}

func (c *client) call(method string, params any) json.RawMessage {
	c.t.Helper()
	c.mu.Lock()
	c.seq++
	id := mustJSON(c.seq)
	ch := make(chan *msg, 1)
	c.resp[string(id)] = ch
	c.mu.Unlock()
	if err := c.conn.write(&msg{ID: id, Method: method, Params: mustJSON(params)}); err != nil {
		c.t.Fatal(err)
	}
	select {
	case m := <-ch:
		if m.Error != nil {
			c.t.Fatalf("%s: %s", method, m.Error)
		}
		return m.Result
	case <-time.After(60 * time.Second):
		c.t.Fatalf("%s: timeout", method)
	}
	return nil
}

func (c *client) notify(method string, params any) {
	if err := c.conn.write(&msg{Method: method, Params: mustJSON(params)}); err != nil {
		c.t.Fatal(err)
	}
}

// waitDiags waits until diagnostics for uri satisfy ok.
func (c *client) waitDiags(uri string, ok func([]any) bool) []any {
	c.t.Helper()
	deadline := time.After(60 * time.Second)
	for {
		c.mu.Lock()
		d, seen := c.diags[uri]
		c.mu.Unlock()
		if seen && ok(d) {
			return d
		}
		select {
		case <-c.dch:
		case <-deadline:
			c.t.Fatalf("diagnostics for %s: timeout; last: %v", uri, d)
		}
	}
}

func pos(src, needle string, delta int) map[string]any {
	off := strings.Index(src, needle) + delta
	t := newText(src)
	l, ch := t.position(off)
	return map[string]any{"line": l, "character": ch}
}

const appSrc = `package main

type CardProps struct {
	// Title is shown in bold.
	Title string
	Count int
}

func Card(p CardProps) gox.Node {
	return <box padding={1}>
		<text bold>{p.Title}</text>
	</box>
}

func App() gox.Node {
	name := "x"
	return <Card title={name} count={missing} />
}

func main() { gox.Run(<App />) }
`

func setupModule(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gomod := fmt.Sprintf("module example.com/app\n\ngo 1.24\n\nrequire github.com/rknit/tui.gox v0.0.0\n\nreplace github.com/rknit/tui.gox => %s\n", repoRoot())
	must(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "main.gox"), []byte(appSrc), 0o644))
	// Resolve dependencies from the module cache.
	gen := exec.Command("go", "run", filepath.Join(repoRoot(), "cmd", "goxc"), ".")
	gen.Dir = dir
	gen.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOPROXY=off")
	if out, err := gen.CombinedOutput(); err != nil {
		t.Skipf("cannot prepare module (needs module cache): %v\n%s", err, out)
	}
	return dir
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestGoxls(t *testing.T) {
	goplsPath(t)
	dir := setupModule(t)
	c := startClient(t, dir)
	uri := pathToURI(filepath.Join(dir, "main.gox"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "gox", "version": 1, "text": appSrc}})

	// gopls type errors are reported at .gox positions.
	diags := c.waitDiags(uri, func(d []any) bool { return len(d) > 0 })
	d0 := diags[0].(map[string]any)
	if !strings.Contains(d0["message"].(string), "missing") {
		t.Errorf("diagnostic: %v", d0)
	}
	start := d0["range"].(map[string]any)["start"].(map[string]any)
	want := pos(appSrc, "missing", 0)
	if start["line"].(float64) != float64(want["line"].(int)) || start["character"].(float64) != float64(want["character"].(int)) {
		t.Errorf("diagnostic at %v, want %v", start, want)
	}

	t.Run("hover attribute", func(t *testing.T) {
		res := c.call("textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": pos(appSrc, "title={name}", 1)})
		if !strings.Contains(string(res), "Title string") || !strings.Contains(string(res), "shown in bold") {
			t.Errorf("hover: %s", res)
		}
	})

	t.Run("definition of tag", func(t *testing.T) {
		res := c.call("textDocument/definition", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": pos(appSrc, "Card title", 1)})
		want := pos(appSrc, "Card(p CardProps)", 0)
		if !strings.Contains(string(res), uri) || !strings.Contains(string(res), fmt.Sprintf(`"line":%d`, want["line"])) {
			t.Errorf("definition: %s (want line %v)", res, want["line"])
		}
	})

	t.Run("references of field", func(t *testing.T) {
		res := c.call("textDocument/references", map[string]any{
			"textDocument": map[string]any{"uri": uri}, "position": pos(appSrc, "Title string", 0),
			"context": map[string]any{"includeDeclaration": false},
		})
		// p.Title in Card and title= in App.
		if n := strings.Count(string(res), `"uri"`); n != 2 || strings.Contains(string(res), "_gox.go") {
			t.Errorf("references: %s", res)
		}
	})

	t.Run("expression completion", func(t *testing.T) {
		res := c.call("textDocument/completion", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": pos(appSrc, "Title}</text>", 0)})
		if !strings.Contains(string(res), `"Title"`) || !strings.Contains(string(res), `"Count"`) {
			t.Errorf("completion: %.300s", res)
		}
	})

	t.Run("attribute completion", func(t *testing.T) {
		res := c.call("textDocument/completion", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": pos(appSrc, " count=", 1)})
		s := string(res)
		if !strings.Contains(s, `"label":"count"`) || strings.Contains(s, `"label":"title"`) {
			t.Errorf("attribute completion: %s", s)
		}
		res = c.call("textDocument/completion", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": pos(appSrc, " padding=", 1)})
		if !strings.Contains(string(res), `"label":"border"`) || !strings.Contains(string(res), `"label":"onMouse"`) {
			t.Errorf("box attribute completion: %.400s", res)
		}
	})

	t.Run("tag completion", func(t *testing.T) {
		res := c.call("textDocument/completion", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": pos(appSrc, "Card title", 2)})
		s := string(res)
		if !strings.Contains(s, `"label":"Card"`) || !strings.Contains(s, `"label":"box"`) {
			t.Errorf("tag completion: %.300s", s)
		}
	})

	t.Run("closing tag", func(t *testing.T) {
		typed := strings.Replace(appSrc, "<text bold>{p.Title}</text>", "<text bold>", 1)
		c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 9}, "contentChanges": []any{map[string]any{"text": typed}}})
		res := c.call("gox/closingTag", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": pos(typed, "<text bold>", len("<text bold>"))})
		if string(res) != `{"text":"</text>"}` {
			t.Errorf("closingTag: %s", res)
		}
		res = c.call("gox/closingTag", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": pos(typed, "<box padding", 4)})
		if string(res) != "null" {
			t.Errorf("closingTag mid-tag: %s", res)
		}
	})

	t.Run("formatting", func(t *testing.T) {
		messy := strings.Replace(appSrc, "<box padding={1}>", "<box   padding={ 1 }  >", 1)
		c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 10}, "contentChanges": []any{map[string]any{"text": messy}}})
		res := c.call("textDocument/formatting", map[string]any{"textDocument": map[string]any{"uri": uri}, "options": map[string]any{"tabSize": 4, "insertSpaces": false}})
		var edits []struct {
			NewText string `json:"newText"`
		}
		if err := json.Unmarshal(res, &edits); err != nil || len(edits) != 1 || edits[0].NewText != appSrc {
			t.Errorf("formatting: %s", res)
		}
		c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 11}, "contentChanges": []any{map[string]any{"text": appSrc}}})
		if res := c.call("textDocument/formatting", map[string]any{"textDocument": map[string]any{"uri": uri}, "options": map[string]any{}}); string(res) != "[]" {
			t.Errorf("formatting formatted doc: %s", res)
		}
	})

	t.Run("rename", func(t *testing.T) {
		res := c.call("textDocument/rename", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": pos(appSrc, "name :=", 0), "newName": "who"})
		s := string(res)
		if strings.Contains(s, "_gox.go") || strings.Count(s, `"newText":"who"`) != 2 {
			t.Fatalf("rename: %s", s)
		}
		// Both edits must cover exactly "name" in the .gox source.
		for _, needle := range []string{"name :=", "name}"} {
			p := pos(appSrc, needle, 0)
			if !strings.Contains(s, fmt.Sprintf(`"start":{"character":%d,"line":%d}`, p["character"], p["line"])) {
				t.Errorf("rename edit for %q missing: %s", needle, s)
			}
		}
	})

	t.Run("transpile error", func(t *testing.T) {
		bad := strings.Replace(appSrc, "</box>", "</text>", 1)
		c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 2}, "contentChanges": []any{map[string]any{"text": bad}}})
		d := c.waitDiags(uri, func(d []any) bool {
			return len(d) == 1 && d[0].(map[string]any)["source"] == "goxc"
		})
		if !strings.Contains(d[0].(map[string]any)["message"].(string), "expected </box>") {
			t.Errorf("goxc diagnostic: %v", d)
		}
		// Positional requests are not answered from stale code.
		if res := c.call("textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": pos(bad, "title=", 1)}); string(res) != "null" {
			t.Errorf("hover on invalid doc: %s", res)
		}
	})

	t.Run("fix and save generates", func(t *testing.T) {
		fixed := strings.Replace(appSrc, "count={missing}", "count={2}", 1)
		c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 3}, "contentChanges": []any{map[string]any{"text": fixed}}})
		c.waitDiags(uri, func(d []any) bool { return len(d) == 0 })
		must(t, os.WriteFile(filepath.Join(dir, "main.gox"), []byte(fixed), 0o644))
		c.notify("textDocument/didSave", map[string]any{"textDocument": map[string]any{"uri": uri}})
		deadline := time.Now().Add(5 * time.Second)
		for {
			b, _ := os.ReadFile(filepath.Join(dir, "main_gox.go"))
			if strings.Contains(string(b), "Count: 2") {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("generated file not updated:\n%s", b)
			}
			time.Sleep(50 * time.Millisecond)
		}
	})
}

func TestCompletionContext(t *testing.T) {
	cases := []struct {
		src  string // | marks the cursor
		kind completionKind
		tag  string
	}{
		{"return <bo|", tagCompletion, ""},
		{"x := a <b|", noCompletion, ""},
		{"x := a<b|", noCompletion, ""},
		{"<box padding={1} |", attrCompletion, "box"},
		{"<box padding={1} bor|", attrCompletion, "box"},
		{"<List[int] it|", attrCompletion, "List[int]"},
		{"<box a={<text/>} |", attrCompletion, "box"},
		{"<box a=\"x > y\" |", attrCompletion, "box"},
		{"<box>hello |", noCompletion, ""},
		{"<box a={1 |", noCompletion, ""},
		{"if a<b { |", noCompletion, ""},
	}
	for _, c := range cases {
		off := strings.Index(c.src, "|")
		src := c.src[:off] + c.src[off+1:]
		got := completionContext(src, off)
		if got.kind != c.kind || got.tag != c.tag {
			t.Errorf("%q: got kind %d tag %q", c.src, got.kind, got.tag)
		}
	}
}
