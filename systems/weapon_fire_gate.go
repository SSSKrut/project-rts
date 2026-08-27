package systems

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// Matches the dispersion movingFactor threshold so the moving/dispersion
// flags toggle on the same boundary.
const weaponMovingSpeedThreshold float32 = 1.0

// Own Suppression+Injury above this counts as "being shot at" — for the
// BehaviorRules.AllowReturnFire escape from a HoldFire doctrine, and for the
// ReturnFire mode itself.
const weaponReturnFireThreshold float32 = 0.05

// underFire is the ReturnFire question: has anything under this commander
// actually been shot at recently? Suppression (rounds landing close) and
// Injury (hit) are the two channels that mean "at us"; ShotsFired is gunfire
// SOMEWHERE, which is a different fact and must not unlock a squad nobody has
// engaged. The channels decay on their own, so the answer relaxes back to no
// a few seconds after the shooting stops — that decay IS the hysteresis, and
// it is why the mode needs no memory of its own.
func (sys *WeaponSystem) takingFire(ent ecs.Entity) bool {
	t := sys.threatMap.Get(ent)
	return t != nil && t.Suppression+t.Injury >= weaponReturnFireThreshold
}

// snapshotUnderFire answers once per commander per tick. It is a squad-level
// fact on purpose: RoE is a squad standing rule, and a per-man answer would
// leave the two pinned men shooting while six others watch — which reads as a
// squad that is not fighting rather than as a squad holding its fire.
func (sys *WeaponSystem) snapshotUnderFire() {
	mark := func(ent ecs.Entity) {
		if !sys.takingFire(ent) {
			return
		}
		if cmd := sys.commanderOf(ent); cmd != (ecs.Entity{}) {
			sys.underFireMemo[cmd] = true
		}
	}
	q := sys.seerFilter.Query()
	for q.Next() {
		mark(q.Entity())
	}
	qv := sys.vehSeerFilter.Query()
	for qv.Next() {
		mark(qv.Entity())
	}
}

// An ordered aim point (block G) has NO body, so a zero entity is now a legal
// value everywhere downstream of target selection. These accessors take it and
// answer for "a place" rather than panicking — sprinkling the nil check at each
// call site was tried first and missed two of them, one of which reached a
// gate as a SIM PANIC.
func (sys *WeaponSystem) targetIsVehicle(e ecs.Entity) bool {
	return e != (ecs.Entity{}) && sys.vehicleMap.Has(e)
}

func (sys *WeaponSystem) targetIsAircraft(e ecs.Entity) bool {
	return e != (ecs.Entity{}) && sys.aircraftMap.Has(e)
}

func (sys *WeaponSystem) targetStanceOf(e ecs.Entity) components.StanceCode {
	if e != (ecs.Entity{}) {
		if st := sys.stanceMap.Get(e); st != nil {
			return st.Code
		}
	}
	return components.StanceStand
}

func (sys *WeaponSystem) targetVehicleOf(e ecs.Entity) *components.Vehicle {
	if e == (ecs.Entity{}) {
		return nil
	}
	return sys.vehicleMap.Get(e)
}

func (sys *WeaponSystem) targetMotionOf(e ecs.Entity) *components.Motion {
	if e == (ecs.Entity{}) {
		return nil
	}
	return sys.motionMap.Get(e)
}

