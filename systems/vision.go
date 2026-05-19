package systems

import (
	"math"
	"time"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// Phase 7 P9: Vision system caps trace length at one chunk (64 m) and walks
// only the 3x3-chunk window around the seer.
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

// Phase 15 M15.A.3 - audio detection constants. Per-candidate emission radius
// is base * pace * posture (clamped at visionMaxRange so it shares the chunk-
// window cap with the visual cone). Walls do not attenuate audio in the
// skeleton; full propagation polish is deferred.
const (
	audioBaseRadius   float32 = 25.0
	audioPostureQuiet float32 = 0.4
)

// audioPaceMul indexes pace -> emission multiplier. Walk is the silent
// reference, Sprint roughly doubles the bubble.
var audioPaceMul = [...]float32{
	components.PaceWalk:   1.0,
	components.PaceRun:    1.5,
	components.PaceSprint: 2.0,
}

// VisionSystem updates each unit's Awareness.LastSeen ring with every other
// unit it can see (range + cone + LOS). Phase 15 M15.A.3 adds an audio
// channel: candidates within their own emission bubble (Pace * Posture)
// register on the seer's Awareness even outside the FOV cone or behind a
// wall, modelling "I hear running boots near me".
type VisionSystem struct {
	unitFilter         *ecs.Filter5[components.Unit, components.WorldPos, components.Motion, components.Vision, components.Awareness]
	wallFilter         *ecs.Filter2[components.WorldPos, components.WallSegment]
	doorMap            *ecs.Map[components.Door]
	pool               *core.WorkerPool
	squadMemberMap     *ecs.Map[components.SquadMember]
	movementProfileMap *ecs.Map[components.MovementProfile]
	motionMap          *ecs.Map[components.Motion]

	// Phase 11.6 M11.6.2: reusable snapshot buffers + wall map. unitsBuf and
	// seersBuf are reset to len=0 each Update; wallsByChunk is reused via
	// clear() so capacity persists.
	unitsBuf     []visionUnit
	seersBuf     []seerWork
	wallsByChunk map[components.ChunkCoord][]losWall

	elapsed float32
}

// NewVisionSystem wires the system with a worker pool. nil pool falls back to
// serial execution.
func NewVisionSystem(pool *core.WorkerPool) *VisionSystem {
	return &VisionSystem{
		pool:         pool,
		unitsBuf:     make([]visionUnit, 0, 64),
		seersBuf:     make([]seerWork, 0, 64),
		wallsByChunk: make(map[components.ChunkCoord][]losWall, 32),
	}
}

func (sys *VisionSystem) InitUI(w *ecs.World) {
	sys.unitFilter = ecs.NewFilter5[components.Unit, components.WorldPos, components.Motion, components.Vision, components.Awareness](w)
	sys.wallFilter = ecs.NewFilter2[components.WorldPos, components.WallSegment](w)
	sys.doorMap = ecs.NewMap[components.Door](w)
	sys.squadMemberMap = ecs.NewMap[components.SquadMember](w)
	sys.movementProfileMap = ecs.NewMap[components.MovementProfile](w)
	sys.motionMap = ecs.NewMap[components.Motion](w)
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

// visionUnit - per-tick snapshot of a candidate target.
type visionUnit struct {
	ent          ecs.Entity
	pos          components.WorldPos
	chunk        components.ChunkCoord
	audioRadius  float32 // Phase 15 M15.A.3 - emission bubble (m). 0 = silent.
}

// seerWork - per-seer snapshot row for the parallel pass.
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
	sys.unitsBuf = sys.unitsBuf[:0]
	sys.seersBuf = sys.seersBuf[:0]
	q := sys.unitFilter.Query()
	for q.Next() {
		ent := q.Entity()
		_, pos, mot, vision, aware := q.Get()
		audio := sys.audioEmissionRadius(ent, mot.Speed)
		sys.unitsBuf = append(sys.unitsBuf, visionUnit{
			ent: ent, pos: *pos, chunk: pos.Chunk, audioRadius: audio,
		})
		sys.seersBuf = append(sys.seersBuf, seerWork{
			ent: ent, pos: *pos, yaw: mot.Yaw,
			vision: vision, aware: aware,
		})
	}
	units := sys.unitsBuf
	seers := sys.seersBuf

	// Snapshot LOS walls by chunk so the raycast pass can pull the 9 chunks
	// around any seer in O(1). clear() empties the map but keeps the
	// underlying buckets, so we skip the per-tick map allocation that the
	// pre-11.6 build paid.
	clear(sys.wallsByChunk)
	qW := sys.wallFilter.Query()
	for qW.Next() {
		pos, w := qW.Get()
		doorState := components.DoorClosed
		if d := sys.doorMap.Get(qW.Entity()); d != nil {
			doorState = d.State
		}
		sys.wallsByChunk[pos.Chunk] = append(sys.wallsByChunk[pos.Chunk], makeLosWall(*pos, *w, doorState))
	}

	elapsed := sys.elapsed
	wallsByChunk := sys.wallsByChunk
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
	// AngleDot = cos(half-FOV). 0 = full 180 deg cone; -1 = full 360 deg.

	rng := s.vision.RangeM
	if rng > visionMaxRange {
		rng = visionMaxRange
	}
	rngSq := rng * rng

	for _, cand := range units {
		if cand.ent == s.ent {
			continue
		}
		// 3x3 chunk window - anything outside is automatic miss.
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
		if dSq < 1e-4 {
			continue
		}
		// Phase 15 M15.A.3 - audio gate. A loud candidate's emission bubble
		// reaches the seer regardless of FOV cone or LOS. Quiet movement +
		// crouch keeps the bubble small so stealth doctrine actually pays off.
		audibleSq := cand.audioRadius * cand.audioRadius
		if cand.audioRadius > 0 && dSq <= audibleSq {
			recordSighting(s.aware, cand.ent, cand.pos, elapsed)
			continue
		}
		if dSq > rngSq {
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

// audioEmissionRadius returns the noise-bubble radius (m) for `unit`. Reads
// the squad's MovementProfile if present (Posture + Pace), otherwise treats
// the unit as moving at Walk + Standard. Stationary units (Motion.Speed
// below threshold) emit zero so a parked unit doesn't unconditionally
// register on every nearby seer's Awareness.
func (sys *VisionSystem) audioEmissionRadius(unit ecs.Entity, motionSpeed float32) float32 {
	if motionSpeed < 0.2 {
		return 0
	}
	pace := components.PaceWalk
	posture := components.PostureStandard
	if sm := sys.squadMemberMap.Get(unit); sm != nil && sm.Squad != (ecs.Entity{}) {
		if mp := sys.movementProfileMap.Get(sm.Squad); mp != nil {
			pace = mp.Pace
			posture = mp.Posture
		}
	}
	mul := audioPaceMul[pace]
	if posture == components.PostureQuiet {
		mul *= audioPostureQuiet
	}
	r := audioBaseRadius * mul
	if r > visionMaxRange {
		r = visionMaxRange
	}
	return r
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
