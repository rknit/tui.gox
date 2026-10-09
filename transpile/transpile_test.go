package transpile

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func gen(t *testing.T, src string, comps map[string]Signature) string {
	t.Helper()
	out, err := Transpile([]byte(src), Options{Filename: "x.gox", Components: comps})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	s := string(out)
	if _, err := parser.ParseFile(token.NewFileSet(), "x.go", out, 0); err != nil {
		t.Fatalf("generated code does not parse: %v\n%s", err, s)
	}
	return s
}

// body returns the generated code after the //line directive.
func body(s string) string {
	_, b, _ := strings.Cut(s, "//line x.gox:1\n")
	return b
}

func expr(t *testing.T, e string, comps map[string]Signature) string {
	t.Helper()
	src := "package p\n\nimport \"github.com/rknit/tui.gox/gox\"\n\nvar _ = " + e + "\n"
	b := body(gen(t, src, comps))
	_, after, _ := strings.Cut(b, "var _ = ")
	return strings.TrimSpace(after)
}

func TestElements(t *testing.T) {
	cases := []struct{ in, want string }{
		{`<box />`, `gox.C(gox.Box, gox.BoxProps{})`},
		{`<box padding={1} border="rounded" hidden />`, `gox.C(gox.Box, gox.BoxProps{Padding: 1, Border: "rounded", Hidden: true})`},
		{`<text>Hello</text>`, `gox.C(gox.Text, gox.TextProps{Children: "Hello"})`},
		{`<text>a {x} b</text>`, `gox.C(gox.Text, gox.TextProps{Children: gox.F("a ", x, " b")})`},
		{`<Card title="t" />`, `gox.C(Card, CardProps{Title: "t"})`},
		{`<ui.Card on-press={f} />`, `gox.C(ui.Card, ui.CardProps{OnPress: f})`},
		{`<>a<br/></>`, `gox.F("a", gox.C(gox.Br, gox.BrProps{}))`},
		{`<Item key={id} />`, `gox.K(id, gox.C(Item, ItemProps{}))`},
		{`<text>{/* comment */}x&amp;y</text>`, `gox.C(gox.Text, gox.TextProps{Children: "x&y"})`},
		{`<show fallback={<text>no</text>} />`, `gox.C(gox.Show, gox.ShowProps{Fallback: gox.C(gox.Text, gox.TextProps{Children: "no"})})`},
		{`<text>{f(func() gox.Node { return <br/> })}</text>`, `gox.C(gox.Text, gox.TextProps{Children: f(func() gox.Node { return gox.C(gox.Br, gox.BrProps{}) })})`},
		{`<text>{"}"}</text>`, `gox.C(gox.Text, gox.TextProps{Children: "}"})`},
	}
	for _, c := range cases {
		if got := expr(t, c.in, nil); got != c.want {
			t.Errorf("%s\n got: %s\nwant: %s", c.in, got, c.want)
		}
	}
}

func TestResolution(t *testing.T) {
	comps := map[string]Signature{
		"App":  {NoProps: true},
		"Card": {PropsType: "*CardOptions"},
		"List": {PropsType: "ListArgs"},
	}
	cases := []struct{ in, want string }{
		{`<App />`, `gox.C0(App)`},
		{`<Card a={1} />`, `gox.C(Card, &CardOptions{A: 1})`},
		{`<List>x</List>`, `gox.C(List, ListArgs{Children: "x"})`},
	}
	for _, c := range cases {
		if got := expr(t, c.in, comps); got != c.want {
			t.Errorf("%s\n got: %s\nwant: %s", c.in, got, c.want)
		}
	}
}

func TestWhitespace(t *testing.T) {
	got := expr(t, "<text>\n    Hello\n    world   {name}\n\n  </text>", nil)
	want := "gox.C(gox.Text, gox.TextProps{\nChildren: gox.F(\"Hello world   \", \nname,\n\n)})"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestOperatorsAreNotElements(t *testing.T) {
	src := `package p

func f(a, b int, ch chan int) bool {
	x := a<b
	y := a < b && b <= a
	z := (a)<b
	ch <- a
	_ = <-ch
	_ = a<<b
	return x || y || z
}
`
	if b := body(gen(t, src, nil)); b != src {
		t.Errorf("source changed:\n%s", b)
	}
}

func TestLinesPreserved(t *testing.T) {
	src := `package p

import "github.com/rknit/tui.gox/gox"

func View(items []string) gox.Node {
	return (
		<box
			padding={1}
			border="rounded">
			<text bold>
				Title
			</text>
			{gox.Map(items, func(s string, i int) gox.Node {
				return <text key={i}>{s}</text>
			})}
			<Footer />
		</box>
	)
}

var marker = 1
`
	out := gen(t, src, nil)
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", out, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"marker"} {
		obj := f.Scope.Lookup(name)
		if obj == nil {
			t.Fatalf("%s not found", name)
		}
		pos := fset.Position(obj.Pos())
		if pos.Filename != "x.gox" || pos.Line != 20 {
			t.Errorf("%s at %s:%d, want x.gox:20", name, pos.Filename, pos.Line)
		}
	}
	// The map callback body must stay on its source line.
	for i, l := range strings.Split(body(out), "\n") {
		if strings.Contains(l, "return gox.K(i,") && i+1 != 14 {
			t.Errorf("keyed element on line %d, want 14", i+1)
		}
	}
}

