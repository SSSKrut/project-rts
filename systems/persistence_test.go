package systems

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"

	"rts-go/components"
)

func heightPattern(seed float32) *[persistHeightCount]float32 {
	var h [persistHeightCount]float32
	for i := range h {
		h[i] = seed + float32(i)*0.25
	}
	return &h
}

func TestChunkRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cc := components.ChunkCoord{X: 3, Z: -7}
	want := heightPattern(1.5)

	if err := WriteChunk(dir, cc, want); err != nil {
		t.Fatalf("write: %v", err)
	}
	var got [persistHeightCount]float32
	ok, err := ReadChunk(dir, cc, &got)
	if err != nil || !ok {
		t.Fatalf("read: ok=%v err=%v", ok, err)
	}
	if got != *want {
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("height %d: got %v, want %v", i, got[i], want[i])
			}
		}
	}
}

func TestChunkRoundTripSurvivesNegativeCoords(t *testing.T) {
	dir := t.TempDir()
	for _, cc := range []components.ChunkCoord{
		{X: 0, Z: 0}, {X: -1, Z: -1}, {X: -128, Z: 512}, {X: 1 << 20, Z: -(1 << 20)},
	} {
		h := heightPattern(float32(cc.X))
		if err := WriteChunk(dir, cc, h); err != nil {
			t.Fatalf("%+v write: %v", cc, err)
		}
		var got [persistHeightCount]float32
		ok, err := ReadChunk(dir, cc, &got)
		if err != nil || !ok {
			t.Fatalf("%+v read: ok=%v err=%v", cc, ok, err)
		}
		if got[0] != h[0] {
			t.Fatalf("%+v: first height %v, want %v", cc, got[0], h[0])
		}
	}
}

// Distinct chunks must never collide on one file — the name is the only key.
func TestChunkFilePathsAreDistinct(t *testing.T) {
	seen := map[string]components.ChunkCoord{}
	for _, cc := range []components.ChunkCoord{
		{X: 1, Z: 23}, {X: 12, Z: 3}, {X: -1, Z: -23}, {X: -12, Z: -3},
		{X: 1, Z: -23}, {X: -1, Z: 23},
	} {
		p := chunkFilePath("/save", cc)
		if prev, dup := seen[p]; dup {
			t.Fatalf("%+v and %+v share the path %s", prev, cc, p)
		}
		seen[p] = cc
	}
}

func TestChunkRoundTripPreservesSpecialFloats(t *testing.T) {
	dir := t.TempDir()
	cc := components.ChunkCoord{}
	var h [persistHeightCount]float32
	h[0] = float32(math.Inf(1))
	h[1] = float32(math.Inf(-1))
	h[2] = -0.0
	h[3] = 1e-38 // subnormal territory
	if err := WriteChunk(dir, cc, &h); err != nil {
		t.Fatalf("write: %v", err)
	}
	var got [persistHeightCount]float32
	if _, err := ReadChunk(dir, cc, &got); err != nil {
		t.Fatalf("read: %v", err)
	}
	if !math.IsInf(float64(got[0]), 1) || !math.IsInf(float64(got[1]), -1) {
		t.Errorf("infinities not preserved: %v %v", got[0], got[1])
	}
	if got[3] != h[3] {
		t.Errorf("small value %v became %v", h[3], got[3])
	}
}

func TestReadChunkTreatsAMissingFileAsPristine(t *testing.T) {
	var out [persistHeightCount]float32
	ok, err := ReadChunk(t.TempDir(), components.ChunkCoord{X: 9, Z: 9}, &out)
	if ok {
		t.Error("a missing chunk reported as loaded")
	}
	if err != nil {
		t.Errorf("a missing chunk is not an error, got %v", err)
	}
}

func TestWriteChunkLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	cc := components.ChunkCoord{X: 2, Z: 2}
	if err := WriteChunk(dir, cc, heightPattern(0)); err != nil {
		t.Fatalf("write: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "chunks"))
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Fatalf("temp file survived the write: %s", e.Name())
		}
	}
	if len(entries) != 1 {
		t.Fatalf("expected one chunk file, got %d", len(entries))
	}
}

