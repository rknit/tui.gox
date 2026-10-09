package editors_test

import (
	"os"
	"testing"
)

// The Neovim plugin ships a copy of the tree-sitter highlight query.
func TestHighlightQueryInSync(t *testing.T) {
	a, err := os.ReadFile("tree-sitter-gox/queries/highlights.scm")
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile("nvim/queries/gox/highlights.scm")
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatal("editors/nvim/queries/gox/highlights.scm differs from editors/tree-sitter-gox/queries/highlights.scm; copy it over")
	}
}
