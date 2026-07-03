package systems

import (
	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// WeaponSystem fires one shot per ready unit per tick. Shot resolves through
// walls (LOS) and against any unit body crossing the ray; friendly fire is
// allowed by design.
//
// Pipeline:
//   1. Serial snapshot — walk seer filter, resolve targets via Awareness +
//      Faction gate, pre-commit Ammo / LastFiredAt.
//   2. Parallel raycast — per shot apply dispersion, test wall LOS + unit-
//      vs-ray in the 3×3 chunk window. Workers write only to per-worker
//      scratch buffers.
//   3. Serial post-pass — merge buffers; DamageService.Apply, ThreatSource
//      spawn, propagateSuppression, particle entities.
//
// Bodies split: weapon_fire_gate.go (shouldFire / pickTarget),
// weapon_resolve.go (resolveShot + ray math), weapon_postpass.go (splash /
// suppression / particle bursts).
type WeaponSystem struct {
	pool      *core.WorkerPool
	damage    *DamageService
	particles *SpawnParticleHandles

	seerFilter   *ecs.Filter6[components.Unit, components.WorldPos, components.Motion, components.Equipment, components.Awareness, components.Faction]
	targetFilter *ecs.Filter4[components.Unit, components.WorldPos, components.Stance, components.Faction]
	wallFilter   *ecs.Filter2[components.WorldPos, components.WallSegment]

	posMap      *ecs.Map[components.WorldPos]
	stanceMap   *ecs.Map[components.Stance]
	motionMap   *ecs.Map[components.Motion]
	weaponMap   *ecs.Map[components.Weapon]
	factionMap  *ecs.Map[components.Faction]
	threatMap   *ecs.Map[components.Threat]
	doorMap     *ecs.Map[components.Door]
	colliderMap *ecs.Map[components.Collider]
	// shouldFire walks SquadMember → Squad → EngagementRules / order flags.
	squadMemberMap             *ecs.Map[components.SquadMember]
	engagementRulesMap         *ecs.Map[components.EngagementRules]
	orderQueueMap              *ecs.Map[components.OrderQueueHead]
	orderAttackMoveMap         *ecs.Map[components.OrderParamAttackMove]
	orderKindMap               *ecs.Map[components.OrderKind]
	orderEngagementOverrideMap *ecs.Map[components.OrderParamEngagementOverride]
	// Utility Mode gate: Reloading and Suppressed silence the unit.
	blackboardMap *ecs.Map[components.LocalBlackboard]

	targetsBuf     []targetSnap
	shotsBuf       []shotWork
	wallsByChunk   map[components.ChunkCoord][]losWall
	targetsByChunk map[components.ChunkCoord][]int32

	// Per-worker scratch.
	workerDamage      [][]damageEvent
	workerTracer      [][]tracerSpec
	workerImpact      [][]impactSpec
	workerThreat      [][]threatEvent
	workerSuppression [][]suppressionEvent
	workerSplash      [][]splashEvent

	threatSourceMap *ecs.Map[components.ThreatSource]
	dangerBufMap    *ecs.Map[components.DangerBuffer]
	worldRef        *ecs.World

	spatialHash ecs.Resource[core.SpatialHash]

	elapsed  float32
	lastTick float32
}

// targetSnap is a read-only candidate-target snapshot. Stance + Faction
// inlined so the parallel pass skips component dereferences.
type targetSnap struct {
	ent     ecs.Entity
	pos     components.WorldPos
	chunk   components.ChunkCoord
	radius  float32
	stance  components.StanceCode
	faction uint8
}

// shotWork is one queued shot for the parallel raycast pass.
type shotWork struct {
	shooter      ecs.Entity
	muzzle       rl.Vector3
	aim          rl.Vector3
	dispersion   float32
	rangeMax     float32
	damage       float32
	targetEntity ecs.Entity
	targetStance components.StanceCode
	tracerColor  rl.Color
	shooterChunk components.ChunkCoord
	muzzlePos    components.WorldPos
	rngSeed      uint64 // deterministic per-shot RNG seed
	// AoE knobs: SplashRadius > 0 turns the shot into a splash event.
	splashRadius  float32
	splashFalloff float32
}

// hitKind classifies what a shot struck. Drives per-kind particle spawn.
type hitKind uint8

const (
	hitKindMiss hitKind = iota
	hitKindTerrain
	hitKindWall
	hitKindUnitFlag
)

// splashEvent is a per-shot splash request handed to the serial post-pass.
type splashEvent struct {
	pos      rl.Vector3
	radius   float32
	falloff  float32
	damage   float32
	excluded ecs.Entity // direct-hit target — skip to avoid double-count
}

type damageEvent struct {
	target ecs.Entity
	amount float32
}

type tracerSpec struct {
	From, To  rl.Vector3
	Color     rl.Color
	SpawnTime float32
	TTL       float32
}

type impactSpec struct {
	Pos       rl.Vector3
	Color     rl.Color
	SpawnTime float32
	TTL       float32
	Hit       hitKind
}

type threatEvent struct {
	origin   components.WorldPos
	severity float32
}

// suppressionEvent: per-impact propagation. hitMul = 0.5 on direct hit, 0.2
// on miss (distance-scaled in the post-pass).
type suppressionEvent struct {
	impact  rl.Vector3
	hitMul  float32
	shooter ecs.Entity
}

const (
	// Muzzle Y above foot — standing rifle fire at chest height; below
	// the 1.5 m eye height so muzzle flash sits below the role label.
	weaponEyeHeight float32 = 1.35
	// System-wide range cap. LOS window is 3 chunks = 192 m; this hard-cap
	// stops misconfigured Weapon.RangeM from raycasting across the map.
	weaponMaxRange float32 = 192.0
	// Drop awareness entries older than this when picking a firing target.
	weaponAwarenessMaxAge float32 = 3.0
	// Visual fade durations (seconds).
	weaponTracerTTL float32 = 0.15
	weaponImpactTTL float32 = 0.25
	// Fallback hit cylinder when a unit has no Collider component.
	weaponDefaultRadius float32 = 0.4
	// Suppression propagation radius.
	suppressionRadius float32 = 5.0
	// Direct hit / miss weights.
	suppressionHitMul  float32 = 0.5
	suppressionMissMul float32 = 0.2
	// ThreatSource entity lifetime (seconds).
	threatTTL float32 = 3.0
)

// NewWeaponSystem. `damage` and `particles` must both be non-nil.
func NewWeaponSystem(pool *core.WorkerPool, damage *DamageService, particles *SpawnParticleHandles) *WeaponSystem {
	workers := 1
	if pool != nil && pool.Workers() > 0 {
		workers = pool.Workers()
	}
	return &WeaponSystem{
		pool:              pool,
		damage:            damage,
		particles:         particles,
		targetsBuf:        make([]targetSnap, 0, 64),
		shotsBuf:          make([]shotWork, 0, 32),
		wallsByChunk:      make(map[components.ChunkCoord][]losWall, 32),
		targetsByChunk:    make(map[components.ChunkCoord][]int32, 32),
		workerDamage:      make([][]damageEvent, workers),
		workerTracer:      make([][]tracerSpec, workers),
		workerImpact:      make([][]impactSpec, workers),
		workerThreat:      make([][]threatEvent, workers),
		workerSuppression: make([][]suppressionEvent, workers),
		workerSplash:      make([][]splashEvent, workers),
	}
}

func (sys *WeaponSystem) InitUI(w *ecs.World) {
	sys.seerFilter = ecs.NewFilter6[components.Unit, components.WorldPos, components.Motion, components.Equipment, components.Awareness, components.Faction](w)
	sys.targetFilter = ecs.NewFilter4[components.Unit, components.WorldPos, components.Stance, components.Faction](w)
	sys.wallFilter = ecs.NewFilter2[components.WorldPos, components.WallSegment](w)

	sys.posMap = ecs.NewMap[components.WorldPos](w)
	sys.stanceMap = ecs.NewMap[components.Stance](w)
	sys.motionMap = ecs.NewMap[components.Motion](w)
	sys.weaponMap = ecs.NewMap[components.Weapon](w)
	sys.factionMap = ecs.NewMap[components.Faction](w)
	sys.threatMap = ecs.NewMap[components.Threat](w)
	sys.doorMap = ecs.NewMap[components.Door](w)
	sys.colliderMap = ecs.NewMap[components.Collider](w)
	sys.squadMemberMap = ecs.NewMap[components.SquadMember](w)
	sys.engagementRulesMap = ecs.NewMap[components.EngagementRules](w)
	sys.orderQueueMap = ecs.NewMap[components.OrderQueueHead](w)
	sys.orderAttackMoveMap = ecs.NewMap[components.OrderParamAttackMove](w)
	sys.orderKindMap = ecs.NewMap[components.OrderKind](w)
	sys.orderEngagementOverrideMap = ecs.NewMap[components.OrderParamEngagementOverride](w)
	sys.blackboardMap = ecs.NewMap[components.LocalBlackboard](w)
	sys.threatSourceMap = ecs.NewMap[components.ThreatSource](w)
	sys.dangerBufMap = ecs.NewMap[components.DangerBuffer](w)
	sys.worldRef = w

	sys.spatialHash = ecs.NewResource[core.SpatialHash](w)
}

func (WeaponSystem) Name() string { return "weapon" }

func (WeaponSystem) LODPolicy() core.LODPolicy {
	// Universal sim, every tick - matches Vision / UnitMovement cadence. RoF
	// gates the actual shot density per-weapon so the per-tick walk is cheap
	// when nothing is firing.
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

func (sys *WeaponSystem) Update(ctx core.UpdateContext) {
	sys.elapsed += float32(ctx.Delta.Seconds())
	now := sys.elapsed
	dt := now - sys.lastTick
	if dt < 0 || dt > 1.0 {
		// First tick or large gap (paused) - bound the decay so we don't
		// instantly clear Suppression on resume.
		dt = float32(ctx.Delta.Seconds())
	}
	sys.lastTick = now

	// Reset reusable buffers.
	sys.targetsBuf = sys.targetsBuf[:0]
	sys.shotsBuf = sys.shotsBuf[:0]
	clear(sys.wallsByChunk)
	clear(sys.targetsByChunk)
	for i := range sys.workerDamage {
		sys.workerDamage[i] = sys.workerDamage[i][:0]
		sys.workerTracer[i] = sys.workerTracer[i][:0]
		sys.workerImpact[i] = sys.workerImpact[i][:0]
		sys.workerThreat[i] = sys.workerThreat[i][:0]
		sys.workerSuppression[i] = sys.workerSuppression[i][:0]
		sys.workerSplash[i] = sys.workerSplash[i][:0]
	}

	// Phase 17 M17.0.2: Suppression decay moved out of WeaponSystem into
	// ThreatSystem (which now owns all per-channel decay). dt is no longer
	// consumed here but kept tracked for parity with future producers.
	_ = dt

	sys.snapshotTargetsAndWalls()
	sys.snapshotShots(now)

	if len(sys.shotsBuf) > 0 {
		sys.runParallelResolve(now)
	}

	sys.applyPostPass(now)
}

// snapshotTargetsAndWalls is the read-only snapshot phase: every live Unit
// becomes a candidate target (cached in targetsBuf + bucketed by chunk),
// every WallSegment becomes an LOS occluder. Both are reset at the top of
// Update; we just append here.
func (sys *WeaponSystem) snapshotTargetsAndWalls() {
	qT := sys.targetFilter.Query()
	for qT.Next() {
		_, pos, stance, fac := qT.Get()
		ent := qT.Entity()
		radius := weaponDefaultRadius
		if col := sys.colliderMap.Get(ent); col != nil && col.Radius > 0 {
			radius = col.Radius
		}
		idx := int32(len(sys.targetsBuf))
		sys.targetsBuf = append(sys.targetsBuf, targetSnap{
			ent: ent, pos: *pos, chunk: pos.Chunk,
			radius: radius, stance: stance.Code, faction: fac.ID,
		})
		sys.targetsByChunk[pos.Chunk] = append(sys.targetsByChunk[pos.Chunk], idx)
	}

	qW := sys.wallFilter.Query()
	for qW.Next() {
		pos, w := qW.Get()
		doorState := components.DoorClosed
		if d := sys.doorMap.Get(qW.Entity()); d != nil {
			doorState = d.State
		}
		sys.wallsByChunk[pos.Chunk] = append(sys.wallsByChunk[pos.Chunk],
			makeLosWall(*pos, *w, doorState))
	}
}

// snapshotShots walks every seer, gates each through RoE / RoF / pickTarget,
// and queues a shotWork for the parallel raycast pass. Ammo / LastFiredAt
// are pre-committed here so a paused parallel pass cannot double-fire.
func (sys *WeaponSystem) snapshotShots(now float32) {
	qS := sys.seerFilter.Query()
	for qS.Next() {
		_, pos, motion, eq, aware, fac := qS.Get()
		shooter := qS.Entity()
		weaponEnt := eq.Primary
		if weaponEnt == (ecs.Entity{}) {
			continue
		}
		weapon := sys.weaponMap.Get(weaponEnt)
		if weapon == nil || weapon.Ammo == 0 || weapon.RoF <= 0 || weapon.Damage == 0 {
			continue
		}
		// RoF cooldown gate.
		cooldown := 1.0 / weapon.RoF
		if weapon.LastFiredAt != 0 && now-weapon.LastFiredAt < cooldown {
			continue
		}
		// Pick a hostile target from awareness (most recent + alive + range).
		target, targetPos, ok := sys.pickTarget(shooter, fac.ID, pos, aware, weapon, now)
		if !ok {
			continue
		}
		// Phase 14 M14.3 - RoE + AttackMove gate. Skip silently (no ammo
		// decrement, no cooldown bump) so a HoldFire squad can resume fire
		// the instant the player flips the rule.
		if !sys.shouldFire(shooter, motion.Speed, pos, targetPos) {
			continue
		}
		targetStance := components.StanceStand
		if st := sys.stanceMap.Get(target); st != nil {
			targetStance = st.Code
		}

		// Effective dispersion (PHASE-14.md cross-cut note):
		//   eff = base * (1 + 0.3 * movingFactor + 0.5 * suppression)
		dispersion := weapon.Dispersion
		moving := float32(0)
		if motion.Speed > 1.0 {
			moving = 1
		}
		supp := float32(0)
		if sp := sys.threatMap.Get(shooter); sp != nil {
			supp = sp.Suppression
		}
		dispersion *= 1 + 0.3*moving + 0.5*supp

		muzzle := worldXYZ(*pos, weaponEyeHeight)
		aim := worldXYZ(targetPos, components.SpecForStance(targetStance).TargetCenterY)

		// Pre-commit the shot: bump cooldown and decrement ammo NOW so we
		// don't double-fire on a fresh `Get` next tick. Done before the
		// parallel pass because the workers don't write back to Weapon.
		weapon.LastFiredAt = now
		weapon.Ammo--

		// Phase 14.5 M14.5.5 - pull splash params from WeaponSpec.
		wspec := components.SpecForWeapon(weapon.Kind)
		sys.shotsBuf = append(sys.shotsBuf, shotWork{
			shooter:       shooter,
			muzzle:        muzzle,
			aim:           aim,
			dispersion:    dispersion,
			rangeMax:      clampWeaponRange(weapon.RangeM),
			damage:        float32(weapon.Damage),
			targetEntity:  target,
			targetStance:  targetStance,
			tracerColor:   tracerColorFor(weapon.Kind),
			shooterChunk:  pos.Chunk,
			muzzlePos:     *pos,
			rngSeed:       uint64(shooter.ID()) ^ uint64(now*1000.0),
			splashRadius:  wspec.SplashRadius,
			splashFalloff: wspec.SplashFalloff,
		})
	}
}

// runParallelResolve fans out resolveShot across the worker pool. Workers
// write only into their per-worker scratch slices; the merge happens in
// applyPostPass.
func (sys *WeaponSystem) runParallelResolve(now float32) {
	shots := sys.shotsBuf
	targets := sys.targetsBuf
	targetsByChunk := sys.targetsByChunk
	wallsByChunk := sys.wallsByChunk
	workerDamage := sys.workerDamage
	workerTracer := sys.workerTracer
	workerImpact := sys.workerImpact
	workerThreat := sys.workerThreat
	workerSuppression := sys.workerSuppression
	workerSplash := sys.workerSplash

	sys.pool.ParallelForIndexed(len(shots), func(wIdx, start, end int) {
		for i := start; i < end; i++ {
			resolveShot(&shots[i], targets, targetsByChunk, wallsByChunk, now,
				&workerDamage[wIdx], &workerTracer[wIdx], &workerImpact[wIdx],
				&workerThreat[wIdx], &workerSuppression[wIdx], &workerSplash[wIdx])
		}
	})
}

// applyPostPass merges every worker buffer back into shared state in serial:
// particle spawns through SpawnParticleHandles, damage through DamageService,
// ThreatSource entities through worldRef.NewEntity, suppression propagation
// through the spatial hash.
func (sys *WeaponSystem) applyPostPass(now float32) {
	// Phase 14.5 M14.5.4/M14.5.5: tracers / impacts / muzzle flashes are
	// ECS entities. Per-impact-kind dust/debris/smoke spawns layer on top.
	if sys.particles != nil {
		for w := range sys.workerTracer {
			for _, t := range sys.workerTracer[w] {
				sys.particles.SpawnTracer(t.From, t.To, t.Color, t.SpawnTime, t.TTL)
				// Muzzle flash at the From end of each tracer - short-lived
				// bright sphere, colour matches tracer hue.
				sys.particles.SpawnMuzzleFlash(t.From, t.Color, t.SpawnTime)
			}
			for _, im := range sys.workerImpact[w] {
				sys.particles.SpawnImpact(im.Pos, im.Color, im.SpawnTime, im.TTL)
				// Per-kind extras (Phase 14.5 M14.5.5).
				switch im.Hit {
				case hitKindTerrain:
					sys.spawnDustBurst(im.Pos, im.SpawnTime, 3, im.Color)
				case hitKindWall:
					sys.spawnDebrisBurst(im.Pos, im.SpawnTime, 4)
				}
			}
		}
		// Splash bursts: dense smoke + bigger debris cluster.
		for w := range sys.workerSplash {
			for _, ev := range sys.workerSplash[w] {
				sys.particles.SpawnSmoke(ev.pos, rl.Color{R: 90, G: 90, B: 90, A: 200}, now)
				sys.spawnDebrisBurst(ev.pos, now, 10)
			}
		}
	}
	// Phase 14.5 M14.5.5: apply splash damage. Single serial pass per splash
	// event - each event walks the spatial hash inside its radius, distance-
	// falls damage off, and submits a damage write through DamageService.
	// Direct-hit target (excluded) is skipped so it doesn't double-dip on
	// the AoE.
	if hash := sys.spatialHash.Get(); hash != nil {
		for w := range sys.workerSplash {
			for _, ev := range sys.workerSplash[w] {
				sys.applySplashDamage(ev, hash)
			}
		}
	}
	for w := range sys.workerDamage {
		for _, ev := range sys.workerDamage[w] {
			sys.damage.Apply(ev.target, ev.amount)
		}
	}
	// Phase 14 M14.5: spawn ThreatSource entities (archetype mutation must
	// be serial). One entity per shot.
	for w := range sys.workerThreat {
		for _, ev := range sys.workerThreat[w] {
			ent := sys.worldRef.NewEntity()
			origin := ev.origin
			sys.posMap.Add(ent, &origin)
			sys.threatSourceMap.Add(ent, &components.ThreatSource{
				Severity: ev.severity, SpawnTime: now, TTL: threatTTL,
			})
		}
	}
	// Suppression propagation: per-impact, push DangerBulletImpact events
	// into each nearby unit's DangerBuffer. ThreatSystem drains the buffer
	// next tick and translates Strength + impact-relative direction into
	// Threat.Suppression + ThreatDir.
	for w := range sys.workerSuppression {
		for _, ev := range sys.workerSuppression[w] {
			sys.propagateSuppression(ev, now)
		}
	}
}
