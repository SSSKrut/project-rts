package systems

import (
	"bytes"
	"encoding/binary"
	"hash/fnv"
	"math"
	"os"
	"path/filepath"
	"unsafe"

	"github.com/mlange-42/ark/ecs"
)

const (
	saveMagic   = "RTSS"
	saveVersion = 1
)

// Pool round-trips through an unsafe []Entity cast; pin the layout.
var (
	_ [8 - unsafe.Sizeof(ecs.Entity{})]byte
	_ [unsafe.Sizeof(ecs.Entity{}) - 8]byte
)

type SaveMeta struct {
	MapName    string
	SimNow     float64
	TickIndex  uint64
	FrameIndex uint64
}

type saveBuf struct{ bytes.Buffer }

func (b *saveBuf) u16(v uint16) {
	var t [2]byte
	binary.LittleEndian.PutUint16(t[:], v)
	b.Write(t[:])
}

func (b *saveBuf) u32(v uint32) {
	var t [4]byte
	binary.LittleEndian.PutUint32(t[:], v)
	b.Write(t[:])
}

func (b *saveBuf) u64(v uint64) {
	var t [8]byte
	binary.LittleEndian.PutUint64(t[:], v)
	b.Write(t[:])
}

func (b *saveBuf) f64(v float64) { b.u64(math.Float64bits(v)) }

func (b *saveBuf) str(s string) {
	b.u16(uint16(len(s)))
	b.WriteString(s)
}

// SaveWorld writes a full-pool snapshot: entity pool verbatim, then
// per-archetype component blobs in Filter iteration order (= table row
// order), so the loader rebuilds tables with identical layout (P3).
func SaveWorld(w *ecs.World, path string, meta SaveMeta) error {
	if err := VerifySaveSpec(w); err != nil {
		return err
	}
	u := w.Unsafe()

	type schemaEntry struct {
		name string
		size uint32
	}
	var schema []schemaEntry
	fileID := make(map[uint8]uint16)
	for _, id := range ecs.ComponentIDs(w) {
		info, ok := ecs.ComponentInfo(w, id)
		if !ok {
			continue
		}
		pol, err := savePolicyFor(info.Type)
		if err != nil {
			return err
		}
		if pol == SaveSkip {
			continue
		}
		fileID[id.Index()] = uint16(len(schema))
		schema = append(schema, schemaEntry{info.Type.String(), uint32(info.Type.Size())})
	}

	type archBucket struct {
		fids []uint16
		lids []ecs.ID
		ents []ecs.Entity
	}
	var archs []*archBucket
	bySig := make(map[string]*archBucket)
	q := ecs.NewFilter0(w).Query()
	for q.Next() {
		e := q.Entity()
		ids := u.IDs(e)
		sig := make([]byte, 0, ids.Len()*2)
		fids := make([]uint16, 0, ids.Len())
		lids := make([]ecs.ID, 0, ids.Len())
		for i := 0; i < ids.Len(); i++ {
			id := ids.Get(i)
			fid, ok := fileID[id.Index()]
			if !ok {
				continue
			}
			sig = append(sig, byte(fid), byte(fid>>8))
			fids = append(fids, fid)
			lids = append(lids, id)
		}
		b := bySig[string(sig)]
		if b == nil {
			b = &archBucket{fids: fids, lids: lids}
			bySig[string(sig)] = b
			archs = append(archs, b)
		}
		b.ents = append(b.ents, e)
	}

	var out saveBuf
	out.WriteString(saveMagic)
	out.u16(saveVersion)
	out.u16(0)
	out.f64(meta.SimNow)
	out.u64(meta.TickIndex)
	out.u64(meta.FrameIndex)
	out.str(meta.MapName)

	var sch saveBuf
	sch.u16(uint16(len(schema)))
	for _, s := range schema {
		sch.str(s.name)
		sch.u32(s.size)
	}
	h := fnv.New64a()
	h.Write(sch.Bytes())
	out.u64(h.Sum64())
	out.Write(sch.Bytes())

	dump := u.DumpEntities()
	out.u32(uint32(len(dump.Entities)))
	if len(dump.Entities) > 0 {
		out.Write(unsafe.Slice((*byte)(unsafe.Pointer(&dump.Entities[0])), len(dump.Entities)*8))
	}
	out.u32(uint32(len(dump.Alive)))
	for _, a := range dump.Alive {
		out.u32(a)
	}
	out.u32(dump.Next)
	out.u32(dump.Available)

	out.u32(uint32(len(archs)))
	for _, b := range archs {
		out.u16(uint16(len(b.fids)))
		for _, f := range b.fids {
			out.u16(f)
		}
		out.u32(uint32(len(b.ents)))
		for _, e := range b.ents {
			out.u32(e.ID())
		}
		for ci, lid := range b.lids {
			size := uintptr(schema[b.fids[ci]].size)
			if size == 0 {
				continue
			}
			for _, e := range b.ents {
				ptr := u.GetUnchecked(e, lid)
				out.Write(unsafe.Slice((*byte)(ptr), size))
			}
		}
	}

	if err := appendResourceSection(w, &out); err != nil {
		return err
	}
	if err := SaveSymbologySidecar(w); err != nil {
		return err
	}

	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
