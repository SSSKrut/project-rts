package systems

import (
	"math"

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
// EngagementRules + AttackMove gating (PHASE-14.md M14.3) are layered onto
// the snapshot pass in the next milestone - M14.2 ships with the minimum
// gate: "target is alive, faction differs, in range, RoF ready, has ammo".
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
	posMap         *ecs.Map[components.WorldPos]
	stanceMap      *ecs.Map[components.Stance]
	motionMap      *ecs.Map[components.Motion]
	weaponMap      *ecs.Map[components.Weapon]
	factionMap     *ecs.Map[components.Faction]
	suppressionMap *ecs.Map[components.Suppression]
	doorMap        *ecs.Map[components.Door]
	colliderMap    *ecs.Map[components.Collider]
	// Phase 14 M14.3 - RoE + AttackMove gating. shouldFire walks the seer's
	// SquadMember -> Squad -> EngagementRules + OrderQueueHead.First to check
	// the standing fire mode and whether the active order carries the
	// AttackMove flag.
	squadMemberMap     *ecs.Map[components.SquadMember]
	engagementRulesMap *ecs.Map[components.EngagementRules]
	orderQueueMap      *ecs.Map[components.OrderQueueHead]
	orderAttackMoveMap *ecs.Map[components.OrderParamAttackMove]
	orderKindMap       *ecs.Map[components.OrderKind]

	// Reusable snapshot buffers.
	targetsBuf     []targetSnap
	shotsBuf       []shotWork
	wallsByChunk   map[components.ChunkCoord][]losWall
	targetsByChunk map[components.ChunkCoord][]int32

	// Per-worker scratch. TracerSpec / ImpactSpec are job-description
	// structs (Phase 14.5 M14.5.4 - previously lived in components.VisualEvents
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
	threatSourceMap   *ecs.Map[components.ThreatSource]
	suppressionFilter *ecs.Filter2[components.Unit, components.Suppression]
	worldRef          *ecs.World

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
	// Phase 14.5 M14.5.5 - AoE knobs. SplashRadius > 0 turns the shot into
	// a splash event: serial post-pass applies damage via the spatial hash
	// + spawns debris/smoke particles. Falloff exponents the
	// (1 - dSq/radiusSq) term.
	splashRadius  float32
	splashFalloff float32
}

// hitKind classifies what a shot ultimately struck. Workers tag the impact;
// the serial post-pass dispatches per-kind particle spawns (dust on
// terrain, debris on wall, no extra particles on unit beyond the impact
// sphere).
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
// resolveShot workers to the serial post-pass that materialises ECS
// particle entities via SpawnParticleHandles. Phase 14.5 M14.5.4.
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
// post-pass. Phase 14 M14.5: one ThreatSource per shot; Phase 14.5 may
// dedupe (multiple shots from same muzzle -> single merged entry).
type threatEvent struct {
	origin   components.WorldPos
	severity float32
}

// suppressionEvent - per-impact propagation. Each shot generates one event;
// the serial post-pass walks units within suppressionRadius of impact and
// adjusts Suppression.Level / ThreatDir. hitMul switches between direct-hit
// (0.5) and miss-radius (0.2) coefficients per PHASE-14.md P5.
type suppressionEvent struct {
	impact rl.Vector3
	hitMul float32
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
	// suppressionDecayRate - per-second decay applied each tick. 0.1 means
	// full suppression (Level=1.0) clears in 10 s without new pressure.
	suppressionDecayRate float32 = 0.1
	// threatTTL - ThreatSource entity lifetime in seconds. Phase 15
	// SurvivalInstinct reads the cluster; longer TTL = stickier "I know
	// where the danger came from" memory.
	threatTTL float32 = 3.0
)

// Phase 14.5 M14.5.1 - weaponTargetY + stanceDamageMul folded into
// components.StanceSpecs (TargetCenterY / DamageMultiplier fields).
// Readers use components.SpecForStance(code).

