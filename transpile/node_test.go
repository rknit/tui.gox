package transpile

import (
	"go/parser"
	"go/token"
	"reflect"
	"strings"
	"testing"
)

func TestNodeDecl(t *testing.T) {
	src := `package p

// MyNode has docs.
node MyNode[T](value1 string, foo T, check bool) {
	return <text>{value1}</text>
}
`
	want := `package p; import gox "github.com/rknit/tui.gox/gox"

// MyNode has docs.
func MyNode[T any](gox_props_ MyNodeProps[T]) gox.Node { return gox_MyNode[T](gox_props_.Value1, gox_props_.Foo, gox_props_.Check) }; func gox_MyNode[T any](value1 string, foo T, check bool) gox.Node {
	return gox.C(gox.Text, gox.TextProps{Children: value1})
}

//line x.gox:4
type MyNodeProps[T any] struct { Value1 string; Foo T; Check bool; }
`
	if got := body(gen(t, src, nil)); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestNodeForms(t *testing.T) {
	cases := []struct{ in, want string }{
		{"node App() {", "func App() gox.Node {"},
		{"node app () {", "func app() gox.Node {"},
		{"node Empty[T]() {", "func Empty[T any]() gox.Node {"},
		{"node Pair[K, V](k K, v V) {", "func Pair[K, V any](gox_props_ PairProps[K, V]) gox.Node { return gox_Pair[K, V](gox_props_.K, gox_props_.V) }; func gox_Pair[K, V any](k K, v V) gox.Node {"},
		{"node Map[K comparable, V any](m map[K]V) {", "func Map[K comparable, V any](gox_props_ MapProps[K, V]) gox.Node { return gox_Map[K, V](gox_props_.M) }; func gox_Map[K comparable, V any](m map[K]V) gox.Node {"},
		{"node Grouped(a, b int, children gox.Node) {", "func Grouped(gox_props_ GroupedProps) gox.Node { return gox_Grouped(gox_props_.A, gox_props_.B, gox_props_.Children) }; func gox_Grouped(a, b int, children gox.Node) gox.Node {"},
	}
	for _, c := range cases {
		src := "package p\n\n" + c.in + "\n}\n"
		got := body(gen(t, src, nil))
		line := strings.Split(got, "\n")[2]
		if line != c.want {
			t.Errorf("%s\n got: %s\nwant: %s", c.in, line, c.want)
		}
	}
	got := body(gen(t, "package p\n\nnode Map[K comparable, V any](m map[K]V) {\n}\n", nil))
	if !strings.Contains(got, "type MapProps[K comparable, V any] struct { M map[K]V; }") {
		t.Errorf("props type:\n%s", got)
	}
}

// node is still an ordinary identifier outside top-level declarations.
func TestNodeIdentifier(t *testing.T) {
	src := `package p

var node = f(1)

var (
	node2 int
	n     node[int]
)

func f(node int) int {
	node := node + 1
	node(x)
	return node
}
`
	if b := body(gen(t, src, nil)); b != src {
		t.Errorf("source changed:\n%s", b)
	}
}

// Multi-line parameter lists keep every line in place: the body stays on its
// lines and each props field is declared on its parameter's line.
func TestNodeLines(t *testing.T) {
	src := `package p

node Card(
	title string,
	count int, // comment
	children gox.Node,
) {
	return <text>{title}</text>
}

var marker = 1
`
	out := gen(t, src, nil)
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", out, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	// Lines (after //line directives) of declarations and props fields.
	lines := map[string]int{}
	for _, name := range []string{"Title", "Count", "Children", "marker", "Card"} {
		i := strings.Index(string(out), " "+name+" ") + 1
		if name == "Card" {
			i = strings.Index(string(out), "func Card") + len("func ")
		}
		if name == "marker" {
			i = strings.Index(string(out), "var marker") + len("var ")
		}
		lines[name] = fset.Position(fset.File(f.Pos()).Pos(i)).Line
	}
	want := map[string]int{"Card": 3, "Title": 4, "Count": 5, "Children": 6, "marker": 11}
	if !reflect.DeepEqual(lines, want) {
		t.Errorf("lines %v, want %v\n%s", lines, want, out)
	}
	if strings.Split(body(string(out)), "\n")[7] != "\treturn gox.C(gox.Text, gox.TextProps{Children: title})" {
		t.Errorf("body moved:\n%s", out)
	}
}

func TestNodeErrors(t *testing.T) {
	cases := []struct{ in, msg string }{
		{"node A(string) {", "node parameters must be named"},
		{"node A(xs ...int) {", "cannot be variadic"},
		{"node A(_ int) {", "cannot be named _"},
		{"node A() gox.Node {", "node A cannot declare results"},
		{"node A()\n{", "expected '{' after parameters of node A"},
		{"node A(x int {", "unterminated parameters"},
		{"node A[](x int) {", "x.gox:3:"},
		{"node A(x int,, y int) {", "x.gox:3:"},
	}
	for _, c := range cases {
		_, err := Transpile([]byte("package p\n\n"+c.in+"\n}\n"), Options{Filename: "x.gox"})
		if err == nil || !strings.Contains(err.Error(), c.msg) {
			t.Errorf("%s: got %v, want %q", c.in, err, c.msg)
		}
		if err != nil && !strings.HasPrefix(err.Error(), "x.gox:3:") {
			t.Errorf("%s: error not on the declaration: %v", c.in, err)
		}
	}
}

func TestNodeSignatures(t *testing.T) {
	src := "package p\n\nnode List[T](items []T) {\n}\n\nnode App() {\n}\n"
	out, err := Transpile([]byte(src), Options{Filename: "x.gox"})
	if err != nil {
		t.Fatal(err)
	}
	sigs := map[string]Signature{}
	CollectSignatures(out, sigs)
	want := map[string]Signature{
		"List": {TypeParams: []string{"T"}, PropsType: "ListProps[T]"},
		"App":  {NoProps: true},
	}
	if !reflect.DeepEqual(sigs, want) {
		t.Errorf("got %+v", sigs)
	}
	got := expr(t, `<List[string] items={xs} />`, sigs)
	if want := `gox.C(List[string], ListProps[string]{Items: xs})`; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestNodeSourceMap(t *testing.T) {
	src := `package p

node Card[T](title string, item T) {
	return <text>{title}</text>
}
`
	out, sm, err := TranspileMap([]byte(src), Options{Filename: "x.gox"})
	if err != nil {
		t.Fatal(err)
	}
	gen := string(out)
	probes := []struct{ src, gen string }{
		{"Card[", "Card[T any]("},
		{"T](", "T any]("},
		// Parameters map to the parameters of the body func.
		{"title string", "title string, item T)"},
		{"item T", "item T)"},
		{"string,", "string, item T)"},
		{"title}<", "title})"},
	}
	for _, p := range probes {
		off := strings.Index(src, p.src)
		d, ok := sm.ToGenerated(off)
		if !ok || !strings.HasPrefix(gen[d:], p.gen) {
			t.Errorf("%q -> %q, want prefix %q", p.src, gen[d:min(d+20, len(gen))], p.gen)
			continue
		}
		if back, ok := sm.ToSource(d); !ok || back != off {
			t.Errorf("%q: round trip %d -> %d -> %d", p.src, off, d, back)
		}
	}
	// The props field maps back to the parameter too.
	d := strings.Index(gen, "Title string")
	if back, ok := sm.ToSource(d); !ok || back != strings.Index(src, "title string") {
		t.Errorf("field Title -> %d", back)
	}
}

func TestFormatNode(t *testing.T) {
	in := "package p\n\n// Doc.\nnode  Card [T]( title  string,item T ) {\nreturn <text>{title}</text>\n}\n\nnode Pair[K comparable,V any](k K) {\n\treturn nil\n}\n"
	want := "package p\n\n// Doc.\nnode Card[T](title string, item T) {\n\treturn <text>{title}</text>\n}\n\nnode Pair[K comparable, V any](k K) {\n\treturn nil\n}\n"
	checkFormat(t, in, want)
}
