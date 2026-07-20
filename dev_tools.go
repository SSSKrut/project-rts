package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"strconv"
	"strings"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/core"
)

// simFatalState — a recovered sim panic. The world may be mid-mutation /
// query-locked, so the loop stops advancing AND rendering it; an error
// screen replaces the crash (dev-block: "survive errors").
type simFatalState struct {
	Msg      string
	Stack    []string
	DumpPath string
}

// devPanicAt (RTS_PANIC_AT=N) forces a panic once TickIndex reaches N —
// exercises the guard itself.
var devPanicAt, _ = strconv.ParseUint(os.Getenv("RTS_PANIC_AT"), 10, 64)

func guardedAdvance(app *core.App) (fatal *simFatalState) {
	defer func() {
		if r := recover(); r != nil {
			fatal = capturePanic(r)
		}
	}()
	if devPanicAt != 0 && app.TickIndex() >= devPanicAt {
		panic(fmt.Sprintf("RTS_PANIC_AT=%d test panic", devPanicAt))
	}
	app.Advance()
	return nil
}

func guardedStepOnce(app *core.App) (fatal *simFatalState) {
	defer func() {
		if r := recover(); r != nil {
			fatal = capturePanic(r)
		}
	}()
	app.StepOnce()
	return nil
}

func capturePanic(r any) *simFatalState {
	stack := debug.Stack()
	path := filepath.Join(os.TempDir(), fmt.Sprintf("rts-panic-%d.txt", os.Getpid()))
	_ = os.WriteFile(path, append([]byte(fmt.Sprintf("panic: %v\n\n", r)), stack...), 0o644)
	lines := strings.Split(string(stack), "\n")
	if len(lines) > 26 {
		lines = lines[:26]
	}
	return &simFatalState{
		Msg:      fmt.Sprintf("%v", r),
		Stack:    lines,
		DumpPath: path,
	}
}

func drawFatalScreen(f *simFatalState, font rl.Font) {
	rl.BeginDrawing()
	rl.ClearBackground(rl.Color{R: 26, G: 12, B: 14, A: 255})
	y := float32(40)
	line := func(s string, size float32, col rl.Color) {
		rl.DrawTextEx(font, s, rl.Vector2{X: 40, Y: y}, size, 1, col)
		y += size + 8
	}
	line("SIM PANIC - simulation halted", 30, rl.Color{R: 240, G: 120, B: 110, A: 255})
	line(f.Msg, 20, rl.RayWhite)
	line("full stack: "+f.DumpPath, 16, rl.Color{R: 205, G: 205, B: 160, A: 255})
	y += 10
	for _, ln := range f.Stack {
		line(ln, 12, rl.Color{R: 170, G: 175, B: 185, A: 255})
	}
	rl.EndDrawing()
}

// devComponentDump — raw reflect view of every component on `ent`, one line
// per component. Read-only; long values truncated.
func devComponentDump(w *ecs.World, ent ecs.Entity) []string {
	if ent == (ecs.Entity{}) || !w.Alive(ent) {
		return nil
	}
	u := w.Unsafe()
	ids := u.IDs(ent)
	out := make([]string, 0, ids.Len())
	for i := 0; i < ids.Len(); i++ {
		id := ids.Get(i)
		info, ok := ecs.ComponentInfo(w, id)
		if !ok {
			continue
		}
		t := info.Type
		if t.Size() == 0 {
			out = append(out, t.Name())
			continue
		}
		v := reflect.NewAt(t, u.GetUnchecked(ent, id)).Elem()
		s := fmt.Sprintf("%s %+v", t.Name(), v.Interface())
		if len(s) > 160 {
			s = s[:160] + "..."
		}
		out = append(out, s)
	}
	return out
}
