package lsp

import (
	"net/url"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// text indexes a document for LSP position (UTF-16) <-> byte offset
// conversion.
type text struct {
	s     string
	lines []int // byte offset of each line start
}

func newText(s string) *text {
	t := &text{s: s, lines: []int{0}}
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			t.lines = append(t.lines, i+1)
		}
	}
	return t
}

// offset converts a line and UTF-16 character to a byte offset, clamping
// out-of-range values.
func (t *text) offset(line, char int) int {
	if line < 0 {
		return 0
	}
	if line >= len(t.lines) {
		return len(t.s)
	}
	off := t.lines[line]
	end := len(t.s)
	if line+1 < len(t.lines) {
		end = t.lines[line+1] - 1
	}
	for n := 0; off < end && n < char; {
		r, size := utf8.DecodeRuneInString(t.s[off:])
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
		off += size
	}
	return off
}

// position converts a byte offset to a line and UTF-16 character.
func (t *text) position(off int) (line, char int) {
	off = min(max(off, 0), len(t.s))
	lo, hi := 0, len(t.lines)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if t.lines[mid] <= off {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	for i := t.lines[lo]; i < off; {
		r, size := utf8.DecodeRuneInString(t.s[i:])
		if r >= 0x10000 {
			char += 2
		} else {
			char++
		}
		i += size
	}
	return lo, char
}

func uriToPath(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return ""
	}
	p := u.Path
	// file:///C:/x on Windows.
	if len(p) >= 3 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	return filepath.FromSlash(p)
}

func pathToURI(path string) string {
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}
