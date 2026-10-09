package transpile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func checkFormat(t *testing.T, in, want string) {
	t.Helper()
	got, err := Format([]byte(in))
	if err != nil {
		t.Fatalf("Format: %v\n%s", err, in)
	}
	if string(got) != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	again, err := Format(got)
	if err != nil || string(again) != string(got) {
		t.Errorf("not idempotent: %v\n%s", err, again)
	}
}

func wrap(body string) string {
	return "package p\n\nfunc f() gox.Node {\n" + body + "}\n"
}

func TestFormatGoAndElements(t *testing.T) {
	in := `package p
import "fmt"
func   App( ) gox.Node {
  n,set:=gox.UseState(0)
  return (
  <box   padding={ 1 }    border="rounded"   >
        <text   bold>Count: <span color="10">{ n+1 }</span></text>


     <button onPress={func(){ set(n+1)
     fmt.Println( "x" ) }}>inc</button>
  </box>)
}
`
	want := `package p

import "fmt"

func App() gox.Node {
	n, set := gox.UseState(0)
	return (
		<box padding={1} border="rounded">
			<text bold>Count: <span color="10">{n + 1}</span></text>

			<button onPress={func() {
				set(n + 1)
				fmt.Println("x")
			}}>inc</button>
		</box>
	)
}
`
	checkFormat(t, in, want)
}

func TestFormatCases(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"self closing and empty", "\treturn <box></box>\n", "\treturn <box />\n"},
		{"empty with newline", "\treturn <box>\n\t</box>\n", "\treturn <box />\n"},
		{"fragment", "\treturn <>  a  </>\n", "\treturn <>  a  </>\n"},
		{"no parens multi-line", "\treturn <box>\n<text>a</text>\n</box>\n", "\treturn <box>\n\t\t<text>a</text>\n\t</box>\n"},
		{"parens single line", "\treturn (\n<text>a</text>\n)\n", "\treturn (<text>a</text>)\n"},
		{"attributes over lines", "\treturn <box\npadding={1}\n  hidden />\n", "\treturn <box\n\t\tpadding={1}\n\t\thidden\n\t/>\n"},
		{"spread and element attr", "\treturn <show {...p}   fallback={<text>no</text>}/>\n", "\treturn <show {...p} fallback={<text>no</text>} />\n"},
		{"generic tag", "\treturn <List[int]  items={xs}></List>\n", "\treturn <List[int] items={xs} />\n"},
		{"comment child", "\treturn <box>{/* c */}</box>\n", "\treturn <box>{/* c */}</box>\n"},
		{"significant whitespace stays", "\treturn <text> a <b>x</b> c </text>\n", "\treturn <text> a <b>x</b> c </text>\n"},
		{"leading space kept on tag line", "\treturn <text> Hello\n   world</text>\n", "\treturn <text> Hello\n\t\tworld\n\t</text>\n"},
		{"trailing comment in expression", "\treturn <text>{x // c\n}</text>\n", "\treturn <text>\n\t\t{x // c\n\t\t}\n\t</text>\n"},
		{"call argument", "\tgox.Run(<App\n/>)\n\treturn nil\n", "\tgox.Run(<App />)\n\treturn nil\n"},
		{"entities verbatim", "\treturn <text>a &amp; b</text>\n", "\treturn <text>a &amp; b</text>\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			checkFormat(t, wrap(c.in), wrap(c.want))
		})
	}
}

func TestFormatErrors(t *testing.T) {
	for _, in := range []string{
		"\treturn <box></text>\n",              // mismatched tag
		"\treturn <box a={1 +}></box>\n",       // Go syntax error
		"\treturn <box /* c */ a={1}></box>\n", // comment in tag
	} {
		if _, err := Format([]byte(wrap(in))); err == nil {
			t.Errorf("expected error for %q", in)
		}
	}
}

func TestFormatExamplesStable(t *testing.T) {
	files, _ := filepath.Glob("../examples/*/*.gox")
	if len(files) == 0 {
		t.Skip("no examples")
	}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		out, err := Format(src)
		if err != nil {
			t.Errorf("%s: %v", f, err)
			continue
		}
		if string(out) != string(src) {
			t.Errorf("%s is not formatted:\n%s", f, out)
		}
	}
}

func TestSameProgram(t *testing.T) {
	a := wrap("\treturn <text>a b</text>\n")
	if err := sameProgram([]byte(a), []byte(strings.Replace(a, "a b", "a  b", 1))); err == nil {
		t.Error("whitespace change inside text must be detected")
	}
	if err := sameProgram([]byte(a), []byte(strings.Replace(a, "<text>a b", "<text>\n\t\ta b\n\t", 1))); err != nil {
		t.Errorf("layout-only change rejected: %v", err)
	}
}

func TestClosingTag(t *testing.T) {
	cases := []struct {
		src  string // | marks the cursor
		want string
	}{
		{"var _ = <box padding={1}>|", "</box>"},
		{"var _ = <box padding={1}>|\nvar y = 1", "</box>"},
		{"var _ = <List[int] items={xs}>|", "</List>"},
		{"var _ = <ui.Card>|", "</ui.Card>"},
		{"var _ = <>|", "</>"},
		{"var _ = <box />|", ""},
		{"var _ = a >|", ""},
		{"var _ = <box a={x >|", ""},
		{"var _ = <box><text>|</box>", "</text>"},
		{"var _ = <box>|</box>", ""},              // already balanced
		{"var _ = <box>|</box>\nvar y = <x>", ""}, // closing tag already follows
		{"var _ = <box><text>hi</text></|", "box>"},
		{"var _ = <box><text>hi</|", "text>"},
		{"var _ = <box></|box>", ""},
		{"var _ = a </|", ""},
	}
	for _, c := range cases {
		off := strings.Index(c.src, "|")
		src := "package p\n" + c.src[:off] + c.src[off+1:]
		if got := ClosingTag([]byte(src), off+len("package p\n")); got != c.want {
			t.Errorf("%q: got %q, want %q", c.src, got, c.want)
		}
	}
}
