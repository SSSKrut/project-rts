package systems

import (
	"time"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// detectionThreshold gates effective-range strength; sensor curves output a
// 0..1 magnitude, post-multiplier ≥ 0.5 + LOS + dimension match = detected.
const detectionThreshold float32 = 0.5

// contactCloseRangeM is the per-unit close-LOS radius that promotes a
// contact's Source to CloseRangeID and exposes ground-truth Affil/Dim. One
// shared constant on Phase 18.5; later phases can dimension-tune.
const contactCloseRangeM float32 = 25.0

// ContactSystem absorbs Phase 7 VisionSystem. One pass does sensor math
// (effRange × facing × concealment) + audio detection (legacy carry-over) +
// Awareness writeback (preserves the contract WeaponSystem / UtilityEvaluator
// rely on) + Contact upsert for non-PlayerFaction targets.
//
// Post-passes promote Source by CombatEvidence (ThreatSource match) and
// CloseRangeID (close LOS) without downgrading PlayerClassified.
type ContactSystem struct {
	unitFilter    *ecs.Filter6[components.Unit, components.WorldPos, components.Motion, components.Sensors, components.Awareness, components.Faction]
	wallFilter    *ecs.Filter2[components.WorldPos, components.WallSegment]
	contactFilter *ecs.Filter1[components.Contact]
	threatFilter  *ecs.Filter2[components.ThreatSource, components.WorldPos]

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
	pool        *core.WorkerPool
	worldRef    *ecs.World

	// Per-tick scratch — reset with [:0] / clear() at top of Update.
	unitsBuf     []contactUnit
	seersBuf     []contactSeer
	wallsByChunk map[components.ChunkCoord][]losWall
	contactsBuf  []contactRec

	elapsed float32
}

// contactUnit is the per-tick candidate snapshot.
type contactUnit struct {
	ent         ecs.Entity
	pos         components.WorldPos
	chunk       components.ChunkCoord
	faction     uint8
	dimMask     components.DimensionMask
	concealment float32 // 0..1, lower = harder to spot
	audioRadius float32
}

// contactSeer is the per-tick observer snapshot.
type contactSeer struct {
	ent      ecs.Entity
	pos      components.WorldPos
	yaw      float32
	faction  uint8
	sensors  *components.Sensors
	aware    *components.Awareness
	maxRange float32 // max BaseRange across channels — used for cull
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
	return &ContactSystem{
		pool:         pool,
		unitsBuf:     make([]contactUnit, 0, 64),
		seersBuf:     make([]contactSeer, 0, 64),
		wallsByChunk: make(map[components.ChunkCoord][]losWall, 32),
		contactsBuf:  make([]contactRec, 0, 64),
	}
}

func (sys *ContactSystem) InitUI(w *ecs.World) {
	sys.worldRef = w
	sys.unitFilter = ecs.NewFilter6[components.Unit, components.WorldPos, components.Motion, components.Sensors, components.Awareness, components.Faction](w)
	sys.wallFilter = ecs.NewFilter2[components.WorldPos, components.WallSegment](w)
	sys.contactFilter = ecs.NewFilter1[components.Contact](w)
	sys.threatFilter = ecs.NewFilter2[components.ThreatSource, components.WorldPos](w)
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
}

func (ContactSystem) Name() string { return "contact" }

func (ContactSystem) LODPolicy() core.LODPolicy {
	// Active-only: unitFilter is not tier-scoped, so the Active pass already
	// covers every unit. A second Relevant-tier call would re-run the full
	// detect pass and double-advance the elapsed clock (FoW fade at 2×).
	return core.LODPolicy{
		ActiveEvery:   250 * time.Millisecond,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

func (sys *ContactSystem) Update(ctx core.UpdateContext) {
	sys.elapsed += float32(ctx.Delta.Seconds())
	sys.runDetectPass()
	sys.applyContactUpsert()
	sys.applyCombatEvidence()
}