func TestImportInsertion(t *testing.T) {
	out := gen(t, "package p\n\nvar _ = <box/>\n", nil)
	if !strings.Contains(out, "package p; import gox \""+RuntimeImport+"\"\n") {
		t.Errorf("import not inserted:\n%s", out)
	}
	out = gen(t, "package p\n\nimport ui \""+RuntimeImport+"\"\n\nvar _ = <box/>\n", nil)
	if !strings.Contains(out, "ui.C(ui.Box, ui.BoxProps{})") {
		t.Errorf("alias not used:\n%s", out)
	}
	out = gen(t, "package p\n\nvar x = 1 < 2\n", nil)
	if strings.Contains(out, "import") {
		t.Errorf("import inserted without elements:\n%s", out)
	}
}

func TestErrors(t *testing.T) {
	cases := []struct{ in, msg string }{
		{"<box></text>", "expected </box>, got </text>"},
		{"<box>", "missing closing tag </box>"},
		{"<box a=1 />", "attribute value must be"},
		{"<box a={1} a={2} />", "duplicate attribute a"},
		{"<App x={1} />", "component App takes no props"},
		{"<text>}</text>", "unexpected '}'"},
	}
	for _, c := range cases {
		_, err := Transpile([]byte("package p\nvar _ = "+c.in+"\n"), Options{Filename: "x.gox", Components: map[string]Signature{"App": {NoProps: true}}})
		if err == nil || !strings.Contains(err.Error(), c.msg) {
			t.Errorf("%s: got %v, want %q", c.in, err, c.msg)
		}
		if err != nil && !strings.HasPrefix(err.Error(), "x.gox:2:") {
			t.Errorf("%s: error lacks position: %v", c.in, err)
		}
	}
}

func TestCollectSignatures(t *testing.T) {
	src := `package p
func A() gox.Node { return nil }
func B(p BProps) gox.Node { return nil }
func C(p *COpts) Node { return nil }
func D(x int) string { return "" }
func (r R) E() gox.Node { return nil }
`
	sigs := map[string]Signature{}
	CollectSignatures([]byte(src), sigs)
	want := map[string]Signature{"A": {NoProps: true}, "B": {PropsType: "BProps"}, "C": {PropsType: "*COpts"}}
	if len(sigs) != len(want) {
		t.Fatalf("got %v", sigs)
	}
	for k, v := range want {
		if sigs[k] != v {
			t.Errorf("%s: got %+v want %+v", k, sigs[k], v)
		}
	}
}

func TestImplicitRuntimeImport(t *testing.T) {
	out := gen(t, "package p\n\nfunc App() gox.Node {\n\tn, _ := gox.UseState(0)\n\treturn n\n}\n", nil)
	if !strings.Contains(out, "package p; import gox \""+RuntimeImport+"\"\n") {
		t.Errorf("import not inserted for gox.X usage:\n%s", out)
	}
	out = gen(t, "package p\n\nvar _ = x.gox.Y\n", nil)
	if strings.Contains(out, "import") {
		t.Errorf("import inserted for field selector:\n%s", out)
	}
}

func TestLowercaseComponents(t *testing.T) {
	comps := map[string]Signature{"app": {NoProps: true}, "row": {PropsType: "rowProps"}}
	if got := expr(t, `<app />`, comps); got != `gox.C0(app)` {
		t.Errorf("got %s", got)
	}
	if got := expr(t, `<row a={1} />`, comps); got != `gox.C(row, rowProps{A: 1})` {
		t.Errorf("got %s", got)
	}
	_, err := Transpile([]byte("package p\nvar _ = <nope />\n"), Options{Filename: "x.gox", Components: comps})
	if err == nil || !strings.Contains(err.Error(), "x.gox:2:9: unknown element <nope>") {
		t.Errorf("got %v", err)
	}
}
