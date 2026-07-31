package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
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
var fpsFlag = flag.Int("fps", 60, "target FPS; 0 = uncapped (for render profiling)")
var memProfileFlag = flag.String("memprofile", "", "dev: write a heap profile at exit")

var saveAtDone bool

// writeMemProfile dumps a live-heap profile at clean shutdown. Runs GC first so
// the profile shows what is actually retained, not garbage awaiting collection.
func writeMemProfile() {
	if *memProfileFlag == "" {
		return
	}
	f, err := os.Create(*memProfileFlag)
	if err != nil {
		fmt.Printf("memprofile: %v\n", err)
		return
	}
	defer f.Close()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	const mb = 1024 * 1024
	fmt.Printf("[mem] pre-GC HeapAlloc=%.1f MB  post-GC HeapAlloc=%.1f MB  TotalAlloc=%.1f MB  Sys=%.1f MB  NumGC=%d\n",
		float64(before.HeapAlloc)/mb, float64(after.HeapAlloc)/mb,
		float64(after.TotalAlloc)/mb, float64(after.Sys)/mb, after.NumGC)
	if err := pprof.WriteHeapProfile(f); err != nil {
		fmt.Printf("memprofile: %v\n", err)
	}
}

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
	defer writeMemProfile()

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
