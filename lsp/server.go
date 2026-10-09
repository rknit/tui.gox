// Package lsp implements goxls, a language server for .gox files. It proxies
// gopls: .gox buffers are transpiled in memory and exposed to gopls as
// overlays of their generated _gox.go files, and positions in requests,
// responses and notifications are translated through the transpiler's
// source maps. Plain .go files pass through unchanged.
package lsp

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rknit/tui.gox/transpile"
)

// Options configure the server.
type Options struct {
	// Gopls is the gopls command (default "gopls").
	Gopls string
	// GoplsArgs are extra arguments passed to gopls.
	GoplsArgs []string
	// Log receives debug output (default: discarded).
	Log io.Writer
}

// Run serves LSP on in/out until the client exits.
func Run(in io.Reader, out io.Writer, opts Options) error {
	if opts.Gopls == "" {
		opts.Gopls = "gopls"
	}
	if opts.Log == nil {
		opts.Log = io.Discard
	}
	cmd := exec.Command(opts.Gopls, opts.GoplsArgs...)
	cmd.Stderr = opts.Log
	gin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	gout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting gopls: %w (install it with: go install golang.org/x/tools/gopls@latest)", err)
	}
	s := newServer(newConn(in, out), newConn(gout, gin), log.New(opts.Log, "goxls: ", log.Ltime|log.Lmicroseconds))
	done := make(chan error, 2)
	go func() { done <- s.serveGopls() }()
	go func() { done <- s.serveClient() }()
	err = <-done
	_ = gin.Close()
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	if err == io.EOF || s.exiting.Load() {
		return nil
	}
	return err
}

type server struct {
	client, gopls *conn
	log           *log.Logger

	mu      sync.Mutex
	docs    map[string]*doc // by .gox path
	pending map[string]pendingReq
	own     map[string]chan *msg
	ownSeq  int
	exiting atomic.Bool

	generateOnSave bool
	disk           map[string]*diskMap
}

type pendingReq struct {
	method string
	uri    string // document the request was about, as sent to gopls
}

func newServer(client, gopls *conn, l *log.Logger) *server {
	return &server{
		client: client, gopls: gopls, log: l,
		docs:           map[string]*doc{},
		pending:        map[string]pendingReq{},
		own:            map[string]chan *msg{},
		disk:           map[string]*diskMap{},
		generateOnSave: true,
	}
}

// doc is an open .gox document.
type doc struct {
	uri     string
	path    string
	version int
	src     *text // current client text

	// Last successful transpilation, as known to gopls.
	good       *text // source text it was generated from
	gen        *text
	sm         *transpile.SourceMap
	genVersion int
	opened     bool

	err   *transpile.Error
	diags []any // gopls diagnostics mapped to good
}

func (d *doc) genURI() string { return pathToURI(transpile.OutputName(d.path)) }

// valid reports whether positional requests can be answered: the current
// text transpiled successfully.
func (d *doc) valid() bool { return d.sm != nil && d.err == nil && d.good.s == d.src.s }

// ---------------------------------------------------------------------------
// Client -> gopls

func (s *server) serveClient() error {
	for {
		m, err := s.client.read()
		if err != nil {
			return err
		}
		if err := s.handleClient(m); err != nil {
			s.log.Printf("client %s: %v", m.Method, err)
		}
		if m.Method == "exit" {
			s.exiting.Store(true)
			time.Sleep(100 * time.Millisecond)
			return nil
		}
	}
}

