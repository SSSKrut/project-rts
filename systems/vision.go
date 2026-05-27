package systems

import (
	"math"
	"time"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// Trace capped at one chunk (64 m); per-seer pass walks only the 3×3 chunk
// window. Parallel: each seer writes its own Awareness ring; candidate /
// wall snapshots are read-only across workers.
const (
	visionMaxRange     float32 = 64.0
	visionEyeHeight    float32 = 1.5
	visionTargetHeight float32 = 0.9 // torso centre at standing
)

// Audio detection: emission radius = base * pace * posture, clamped at
// visionMaxRange. Walls don't attenuate audio in the current skeleton.
const (
	audioBaseRadius   float32 = 25.0
	audioPostureQuiet float32 = 0.4
)

var audioPaceMul = [...]float32{
	components.PaceWalk:   1.0,
	components.PaceRun:    1.5,
	components.PaceSprint: 2.0,
}

// VisionSystem updates each unit's Awareness.LastSeen ring with every unit
// it can see (range + cone + LOS) plus an audio channel: candidates inside
// their own emission bubble register even outside FOV or behind a wall
// ("I hear running boots near me").
type VisionSystem struct {
	unitFilter         *ecs.Filter5[components.Unit, components.WorldPos, components.Motion, components.Vision, components.Awareness]
	wallFilter         *ecs.Filter2[components.WorldPos, components.WallSegment]
	doorMap            *ecs.Map[components.Door]
	pool               *core.WorkerPool
	squadMemberMap     *ecs.Map[components.SquadMember]
	movementProfileMap *ecs.Map[components.MovementProfile]
	motionMap          *ecs.Map[components.Motion]

	unitsBuf     []visionUnit
	seersBuf     []seerWork
	wallsByChunk map[components.ChunkCoord][]losWall

	elapsed float32
}

// NewVisionSystem. nil pool falls back to serial execution.
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
	return core.LODPolicy{
		ActiveEvery:   500 * time.Millisecond,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

// visionUnit is a per-tick snapshot of a candidate target.
type visionUnit struct {
	ent         ecs.Entity
	pos         components.WorldPos
	chunk       components.ChunkCoord
	audioRadius float32 // emission bubble (m); 0 = silent
}

// seerWork is the per-seer snapshot row for the parallel pass.
type seerWork struct {
	ent    ecs.Entity
	pos    components.WorldPos
	yaw    float32
	vision *components.Vision
	aware  *components.Awareness
}

func (sys *VisionSystem) Update(ctx core.UpdateContext) {
	sys.elapsed += float32(ctx.Delta.Seconds())

	// Two shapes: candidates are positional only (small, copied), seers
	// carry pointers to Awareness for the write.
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

	// Bucket walls by chunk for O(1) 9-chunk lookup per seer. clear() keeps
	// the underlying buckets to skip per-tick map allocation.
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
	// AngleDot = cos(half-FOV); 0 = 180° cone, -1 = 360°.

	rng := s.vision.RangeM
	if rng > visionMaxRange {
		rng = visionMaxRange
	}
	rngSq := rng * rng

	for _, cand := range units {
		if cand.ent == s.ent {
			continue
		}
		// 3×3 chunk window — outside = automatic miss.
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
		// Audio gate: emission bubble reaches the seer regardless of FOV /
		// LOS. Quiet + crouch keeps the bubble small for stealth doctrine.
		audibleSq := cand.audioRadius * cand.audioRadius
		if cand.audioRadius > 0 && dSq <= audibleSq {
			recordSighting(s.aware, cand.ent, cand.pos, elapsed)
			continue
		}
		if dSq > rngSq {
			continue
		}
		d := float32(math.Sqrt(float64(dSq)))
		invD := 1 / d
		dotF := (dx*fwdX + dz*fwdZ) * invD
		if dotF < s.vision.AngleDot {
			continue
		}
		if anyLosWallBlocks(localWalls, seerX, seerZ, candX, candZ) {
			continue
		}
		recordSighting(s.aware, cand.ent, cand.pos, elapsed)
	}
}

// audioEmissionRadius returns the noise-bubble radius. Stationary units
// (Motion.Speed below threshold) emit zero.
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

// recordSighting refreshes the FIFO entry for `target`, evicting the oldest
// slot for new targets.
func recordSighting(aware *components.Awareness, target ecs.Entity, pos components.WorldPos, t float32) {
	for i := range aware.LastSeen {
		if aware.LastSeen[i].Time != 0 && aware.LastSeen[i].Target == target {
			aware.LastSeen[i].Pos = pos
			aware.LastSeen[i].Time = t
			return
		}
	}
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
