package systems

import (
	"math"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

const (
	// Turret must point within this of the target bearing to fire.
	vehAimToleranceRad float32 = 0.06
	// Firing on the move widens dispersion (crude stabilisation model).
	vehMoveDispersionMul float32 = 0.8
	vehMovingSpeed       float32 = 0.5
)

// snapshotVehicleShots is the vehicle Gunner pass: each armed vehicle slews
// its turret toward the primary weapon's target and queues shots per weapon.
// The single turret serves both barrels — the coax only fires when the
// turret already points near its own target.
func (sys *WeaponSystem) snapshotVehicleShots(now, dt float32) {
	qS := sys.vehSeerFilter.Query()
	for qS.Next() {
		veh, pos, motion, eq, aware, fac := qS.Get()
		shooter := qS.Entity()
		spec := components.SpecForVehicle(veh.Kind)
		turret := sys.turretMap.Get(shooter)
		turretAimed := false

		for slot, weaponEnt := range [2]ecs.Entity{eq.Primary, eq.Secondary} {
			if weaponEnt == (ecs.Entity{}) {
				continue
			}
			weapon := sys.weaponMap.Get(weaponEnt)
			if weapon == nil || weapon.Ammo == 0 || weapon.RoF <= 0 || weapon.Damage == 0 {
				continue
			}
			wspec := components.SpecForWeapon(weapon.Kind)
			// Same two questions as the infantry path: a named place first,
			// the awareness FIFO second.
			var target ecs.Entity
			targetPos, ok := sys.orderedAim(shooter, fac.ID, pos, weapon, wspec, now)
			if !ok {
				target, targetPos, ok = sys.pickTarget(shooter, fac.ID, pos, aware, weapon, wspec, now)
			}
			if !ok {
				continue
			}
			targetIsAir := sys.targetIsAircraft(target)
			if !sys.shouldFire(shooter, 0, pos, targetPos,
				sys.targetIsVehicle(target), targetIsAir) {
				continue
			}

			diff := targetPos.Sub(*pos)
			bearing := float32(math.Atan2(float64(diff.X), float64(diff.Z)))
			if turret != nil && spec.TurretSlewDps > 0 {
				desired := wrapAngle(bearing - motion.Yaw)
				if slot == 0 && !turretAimed {
					turretAimed = true
					maxStep := spec.TurretSlewDps * (math.Pi / 180) * dt
					step := wrapAngle(desired - turret.Yaw)
					if step > maxStep {
						step = maxStep
					} else if step < -maxStep {
						step = -maxStep
					}
					turret.Yaw = wrapAngle(turret.Yaw + step)
				}
				if err := wrapAngle(desired - turret.Yaw); err > vehAimToleranceRad || err < -vehAimToleranceRad {
					continue
				}
			}

			cooldown := 1.0 / weapon.RoF
			if weapon.LastFiredAt != 0 && now-weapon.LastFiredAt < cooldown {
				continue
			}

			// Air targets resolve analytically (weapon_air.go) — the raycast
			// window cannot reach them and does not need to. LOS is checked
			// BEFORE the ammo commit: no rounds into a ridge.
			if targetIsAir {
				muzzle := worldXYZ(*pos, spec.BoxHgt)
				if sys.airShotBlocked(muzzle, targetPos) {
					continue
				}
				weapon.LastFiredAt = now
				weapon.Ammo--
				velX, velZ := targetVelXZ(sys.targetMotionOf(target))
				sys.resolveAirShotSerial(muzzle, *pos, target, targetPos, velX, velZ,
					weapon, wspec, now,
					uint64(shooter.ID())^uint64(now*1000.0)^uint64(slot)<<32)
				continue
			}

			targetStance := sys.targetStanceOf(target)
			dispersion := weapon.Dispersion
			if motion.Speed > vehMovingSpeed || motion.Speed < -vehMovingSpeed {
				dispersion *= 1 + vehMoveDispersionMul
			}
			targetY := components.SpecForStance(targetStance).TargetCenterY
			if tv := sys.targetVehicleOf(target); tv != nil {
				targetY = components.SpecForVehicle(tv.Kind).BoxHgt * 0.5
			}

			weapon.LastFiredAt = now
			weapon.Ammo--
			sys.shotsBuf = append(sys.shotsBuf, shotWork{
				shooter:       shooter,
				muzzle:        worldXYZ(*pos, spec.BoxHgt),
				aim:           worldXYZ(targetPos, targetY),
				dispersion:    dispersion,
				rangeMax:      clampWeaponRange(weapon.RangeM),
				damage:        float32(weapon.Damage),
				targetEntity:  target,
				targetStance:  targetStance,
				tracerColor:   tracerColorFor(weapon.Kind),
				shooterChunk:  pos.Chunk,
				muzzlePos:     *pos,
				rngSeed:       uint64(shooter.ID()) ^ uint64(now*1000.0) ^ uint64(slot)<<32,
				splashRadius:  wspec.SplashRadius,
				splashFalloff: wspec.SplashFalloff,
				vsSoft:        wspec.VsSoft,
				vsLight:       wspec.VsLight,
				vsHeavy:       wspec.VsHeavy,
				trail:         weapon.Kind == components.WeaponRPG7 || weapon.Kind == components.WeaponATGM,
			})
		}
	}
}
