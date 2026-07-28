package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/core"
	"rts-go/systems"
)

const (
	initialScreenWidth  int32 = 800 * 2
	initialScreenHeight int32 = 450 * 2
)

// workersFlag picks the worker-pool size. 0 (default) -> runtime.NumCPU().
var workersFlag = flag.Int("workers", 0, "worker pool size (default = NumCPU)")

var replayHashFlag = flag.String("replay-hash", "", "write per-100-tick state hashes to path (headless scenes)")

var saveAtFlag = flag.Uint64("save-at", 0, "write world snapshot at tick N (0 = off)")
var savePathFlag = flag.String("save-path", "save/snapshot.rtss", "snapshot path for -save-at")
var loadFlag = flag.String("load", "", "load world snapshot and continue")
var runTicksFlag = flag.Uint64("run-ticks", 0, "headless: exit after tick N (overrides verdict exit)")

var saveAtDone bool

func maybeSaveAt(app *core.App) {
	if *saveAtFlag == 0 || saveAtDone || app.TickIndex() < *saveAtFlag {
		return
	}
	saveAtDone = true
	meta := systems.SaveMeta{
		MapName:    worldMap.Name,
		SimNow:     app.Elapsed().Seconds(),
		TickIndex:  app.TickIndex(),
		FrameIndex: uint64(app.FrameIndex()),
	}
	if err := systems.SaveWorld(app.World, *savePathFlag, meta); err != nil {
		fmt.Printf("save: %v\n", err)
		return
	}
	fmt.Printf("save: wrote %s at tick %d\n", *savePathFlag, app.TickIndex())
}

func quicksavePath() string {
	return filepath.Join(systems.SaveDir, "quick.rtss")
}

// relaunchWithLoad execs the binary with -load: in-place reload would need a
// full system teardown/reboot; exec is the honest prototype path (Linux).
func relaunchWithLoad(app *core.App, path string) {
	exe, err := os.Executable()
	if err != nil {
		fmt.Printf("quickload: %v\n", err)
		return
	}
	systems.FlushModifiedChunks(app.World, systems.SaveDir)
	rl.CloseWindow()
	if err := syscall.Exec(exe, []string{exe, "-load=" + path}, os.Environ()); err != nil {
		fmt.Printf("quickload: exec: %v\n", err)
	}
}

var (
	replayHasher   *systems.ReplayHasher
	replayHashFile *os.File
	replayLastTick = ^uint64(0)
)

var replayHashEvery uint64

func writeReplayHash(app *core.App) {
	path := *replayHashFlag
	if replayHashEvery == 0 {
		replayHashEvery = 100
		if v, err := strconv.ParseUint(os.Getenv("RTS_HASH_EVERY"), 10, 64); err == nil && v > 0 {
			replayHashEvery = v
		}
	}
	if path == "" || app.TickIndex()%replayHashEvery != 0 || app.TickIndex() == replayLastTick {
		return
	}
	if replayHasher == nil {
		replayHasher = systems.NewReplayHasher(app.World)
		f, err := os.Create(path)
		if err != nil {
			fmt.Printf("replay-hash: %v\n", err)
			*replayHashFlag = ""
			return
		}
		replayHashFile = f
	}
	replayLastTick = app.TickIndex()
	fmt.Fprintf(replayHashFile, "%d %016x\n", app.TickIndex(), replayHasher.Hash())
	if at, err := strconv.ParseUint(os.Getenv("RTS_DUMP_AT"), 10, 64); err == nil && at == app.TickIndex() {
		if err := replayHasher.DumpState(os.Getenv("RTS_DUMP_FILE")); err != nil {
			fmt.Printf("dump: %v\n", err)
		}
	}
}

func main() {
	flag.Parse()

	g := bootGame()
	defer g.Shutdown()

	g.registerSystems()

	g.spawnAnchorAndCamera()
	g.spawnWorldRoots()
	g.initGameplayHandles()
	g.spawnScene()
	g.loadSnapshot()
	g.initRenderHandles()

	g.initUI()
	defer g.shutdownUI()

	for !rl.WindowShouldClose() {
		if !g.RunFrame() {
			break
		}
	}
}
