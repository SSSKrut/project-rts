package systems

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"unsafe"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

type saveReader struct {
	b   []byte
	off int
	err error
}

func (r *saveReader) fail(what string) {
	if r.err == nil {
		r.err = fmt.Errorf("snapshot truncated at %s (off=%d)", what, r.off)
	}
}

func (r *saveReader) bytes(n int, what string) []byte {
	if r.err != nil || r.off+n > len(r.b) {
		r.fail(what)
		return nil
	}
	out := r.b[r.off : r.off+n]
	r.off += n
	return out
}

func (r *saveReader) u16(what string) uint16 {
	b := r.bytes(2, what)
	if b == nil {
		return 0
	}
	return binary.LittleEndian.Uint16(b)
}

func (r *saveReader) u32(what string) uint32 {
	b := r.bytes(4, what)
	if b == nil {
		return 0
	}
	return binary.LittleEndian.Uint32(b)
}

func (r *saveReader) u64(what string) uint64 {
	b := r.bytes(8, what)
	if b == nil {
		return 0
	}
	return binary.LittleEndian.Uint64(b)
}

func (r *saveReader) f64(what string) float64 { return math.Float64frombits(r.u64(what)) }

func (r *saveReader) str(what string) string {
	n := int(r.u16(what))
	b := r.bytes(n, what)
	if b == nil {
		return ""
	}
	return string(b)
}

func (r *saveReader) header() (SaveMeta, error) {
	var meta SaveMeta
	if string(r.bytes(4, "magic")) != saveMagic {
		return meta, fmt.Errorf("snapshot: bad magic")
	}
	if v := r.u16("version"); v != saveVersion {
		return meta, fmt.Errorf("snapshot: version %d, want %d", v, saveVersion)
	}
	r.u16("flags")
	meta.SimNow = r.f64("simNow")
	meta.TickIndex = r.u64("tick")
	meta.FrameIndex = r.u64("frame")
	meta.MapName = r.str("mapName")
	return meta, r.err
}

// LoadSnapshotMeta reads only the header — main.go needs MapName before the
// world boots.
func LoadSnapshotMeta(path string) (SaveMeta, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SaveMeta{}, err
	}
	r := &saveReader{b: data}
	return r.header()
}

// LoadWorld restores a snapshot into a freshly booted world (resources +
// InitUI done, zero entities spawned). Component IDs are translated file ->
// live by type name via the save_spec registrars; blobs are re-added in file
// order so table row order matches the writer (P3).
func LoadWorld(w *ecs.World, path string) (SaveMeta, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SaveMeta{}, err
	}
	r := &saveReader{b: data}
	meta, err := r.header()
	if err != nil {
		return meta, err
	}
	u := w.Unsafe()

	r.u64("schemaHash")
	schemaCount := int(r.u16("schemaCount"))
	liveID := make([]ecs.ID, schemaCount)
	sizes := make([]int, schemaCount)
	for i := 0; i < schemaCount; i++ {
		name := r.str("schemaName")
		size := int(r.u32("schemaSize"))
		if r.err != nil {
			return meta, r.err
		}
		entry, ok := saveComponents[name]
		if !ok {
			return meta, fmt.Errorf("snapshot: unknown component %s", name)
		}
		id := entry.LiveID(w)
		info, _ := ecs.ComponentInfo(w, id)
		if int(info.Type.Size()) != size {
			return meta, fmt.Errorf("snapshot: %s size %d, live %d — incompatible build", name, size, info.Type.Size())
		}
		liveID[i] = id
		sizes[i] = size
	}

	poolLen := int(r.u32("poolLen"))
	pool := make([]ecs.Entity, poolLen)
	if poolLen > 0 {
		src := r.bytes(poolLen*8, "pool")
		if src == nil {
			return meta, r.err
		}
		copy(unsafe.Slice((*byte)(unsafe.Pointer(&pool[0])), poolLen*8), src)
	}
	aliveLen := int(r.u32("aliveLen"))
	alive := make([]uint32, aliveLen)
	for i := range alive {
		alive[i] = r.u32("alive")
	}
	next := r.u32("next")
	available := r.u32("available")
	if r.err != nil {
		return meta, r.err
	}
	u.LoadEntities(&ecs.EntityDump{Entities: pool, Alive: alive, Next: next, Available: available})

	archCount := int(r.u32("archCount"))
	for a := 0; a < archCount; a++ {
		compCount := int(r.u16("compCount"))
		fids := make([]uint16, compCount)
		ids := make([]ecs.ID, compCount)
		for i := range fids {
			fids[i] = r.u16("fid")
			if int(fids[i]) >= schemaCount {
				r.fail("fid range")
				return meta, r.err
			}
			ids[i] = liveID[fids[i]]
		}
		entCount := int(r.u32("entCount"))
		ents := make([]ecs.Entity, entCount)
		for i := range ents {
			eid := r.u32("entID")
			if int(eid) >= poolLen {
				r.fail("entity id range")
				return meta, r.err
			}
			ents[i] = pool[eid]
		}
		if r.err != nil {
			return meta, r.err
		}
		if compCount > 0 {
			for _, e := range ents {
				u.Add(e, ids...)
			}
		}
		for ci := range ids {
			size := sizes[fids[ci]]
			if size == 0 {
				continue
			}
			for _, e := range ents {
				src := r.bytes(size, "blob")
				if src == nil {
					return meta, r.err
				}
				copy(unsafe.Slice((*byte)(u.GetUnchecked(e, ids[ci])), size), src)
			}
		}
	}

	if err := readResourceSection(w, r); err != nil {
		return meta, err
	}

	restoreSkippedComponents(w)
	return meta, nil
}

