package systems

import (
	"math"
	"time"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// Phase 7 P9: Vision system caps trace length at one chunk (64 m) and walks
// only the 3×3-chunk window around the seer.
//
// Phase 11.5 M11.5.3 / M11.5.5: tier-gating removed and the per-seer pass is
// dispatched through WorkerPool.ParallelFor. Each seer writes its own
// Awareness ring; the candidate / wall snapshots are read-only across
// workers.
const (
	visionMaxRange     float32 = 64.0
	visionEyeHeight    float32 = 1.5
	visionTargetHeight float32 = 0.9 // approx torso centre at standing height
)

// VisionSystem updates each unit's Awareness.LastSeen ring with every other
// unit it can see (range + cone + LOS). Phase 7 leaves the buffer as pure
// data; consumers arrive in Phase 10 (TacticalAI) and Phase 11 (combat).
type VisionSystem struct {
	unitFilter *ecs.Filter5[components.Unit, components.WorldPos, components.Motion, components.Vision, components.Awareness]
	wallFilter *ecs.Filter2[components.WorldPos, components.WallSegment]
	doorMap    *ecs.Map[components.Door]
	pool       *core.WorkerPool
	elapsed    float32
}

// NewVisionSystem wires the system with a worker pool. nil pool falls back to
// serial execution.
func NewVisionSystem(pool *core.WorkerPool) *VisionSystem {
	return &VisionSystem{pool: pool}
}

func (sys *VisionSystem) InitUI(w *ecs.World) {
	sys.unitFilter = ecs.NewFilter5[components.Unit, components.WorldPos, components.Motion, components.Vision, components.Awareness](w)
	sys.wallFilter = ecs.NewFilter2[components.WorldPos, components.WallSegment](w)
	sys.doorMap = ecs.NewMap[components.Door](w)
}

func (VisionSystem) Name() string { return "vision" }

func (VisionSystem) LODPolicy() core.LODPolicy {
	// Phase 11.5 P1: universal sim, single interval. 500 ms matches the prior
	// Active cadence; Relevant/Dormant tiers no longer exist for this system.
	return core.LODPolicy{
		ActiveEvery:   500 * time.Millisecond,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

// visionUnit — per-tick snapshot of a candidate target.
type visionUnit struct {
	ent   ecs.Entity
	pos   components.WorldPos
	chunk components.ChunkCoord
}

// seerWork — per-seer snapshot row for the parallel pass.
type seerWork struct {
	ent    ecs.Entity
	pos    components.WorldPos
	yaw    float32
	vision *components.Vision
	aware  *components.Awareness
}

func (sys *VisionSystem) Update(ctx core.UpdateContext) {
	sys.elapsed += float32(ctx.Delta.Seconds())

	// Snapshot every unit as both a candidate target and a seer. We need two
	// shapes because candidates are positional only (small struct, copied)
	// whereas seers carry pointers to Awareness for the write.
	var units []visionUnit
	var seers []seerWork
	q := sys.unitFilter.Query()
	for q.Next() {
		_, pos, mot, vision, aware := q.Get()
		units = append(units, visionUnit{ent: q.Entity(), pos: *pos, chunk: pos.Chunk})
		seers = append(seers, seerWork{
			ent: q.Entity(), pos: *pos, yaw: mot.Yaw,
			vision: vision, aware: aware,
		})
	}

	// Snapshot LOS walls by chunk so the raycast pass can pull the 9 chunks
	// around any seer in O(1).
	wallsByChunk := map[components.ChunkCoord][]losWall{}
	qW := sys.wallFilter.Query()
	for qW.Next() {
		pos, w := qW.Get()
		doorState := components.DoorClosed
		if d := sys.doorMap.Get(qW.Entity()); d != nil {
			doorState = d.State
		}
		wallsByChunk[pos.Chunk] = append(wallsByChunk[pos.Chunk], makeLosWall(*pos, *w, doorState))
	}

	elapsed := sys.elapsed
	sys.pool.ParallelFor(len(seers), func(start, end int) {
		for i := start; i < end; i++ {
			s := seers[i]
			processVisionSeer(s, units, wallsByChunk, elapsed)
		}
	})
}

func processVisionSeer(
	s seerWork,
	units []visionUnit,
	wallsByChunk map[components.ChunkCoord][]losWall,
	elapsed float32,
) {
	// Pull 9-chunk wall window.
	var localWalls []losWall
	for dz := int32(-1); dz <= 1; dz++ {
		for dx := int32(-1); dx <= 1; dx++ {
			nb := components.ChunkCoord{X: s.pos.Chunk.X + dx, Z: s.pos.Chunk.Z + dz}
			localWalls = append(localWalls, wallsByChunk[nb]...)
		}
	}

	seerX := float32(s.pos.Chunk.X)*components.ChunkSize + s.pos.Local.X
	seerZ := float32(s.pos.Chunk.Z)*components.ChunkSize + s.pos.Local.Z
	fwdX := float32(math.Sin(float64(s.yaw)))
	fwdZ := float32(math.Cos(float64(s.yaw)))
	// AngleDot = cos(half-FOV). 0 = full 180° cone; -1 = full 360°.

	rng := s.vision.RangeM
	if rng > visionMaxRange {
		rng = visionMaxRange
	}
	rngSq := rng * rng

	for _, cand := range units {
		if cand.ent == s.ent {
			continue
		}
		// 3×3 chunk window — anything outside is automatic miss.
		dcx := cand.chunk.X - s.pos.Chunk.X
		if dcx < -1 || dcx > 1 {
			continue
		}
		dcz := cand.chunk.Z - s.pos.Chunk.Z
		if dcz < -1 || dcz > 1 {
			continue
		}
		candX := float32(cand.pos.Chunk.X)*components.ChunkSize + cand.pos.Local.X
		candZ := float32(cand.pos.Chunk.Z)*components.ChunkSize + cand.pos.Local.Z
		dx := candX - seerX
		dz := candZ - seerZ
		dSq := dx*dx + dz*dz
		if dSq > rngSq || dSq < 1e-4 {
			continue
		}
		// Angle gate.
		d := float32(math.Sqrt(float64(dSq)))
		invD := 1 / d
		dotF := (dx*fwdX + dz*fwdZ) * invD
		if dotF < s.vision.AngleDot {
			continue
		}
		// Wall raycast.
		if anyLosWallBlocks(localWalls, seerX, seerZ, candX, candZ) {
			continue
		}
		recordSighting(s.aware, cand.ent, cand.pos, elapsed)
	}
}

// recordSighting pushes (or refreshes) a target entry in the FIFO. Existing
// entries for the same target get their position + time updated in place; new
// targets evict the oldest slot.
func recordSighting(aware *components.Awareness, target ecs.Entity, pos components.WorldPos, t float32) {
	// Refresh existing.
	for i := range aware.LastSeen {
		if aware.LastSeen[i].Time != 0 && aware.LastSeen[i].Target == target {
			aware.LastSeen[i].Pos = pos
			aware.LastSeen[i].Time = t
			return
		}
	}
	// Find empty slot or oldest.
	oldest := 0
	oldestT := aware.LastSeen[0].Time
	for i := 1; i < components.AwarenessSlots; i++ {
		if aware.LastSeen[i].Time == 0 {
			oldest = i
			break
		}
		if aware.LastSeen[i].Time < oldestT {
			oldestT = aware.LastSeen[i].Time
			oldest = i
		}
	}
	aware.LastSeen[oldest] = components.AwarenessEntry{Target: target, Pos: pos, Time: t}
}