// shouldFire is the RoE + AttackMove + Sector gate. HoldFire wins over
// AttackMove, but an active order whose spec sets OverridesHoldFire bypasses
// the HoldFire silence (AttackTarget / SuppressFire carry this flag;
// AttackMove does not).
func (sys *WeaponSystem) shouldFire(shooter ecs.Entity, motionSpeed float32,
	shooterPos *components.WorldPos, targetPos components.WorldPos,
	targetIsVeh, targetIsAir bool) bool {
	// Reloading / Suppressed silence the unit unconditionally — animation /
	// shock state forbids firing even with FreeFire or AttackTarget override.
	if b := sys.blackboardMap.Get(shooter); b != nil {
		switch b.CurrentMode {
		case components.ModeReloading, components.ModeSuppressed:
			return false
		}
	}

	// Soloist fallback carries FireOnAir — for anything that is not an AA
	// asset the VsAir zero in pickTarget is the real gate, and an AA gun
	// outside a squad must be allowed to defend its sky.
	rules := components.EngagementRules{
		Mode: components.FreeFire, FireOnInf: true, FireOnArm: true, FireOnAir: true,
	}
	attackMoveOn := false
	overridesHoldFire := false
	returnFireOK := false

	cmd := sys.commanderOf(shooter)
	if cmd != (ecs.Entity{}) {
		if r := sys.engagementRulesMap.Get(cmd); r != nil {
			rules = *r
		}
		// BehaviorRules.AllowReturnFire: under a HoldFire doctrine the unit
		// may still answer fire it is actually taking (suppression / injury
		// live on its own Threat).
		if br := sys.behaviorRulesMap.Get(cmd); br != nil && br.AllowReturnFire {
			if t := sys.threatMap.Get(shooter); t != nil &&
				t.Suppression+t.Injury >= weaponReturnFireThreshold {
				returnFireOK = true
			}
		}
		if head := sys.orderQueueMap.Get(cmd); head != nil && head.First != (ecs.Entity{}) {
			if sys.orderAttackMoveMap.Has(head.First) {
				attackMoveOn = true
			}
			if kind := sys.orderKindMap.Get(head.First); kind != nil {
				if components.SpecForOrderKind(kind.Code).OverridesHoldFire {
					overridesHoldFire = true
				}
			}
			// Per-order EngagementMode override (e.g. Hidden position preset).
			// Swaps Mode only; FireOn* / Sector remain the standing rules.
			if override := sys.orderEngagementOverrideMap.Get(head.First); override != nil {
				rules.Mode = override.Mode
			}
		}
	}

	switch rules.Mode {
	case components.HoldFire:
		if !overridesHoldFire && !returnFireOK {
			return false
		}
	case components.ReturnFire:
		// Was an empty arm sharing a case with FreeFire, so "return fire" and
		// "free fire" were one mode wearing two names: a squad set to answer
		// opened up on the first thing it saw.
		if !overridesHoldFire && !sys.underFireMemo[cmd] {
			return false
		}
	case components.FreeFire:
	}

	fireOn := rules.FireOnInf
	if targetIsVeh {
		fireOn = rules.FireOnArm
	}
	if targetIsAir {
		fireOn = rules.FireOnAir
	}
	if !fireOn && !overridesHoldFire {
		return false
	}

	if motionSpeed > weaponMovingSpeedThreshold && !attackMoveOn && !overridesHoldFire {
		return false
	}

	// SectorHalfDot is cos(half-angle); 0 disables the cone.
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

// Suppression is deliberate fire, not a magazine dump. A magazine is 30 rounds
// and nothing ever refills it, so at full cadence a squad would be dry in seven
// seconds of a thirty-second order and defenceless for the rest of the fight.
const (
	suppressRoFMul      float32 = 0.4
	suppressAmmoReserve float32 = 0.34
	suppressMinSpreadM  float32 = 2.0
	// Area fire is about VOLUME. A three-round launcher has none to give, and
	// spending two of its rockets hosing a treeline costs the squad the only
	// thing it can answer armour with. Rifles, machine guns and tank guns are
	// in; the RPG and the underbarrel grenade are not.
	suppressMinMagazine uint16 = 10
	// Margin on the shooter's OWN radius before a friendly counts as being in
	// the line. It has to scale with the shooter: a flat number big enough to
	// ignore a hull parked against the tank also ignores the whole front rank
	// of a rifle squad, and the squad is exactly who the veto is for.
	friendClearMarginM float32 = 0.5
)

// worldDist is the plain XZ length of a segment.
func worldDist(ax, az, bx, bz float32) float32 {
	dx, dz := bx-ax, bz-az
	return float32(math.Sqrt(float64(dx*dx + dz*dz)))
}

// orderedAim is P1's first question: does this shooter's commander name a
// PLACE this barrel should service? It OUTRANKS the awareness FIFO — putting
// rounds where nobody has been seen is the entire point, and fire that stops
// the moment a target appears is not suppression. A player who wants target
// priority has AttackTarget for it.
//
// Two orders name a place and WeaponSpec.Release decides which answers which:
// Suppress is the ordinary barrels' job, a missile strike is the player-
// released barrel's and nothing else's. That pairing is the whole of P3 — one
// spec field instead of a weapon panel and an aim mode.
//
// The returned position carries no entity: the raycast still hits whoever is
// standing there, but nothing is aimed AT them.
func (sys *WeaponSystem) orderedAim(shooter ecs.Entity, ownFaction uint8,
	selfPos *components.WorldPos, weapon *components.Weapon,
	wspec *components.WeaponSpec, now float32) (components.WorldPos, bool) {
	cmd := sys.commanderOf(shooter)
	if cmd == (ecs.Entity{}) {
		return components.WorldPos{}, false
	}
	head := sys.orderQueueMap.Get(cmd)
	if head == nil || head.First == (ecs.Entity{}) || !sys.worldRef.Alive(head.First) {
		return components.WorldPos{}, false
	}
	k := sys.orderKindMap.Get(head.First)
	if k == nil {
		return components.WorldPos{}, false
	}
	switch k.Code {
	case components.OrderKindSuppressFire:
		if wspec.Release != components.ReleaseAuto {
			return components.WorldPos{}, false
		}
		if wspec.Ammo < suppressMinMagazine {
			return components.WorldPos{}, false
		}
		// A man keeps a third of his magazine for something he can actually see.
		if wspec.Ammo > 0 &&
			float32(weapon.Ammo) <= float32(wspec.Ammo)*suppressAmmoReserve {
			return components.WorldPos{}, false
		}
		if weapon.RoF > 0 {
			cooldown := 1.0 / (weapon.RoF * suppressRoFMul)
			if weapon.LastFiredAt != 0 && now-weapon.LastFiredAt < cooldown {
				return components.WorldPos{}, false
			}
		}
	case components.OrderKindMissileStrike:
		// No rate penalty and no reserve: the player spent this round on
		// purpose, and the weapon's own reload is the only limit that applies.
		if wspec.Release != components.ReleaseManual {
			return components.WorldPos{}, false
		}
		// A GUIDED round needs a body to guide onto. Sent at bare ground it
		// reached queueMissile with a zero target and took the sim down there;
		// even past that it would fly to the spot and hurt nobody, having no
		// blast radius of its own. Guided answers "attack THAT", unguided
		// answers "put it THERE" — the player reaches the missile either way.
		if wspec.MissileSpeedM > 0 {
			return components.WorldPos{}, false
		}
	default:
		return components.WorldPos{}, false
	}
	tgt := sys.orderTargetMap.Get(head.First)
	if tgt == nil {
		return components.WorldPos{}, false
	}
	aim := tgt.Pos
	if worldDistSq(*selfPos, aim) > weapon.RangeM*weapon.RangeM {
		return components.WorldPos{}, false
	}
	// Scatter across the ordered area, deterministically per shooter and tick:
	// every round on one point is a sniper duel with the grass, not an area
	// being made unhealthy.
	spread := float32(0)
	if k.Code == components.OrderKindSuppressFire {
		spread = suppressMinSpreadM
		if p := sys.orderSuppressMap.Get(head.First); p != nil && p.Radius > spread {
			spread = p.Radius
		}
	}
	// Seeded on the LAST SHOT, not on now: the aim point must stand still
	// between rounds. Re-rolling it every tick moved the point ±5.7 deg at
	// 80 m while a turret slews 0.67 deg per tick against a 3.4 deg firing
	// tolerance — so a hull chased the scatter and never once reported "on
	// target". Infantry has no turret gate and never noticed (owner report
	// 2026-08-27, lite_roe_veh_suppress).
	rng := uint64(shooter.ID())*0x9e3779b97f4a7c15 ^ uint64(weapon.LastFiredAt*1000.0)
	dx := (float32(splitmix(&rng))/float32(0x40000000) - 1) * spread
	dz := (float32(splitmix(&rng))/float32(0x40000000) - 1) * spread
	aim = aim.Add(rl.Vector3{X: dx, Z: dz})
	// Explosive rounds only (see friendInLineOfFire): at 2 m of formation
	// spacing a rear-rank rifleman never has a clear lane past the man in
	// front, so vetoing every round would simply silence the squad.
	if wspec.SplashRadius > 0 &&
		sys.friendInLineOfFire(shooter, ownFaction, selfPos, aim, wspec.SplashRadius) {
		return components.WorldPos{}, false
	}
	return aim, true
}

// friendInLineOfFire vetoes an EXPLOSIVE suppression round that would pass
// through one of our own. Every other shot in the game aims at a body somebody
// can SEE, and that is the only thing that has been keeping squads out of their
// own line — the combat pipeline has no notion of friendly fire, in the ray or
// in the splash. Firing at a PLACE removes that accidental guard, and the first
// measured run of lite_roe_suppress cost lane A four of its eight men: the AT
// gunner put two rockets into his squadmates' backs and a 3.5 m splash did the
// rest.
//
// Clearance is the splash radius itself rather than a tuned number, because
// that IS the distance at which the round hurts a friend. Small arms are left
// alone deliberately: the general friendly-fire hole is pre-existing and
// closing it would change who every existing shot in the game hits (block F).
func (sys *WeaponSystem) friendInLineOfFire(shooter ecs.Entity, ownFaction uint8,
	selfPos *components.WorldPos, aim components.WorldPos, clearM float32) bool {
	ax := float32(selfPos.Chunk.X)*components.ChunkSize + selfPos.Local.X
	az := float32(selfPos.Chunk.Z)*components.ChunkSize + selfPos.Local.Z
	bx := float32(aim.Chunk.X)*components.ChunkSize + aim.Local.X
	bz := float32(aim.Chunk.Z)*components.ChunkSize + aim.Local.Z
	// Anything inside the shooter's own footprint is beside him, not in front:
	// a scene relay parked on the same spot as a tank was projecting onto the
	// very start of the segment and vetoing every round it ever tried to fire.
	standoff := friendClearMarginM
	if col := sys.colliderMap.Get(shooter); col != nil {
		standoff += col.Radius
	}
	for i := range sys.targetsBuf {
		t := &sys.targetsBuf[i]
		if t.ent == shooter || t.faction != ownFaction {
			continue
		}
		px := float32(t.chunk.X)*components.ChunkSize + t.pos.Local.X
		pz := float32(t.chunk.Z)*components.ChunkSize + t.pos.Local.Z
		// segmentPointHit reports CLEAR, not hit — every other caller names the
		// bool `miss`. Reading it the other way made this veto fire on any
		// friendly anywhere on the map, which silenced every explosive barrel
		// under a suppression order and hid itself as "splash weapons hold
		// their fire" (owner report 2026-08-27).
		hitT, clear := segmentPointHit(ax, az, bx, bz, px, pz, t.radius+clearM)
		if clear {
			continue
		}
		// Downrange only. A mate standing beside the hull projects onto the
		// very start of the segment; a gunner does not refuse to shoot because
		// someone is next to him, he refuses because someone is in the beaten
		// zone.
		if seg := worldDist(ax, az, bx, bz); seg > 0 && hitT*seg < standoff {
			continue
		}
		return true
	}
	return false
}

// pickTarget walks the seer's Awareness FIFO and returns the best hostile
// sighting: highest weapon-vs-class multiplier first (a cannon prefers
// armour, a coax prefers infantry), most recent among equals. Classes the
// weapon can't hurt are skipped entirely.
func (sys *WeaponSystem) pickTarget(
	self ecs.Entity, ownFaction uint8, selfPos *components.WorldPos,
	aware *components.Awareness, weapon *components.Weapon,
	wspec *components.WeaponSpec, now float32,
) (ecs.Entity, components.WorldPos, bool) {
	focus := sys.focusTarget(self)
	// A player-released barrel has no opinion of its own. It obeys an order
	// that NAMES something — a point (orderedAim) or this target — and picks
	// nothing for itself. Honouring AttackTarget matters: "shoot that one" is
	// as much the player's decision as "put it there", and refusing it would
	// make the missile reachable only through one of the two.
	if wspec.Release == components.ReleaseManual && focus == (ecs.Entity{}) {
		return ecs.Entity{}, components.WorldPos{}, false
	}
	var bestEnt ecs.Entity
	var bestPos components.WorldPos
	bestTime := float32(0)
	bestMul := float32(0)
	rngSq := weapon.RangeM * weapon.RangeM
	for i := range aware.LastSeen {
		e := aware.LastSeen[i]
		if e.Time == 0 || e.Target == (ecs.Entity{}) || e.Target == self {
			continue
		}
		// Alive-check BEFORE any Map.Get: Vision tick can lag Death by one
		// cadence, so the FIFO may hold a recycled slot; touching a dead id
		// crashes via Ark's slot-reuse path.
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
		var mul float32
		if sys.aircraftMap.Has(e.Target) {
			mul = wspec.VsAir
		} else {
			class := components.ArmorClassSoft
			if tv := sys.vehicleMap.Get(e.Target); tv != nil {
				class = components.SpecForVehicle(tv.Kind).Class
			}
			mul = components.VsClassMul(wspec, class)
		}
		if mul < 0.05 {
			continue
		}
		// Awareness.Pos is stale by up to one Vision tick; prefer live.
		pos := e.Pos
		if live := sys.posMap.Get(e.Target); live != nil {
			pos = *live
		} else {
			continue
		}
		dSq := worldDistSq(*selfPos, pos)
		if dSq > rngSq {
			continue
		}
		// A squadmate-shared sighting needs an own-LOS confirm before the
		// unit commits ammo to it; Direct sightings fire straight away.
		if e.Flags&components.AwareDirect == 0 && sys.sharedLosBlocked(selfPos, e.Target, pos) {
			continue
		}
		// P9: the squad's AttackTarget order names ONE enemy — while he is
		// visible and this barrel can hurt him, he is the target. Everything
		// below is the free-choice fallback.
		if focus != (ecs.Entity{}) && e.Target == focus {
			return e.Target, pos, true
		}
		if mul > bestMul || (mul == bestMul && e.Time > bestTime) {
			bestMul = mul
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

// commanderOf is whoever answers for this shooter's rules and orders: its
// squad, or itself. A soloist under orders owns an OrderQueueHead of its own
// (Phase 20.7 L0), and an airframe carries its own EngagementRules — asking
// only SquadMember left both of them on the hardcoded fallback.
func (sys *WeaponSystem) commanderOf(shooter ecs.Entity) ecs.Entity {
	if sm := sys.squadMemberMap.Get(shooter); sm != nil && sm.Squad != (ecs.Entity{}) {
		return sm.Squad
	}
	return shooter
}

// focusTarget is the enemy the commander's head AttackTarget order names. Zero
// when there is no such order — the shooter then picks freely.
func (sys *WeaponSystem) focusTarget(shooter ecs.Entity) ecs.Entity {
	cmd := sys.commanderOf(shooter)
	if cmd == (ecs.Entity{}) {
		return ecs.Entity{}
	}
	head := sys.orderQueueMap.Get(cmd)
	if head == nil || head.First == (ecs.Entity{}) || !sys.worldRef.Alive(head.First) {
		return ecs.Entity{}
	}
	if kind := sys.orderKindMap.Get(head.First); kind == nil ||
		kind.Code != components.OrderKindAttackTarget {
		return ecs.Entity{}
	}
	tgt := sys.orderTargetMap.Get(head.First)
	if tgt == nil {
		return ecs.Entity{}
	}
	return tgt.Entity
}

// sharedLosBlocked checks walls + terrain from the shooter to a shared
// sighting. Serial (snapshotShots) — live map reads are safe here.
func (sys *WeaponSystem) sharedLosBlocked(selfPos *components.WorldPos, target ecs.Entity, targetPos components.WorldPos) bool {
	sx := float32(selfPos.Chunk.X)*components.ChunkSize + selfPos.Local.X
	sz := float32(selfPos.Chunk.Z)*components.ChunkSize + selfPos.Local.Z
	tx := float32(targetPos.Chunk.X)*components.ChunkSize + targetPos.Local.X
	tz := float32(targetPos.Chunk.Z)*components.ChunkSize + targetPos.Local.Z
	if anyLosWallBlocks(localWalls(sys.wallsByChunk, selfPos.Chunk), sx, sz, tx, tz) {
		return true
	}
	targetY := targetPos.Local.Y + components.SpecForStance(components.StanceStand).TargetCenterY
	if st := sys.stanceMap.Get(target); st != nil {
		targetY = targetPos.Local.Y + components.SpecForStance(st.Code).TargetCenterY
	}
	return terrainBlocksLOS(sys.heightmaps, sx, sz, selfPos.Local.Y+weaponEyeHeight, tx, tz, targetY)
}
