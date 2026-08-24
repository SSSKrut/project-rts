package systems

import (
	"time"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// Detection meter (Detection 2.0): signal magnitude m (falloff × facing ×
// concealment × motion) fills Detectability.Meter at detectFillRate×m² per
// second under clear LOS; a sighting fires when the meter reaches 1. m=1 is
// instant, m=0.5 ≈ 0.5 s, m=0.15 ≈ seconds of grace for a still prone.
const (
	detectFillRate     float32 = 8.0
	detectDecayPerSec  float32 = 0.5
	detectMinMagnitude float32 = 0.05
	detectStillMul     float32 = 0.5
	detectRunMul       float32 = 1.3
	detectStillSpeed   float32 = 0.2
	detectRunSpeed     float32 = 2.5
)

// contactCloseRangeM is the per-unit close-LOS radius that promotes a
// contact's Source to CloseRangeID and exposes ground-truth Affil/Dim. One
// shared constant on Phase 18.5; later phases can dimension-tune.
const contactCloseRangeM float32 = 25.0

// contactCap bounds the live Contact population (WS-E ш.2). Oldest Unknown
// contacts are evicted first; player-classified pins are never evicted.
const contactCap = 128

// ContactSystem absorbs Phase 7 VisionSystem. One pass does sensor math
// (effRange × facing × concealment) + audio detection (legacy carry-over) +
// Awareness writeback (preserves the contract WeaponSystem / UtilityEvaluator
// rely on) + Contact upsert for non-PlayerFaction targets.
//
// Post-passes promote Source by CombatEvidence (ThreatSource match) and
// CloseRangeID (close LOS) without downgrading PlayerClassified.
type ContactSystem struct {
	unitFilter    *ecs.Filter6[components.Unit, components.WorldPos, components.Motion, components.Sensors, components.Awareness, components.Faction]
	vehFilter     *ecs.Filter6[components.Vehicle, components.WorldPos, components.Motion, components.Sensors, components.Awareness, components.Faction]
	airFilter     *ecs.Filter6[components.Aircraft, components.WorldPos, components.Motion, components.Sensors, components.Awareness, components.Faction]
	wallFilter    *ecs.Filter2[components.WorldPos, components.WallSegment]
	contactFilter *ecs.Filter1[components.Contact]
	threatFilter  *ecs.Filter2[components.ThreatSource, components.WorldPos]
	smokeFilter   *ecs.Filter2[components.SmokeField, components.WorldPos]

	doorMap            *ecs.Map[components.Door]
	stanceMap          *ecs.Map[components.Stance]
	factionMap         *ecs.Map[components.Faction]
	posMap             *ecs.Map[components.WorldPos]
	contactMap         *ecs.Map[components.Contact]
	contactPlayerSet   *ecs.Map[components.ContactPlayerSet]
	squadMemberMap     *ecs.Map[components.SquadMember]
	movementProfileMap *ecs.Map[components.MovementProfile]
	weaponMap          *ecs.Map[components.Weapon]
	equipMap           *ecs.Map[components.Equipment]

	registryRes *ecs.Resource[components.ContactRegistry]
	eventLogRes ecs.Resource[components.EventLog]
	indexRes    ecs.Resource[TerrainChunkIndex]
	hmMap       *ecs.Map[components.Heightmap]
	dtMap       *ecs.Map[components.Detectability]
	pool        *core.WorkerPool
	worldRef    *ecs.World

	// Per-tick scratch — reset with [:0] / clear() at top of Update.
	unitsBuf     []contactUnit
	smokeBuf     []smokeVol
	seersBuf     []contactSeer
	wallsByChunk map[components.ChunkCoord][]losWall
	heightmaps   map[components.ChunkCoord][]float32
	groupsBuf    []detectGroup
	groupIdx     map[ecs.Entity]int32
	contactsBuf  []contactRec
	emitBuf      []emitterRec
	// Per-worker collectors (indexed by ParallelForIndexed chunkIdx),
	// drained in worker order → deterministic contactsBuf ordering.
	workerContacts [][]contactRec
	workerMeter    [][]meterEvent
	meterEvents    []meterEvent
	meterFill      [][components.FactionCount]float32
	meterSrc       [][components.FactionCount]int32

	elapsed float32
}

// smokeConcealMul multiplies a target's concealment when it stands inside a
// live SmokeField — dropped by the SmokeAndReverse reflex to break contact.
const smokeConcealMul float32 = 0.35

// smokeVol is a per-tick snapshot of a live smoke field (world XZ + squared
// radius) used to attenuate concealment.
type smokeVol struct {
	x, z, rSq float32
}

// contactUnit is the per-tick candidate snapshot.
type contactUnit struct {
	ent         ecs.Entity
	pos         components.WorldPos
	chunk       components.ChunkCoord
	x, z        float32 // world XZ
	targetY     float32 // stance-aware silhouette Y for terrain-LOS
	faction     uint8
	dimMask     components.DimensionMask
	concealment float32 // stance × motion, lower = harder to spot
	audioRadius float32
	emitRange   float32 // how far an ESM receiver hears this one radiating
	meter       [components.FactionCount]float32
	det         *components.Detectability // serial apply target; nil = instant
}

// airWallClearM is the vertical separation past which wall segments stop
// occluding. losWall is a 2D XZ record with no height, so the alternative to a
// threshold is walls that block the sky. Set above the tallest building the
// generators produce (5 storeys), which is why a man on an upper floor still
// shoots through windows and a helicopter at 30 m is still masked by a facade.
const airWallClearM float32 = 20.0

// meterEvent: one group's best observation of a target this tick; serial
// apply takes the max fill per (target, faction) and fires the sighting on
// meter crossing.
type meterEvent struct {
	group    int32
	spotter  int32
	target   int32
	faction  uint8
	closeLOS bool
	fill     float32
}

// contactSeer is the per-tick observer snapshot.
type contactSeer struct {
	ent        ecs.Entity
	pos        components.WorldPos
	x, z       float32
	eyeY       float32
	fwdX, fwdZ float32
	yaw        float32
	faction    uint8
	sensors    *components.Sensors
	aware      *components.Awareness
	maxRange   float32 // max BaseRange across channels — used for cull
}

// detectGroup is one squad (or one solo unit) processed as a unit: one cull
// gate per candidate, up to two member-origins per LOS attempt, sightings
// shared to every member.
type detectGroup struct {
	members                    []int32 // indices into seersBuf
	cx, cz, cullR              float32
	span                       int32 // chunk radius shared by the wall window and the pair gate
	minCX, maxCX, minCZ, maxCZ int32 // member chunk bounds for the wall window
}

// contactRec is one detection event from the detect pass; the serial post-
// pass consumes these to upsert contact entities. Kept separate from the
// parallel awareness write so contact creation stays single-threaded (Ark
// archetype changes are not concurrency-safe).
type contactRec struct {
	observer  ecs.Entity
	target    ecs.Entity
	pos       components.WorldPos
	dim       components.Dimension
	closeLOS  bool
	obsFactID uint8
}

func NewContactSystem(pool *core.WorkerPool) *ContactSystem {
	workers := 1
	if pool != nil && pool.Workers() > 0 {
		workers = pool.Workers()
	}
	return &ContactSystem{
		pool:           pool,
		unitsBuf:       make([]contactUnit, 0, 64),
		seersBuf:       make([]contactSeer, 0, 64),
		wallsByChunk:   make(map[components.ChunkCoord][]losWall, 32),
		heightmaps:     make(map[components.ChunkCoord][]float32, 64),
		groupsBuf:      make([]detectGroup, 0, 16),
		groupIdx:       make(map[ecs.Entity]int32, 16),
		contactsBuf:    make([]contactRec, 0, 64),
		workerContacts: make([][]contactRec, workers),
		workerMeter:    make([][]meterEvent, workers),
	}
}

func (sys *ContactSystem) InitUI(w *ecs.World) {
	sys.worldRef = w
	sys.unitFilter = ecs.NewFilter6[components.Unit, components.WorldPos, components.Motion, components.Sensors, components.Awareness, components.Faction](w)
	sys.vehFilter = ecs.NewFilter6[components.Vehicle, components.WorldPos, components.Motion, components.Sensors, components.Awareness, components.Faction](w)
	sys.airFilter = ecs.NewFilter6[components.Aircraft, components.WorldPos, components.Motion, components.Sensors, components.Awareness, components.Faction](w)
	sys.wallFilter = ecs.NewFilter2[components.WorldPos, components.WallSegment](w)
	sys.contactFilter = ecs.NewFilter1[components.Contact](w)
	sys.threatFilter = ecs.NewFilter2[components.ThreatSource, components.WorldPos](w)
	sys.smokeFilter = ecs.NewFilter2[components.SmokeField, components.WorldPos](w)
	sys.doorMap = ecs.NewMap[components.Door](w)
	sys.stanceMap = ecs.NewMap[components.Stance](w)
	sys.factionMap = ecs.NewMap[components.Faction](w)
	sys.posMap = ecs.NewMap[components.WorldPos](w)
	sys.contactMap = ecs.NewMap[components.Contact](w)
	sys.contactPlayerSet = ecs.NewMap[components.ContactPlayerSet](w)
	sys.squadMemberMap = ecs.NewMap[components.SquadMember](w)
	sys.movementProfileMap = ecs.NewMap[components.MovementProfile](w)
	sys.weaponMap = ecs.NewMap[components.Weapon](w)
	sys.equipMap = ecs.NewMap[components.Equipment](w)
	r := ecs.NewResource[components.ContactRegistry](w)
	sys.registryRes = &r
	sys.eventLogRes = ecs.NewResource[components.EventLog](w)
	sys.indexRes = ecs.NewResource[TerrainChunkIndex](w)
	sys.hmMap = ecs.NewMap[components.Heightmap](w)
	sys.dtMap = ecs.NewMap[components.Detectability](w)
}

func (ContactSystem) Name() string { return "contact" }

func (ContactSystem) LODPolicy() core.LODPolicy {
	// Active-only: the filter is not tier-scoped, a second tier doubles work.
	return core.LODPolicy{
		ActiveEvery:   250 * time.Millisecond,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

func (sys *ContactSystem) Update(ctx core.UpdateContext) {
	sys.elapsed = float32(ctx.SimNow)
	sys.runDetectPass(float32(ctx.Delta.Seconds()))
	sys.runEmitterPass()
	sys.applyContactUpsert()
	sys.applyEmitterUpsert()
	sys.applyCombatEvidence()
}
