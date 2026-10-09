package gox_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rknit/tui.gox/gox"
	"github.com/rknit/tui.gox/gox/goxtest"
)

func TestMouseBuiltins(t *testing.T) {
	presses, picked := 0, -1
	root := gox.C0(func() gox.Node {
		v, set := gox.UseState("abc")
		return box(gox.BoxProps{},
			gox.C(gox.Input, gox.InputProps{Value: v, OnChange: set, Prompt: "> "}),
			gox.C(gox.Button, gox.ButtonProps{OnPress: func() { presses++ }, Children: "ok"}),
			gox.C(gox.Checkbox, gox.CheckboxProps{Children: "check"}),
			gox.C(gox.Select, gox.SelectProps{Options: []string{"one", "two", "three"}, OnSelect: func(i int) { picked = i }}),
		)
	})
	d := goxtest.New(root, 30, 10)
	defer d.Close()

	d.ClickText("ok").ClickText("ok")
	if presses != 2 {
		t.Errorf("presses=%d", presses)
	}
	d.ClickText("check")
	if !strings.Contains(d.View(), "[x] check") {
		t.Errorf("checkbox not toggled:\n%s", d.View())
	}
	d.ClickText("three")
	if !strings.Contains(d.View(), "❯ three") || picked != -1 {
		t.Errorf("select click: picked=%d\n%s", picked, d.View())
	}
	d.ClickText("three")
	if picked != 2 {
		t.Errorf("second click should select, picked=%d", picked)
	}
	x, y := d.Find("two")
	d.Wheel(x, y, -1)
	if !strings.Contains(d.View(), "❯ two") {
		t.Errorf("wheel did not move select:\n%s", d.View())
	}
	// Clicking into the input focuses it and moves the cursor before "b".
	d.Click(3, 0).Type("X")
	if !strings.HasPrefix(d.View(), "> aXbc") {
		t.Errorf("input click:\n%s", d.View())
	}
}

func TestMouseBubbling(t *testing.T) {
	var log []string
	root := box(gox.BoxProps{Border: "single", OnMouse: func(e gox.MouseEvent) bool {
		log = append(log, fmt.Sprintf("box %d,%d", e.X, e.Y))
		return true
	}},
		gox.C(gox.Text, gox.TextProps{Children: "pass", OnMouse: func(e gox.MouseEvent) bool {
			log = append(log, fmt.Sprintf("text %d,%d", e.X, e.Y))
			return false
		}}),
		gox.C(gox.Text, gox.TextProps{Children: "stop", OnMouse: func(gox.MouseEvent) bool {
			log = append(log, "stop")
			return true
		}}),
	)
	d := goxtest.New(root, 20, 5)
	defer d.Close()
	d.Click(2, 1).Click(1, 2).Click(10, 2).Click(15, 4)
	want := "text 1,0|box 2,1|stop|box 10,2"
	if got := strings.Join(log, "|"); got != want {
		t.Errorf("got %s\nwant %s", got, want)
	}
}

func lines(n int) gox.Node {
	out := make(gox.Fragment, n)
	for i := range out {
		out[i] = text(fmt.Sprintf("line %d", i))
	}
	return out
}

func TestScroll(t *testing.T) {
	root := gox.C(gox.Scroll, gox.ScrollProps{Height: 3, AutoFocus: true, Children: lines(10)})
	d := goxtest.New(root, 12, 5)
	defer d.Close()
	expect(t, d.View(), "line 0     ┃\nline 1     │\nline 2     │")
	d.Press("down", "down")
	expect(t, d.View(), "line 2     │\nline 3     ┃\nline 4     │")
	d.Press("end")
	expect(t, d.View(), "line 7     │\nline 8     │\nline 9     ┃")
	d.Wheel(0, 0, -2)
	expect(t, d.View(), "line 1     ┃\nline 2     │\nline 3     │")
	d.Press("pgdown")
	expect(t, d.View(), "line 3     │\nline 4     ┃\nline 5     │")
}

func TestScrollFillsAndFollows(t *testing.T) {
	var add func()
	root := gox.C0(func() gox.Node {
		n, set := gox.UseState(3)
		add = func() { set(n + 1) }
		return box(gox.BoxProps{Height: 4},
			text("header"),
			gox.C(gox.Scroll, gox.ScrollProps{FollowBottom: true, NoScrollbar: true, Children: lines(n)}),
		)
	})
	d := goxtest.New(root, 12, 10)
	defer d.Close()
	expect(t, d.View(), "header\nline 0\nline 1\nline 2")
	add()
	d.Send(struct{}{})
	expect(t, d.View(), "header\nline 1\nline 2\nline 3")
}

func TestRenderEffect(t *testing.T) {
	runs, cleanups := 0, 0
	root := gox.C0(func() gox.Node {
		n, set := gox.UseState(0)
		gox.UseRenderEffect(func() func() { runs++; return func() { cleanups++ } })
		gox.UseInput(func(gox.Key) bool { set(n + 1); return true })
		return text(n)
	})
	d := goxtest.New(root, 10, 2)
	d.Press("a", "b") // initial render + resize + 2 updates
	if runs != 4 || cleanups != 3 {
		t.Errorf("runs=%d cleanups=%d", runs, cleanups)
	}
	d.Close()
	if cleanups != 4 {
		t.Errorf("cleanups after close=%d", cleanups)
	}
}

func TestIntervalSeesFreshState(t *testing.T) {
	root := gox.C0(func() gox.Node {
		ticks, set := gox.UseState([]int{})
		gox.UseInterval(time.Millisecond, func() { set(append(ticks, len(ticks))) })
		return text(len(ticks))
	})
	d := goxtest.New(root, 10, 2)
	defer d.Close()
	d.Settle(30 * time.Millisecond) // many ticks queued before one flush
	if v := d.View(); v == "0" || v == "1" {
		t.Errorf("ticks lost: %s", v)
	}
}