func (s *server) handleClient(m *msg) error {
	if m.isResponse() {
		return s.gopls.write(m) // reply to a gopls -> client request
	}
	switch m.Method {
	case "initialize":
		return s.initialize(m)
	case "textDocument/didOpen", "textDocument/didChange", "textDocument/didClose", "textDocument/didSave":
		var p struct {
			TextDocument struct {
				URI     string `json:"uri"`
				Text    string `json:"text"`
				Version int    `json:"version"`
			} `json:"textDocument"`
			ContentChanges []struct {
				Range *struct {
					Start struct{ Line, Character int } `json:"start"`
					End   struct{ Line, Character int } `json:"end"`
				} `json:"range"`
				Text string `json:"text"`
			} `json:"contentChanges"`
		}
		_ = json.Unmarshal(m.Params, &p)
		path := uriToPath(p.TextDocument.URI)
		if !strings.HasSuffix(path, ".gox") {
			return s.gopls.write(m)
		}
		switch m.Method {
		case "textDocument/didOpen":
			s.mu.Lock()
			d := &doc{uri: p.TextDocument.URI, path: path, version: p.TextDocument.Version, src: newText(p.TextDocument.Text)}
			s.docs[path] = d
			s.mu.Unlock()
			s.refresh(filepath.Dir(path))
		case "textDocument/didChange":
			s.mu.Lock()
			d := s.docs[path]
			if d == nil {
				s.mu.Unlock()
				return nil
			}
			cur := d.src.s
			for _, c := range p.ContentChanges {
				if c.Range == nil {
					cur = c.Text
					continue
				}
				t := newText(cur)
				a := t.offset(c.Range.Start.Line, c.Range.Start.Character)
				b := t.offset(c.Range.End.Line, c.Range.End.Character)
				cur = cur[:a] + c.Text + cur[b:]
			}
			d.src = newText(cur)
			d.version = p.TextDocument.Version
			s.mu.Unlock()
			s.refresh(filepath.Dir(path))
		case "textDocument/didSave":
			s.mu.Lock()
			d := s.docs[path]
			gen := ""
			if d != nil && d.valid() && s.generateOnSave {
				gen = d.gen.s
			}
			s.mu.Unlock()
			if gen != "" {
				target := transpile.OutputName(path)
				if old, _ := os.ReadFile(target); string(old) != gen {
					if err := os.WriteFile(target, []byte(gen), 0o644); err != nil {
						s.showMessage(1, "goxls: writing "+target+": "+err.Error())
					}
				}
			}
		case "textDocument/didClose":
			s.mu.Lock()
			d := s.docs[path]
			delete(s.docs, path)
			s.mu.Unlock()
			if d != nil && d.opened {
				_ = s.notifyGopls("textDocument/didClose", map[string]any{"textDocument": map[string]any{"uri": d.genURI()}})
			}
			_ = s.notifyClient("textDocument/publishDiagnostics", map[string]any{"uri": p.TextDocument.URI, "diagnostics": []any{}})
		}
		return nil
	}

	// Generic request or notification: translate .gox positions.
	var params any
	if len(m.Params) > 0 {
		if err := json.Unmarshal(m.Params, &params); err != nil {
			return err
		}
	}
	goxURI := textDocumentURI(params)
	if goxURI != "" && strings.HasSuffix(uriToPath(goxURI), ".gox") {
		if m.isRequest() {
			if res, handled := s.goxRequest(m, goxURI, params); handled {
				if e, ok := res.(rpcError); ok {
					return s.client.write(&msg{ID: m.ID, Error: mustJSON(e)})
				}
				return s.reply(m.ID, res)
			}
		}
		w := &walker{s: s, dir: toGen}
		params = w.walk(params, goxURI)
		if w.failed {
			if m.isRequest() {
				return s.reply(m.ID, nil)
			}
			return nil
		}
		m.Params = mustJSON(params)
	}
	if m.isRequest() {
		s.mu.Lock()
		s.pending[string(m.ID)] = pendingReq{method: m.Method, uri: textDocumentURI(params)}
		s.mu.Unlock()
	}
	return s.gopls.write(m)
}

