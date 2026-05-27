package systems

import (
	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// WeaponSystem - Phase 14 M14.2 combat loop. Per-tick, for every unit with a
// primary weapon and a valid hostile awareness target, it fires one shot if
// the RoF cooldown is ready. The shot resolves through walls (LOS) and
// against any unit body crossing the ray (friendly fire allowed by design -
// PHASE-14.md notes sec Friendly fire).
//
// Pipeline shape mirrors VisionSystem / UnitMovementSystem (Phase 11.6
// pattern):
//
//  1. Serial snapshot - walk the seer filter, resolve each unit's chosen
//     target via Awareness + Faction gate, decrement Ammo + bump
//     LastFiredAt. Snapshot of all live units (candidates for raycast hits)
//     stays read-only.
//  2. Parallel raycast - per shot apply dispersion, test wall LOS, test
//     unit-vs-ray distance against every candidate in the 3x3 chunk window.
//     Workers write into their own scratch buffers (damage / tracer /
//     impact); never into shared maps.
//  3. Serial post-pass - merge worker buffers. Damage events run through
//     DamageService.Apply (which despawns the unit if HP <= 0). Tracer +
//     impact specs land in the VisualEvents resource.
//
// The bulk of the bodies lives in sibling files:
//
//	weapon_fire_gate.go - shouldFire + pickTarget (RoE / target selection)
//	weapon_resolve.go   - resolveShot + ray / segment math (parallel pass)
//	weapon_postpass.go  - applySplashDamage, propagateSuppression, particle bursts
type WeaponSystem struct {
	pool   *core.WorkerPool
	damage *DamageService
	// Phase 14.5 M14.5.4 - particles spawned via SpawnHandles in serial
	// post-pass. Replaces the old VisualEvents resource.
	particles *SpawnParticleHandles

	// Filters.
	seerFilter   *ecs.Filter6[components.Unit, components.WorldPos, components.Motion, components.Equipment, components.Awareness, components.Faction]
	targetFilter *ecs.Filter4[components.Unit, components.WorldPos, components.Stance, components.Faction]
	wallFilter   *ecs.Filter2[components.WorldPos, components.WallSegment]

	// Map handles.
	posMap      *ecs.Map[components.WorldPos]
	stanceMap   *ecs.Map[components.Stance]
	motionMap   *ecs.Map[components.Motion]
	weaponMap   *ecs.Map[components.Weapon]
	factionMap  *ecs.Map[components.Faction]
	threatMap   *ecs.Map[components.Threat]
	doorMap     *ecs.Map[components.Door]
	colliderMap *ecs.Map[components.Collider]
	// Phase 14 M14.3 - RoE + AttackMove gating. shouldFire walks the seer's
	// SquadMember -> Squad -> EngagementRules + OrderQueueHead.First to check
	// the standing fire mode and whether the active order carries the
	// AttackMove flag.
	squadMemberMap     *ecs.Map[components.SquadMember]
	engagementRulesMap *ecs.Map[components.EngagementRules]
	orderQueueMap      *ecs.Map[components.OrderQueueHead]
	orderAttackMoveMap *ecs.Map[components.OrderParamAttackMove]
	orderKindMap       *ecs.Map[components.OrderKind]
	// Phase 17.6 M17.6.6 — per-order RoE override (Hidden position preset).
	orderEngagementOverrideMap *ecs.Map[components.OrderParamEngagementOverride]
	// Phase 17.8 M17.8.3 — Utility evaluator Mode gate. Reloading and
	// Suppressed silence the unit; other modes pass through.
	blackboardMap *ecs.Map[components.LocalBlackboard]

	// Reusable snapshot buffers.
	targetsBuf     []targetSnap
	shotsBuf       []shotWork
	wallsByChunk   map[components.ChunkCoord][]losWall
	targetsByChunk map[components.ChunkCoord][]int32

	// Per-worker scratch. TracerSpec / ImpactSpec are job-description structs
	// (Phase 14.5 M14.5.4 - previously lived in components.VisualEvents
	// before that resource was retired in favour of ECS-entity particles).
	workerDamage      [][]damageEvent
	workerTracer      [][]tracerSpec
	workerImpact      [][]impactSpec
	workerThreat      [][]threatEvent      // M14.5: ThreatSource spawn queue
	workerSuppression [][]suppressionEvent // M14.5: per-impact propagation
	workerSplash      [][]splashEvent      // M14.5.5: AoE damage events

	// M14.5 - handles used by the serial post-pass for ThreatSource spawn
	// and Suppression.Level decay. ecs.Map[ThreatSource] for archetype
	// mutation in the post-pass; the decay walk uses the registered
	// SuppressionFilter so the system stays serial.
	threatSourceMap *ecs.Map[components.ThreatSource]
	dangerBufMap    *ecs.Map[components.DangerBuffer]
	worldRef        *ecs.World

	// Phase 14.5 M14.5.3 - shared spatial hash for unit-vs-ray, propagateSuppression.
	spatialHash ecs.Resource[core.SpatialHash]

	elapsed  float32
	lastTick float32 // session-time of the previous Update (for Suppression decay dt)
}

// targetSnap - read-only snapshot of one candidate target. WorldPos is by
// value (small struct, cache-friendly); Stance + Faction inlined so the
// parallel pass doesn't have to dereference component pointers.
type targetSnap struct {
	ent     ecs.Entity
	pos     components.WorldPos
	chunk   components.ChunkCoord
	radius  float32
	stance  components.StanceCode
	faction uint8
}

// shotWork - one queued shot resolved from the serial snapshot pass. The
// parallel raycast pass reads this read-only and writes results to its
// per-worker buffers indexed by shot.
type shotWork struct {
	shooter      ecs.Entity
	muzzle       rl.Vector3 // world XYZ (chunk-base resolved)
	aim          rl.Vector3 // world XYZ (target torso) - pre-dispersion
	dispersion   float32    // effective angle in radians
	rangeMax     float32    // weapon range cap
	damage       float32
	targetEntity ecs.Entity // intended target - used for "best-effort" hit when geom check fails
	targetStance components.StanceCode
	tracerColor  rl.Color
	shooterChunk components.ChunkCoord
	muzzlePos    components.WorldPos // chunk-aware copy for ThreatSource spawn
	rngSeed      uint64              // deterministic per-shot RNG seed
	// Phase 14.5 M14.5.5 - AoE knobs. SplashRadius > 0 turns the shot into a
	// splash event: serial post-pass applies damage via the spatial hash +
	// spawns debris/smoke particles. Falloff exponents the
	// (1 - dSq/radiusSq) term.
	splashRadius  float32
	splashFalloff float32
}

// hitKind classifies what a shot ultimately struck. Workers tag the impact;
// the serial post-pass dispatches per-kind particle spawns (dust on terrain,
// debris on wall, no extra particles on unit beyond the impact sphere).
type hitKind uint8

const (
	hitKindMiss     hitKind = iota // ray flew past everything (rare; goes to "aim" point)
	hitKindTerrain                 // ground / out-of-range terminus
	hitKindWall                    // wall block (LOS hit)
	hitKindUnitFlag                // unit body
)

// splashEvent - Phase 14.5 M14.5.5 per-shot splash request handed from the
// parallel pass to the serial post-pass. Damage application iterates the
// spatial hash, so we need the impact position + radius + falloff + the
// shot's nominal damage to compute per-target attenuation.
type splashEvent struct {
	pos      rl.Vector3
	radius   float32
	falloff  float32
	damage   float32
	excluded ecs.Entity // direct-hit target already damaged via dmgBuf; skip in splash to avoid double-count.
}

// damageEvent - per-worker damage write request, merged in serial post-pass.
type damageEvent struct {
	target ecs.Entity
	amount float32
}

// tracerSpec / impactSpec - job-description structs handed from parallel
// resolveShot workers to the serial post-pass that materialises ECS particle
// entities via SpawnParticleHandles. Phase 14.5 M14.5.4.
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
	// Phase 14.5 M14.5.5: tag for per-kind particle spawn in serial post-pass
	// (dust on terrain, debris on wall, smoke + debris cluster on splash).
	Hit hitKind
}