// NewWeaponSystem wires the system. `damage` must be non-nil - the death
// path runs through it during the serial post-pass. `particles` is the
// shared spawn-handles object owned by main.go; must be non-nil after
// Phase 14.5 M14.5.4.
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
	sys.suppressionMap = ecs.NewMap[components.Suppression](w)
	sys.doorMap = ecs.NewMap[components.Door](w)
	sys.colliderMap = ecs.NewMap[components.Collider](w)
	sys.squadMemberMap = ecs.NewMap[components.SquadMember](w)
	sys.engagementRulesMap = ecs.NewMap[components.EngagementRules](w)
	sys.orderQueueMap = ecs.NewMap[components.OrderQueueHead](w)
	sys.orderAttackMoveMap = ecs.NewMap[components.OrderParamAttackMove](w)
	sys.orderKindMap = ecs.NewMap[components.OrderKind](w)
	sys.threatSourceMap = ecs.NewMap[components.ThreatSource](w)
	sys.suppressionFilter = ecs.NewFilter2[components.Unit, components.Suppression](w)
	sys.worldRef = w

	sys.spatialHash = ecs.NewResource[core.SpatialHash](w)
}

// weaponMovingSpeedThreshold - Motion.Speed above this (m/s) counts as
// "moving" for the firing-while-moving gate. PHASE-14.md notes: matches the
// dispersion movingFactor threshold (1.0 m/s) so the two flags toggle on
// the same boundary instead of needing two separate empirical fits.
const weaponMovingSpeedThreshold float32 = 1.0

// shouldFire is the RoE + AttackMove + Sector gate. Returns true when the
// unit is permitted to take the resolved shot.
//
// Decision tree (PHASE-14.md M14.3 + Q2 lock-in: HoldFire wins over
// AttackMove). Phase 14.5 M14.5.0 (Issue #9 fix): if the active order's spec
// declares `OverridesHoldFire`, the HoldFire silence is bypassed for that
// order's duration. AttackTarget and SuppressFire carry this flag; AttackMove
// does NOT (HoldFire still wins per Q2 lock).
//
//   - No squad (soloist) -> default FreeFire-on-Inf. Fires.
//   - Mode=HoldFire -> never, UNLESS the active order has OverridesHoldFire.
//   - Mode=ReturnFire -> always (any awareness hostile counts as "threat in
//     awareness" per P9). Stricter "only after being shot at" deferred.
//   - Mode=FreeFire -> always.
//   - FireOnInf gate - Phase 14 has only Inf targets, so this acts on
//     AT-team-style rules that have FireOnInf=false. Bypassed by the same
//     OverridesHoldFire override.
//   - Fire-while-moving (Motion.Speed > threshold) requires the squad's
//     active order to carry the AttackMove flag.
//   - Phase 15 M15.A.4: Sector cone. When SectorHalfDot > 0 the squad
//     defends only a cone centred on SectorYaw; targets outside silently
//     fail the gate. SectorHalfDot is interpreted as cos(half-angle), so
//     1.0 = zero cone (disabled when 0).
//
// `motionSpeed` + `shooterPos` + `targetPos` are passed in by the caller so
// the seer-loop snapshot avoids re-resolving pointers.
func (sys *WeaponSystem) shouldFire(shooter ecs.Entity, motionSpeed float32,
	shooterPos *components.WorldPos, targetPos components.WorldPos) bool {
	rules := components.EngagementRules{
		Mode: components.FreeFire, FireOnInf: true, FireOnArm: true,
	}
	attackMoveOn := false
	overridesHoldFire := false

	if sm := sys.squadMemberMap.Get(shooter); sm != nil && sm.Squad != (ecs.Entity{}) {
		if r := sys.engagementRulesMap.Get(sm.Squad); r != nil {
			rules = *r
		}
		if head := sys.orderQueueMap.Get(sm.Squad); head != nil && head.First != (ecs.Entity{}) {
			if sys.orderAttackMoveMap.Has(head.First) {
				attackMoveOn = true
			}
			if kind := sys.orderKindMap.Get(head.First); kind != nil {
				if components.SpecForOrderKind(kind.Code).OverridesHoldFire {
					overridesHoldFire = true
				}
			}
		}
	}

	switch rules.Mode {
	case components.HoldFire:
		// Phase 14.5 M14.5.0 (Issue #9): AttackTarget / SuppressFire - the
		// player's explicit fire orders - win over the standing HoldFire.
		if !overridesHoldFire {
			return false
		}
	case components.ReturnFire, components.FreeFire:
		// Allowed; target-type and movement gates apply below.
	}

	// Phase 14 target-type gate: every target is Inf for now (vehicles in
	// Phase 16). FireOnInf=false (AT team default) silences the squad -
	// unless an explicit AttackTarget/SuppressFire is overriding.
	if !rules.FireOnInf && !overridesHoldFire {
		return false
	}

	// Fire-while-moving: stationary always fires; moving needs AttackMove
	// or an explicit fire-order override (you can sprint-shoot an
	// AttackTarget).
	if motionSpeed > weaponMovingSpeedThreshold && !attackMoveOn && !overridesHoldFire {
		return false
	}

	// Phase 15 M15.A.4 - Sector gate. SectorHalfDot is cos(half-angle); when
	// positive (i.e. the squad has set a cone) any target outside the cone
	// is filtered out. OverridesHoldFire bypasses the gate so an explicit
	// AttackTarget on an out-of-sector enemy still fires.
	if rules.SectorHalfDot > 0 && !overridesHoldFire {
		dx := targetPos.Local.X - shooterPos.Local.X +
			float32(targetPos.Chunk.X-shooterPos.Chunk.X)*components.ChunkSize
		dz := targetPos.Local.Z - shooterPos.Local.Z +
			float32(targetPos.Chunk.Z-shooterPos.Chunk.Z)*components.ChunkSize
		mag := float32(math.Sqrt(float64(dx*dx + dz*dz)))
		if mag > 0 {
			dx /= mag
			dz /= mag
			fx := float32(math.Sin(float64(rules.SectorYaw)))
			fz := float32(math.Cos(float64(rules.SectorYaw)))
			if dx*fx+dz*fz < rules.SectorHalfDot {
				return false
			}
		}
	}
	return true
}

