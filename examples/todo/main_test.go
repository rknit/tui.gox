package main

import (
	"strings"
	"testing"

	"github.com/rknit/tui.gox/gox"
	"github.com/rknit/tui.gox/gox/goxtest"
)

func TestTodo(t *testing.T) {
	d := goxtest.New(gox.C0(App), 70, 30)
	defer d.Close()

	d.Type("buy milk").Press("enter")
	if v := d.View(); !strings.Contains(v, "[ ] buy milk") || !strings.Contains(v, "2 item(s) left") {
		t.Fatalf("todo not added:\n%s", v)
	}

	// Focus order: input, 3 checkboxes, clear button.
	d.Press("tab", "space", "tab", "tab", "tab", "enter")
	v := d.View()
	if strings.Contains(v, "Write a .gox component") || !strings.Contains(v, "buy milk") {
		t.Fatalf("clear done failed:\n%s", v)
	}
}
