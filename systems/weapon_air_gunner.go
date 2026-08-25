package systems

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

const (
	// How long an AirEngagement stays live after the gunner last confirmed it.
	// The driver reads it one tick later (the VehicleOverride seam) and must
	// not flicker in the gap.
	airEngageHold float32 = 2.0
	// Borrowed altitude: metres per second of climb demand and the ceiling on
	// it. The cap is the point — an unbounded unmask lifts the airframe out of
	// its own terrain masking and into every AA envelope on the field.
	airUnmaskRate float32 = 6.0
	airUnmaskMax  float32 = 60.0
)

// snapshotAirShots is the airframe Gunner pass. It differs from the vehicle
// one in exactly three ways: there is no turret (a gunship aims with its nose,
// which is why AirEngagement.Hold has to exist), firing on the move is normal
// rather than penalised, and the pass writes the engagement the driver flies.
func (sys *WeaponSystem) snapshotAirShots(now, dt float32) {
	q := sys.airSeerFilter.Query()
	for q.Next() {
		ac, pos, eq, aware, fac := q.Get()
		shooter := q.Entity()
		eng := sys.airEngageMap.Get(shooter)
		if ac.Egressing {
			// A departing airframe has no fight left: clearing the engagement
			// is what stops it turning back onto a target on the way out.
			if eng != nil {
				*eng = components.AirEngagement{}
			}
			continue
		}

		var engaged ecs.Entity
		for _, weaponEnt := range [2]ecs.Entity{eq.Primary, eq.Secondary} {
			if weaponEnt == (ecs.Entity{}) {
				continue
			}
			weapon := sys.weaponMap.Get(weaponEnt)
			if weapon == nil || weapon.Ammo == 0 || weapon.RoF <= 0 || weapon.Damage == 0 {
				continue
			}
			wspec := components.SpecForWeapon(weapon.Kind)
			target, targetPos, ok := sys.pickTarget(shooter, fac.ID, pos, aware, weapon, wspec, now)
			if !ok {
				continue
			}
			targetIsAir := sys.aircraftMap.Has(target)
			// motionSpeed 0: a helicopter that must stop to shoot is a rifleman
			// wearing rotors. The vehicle gunner passes 0 for the same reason.
			if !sys.shouldFire(shooter, 0, pos, targetPos, sys.vehicleMap.Has(target), targetIsAir) {
				continue
			}
			// A hitscan barrel cannot reach past weaponMaxRange whatever its
			// spec claims, and a standoff airframe would otherwise empty its
			// cannon into thin air while the missile does the work.
			eff := weapon.RangeM
			if wspec.MissileSpeedM <= 0 {
				eff = clampWeaponRange(weapon.RangeM)
			}
			if worldDistSq(*pos, targetPos) > eff*eff {
				continue
			}
			if engaged == (ecs.Entity{}) {
				engaged = target
			}
			cooldown := 1.0 / weapon.RoF
			if weapon.LastFiredAt != 0 && now-weapon.LastFiredAt < cooldown {
				continue
			}
			muzzle := worldXYZ(*pos, 0)
			if sys.airShotBlocked(muzzle, targetPos) {
				continue
			}
			if wspec.MissileSpeedM > 0 {
				weapon.LastFiredAt = now
				weapon.Ammo--
				sys.queueMissile(target, *pos, muzzle, targetPos, wspec)
				continue
			}
			if targetIsAir {
				// Every air-launched gun has VsAir = 0 today, so pickTarget
				// cannot reach here; a hitscan shot would miss silently anyway,
				// an airframe being absent from the raycast target pool.
				continue
			}
			targetStance := components.StanceStand
			if st := sys.stanceMap.Get(target); st != nil {
				targetStance = st.Code
			}
			targetY := components.SpecForStance(targetStance).TargetCenterY
			if tv := sys.vehicleMap.Get(target); tv != nil {
				targetY = components.SpecForVehicle(tv.Kind).BoxHgt * 0.5
			}
			weapon.LastFiredAt = now
			weapon.Ammo--
			sys.shotsBuf = append(sys.shotsBuf, shotWork{
				shooter:       shooter,
				muzzle:        muzzle,
				aim:           worldXYZ(targetPos, targetY),
				dispersion:    weapon.Dispersion,
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
				vsSoft:        wspec.VsSoft,
				vsLight:       wspec.VsLight,
				vsHeavy:       wspec.VsHeavy,
				trail:         wspec.SplashRadius > 0,
			})
		}
		sys.writeAirEngagement(shooter, eng, pos, engaged, dt, now)
	}
}

// writeAirEngagement turns what the gunner found into the two things the
// driver needs: whether to stop closing, and how much altitude to borrow.
//
// The climb target comes from the ORDER, not from the sensors. A hull behind a
// ridge is by definition absent from this airframe's own awareness, so nothing
// but a named target can ask it to unmask — which is also the border of its
// autonomy: unordered, a gunship works only what it can see from the dial.
//
// Hold, by contrast, needs a real acquisition. Stopping merely because the
// ordered target is inside missile range would park the airframe at 1200 m
// climbing for something its 140 m optics were never going to resolve; it runs
// in and unmasks on the way, and stands still only once it has the shot.
func (sys *WeaponSystem) writeAirEngagement(shooter ecs.Entity, eng *components.AirEngagement,
	pos *components.WorldPos, engaged ecs.Entity, dt, now float32) {
	if eng == nil {
		return
	}
	target := engaged
	blocked := false
	if ordered := sys.focusTarget(shooter); ordered != (ecs.Entity{}) && sys.worldRef.Alive(ordered) {
		if tp := sys.posMap.Get(ordered); tp != nil &&
			worldDistSq(*pos, *tp) <= sys.sensorReachSq(shooter) {
			if target == (ecs.Entity{}) {
				target = ordered
			}
			blocked = sys.airShotBlocked(worldXYZ(*pos, 0), *tp)
		}
	}
	if target == (ecs.Entity{}) {
		eng.Target = ecs.Entity{}
		eng.Hold = false
		if eng.UnmaskAGL > 0 {
			eng.UnmaskAGL -= airUnmaskRate * dt
			if eng.UnmaskAGL < 0 {
				eng.UnmaskAGL = 0
			}
		}
		return
	}
	eng.Target = target
	eng.Until = now + airEngageHold
	eng.Hold = engaged != (ecs.Entity{})
	// Height already bought is KEPT while the target lives. Decaying it the
	// moment the ridge clears would drop the airframe back behind the ridge,
	// blind it, and start the same climb over — pop-up as an oscillator.
	if blocked && eng.UnmaskAGL < airUnmaskMax {
		eng.UnmaskAGL += airUnmaskRate * dt
	}
}

// sensorReachSq is how far this airframe can IMAGE — the same answer the
// detect pass and the coverage rings give, so the climb decision cannot drift
// from what the airframe will actually see when it gets there.
func (sys *WeaponSystem) sensorReachSq(shooter ecs.Entity) float32 {
	sn := sys.sensorsMap.Get(shooter)
	if sn == nil {
		return 0
	}
	r, _ := PassiveSensorProfile(sn)
	return r * r
}