func (WeaponSystem) Name() string { return "weapon" }

func (WeaponSystem) LODPolicy() core.LODPolicy {
	// Universal sim, every tick - matches Vision / UnitMovement cadence.
	// RoF gates the actual shot density per-weapon so the per-tick walk is
	// cheap when nothing is firing.
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

	// -- Phase 14 M14.5: Suppression decay pass. Walks every Unit's
	// Suppression component before the firing pass so this tick's new
	// pressure isn't immediately decayed away. Serial - each unit writes
	// its own component, but the walk is cheap (one filter pass).
	sys.decaySuppression(dt)

	// -- 1a. Snapshot every live unit as a candidate target --
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

	// -- 1b. Snapshot walls by chunk for LOS raycast --
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

	// -- 1c. Walk seers, decide shots, write Ammo + LastFiredAt in serial --
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
		if sp := sys.suppressionMap.Get(shooter); sp != nil {
			supp = sp.Level
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

	// -- 2. Parallel raycast per shot --
	if len(sys.shotsBuf) > 0 {
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

	// -- 3. Serial post-pass: spawn particle entities from worker buffers --
	// Phase 14.5 M14.5.4/M14.5.5: tracers / impacts / muzzle flashes are
	// ECS entities. Per-impact-kind dust/debris/smoke spawns layer on top.
	if sys.particles != nil {
		for w := range sys.workerTracer {
			for _, t := range sys.workerTracer[w] {
				sys.particles.SpawnTracer(t.From, t.To, t.Color, t.SpawnTime, t.TTL)
				// Muzzle flash at the From end of each tracer - short-
				// lived bright sphere, colour matches tracer hue.
				sys.particles.SpawnMuzzleFlash(t.From, t.Color, t.SpawnTime)
			}
			for _, im := range sys.workerImpact[w] {
				sys.particles.SpawnImpact(im.Pos, im.Color, im.SpawnTime, im.TTL)
				// Per-kind extras (Phase 14.5 M14.5.5).
				switch im.Hit {
				case hitKindTerrain:
					// 3 small dust spheres with downward + horizontal vel.
					sys.spawnDustBurst(im.Pos, im.SpawnTime, 3, im.Color)
				case hitKindWall:
					// 4 debris cubes scattering away from the wall.
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
	// Phase 14.5 M14.5.5: apply splash damage. Single serial pass per
	// splash event - each event walks the spatial hash inside its radius,
	// distance-falls damage off, and submits a damage write through
	// DamageService. Direct-hit target (excluded) is skipped so it doesn't
	// double-dip on the AoE.
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
	// Phase 14 M14.5: spawn ThreatSource entities (archetype mutation
	// must be serial). One entity per shot.
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
	// Suppression propagation: per-impact, raise nearby units' Level and
	// point their ThreatDir back toward the impact.
	for w := range sys.workerSuppression {
		for _, ev := range sys.workerSuppression[w] {
			sys.propagateSuppression(ev.impact, ev.hitMul)
		}
	}
}

// spawnDustBurst emits N small dust spheres around `pos` with downward +
// slight outward velocity. Phase 14.5 M14.5.5 - terrain hit visual cue.
// Deterministic-per-shot RNG would be ideal but the visual jitter is
// purely cosmetic; we use the hash address as a cheap seed.
func (sys *WeaponSystem) spawnDustBurst(pos rl.Vector3, now float32, n int, baseColor rl.Color) {
	if sys.particles == nil {
		return
	}
	seed := uint64(now * 10000.0)
	dustColor := rl.Color{R: 180, G: 160, B: 130, A: 200}
	_ = baseColor
	for i := 0; i < n; i++ {
		seed++
		rx := float32(splitmix(&seed))/float32(0x40000000) - 1
		seed++
		rz := float32(splitmix(&seed))/float32(0x40000000) - 1
		vel := rl.Vector3{X: rx * 1.0, Y: 0.5, Z: rz * 1.0}
		sys.particles.SpawnDust(pos, dustColor, vel, now)
	}
}

// spawnDebrisBurst emits N small debris cubes scattering from `pos`. Used
// for wall hits (small N=4) and splash impacts (N=10). Phase 14.5 M14.5.5.
func (sys *WeaponSystem) spawnDebrisBurst(pos rl.Vector3, now float32, n int) {
	if sys.particles == nil {
		return
	}
	seed := uint64(now * 10000.0)
	color := rl.Color{R: 120, G: 110, B: 100, A: 235}
	for i := 0; i < n; i++ {
		seed++
		rx := float32(splitmix(&seed))/float32(0x40000000) - 1
		seed++
		rz := float32(splitmix(&seed))/float32(0x40000000) - 1
		seed++
		ry := float32(splitmix(&seed)) / float32(0x40000000)
		vel := rl.Vector3{X: rx * 3.5, Y: 1.5 + ry*3.0, Z: rz * 3.5}
		sys.particles.SpawnDebris(pos, color, vel, now)
	}
}

// applySplashDamage walks the spatial hash inside the splash radius and
// submits per-target damage to DamageService. Damage falls off as
// `(1 - dSq/radiusSq)^falloff` so a unit at the edge takes a fraction of
// the baseline. Friendly fire enabled per Phase 14 design.
func (sys *WeaponSystem) applySplashDamage(ev splashEvent, hash *core.SpatialHash) {
	rSq := ev.radius * ev.radius
	hash.ForEachInRadius(ev.pos.X, ev.pos.Z, ev.radius, func(ent ecs.Entity, dSq float32) {
		if ent == ev.excluded {
			return // direct-hit target already damaged.
		}
		if !sys.worldRef.Alive(ent) {
			return
		}
		// Only damage Units (Suppression component is on Units; we read
		// HP through DamageService).
		if sys.suppressionMap.Get(ent) == nil {
			return
		}
		t := float32(1) - dSq/rSq
		if t <= 0 {
			return
		}
		// Falloff exponent: 1 = linear, 2 = quadratic. Phase 14.5 ships with
		// these two only; math.Pow only kicks in for arbitrary exponents
		// (none currently configured) - most splash weapons match exactly.
		mul := t
		switch {
		case ev.falloff >= 2:
			mul = t * t
		case ev.falloff <= 1:
			// linear fallthrough
		default:
			// 1 < falloff < 2 - lerp between linear and quadratic.
			mul = t*t*(ev.falloff-1) + t*(2-ev.falloff)
		}
		amt := ev.damage * mul
		if amt > 0 {
			sys.damage.Apply(ent, amt)
		}
	})
}

// decaySuppression walks every Unit's Suppression and decays Level toward
// zero by `suppressionDecayRate * dt`. Cheap serial pass - no archetype
// changes, just float writes.
func (sys *WeaponSystem) decaySuppression(dt float32) {
	if dt <= 0 {
		return
	}
	drop := suppressionDecayRate * dt
	q := sys.suppressionFilter.Query()
	for q.Next() {
		_, supp := q.Get()
		if supp.Level <= 0 {
			continue
		}
		supp.Level -= drop
		if supp.Level < 0 {
			supp.Level = 0
		}
	}
}

// propagateSuppression raises Suppression.Level on every unit within
// suppressionRadius of `impact` and points the per-unit ThreatDir vector
// from the impact toward the unit (i.e. "away from the danger" - Phase 15
// SurvivalInstinct will use it to pick cover slots on the opposite side).
//
// hitMul switches the per-impact intensity:
//   - suppressionHitMul (0.5) - direct hit on a unit (target gets the full
//     amount regardless of radius).
//   - suppressionMissMul (0.2) - near miss, scaled linearly by distance.
//
// Bounded by 0..1.
//
// Phase 14.5 M14.5.3: switched from O(N) Suppression filter walk to a
// SpatialHash ForEachInRadius query. Each shot now touches only the dozen-ish
// units near its impact instead of every unit in the world - big win for
// crowd firefights where O(Nxshots) blew up quickly.
func (sys *WeaponSystem) propagateSuppression(impact rl.Vector3, hitMul float32) {
	hash := sys.spatialHash.Get()
	if hash == nil {
		return
	}
	hash.ForEachInRadius(impact.X, impact.Z, suppressionRadius, func(ent ecs.Entity, dSq float32) {
		// Stale-entity guard - see core/spatial_hash.go invariants.
		if !sys.worldRef.Alive(ent) {
			return
		}
		supp := sys.suppressionMap.Get(ent)
		if supp == nil {
			return // not a Unit (no Suppression component).
		}
		// We need ThreatDir from live pos (the hash entry's X/Z is the
		// rebuild-time snapshot - close enough for ThreatDir but the
		// direction is more meaningful from current pos).
		pos := sys.posMap.Get(ent)
		if pos == nil {
			return
		}
		ux := float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
		uz := float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z
		dx := ux - impact.X
		dz := uz - impact.Z
		_ = dSq // we recompute from live pos below
		d := float32(math.Sqrt(float64(dx*dx + dz*dz)))
		falloff := float32(1) - d/suppressionRadius
		if falloff < 0 {
			falloff = 0
		}
		supp.Level += hitMul * falloff
		if supp.Level > 1 {
			supp.Level = 1
		}
		if d > 1e-3 {
			supp.ThreatDir = rl.Vector3{X: dx / d, Y: 0, Z: dz / d}
		}
	})
}

// pickTarget walks the seer's Awareness FIFO and returns the most recent
// hostile sighting that's still alive, still in range, and within the
// awareness max-age. (ent, pos, true) on hit; (_, _, false) on miss.
func (sys *WeaponSystem) pickTarget(
	self ecs.Entity, ownFaction uint8, selfPos *components.WorldPos,
	aware *components.Awareness, weapon *components.Weapon, now float32,
) (ecs.Entity, components.WorldPos, bool) {
	var bestEnt ecs.Entity
	var bestPos components.WorldPos
	bestTime := float32(0)
	rngSq := weapon.RangeM * weapon.RangeM
	for i := range aware.LastSeen {
		e := aware.LastSeen[i]
		if e.Time == 0 || e.Target == (ecs.Entity{}) || e.Target == self {
			continue
		}
		// Phase 14.6 M14.6.0 (Issue #11): alive-check BEFORE any Map.Get on
		// e.Target. Vision tick can lag Death by up to one cadence, so the
		// FIFO may hold a recycled slot; touching factionMap on a dead id
		// crashes via Ark's slot reuse path.
		if !sys.worldRef.Alive(e.Target) {
			continue
		}
		if now-e.Time > weaponAwarenessMaxAge {
			continue
		}
		f := sys.factionMap.Get(e.Target)
		if f == nil || f.ID == ownFaction {
			continue
		}
		// Use the candidate's live position if the entity is still alive
		// (Awareness.Pos is stale by up to one Vision tick).
		pos := e.Pos
		if live := sys.posMap.Get(e.Target); live != nil {
			pos = *live
		} else {
			// Entity vanished (despawn) - skip.
			continue
		}
		dSq := worldDistSq(*selfPos, pos)
		if dSq > rngSq {
			continue
		}
		if e.Time > bestTime {
			bestTime = e.Time
			bestEnt = e.Target
			bestPos = pos
		}
	}
	if bestEnt == (ecs.Entity{}) {
		return ecs.Entity{}, components.WorldPos{}, false
	}
	return bestEnt, bestPos, true
}

// resolveShot is the parallel work unit. Applies dispersion, walks the LOS,
// finds the closest unit-vs-ray hit, and emits damage / tracer / impact /
// threat / suppression records into per-worker buffers.
func resolveShot(
	s *shotWork, targets []targetSnap, targetsByChunk map[components.ChunkCoord][]int32,
	wallsByChunk map[components.ChunkCoord][]losWall, now float32,
	dmgBuf *[]damageEvent, tracerBuf *[]tracerSpec, impactBuf *[]impactSpec,
	threatBuf *[]threatEvent, suppBuf *[]suppressionEvent, splashBuf *[]splashEvent,
) {
	// Apply lateral dispersion to the aim point. dispersion is small-angle
	// radians; lateral deflection ~ dispersion * range. Sample one uniform
	// per axis from the seeded RNG so the same shot always lands the same
	// way (debuggable replay).
	dx := s.aim.X - s.muzzle.X
	dz := s.aim.Z - s.muzzle.Z
	dist := float32(math.Sqrt(float64(dx*dx + dz*dz)))
	if dist > s.rangeMax {
		// Out-of-range guard (should have been filtered in pickTarget but
		// targets move between picks). Drop the shot silently.
		return
	}

	rng := s.rngSeed
	rx := float32(splitmix(&rng))/float32(0x40000000) - 1 // [-1, 1)
	ry := float32(splitmix(&rng))/float32(0x40000000) - 1
	lateral := s.dispersion * dist
	// Perpendicular-to-LOS XZ basis (rotate (dx,dz) by 90 deg). Adds horizontal
	// scatter; ry adds a small vertical jitter so the impact sphere doesn't
	// always sit on the ground plane.
	invD := float32(1)
	if dist > 1e-4 {
		invD = 1 / dist
	}
	perpX := -dz * invD
	perpZ := dx * invD
	aim := s.aim
	aim.X += perpX * lateral * rx
	aim.Z += perpZ * lateral * rx
	aim.Y += lateral * ry * 0.5

	// Wall LOS - collect walls in the 3x3 chunk window around the shooter.
	walls := localWalls(wallsByChunk, s.shooterChunk)
	wallT, wallBlocks := segmentToWallsT(walls, s.muzzle.X, s.muzzle.Z, aim.X, aim.Z)

	// Unit-vs-ray - walk targets in the 3x3 chunk window. Pick the unit
	// closest to the shooter (along ray) that's within its hit cylinder.
	bestHitT := float32(1.5) // > 1 means no hit yet
	var bestHit ecs.Entity
	var bestPos components.WorldPos
	var bestRadius float32
	for dcZ := int32(-1); dcZ <= 1; dcZ++ {
		for dcX := int32(-1); dcX <= 1; dcX++ {
			cc := components.ChunkCoord{X: s.shooterChunk.X + dcX, Z: s.shooterChunk.Z + dcZ}
			for _, idx := range targetsByChunk[cc] {
				t := &targets[idx]
				if t.ent == s.shooter {
					continue
				}
				tx := float32(t.pos.Chunk.X)*components.ChunkSize + t.pos.Local.X
				tz := float32(t.pos.Chunk.Z)*components.ChunkSize + t.pos.Local.Z
				hitT, miss := segmentPointHit(s.muzzle.X, s.muzzle.Z, aim.X, aim.Z, tx, tz, t.radius)
				if miss {
					continue
				}
				if hitT < bestHitT {
					bestHitT = hitT
					bestHit = t.ent
					bestPos = t.pos
					bestRadius = t.radius
				}
			}
		}
	}

	// Final hit decision: nearest event between unit and wall.
	hitWall := wallBlocks && wallT < bestHitT
	hitUnit := bestHit != (ecs.Entity{}) && !hitWall

	// Compute terminal impact point + classify the hit kind for per-kind
	// particle dispatch in the serial post-pass.
	var impact rl.Vector3
	var hk hitKind
	switch {
	case hitWall:
		impact = lerpVec3(s.muzzle, aim, wallT)
		hk = hitKindWall
	case hitUnit:
		impact = rl.Vector3{
			X: float32(bestPos.Chunk.X)*components.ChunkSize + bestPos.Local.X,
			Y: aim.Y, // approximate impact at aim's torso height
			Z: float32(bestPos.Chunk.Z)*components.ChunkSize + bestPos.Local.Z,
		}
		_ = bestRadius // reserved for cover model in Phase 24
		hk = hitKindUnitFlag
	default:
		impact = aim
		hk = hitKindTerrain
	}

	*tracerBuf = append(*tracerBuf, tracerSpec{
		From: s.muzzle, To: impact,
		Color: s.tracerColor, SpawnTime: now, TTL: weaponTracerTTL,
	})

	impactColor := rl.Color{R: 230, G: 200, B: 80, A: 255}
	if hitUnit {
		impactColor = rl.Color{R: 230, G: 70, B: 70, A: 255}
	}
	*impactBuf = append(*impactBuf, impactSpec{
		Pos: impact, Color: impactColor, SpawnTime: now, TTL: weaponImpactTTL,
		Hit: hk,
	})

	// Phase 14.5 M14.5.5: splash event for AoE weapons. Direct-hit target
	// already lands in dmgBuf; the splash applies to everyone else in
	// radius (friendly fire enabled, matches Phase 14 design).
	if s.splashRadius > 0 {
		*splashBuf = append(*splashBuf, splashEvent{
			pos: impact, radius: s.splashRadius, falloff: s.splashFalloff,
			damage: s.damage, excluded: bestHit,
		})
	}

	if hitUnit {
		// Stance multiplier - smaller silhouette -> less damage transferred.
		// Re-read stance from snapshot (best-effort; stance change between
		// snapshot and apply is rare and acceptable).
		mul := float32(1)
		for _, idx := range targetsByChunk[bestPos.Chunk] {
			if targets[idx].ent == bestHit {
				mul = components.SpecForStance(targets[idx].stance).DamageMultiplier
				break
			}
		}
		*dmgBuf = append(*dmgBuf, damageEvent{
			target: bestHit,
			amount: s.damage * mul,
		})
	}

	// Phase 14 M14.5: every shot spawns one ThreatSource at the muzzle.
	// Severity grows with weapon damage so MG bursts press harder than
	// single-shot rifle fire - clamped to 1.0.
	severity := s.damage / 100
	if severity > 1 {
		severity = 1
	}
	*threatBuf = append(*threatBuf, threatEvent{
		origin:   s.muzzlePos,
		severity: severity,
	})

	// Suppression propagation: every shot (hit or miss) writes one event
	// at the terminal impact point. Hit -> bigger nominal weight; miss ->
	// smaller distance-scaled push. resolveShot can't walk units (workers
	// don't share state) - the serial post-pass does the radius query.
	hitMul := suppressionMissMul
	if hitUnit {
		hitMul = suppressionHitMul
	}
	*suppBuf = append(*suppBuf, suppressionEvent{
		impact: impact,
		hitMul: hitMul,
	})
}

// localWalls returns the concatenated wall slice for the 3x3 chunk window
// around `home`. Returns a fresh slice each call (worker-local - no shared
// mutation), so the parallel pass is race-safe.
func localWalls(wallsByChunk map[components.ChunkCoord][]losWall, home components.ChunkCoord) []losWall {
	var total int
	for dz := int32(-1); dz <= 1; dz++ {
		for dx := int32(-1); dx <= 1; dx++ {
			total += len(wallsByChunk[components.ChunkCoord{X: home.X + dx, Z: home.Z + dz}])
		}
	}
	if total == 0 {
		return nil
	}
	out := make([]losWall, 0, total)
	for dz := int32(-1); dz <= 1; dz++ {
		for dx := int32(-1); dx <= 1; dx++ {
			out = append(out, wallsByChunk[components.ChunkCoord{X: home.X + dx, Z: home.Z + dz}]...)
		}
	}
	return out
}

// segmentToWallsT returns the smallest t in [0,1] along (ax,az)->(bx,bz) at
// which a non-transparent wall is hit, plus a "blocked" flag. Mirrors
// anyLosWallBlocks but reports the parametric distance so we can render
// the tracer up to the wall.
func segmentToWallsT(walls []losWall, ax, az, bx, bz float32) (float32, bool) {
	bestT := float32(2)
	blocked := false
	for i := range walls {
		w := &walls[i]
		toX := w.fromX + w.sa*w.length
		toZ := w.fromZ + w.ca*w.length
		t1, t2, ok := segmentSegmentIntersect2D(ax, az, bx, bz, w.fromX, w.fromZ, toX, toZ)
		if !ok || t1 < 0 || t1 > 1 || t2 < 0 || t2 > 1 {
			continue
		}
		if w.openingPresent {
			wallT := t2 * w.length
			if wallT >= w.openStart && wallT <= w.openEnd {
				if w.openingTransparent {
					continue
				}
			}
		}
		if t1 < bestT {
			bestT = t1
			blocked = true
		}
	}
	return bestT, blocked
}

// segmentPointHit returns (t, miss) - t is the parametric position on the
// (ax,az)->(bx,bz) segment closest to (px,pz). miss=true if that closest
// approach distance exceeds `radius` (no hit) or if the closest point lies
// outside [0,1] (beyond the segment endpoints).
func segmentPointHit(ax, az, bx, bz, px, pz, radius float32) (float32, bool) {
	dx := bx - ax
	dz := bz - az
	lenSq := dx*dx + dz*dz
	if lenSq < 1e-6 {
		return 0, true
	}
	t := ((px-ax)*dx + (pz-az)*dz) / lenSq
	if t < 0 || t > 1 {
		return t, true
	}
	cx := ax + dx*t
	cz := az + dz*t
	d2 := (px-cx)*(px-cx) + (pz-cz)*(pz-cz)
	if d2 > radius*radius {
		return t, true
	}
	return t, false
}

// worldXYZ converts a WorldPos to absolute rl.Vector3 in world coords (chunk
// base + local + extra Y offset).
func worldXYZ(p components.WorldPos, yOff float32) rl.Vector3 {
	return rl.Vector3{
		X: float32(p.Chunk.X)*components.ChunkSize + p.Local.X,
		Y: p.Local.Y + yOff,
		Z: float32(p.Chunk.Z)*components.ChunkSize + p.Local.Z,
	}
}

// worldDistSq returns squared world XZ distance between two WorldPos values.
// Y is ignored - combat range is read on the horizontal plane.
func worldDistSq(a, b components.WorldPos) float32 {
	ax := float32(a.Chunk.X)*components.ChunkSize + a.Local.X
	az := float32(a.Chunk.Z)*components.ChunkSize + a.Local.Z
	bx := float32(b.Chunk.X)*components.ChunkSize + b.Local.X
	bz := float32(b.Chunk.Z)*components.ChunkSize + b.Local.Z
	dx := bx - ax
	dz := bz - az
	return dx*dx + dz*dz
}

// lerpVec3 is plain linear interpolation between two rl.Vector3.
func lerpVec3(a, b rl.Vector3, t float32) rl.Vector3 {
	return rl.Vector3{
		X: a.X + (b.X-a.X)*t,
		Y: a.Y + (b.Y-a.Y)*t,
		Z: a.Z + (b.Z-a.Z)*t,
	}
}

// clampWeaponRange enforces the system-wide max so the LOS window stays
// bounded.
func clampWeaponRange(r float32) float32 {
	if r <= 0 {
		return 0
	}
	if r > weaponMaxRange {
		return weaponMaxRange
	}
	return r
}

// tracerColorFor reads tracer hue from components.WeaponSpecs. Phase 14.5
// M14.5.1 - palette switch replaced with spec table.
func tracerColorFor(k components.WeaponKind) rl.Color {
	return components.SpecForWeapon(k).TracerColor
}

// splitmix is a tiny stateful SplitMix64 used to pick deterministic dispersion
// jitter inside resolveShot. Reading *seed and writing back so successive
// calls advance the stream. Output is uint32 in [0, 2^31); caller divides
// by 2^30 then subtracts 1 to recentre to [-1, 1). Pulling math/rand would
// allocate a *rand.Rand per worker for thread safety, which we avoid here.
func splitmix(seed *uint64) uint32 {
	*seed += 0x9e3779b97f4a7c15
	z := *seed
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	z ^= z >> 31
	return uint32((z >> 33) & 0x7fffffff)
}