// goxRequest handles requests on .gox documents that are not simply
// forwarded. It returns handled=false to forward the request.
func (s *server) goxRequest(m *msg, uri string, params any) (any, bool) {
	path := uriToPath(uri)
	s.mu.Lock()
	d := s.docs[path]
	valid := d != nil && d.valid()
	s.mu.Unlock()
	switch m.Method {
	case "gox/closingTag":
		// Custom request: text to insert for automatic tag closing at the
		// position just after a typed '>' or "</". Result: {"text": "..."}
		// or null.
		if d == nil {
			return nil, true
		}
		p, _ := params.(map[string]any)
		pos, _ := p["position"].(map[string]any)
		line, _ := pos["line"].(float64)
		char, _ := pos["character"].(float64)
		s.mu.Lock()
		src := d.src
		s.mu.Unlock()
		if text := transpile.ClosingTag([]byte(src.s), src.offset(int(line), int(char))); text != "" {
			return map[string]any{"text": text}, true
		}
		return nil, true
	case "textDocument/formatting":
		if d == nil {
			return nil, true
		}
		return s.format(d), true
	case "textDocument/rangeFormatting", "textDocument/onTypeFormatting",
		"textDocument/semanticTokens/full", "textDocument/semanticTokens/range", "textDocument/semanticTokens/full/delta",
		"textDocument/foldingRange", "textDocument/codeLens", "textDocument/documentLink":
		return nil, true
	case "textDocument/completion":
		if d == nil {
			return nil, true
		}
		if res, ok := s.complete(d, params); ok {
			return res, true
		}
	}
	if !valid {
		return nil, true
	}
	return nil, false
}

func (s *server) initialize(m *msg) error {
	var params map[string]any
	if err := json.Unmarshal(m.Params, &params); err != nil {
		return err
	}
	if io, ok := params["initializationOptions"].(map[string]any); ok {
		if g, ok := io["goxls"].(map[string]any); ok {
			if v, ok := g["generateOnSave"].(bool); ok {
				s.generateOnSave = v
			}
			delete(io, "goxls")
		}
	}
	m.Params = mustJSON(params)
	s.mu.Lock()
	s.pending[string(m.ID)] = pendingReq{method: "initialize"}
	s.mu.Unlock()
	return s.gopls.write(m)
}

// ---------------------------------------------------------------------------
// gopls -> client

func (s *server) serveGopls() error {
	for {
		m, err := s.gopls.read()
		if err != nil {
			return err
		}
		if err := s.handleGopls(m); err != nil {
			s.log.Printf("gopls %s: %v", m.Method, err)
		}
	}
}

func (s *server) handleGopls(m *msg) error {
	if m.isResponse() {
		key := string(m.ID)
		s.mu.Lock()
		if ch, ok := s.own[key]; ok {
			delete(s.own, key)
			s.mu.Unlock()
			ch <- m
			return nil
		}
		p := s.pending[key]
		delete(s.pending, key)
		s.mu.Unlock()
		if p.method == "initialize" {
			return s.client.write(s.patchInitialize(m))
		}
		if m.Result != nil && string(m.Result) != "null" {
			var res any
			if err := json.Unmarshal(m.Result, &res); err != nil {
				return err
			}
			w := &walker{s: s, dir: toSrc}
			m.Result = mustJSON(w.walk(res, p.uri))
		}
		return s.client.write(m)
	}

	var params any
	if len(m.Params) > 0 {
		if err := json.Unmarshal(m.Params, &params); err != nil {
			return err
		}
	}
	if m.Method == "textDocument/publishDiagnostics" {
		return s.goplsDiagnostics(params)
	}
	if m.Method == "window/showMessage" && s.ownGeneratedWarning(params) {
		return nil
	}
	w := &walker{s: s, dir: toSrc}
	m.Params = mustJSON(w.walk(params, ""))
	return s.client.write(m)
}

func (s *server) patchInitialize(m *msg) *msg {
	var res map[string]any
	if json.Unmarshal(m.Result, &res) != nil {
		return m
	}
	if caps, ok := res["capabilities"].(map[string]any); ok {
		// Full document sync keeps .gox handling simple; gopls accepts full
		// content changes for .go files too.
		caps["textDocumentSync"] = map[string]any{"openClose": true, "change": 1, "save": map[string]any{"includeText": false}}
		if cp, ok := caps["completionProvider"].(map[string]any); ok {
			tc, _ := cp["triggerCharacters"].([]any)
			cp["triggerCharacters"] = append(tc, "<")
		}
	}
	res["serverInfo"] = map[string]any{"name": "goxls"}
	m.Result = mustJSON(res)
	return m
}

