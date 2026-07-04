package systems

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// SaveDir holds per-chunk binary blobs under chunks/{x}_{z}.bin. Set once at
// startup (per-map dir for -map runs). Future region-file migration touches
// only WriteChunk/ReadChunk; callers stay put.
var SaveDir = "./save/world-default"

// Binary format v1:
//
//	offset  size   field
//	     0     4   magic "RTSC"
//	     4     2   version uint16 LE  (currently 1)
//	     6     2   flags   uint16 LE  (reserved, 0)
//	     8     4   ChunkCoord.X int32 LE
//	    12     4   ChunkCoord.Z int32 LE
//	    16 16900   heights - ChunkResolution^2 x float32 LE, row-major by +Z
//
// Total = 16 916 bytes at ChunkResolution = 65. Bumping the format bumps the
// version: ReadChunk treats version mismatch as "not loadable" and callers
// fall back to procgen rather than crashing.
const (
	persistMagic       = "RTSC"
	persistVersion     = uint16(1)
	persistHeaderSize  = 16
	persistHeightCount = components.ChunkResolution * components.ChunkResolution
	persistHeightBytes = 4 * persistHeightCount
	persistFileSize    = persistHeaderSize + persistHeightBytes
)

func chunkFilePath(saveDir string, cc components.ChunkCoord) string {
	return filepath.Join(saveDir, "chunks", fmt.Sprintf("%d_%d.bin", cc.X, cc.Z))
}

// WriteChunk serializes heights atomically (tmp + rename). No fsync —
// eviction-time durability isn't worth the syscall cost. Heights taken by
// pointer to avoid copying ~17 KB per call.
func WriteChunk(saveDir string, cc components.ChunkCoord, heights *[persistHeightCount]float32) error {
	dir := filepath.Join(saveDir, "chunks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("persistence: mkdir %s: %w", dir, err)
	}

	buf := make([]byte, persistFileSize)
	copy(buf[0:4], persistMagic)
	binary.LittleEndian.PutUint16(buf[4:6], persistVersion)
	binary.LittleEndian.PutUint16(buf[6:8], 0)
	binary.LittleEndian.PutUint32(buf[8:12], uint32(cc.X))
	binary.LittleEndian.PutUint32(buf[12:16], uint32(cc.Z))
	off := persistHeaderSize
	for i := 0; i < persistHeightCount; i++ {
		binary.LittleEndian.PutUint32(buf[off:off+4], math.Float32bits(heights[i]))
		off += 4
	}

	path := chunkFilePath(saveDir, cc)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf, 0o644); err != nil {
		return fmt.Errorf("persistence: write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("persistence: rename %s -> %s: %w", tmp, path, err)
	}
	return nil
}

// ReadChunk loads heights into *out.
//
//   - file missing    → (false, nil); pristine, not an error.
//   - corrupt/version → (false, err); caller falls back to procgen.
//   - success         → (true, nil).
func ReadChunk(saveDir string, cc components.ChunkCoord, out *[persistHeightCount]float32) (bool, error) {
	path := chunkFilePath(saveDir, cc)
	buf, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("persistence: read %s: %w", path, err)
	}
	if len(buf) != persistFileSize {
		return false, fmt.Errorf("persistence: %s: size %d, expected %d", path, len(buf), persistFileSize)
	}
	if string(buf[0:4]) != persistMagic {
		return false, fmt.Errorf("persistence: %s: bad magic", path)
	}
	if v := binary.LittleEndian.Uint16(buf[4:6]); v != persistVersion {
		return false, fmt.Errorf("persistence: %s: unsupported version %d", path, v)
	}
	gotX := int32(binary.LittleEndian.Uint32(buf[8:12]))
	gotZ := int32(binary.LittleEndian.Uint32(buf[12:16]))
	if gotX != cc.X || gotZ != cc.Z {
		return false, fmt.Errorf("persistence: %s: coord mismatch (%d,%d) vs expected (%d,%d)", path, gotX, gotZ, cc.X, cc.Z)
	}
	off := persistHeaderSize
	for i := 0; i < persistHeightCount; i++ {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(buf[off : off+4]))
		off += 4
	}
	return true, nil
}

// FlushModifiedChunks writes every live Heightmap+Modified chunk. Called on
// clean shutdown so unflushed changes survive a restart.
func FlushModifiedChunks(w *ecs.World, saveDir string) {
	filter := ecs.NewFilter3[components.ChunkCoord, components.Heightmap, components.Modified](w)
	q := filter.Query()
	for q.Next() {
		cc, hm, _ := q.Get()
		if err := WriteChunk(saveDir, *cc, &hm.Heights); err != nil {
			fmt.Printf("FlushModifiedChunks: %v: %v\n", *cc, err)
		}
	}
}