// restoreSkippedComponents re-adds skip-policy semantics: terrain chunks get
// a zero ChunkMesh + MeshDirty so the mesh pass rebuilds GPU state.
func restoreSkippedComponents(w *ecs.World) {
	u := w.Unsafe()
	meshID := ecs.ComponentID[components.ChunkMesh](w)
	dirtyID := ecs.ComponentID[components.MeshDirty](w)
	var chunks []ecs.Entity
	q := ecs.NewFilter1[components.TerrainChunk](w).Query()
	for q.Next() {
		chunks = append(chunks, q.Entity())
	}
	for _, e := range chunks {
		if !u.Has(e, meshID) {
			u.Add(e, meshID)
		}
		if !u.Has(e, dirtyID) {
			u.Add(e, dirtyID)
		}
	}
}

// PostLoadRebuild repopulates ResRebuild-class resources from live entities.
// Slice order matters for eviction determinism: filters iterate in table row
// order, which the loader restored to match the writer.
func PostLoadRebuild(w *ecs.World) {
	terrainRes := ecs.NewResource[TerrainChunkIndex](w)
	terrainIdx := terrainRes.Get()
	qc := ecs.NewFilter2[components.ChunkCoord, components.TerrainChunk](w).Query()
	for qc.Next() {
		cc, _ := qc.Get()
		terrainIdx.Loaded[components.ChunkCoord{X: cc.X, Z: cc.Z}] = qc.Entity()
	}

	propRes := ecs.NewResource[PropChunkIndex](w)
	propIdx := propRes.Get()
	qp := ecs.NewFilter2[components.Prop, components.WorldPos](w).Query()
	for qp.Next() {
		_, pos := qp.Get()
		propIdx.Loaded[pos.Chunk] = append(propIdx.Loaded[pos.Chunk], qp.Entity())
	}

	childRes := ecs.NewResource[BuildingChildIndex](w)
	childIdx := childRes.Get()
	qm := ecs.NewFilter1[components.BuildingMember](w).
		Without(ecs.C[components.Level]()).Query()
	for qm.Next() {
		m := qm.Get()
		childIdx.Loaded[m.Building] = append(childIdx.Loaded[m.Building], qm.Entity())
	}

	slotRes := ecs.NewResource[CoverSlotIndex](w)
	slotIdx := slotRes.Get()
	qs := ecs.NewFilter1[components.CoverSlot](w).Query()
	for qs.Next() {
		s := qs.Get()
		slotIdx.ByHost[s.Host] = append(slotIdx.ByHost[s.Host], qs.Entity())
	}

	contactRes := ecs.NewResource[components.ContactRegistry](w)
	contactReg := contactRes.Get()
	qk := ecs.NewFilter1[components.Contact](w).Query()
	for qk.Next() {
		c := qk.Get()
		contactReg.Tracked[c.Tracked] = qk.Entity()
	}

	planRes := ecs.NewResource[BuildingPlanIndex](w)
	planIdx := planRes.Get()
	plansRes := ecs.NewResource[components.BuildingPlanList](w)
	plans := plansRes.Get()
	rootI := 0
	qb := ecs.NewFilter1[components.Building](w).Query()
	for qb.Next() {
		if rootI < len(plans.Plans) {
			planIdx.Plans[qb.Entity()] = &plans.Plans[rootI]
		}
		rootI++
	}
	if rootI != len(plans.Plans) {
		fmt.Printf("load: %d building roots vs %d plans — map/manifest mismatch\n", rootI, len(plans.Plans))
	}
	ql := ecs.NewFilter2[components.Level, components.BuildingMember](w).Query()
	for ql.Next() {
		_, m := ql.Get()
		planIdx.Levels[m.Building] = append(planIdx.Levels[m.Building], ql.Entity())
	}

	if os.Getenv("RTS_DEBUG") != "" {
		fmt.Printf("[load] rebuild: chunks=%d propChunks=%d childRoots=%d slotHosts=%d contacts=%d planRoots=%d levelRoots=%d\n",
			len(terrainIdx.Loaded), len(propIdx.Loaded), len(childIdx.Loaded),
			len(slotIdx.ByHost), len(contactReg.Tracked), len(planIdx.Plans), len(planIdx.Levels))
	}
}