// threatEvent - per-shot ThreatSource spawn request, applied in serial
// post-pass. Phase 14 M14.5: one ThreatSource per shot; Phase 14.5 may dedupe
// (multiple shots from same muzzle -> single merged entry).
type threatEvent struct {
	origin   components.WorldPos
	severity float32
}

// suppressionEvent - per-impact propagation. Each shot generates one event;
// the serial post-pass walks units within suppressionRadius of impact and
// pushes DangerBulletImpact entries into their DangerBuffer (Phase 17
// M17.0.2; previously wrote Suppression.Level directly). hitMul switches
// between direct-hit (0.5) and miss-radius (0.2) coefficients per
// PHASE-14.md P5.
type suppressionEvent struct {
	impact  rl.Vector3
	hitMul  float32
	shooter ecs.Entity
}

const (
	// weaponEyeHeight - muzzle Y offset above the unit foot. Standing rifle
	// fire roughly at chest height (1.35 m). Below visionEyeHeight (1.5 m)
	// so muzzle flash sits below the role label.
	weaponEyeHeight float32 = 1.35
	// weaponMaxRange - system-wide range cap. Phase 14 P10: SVD/PKM go to
	// 600..800 m but the 9-chunk LOS window is 64 m x 3 = 192 m max, so
	// honest range stays bounded by that. Hard-cap here so a misconfigured
	// Weapon.RangeM can't accidentally raycast across the whole map.
	weaponMaxRange float32 = 192.0
	// weaponAwarenessMaxAge - drop awareness entries older than this when
	// picking a firing target. PHASE-14.md notes "prefers most recent
	// target"; 3 s matches roughly the Phase 15 SurvivalInstinct contract.
	weaponAwarenessMaxAge float32 = 3.0
	// weaponTracerTTL / weaponImpactTTL - visual fade durations (seconds).
	weaponTracerTTL float32 = 0.15
	weaponImpactTTL float32 = 0.25
	// weaponDefaultRadius - fallback hit cylinder when a unit has no
	// Collider component (legacy spawns).
	weaponDefaultRadius float32 = 0.4
	// suppressionRadius - Phase 14 M14.5 propagation. Hits / misses raise
	// Suppression.Level on every unit inside this XZ radius of the impact.
	suppressionRadius float32 = 5.0
	// suppressionHitMul / suppressionMissMul - per-PHASE-14.md P5 weights.
	// Direct hit: 0.5 added to target's level. Near miss: 0.2 scaled down
	// linearly with distance.
	suppressionHitMul  float32 = 0.5
	suppressionMissMul float32 = 0.2
	// threatTTL - ThreatSource entity lifetime in seconds. Phase 15
	// SurvivalInstinct reads the cluster; longer TTL = stickier "I know
	// where the danger came from" memory.
	threatTTL float32 = 3.0
)

// NewWeaponSystem wires the system. `damage` must be non-nil - the death
// path runs through it during the serial post-pass. `particles` is the
// shared spawn-handles object owned by main.go; must be non-nil after Phase
// 14.5 M14.5.4.
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