func (s *server) goplsDiagnostics(params any) error {
	p, _ := params.(map[string]any)
	uri, _ := p["uri"].(string)
	path := uriToPath(uri)
	if strings.Contains(filepath.Base(path), syntheticName) {
		return nil
	}
	src := transpile.SourceName(path)
	if src == "" || !s.goxExists(src) {
		return s.notifyClient("textDocument/publishDiagnostics", params)
	}
	w := &walker{s: s, dir: toSrc}
	diags, _ := w.walk(p["diagnostics"], uri).([]any)
	s.mu.Lock()
	d := s.docs[src]
	if d != nil {
		d.diags = diags
	}
	s.mu.Unlock()
	if d != nil {
		return s.publish(src)
	}
	return s.notifyClient("textDocument/publishDiagnostics", map[string]any{"uri": pathToURI(src), "diagnostics": nonNil(diags)})
}

// ownGeneratedWarning reports whether a gopls message is its warning about
// editing a generated file that is in fact an overlay goxls maintains for an
// open .gox document.
func (s *server) ownGeneratedWarning(params any) bool {
	p, _ := params.(map[string]any)
	msg, _ := p["message"].(string)
	base, ok := strings.CutPrefix(msg, "Warning: editing ")
	if !ok {
		return false
	}
	base, ok = strings.CutSuffix(base, ", a generated file.")
	if !ok {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for path := range s.docs {
		if filepath.Base(transpile.OutputName(path)) == base {
			return true
		}
	}
	return false
}

func nonNil(v []any) []any {
	if v == nil {
		return []any{}
	}
	return v
}

// publish sends the merged transpiler and gopls diagnostics of a document.
func (s *server) publish(path string) error {
	s.mu.Lock()
	d := s.docs[path]
	if d == nil {
		s.mu.Unlock()
		return nil
	}
	var diags []any
	if d.err != nil {
		off := d.src.offset(d.err.Line-1, 0) + d.err.Col - 1
		l, c := d.src.position(off)
		diags = append(diags, map[string]any{
			"range":    rng(l, c, l, c+1),
			"severity": 1,
			"source":   "goxc",
			"message":  d.err.Msg,
		})
	} else if d.good != nil && d.good.s == d.src.s {
		diags = append(diags, d.diags...)
	}
	uri := d.uri
	s.mu.Unlock()
	return s.notifyClient("textDocument/publishDiagnostics", map[string]any{"uri": uri, "diagnostics": nonNil(diags)})
}

func rng(l1, c1, l2, c2 int) map[string]any {
	return map[string]any{
		"start": map[string]any{"line": l1, "character": c1},
		"end":   map[string]any{"line": l2, "character": c2},
	}
}

// ---------------------------------------------------------------------------
// Transpilation

// refresh re-transpiles every open .gox document in dir (signatures are
// package-wide) and pushes changed overlays to gopls.
func (s *server) refresh(dir string) {
	s.mu.Lock()
	overlay := map[string][]byte{}
	var docs []*doc
	for p, d := range s.docs {
		if filepath.Dir(p) == dir {
			overlay[p] = []byte(d.src.s)
			docs = append(docs, d)
		}
	}
	s.mu.Unlock()

	pkg, err := transpile.LoadPackage(dir, overlay)
	if err != nil {
		s.log.Printf("load %s: %v", dir, err)
		return
	}
	for _, d := range docs {
		s.mu.Lock()
		src := d.src
		s.mu.Unlock()
		out, sm, err := transpile.TranspileMap([]byte(src.s), transpile.Options{Filename: d.path, Components: pkg.Sigs})

		s.mu.Lock()
		var send *msg
		if err != nil {
			d.err, _ = err.(*transpile.Error)
		} else {
			d.err = nil
			changed := d.gen == nil || d.gen.s != string(out)
			d.good, d.gen, d.sm = src, newText(string(out)), sm
			if changed || !d.opened {
				d.genVersion++
				if !d.opened {
					d.opened = true
					send = &msg{Method: "textDocument/didOpen", Params: mustJSON(map[string]any{"textDocument": map[string]any{
						"uri": d.genURI(), "languageId": "go", "version": d.genVersion, "text": d.gen.s,
					}})}
				} else {
					send = &msg{Method: "textDocument/didChange", Params: mustJSON(map[string]any{
						"textDocument":   map[string]any{"uri": d.genURI(), "version": d.genVersion},
						"contentChanges": []any{map[string]any{"text": d.gen.s}},
					})}
				}
			}
		}
		s.mu.Unlock()
		if send != nil {
			_ = s.gopls.write(send)
		}
		_ = s.publish(d.path)
	}
}

// diskMap is the mapping of a .gox file that is not open in the editor.
type diskMap struct {
	mod      time.Time
	src, gen *text
	sm       *transpile.SourceMap
}

// mapping returns the source map for a .gox path: the open document's or
// one computed from disk.
func (s *server) mapping(path string) (src, gen *text, sm *transpile.SourceMap) {
	s.mu.Lock()
	if d := s.docs[path]; d != nil {
		defer s.mu.Unlock()
		if d.sm == nil {
			return nil, nil, nil
		}
		return d.good, d.gen, d.sm
	}
	s.mu.Unlock()
	fi, err := os.Stat(path)
	if err != nil {
		return nil, nil, nil
	}
	s.mu.Lock()
	if dm := s.disk[path]; dm != nil && dm.mod.Equal(fi.ModTime()) {
		s.mu.Unlock()
		return dm.src, dm.gen, dm.sm
	}
	s.mu.Unlock()
	pkg, err := transpile.LoadPackage(filepath.Dir(path), nil)
	if err != nil || pkg.Files[path] == nil {
		return nil, nil, nil
	}
	out, sm, err := pkg.Transpile(path)
	if err != nil {
		return nil, nil, nil
	}
	dm := &diskMap{mod: fi.ModTime(), src: newText(string(pkg.Files[path])), gen: newText(string(out)), sm: sm}
	s.mu.Lock()
	s.disk[path] = dm
	s.mu.Unlock()
	return dm.src, dm.gen, dm.sm
}

func (s *server) goxExists(path string) bool {
	s.mu.Lock()
	_, open := s.docs[path]
	s.mu.Unlock()
	if open {
		return true
	}
	_, err := os.Stat(path)
	return err == nil
}

// ---------------------------------------------------------------------------
// Messaging helpers

func (s *server) reply(id json.RawMessage, result any) error {
	return s.client.write(&msg{ID: id, Result: mustJSON(result)})
}

func (s *server) notifyClient(method string, params any) error {
	return s.client.write(&msg{Method: method, Params: mustJSON(params)})
}

func (s *server) notifyGopls(method string, params any) error {
	return s.gopls.write(&msg{Method: method, Params: mustJSON(params)})
}

func (s *server) showMessage(typ int, text string) {
	_ = s.notifyClient("window/showMessage", map[string]any{"type": typ, "message": text})
}

// requestGopls sends a request originating from goxls and waits for the
// response.
func (s *server) requestGopls(method string, params any) (*msg, error) {
	s.mu.Lock()
	s.ownSeq++
	id := mustJSON(fmt.Sprintf("goxls-%d", s.ownSeq))
	ch := make(chan *msg, 1)
	s.own[string(id)] = ch
	s.mu.Unlock()
	if err := s.gopls.write(&msg{ID: id, Method: method, Params: mustJSON(params)}); err != nil {
		return nil, err
	}
	select {
	case m := <-ch:
		return m, nil
	case <-time.After(10 * time.Second):
		s.mu.Lock()
		delete(s.own, string(id))
		s.mu.Unlock()
		return nil, fmt.Errorf("%s: timeout", method)
	}
}

func textDocumentURI(params any) string {
	p, _ := params.(map[string]any)
	td, _ := p["textDocument"].(map[string]any)
	uri, _ := td["uri"].(string)
	return uri
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// format formats a .gox document, returning one edit replacing the whole
// text (or no edits when it is already formatted).
func (s *server) format(d *doc) any {
	s.mu.Lock()
	src := d.src
	s.mu.Unlock()
	out, err := transpile.Format([]byte(src.s))
	if err != nil {
		return rpcError{Code: -32603, Message: err.Error()}
	}
	if string(out) == src.s {
		return []any{}
	}
	l, c := src.position(len(src.s))
	return []any{map[string]any{"range": rng(0, 0, l, c), "newText": string(out)}}
}