func TestWriteChunkOverwrites(t *testing.T) {
	dir := t.TempDir()
	cc := components.ChunkCoord{X: 1, Z: 1}
	if err := WriteChunk(dir, cc, heightPattern(1)); err != nil {
		t.Fatalf("first write: %v", err)
	}
	second := heightPattern(99)
	if err := WriteChunk(dir, cc, second); err != nil {
		t.Fatalf("second write: %v", err)
	}
	var got [persistHeightCount]float32
	if _, err := ReadChunk(dir, cc, &got); err != nil {
		t.Fatalf("read: %v", err)
	}
	if got[0] != second[0] {
		t.Fatalf("stale height %v, want %v", got[0], second[0])
	}
}

func TestWrittenFileMatchesTheDocumentedLayout(t *testing.T) {
	dir := t.TempDir()
	cc := components.ChunkCoord{X: -5, Z: 11}
	if err := WriteChunk(dir, cc, heightPattern(2)); err != nil {
		t.Fatalf("write: %v", err)
	}
	raw, err := os.ReadFile(chunkFilePath(dir, cc))
	if err != nil {
		t.Fatalf("read raw: %v", err)
	}
	if len(raw) != persistFileSize {
		t.Fatalf("file size %d, want %d", len(raw), persistFileSize)
	}
	if string(raw[0:4]) != persistMagic {
		t.Errorf("magic %q", raw[0:4])
	}
	if v := binary.LittleEndian.Uint16(raw[4:6]); v != persistVersion {
		t.Errorf("version %d, want %d", v, persistVersion)
	}
	if f := binary.LittleEndian.Uint16(raw[6:8]); f != 0 {
		t.Errorf("flags %d, want 0 (reserved)", f)
	}
	if x := int32(binary.LittleEndian.Uint32(raw[8:12])); x != cc.X {
		t.Errorf("X %d, want %d", x, cc.X)
	}
	if z := int32(binary.LittleEndian.Uint32(raw[12:16])); z != cc.Z {
		t.Errorf("Z %d, want %d", z, cc.Z)
	}
}

// Every rejection path must report "not loaded" so the caller falls back to
// procgen instead of running on half-read heights.
func TestReadChunkRejectsCorruptFiles(t *testing.T) {
	cc := components.ChunkCoord{X: 4, Z: 4}

	cases := []struct {
		name    string
		corrupt func(raw []byte) []byte
	}{
		{"bad magic", func(raw []byte) []byte { copy(raw[0:4], "XXXX"); return raw }},
		{"future version", func(raw []byte) []byte {
			binary.LittleEndian.PutUint16(raw[4:6], persistVersion+1)
			return raw
		}},
		{"truncated", func(raw []byte) []byte { return raw[:len(raw)-8] }},
		{"trailing junk", func(raw []byte) []byte { return append(raw, 0, 0) }},
		{"coord mismatch", func(raw []byte) []byte {
			binary.LittleEndian.PutUint32(raw[8:12], uint32(int32(999)))
			return raw
		}},
		{"empty file", func(raw []byte) []byte { return nil }},
	}
	for _, c := range cases {
		dir := t.TempDir()
		if err := WriteChunk(dir, cc, heightPattern(3)); err != nil {
			t.Fatalf("%s: write: %v", c.name, err)
		}
		path := chunkFilePath(dir, cc)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: read raw: %v", c.name, err)
		}
		if err := os.WriteFile(path, c.corrupt(raw), 0o644); err != nil {
			t.Fatalf("%s: rewrite: %v", c.name, err)
		}

		var out [persistHeightCount]float32
		ok, err := ReadChunk(dir, cc, &out)
		if ok {
			t.Errorf("%s: reported a successful load", c.name)
		}
		if err == nil {
			t.Errorf("%s: no error reported", c.name)
		}
		if out != ([persistHeightCount]float32{}) {
			t.Errorf("%s: heights were partially written before the rejection", c.name)
		}
	}
}

func TestReadChunkDoesNotTouchOtherChunks(t *testing.T) {
	dir := t.TempDir()
	a := components.ChunkCoord{X: 1, Z: 0}
	b := components.ChunkCoord{X: 0, Z: 1}
	if err := WriteChunk(dir, a, heightPattern(10)); err != nil {
		t.Fatalf("write a: %v", err)
	}
	var out [persistHeightCount]float32
	ok, err := ReadChunk(dir, b, &out)
	if ok || err != nil {
		t.Fatalf("reading an unwritten neighbour: ok=%v err=%v", ok, err)
	}
}
