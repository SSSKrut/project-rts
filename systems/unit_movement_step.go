package systems

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
	"rts-go/core"
)

// staminaMarkerOp is the result of one unit step's stamina decision.
type staminaMarkerOp uint8

const (
	staminaMarkerNone staminaMarkerOp = iota
	staminaMarkerAdd
	staminaMarkerRemove
)

// step advances one unit by dt seconds. Race-safe: writes go through the
// snapshot's per-unit pointers, neighbour reads through the hash snapshot.
func (sys *UnitMovementSystem) step(
	w unitWork,
	dt, now float32,
	hash *core.SpatialHash,
	vehHash *core.SpatialHash,
	walls map[components.ChunkCoord][]colWall,
) staminaMarkerOp {
	selfX := float32(w.pos.Chunk.X)*components.ChunkSize + w.pos.Local.X
	selfZ := float32(w.pos.Chunk.Z)*components.ChunkSize + w.pos.Local.Z
	selfRadius := float32(0.4)
	if col := sys.colliderMap.Get(w.ent); col != nil && col.Radius > 0 {
		selfRadius = col.Radius
	}
	// Hulls move infantry, not vice versa (P5) — and idle units too, so this
	// runs before the empty-queue early-out. Two cases per neighbour hull:
	// already overlapping → radial shove; inside the projected corridor of a
	// moving hull → lateral sidestep BEFORE contact. Worker-safe: reads the
	// frozen vehicle snapshot, writes only own pos.
	if vehHash != nil {
		vehHash.ForEachEntryInRadius(selfX, selfZ, vehYieldQueryR, func(e *core.SpatialEntry, dSq float32) {
			// Storey filter: the hash is XZ-only — a hull under a bridge
			// deck must not shove the walker crossing above it.
			if dy := e.Y - w.pos.Local.Y; dy > neighbourStoreyBand || dy < -neighbourStoreyBand {
				return
			}
			minD := e.Radius + selfRadius
			if dSq < minD*minD {
				d := float32(math.Sqrt(float64(dSq)))
				var nx, nz float32
				if d > 1e-4 {
					nx, nz = (selfX-e.X)/d, (selfZ-e.Z)/d
				} else {
					nx, nz = 1, 0
				}
				push := minD - d
				if lim := vehShoveCap * dt; push > lim {
					push = lim
				}
				*w.pos = w.pos.Add(rl.Vector3{X: nx * push, Z: nz * push})
				selfX = float32(w.pos.Chunk.X)*components.ChunkSize + w.pos.Local.X
				selfZ = float32(w.pos.Chunk.Z)*components.ChunkSize + w.pos.Local.Z
				return
			}
			vSq := e.VelX*e.VelX + e.VelZ*e.VelZ
			if vSq < 1 {
				return
			}
			v := float32(math.Sqrt(float64(vSq)))
			ox, oz := selfX-e.X, selfZ-e.Z
			ahead := (ox*e.VelX + oz*e.VelZ) / v
			if ahead < 0 || ahead > v*vehYieldHorizon+minD {
				return
			}
			lat := (ox*e.VelZ - oz*e.VelX) / v
			absLat := lat
			if absLat < 0 {
				absLat = -absLat
			}
			if absLat >= minD+vehYieldLatPad {
				return
			}
			// Step away from the corridor centreline; dead-centre picks a
			// deterministic side from the entity ID.
			var px, pz float32
			if lat > 0 || (lat == 0 && w.ent.ID()&1 == 1) {
				px, pz = e.VelZ/v, -e.VelX/v
			} else {
				px, pz = -e.VelZ/v, e.VelX/v
			}
			push := vehShoveCap * dt
			*w.pos = w.pos.Add(rl.Vector3{X: px * push, Z: pz * push})
			selfX = float32(w.pos.Chunk.X)*components.ChunkSize + w.pos.Local.X
			selfZ = float32(w.pos.Chunk.Z)*components.ChunkSize + w.pos.Local.Z
		})
	}
	// StaminaExhausted forces Walk; otherwise use the profile's Pace, one
	// tier up while the member lags its formation slot (bb.CatchUp).
	effectivePace := w.profile.Pace
	stepBB := sys.blackboardMap.Get(w.ent)
	if w.exhausted {
		effectivePace = components.PaceWalk
	} else if stepBB != nil && stepBB.CatchUp && effectivePace < components.PaceSprint {
		effectivePace++
	}

	// Stance auto-transition when no ActionStance is at the queue head; the
	// LockUntil gate prevents flapping with StanceControllerSystem (which
	// drops the unit into Prone under fire).
	hasActiveStanceAction := w.queue.Count > 0 && w.queue.Actions[w.queue.Head].Kind == components.ActionStance
	if !hasActiveStanceAction && w.stance.Code != w.profile.Stance && now >= w.stance.LockUntil {
		w.stance.Code = w.profile.Stance
	}

	// Drain / regen Stamina. Recovery only when Pace=Walk AND Stance ∈
	// {Stand, Crouch}; Prone doesn't recover.
	markerOp := staminaMarkerNone
	if w.stamina != nil && w.stamina.MaxLevel > 0 {
		drain := components.PaceStaminaDrain[effectivePace] * dt
		switch effectivePace {
		case components.PaceWalk:
			if w.stance.Code != components.StanceProne {
				w.stamina.Current += w.stamina.RecoverRate * dt
				if w.stamina.Current > w.stamina.MaxLevel {
					w.stamina.Current = w.stamina.MaxLevel
				}
			}
		default:
			w.stamina.Current -= drain
			if w.stamina.Current < 0 {
				w.stamina.Current = 0
			}
		}
		// Hysteresis: marker sets at zero, clears past regen threshold.
		switch {
		case !w.exhausted && w.stamina.Current <= 0:
			markerOp = staminaMarkerAdd
		case w.exhausted && w.stamina.Current >= staminaRegenThreshold*w.stamina.MaxLevel:
			markerOp = staminaMarkerRemove
		}
	}

	// Per-unit SpeedMul from a SplitMix hash of entity ID; ±5% so squad
	// members visibly drift instead of lock-stepping.
	maxSpeed := components.SpecForStance(w.stance.Code).MaxSpeed * components.PaceSpeedMul[effectivePace]
	personalityHash := slotHash32(uint32(w.ent.ID()))
	speedJitter := perUnitSpeedSpread * (2*float32(personalityHash&0xFFFF)/0xFFFF - 1)
	maxSpeed *= 1 + speedJitter

	if w.queue.Count == 0 {
		// Brake instead of instant zero so the unit decelerates visibly
		// when the queue drains.
		brake := stanceAccel[w.stance.Code] * dt
		if w.mot.Speed > brake {
			w.mot.Speed -= brake
		} else {
			w.mot.Speed = 0
		}
		return markerOp
	}
	action := &w.queue.Actions[w.queue.Head]
	switch action.Kind {
	case components.ActionMoveTo:
		// Steer at the current MicroPath waypoint when available so the
		// unit follows the planner's route through doors / obstacles;
		// fall back to action.Target when the path is empty.
		shortTerm := action.Target
		morePath := w.microPath != nil && w.microPath.Count > 0 && w.microPath.Head < w.microPath.Count
		if morePath {
			shortTerm = w.microPath.Waypoints[w.microPath.Head]
		}
		finalDiff := action.Target.Sub(*w.pos)
		if finalDiff.X*finalDiff.X+finalDiff.Z*finalDiff.Z < arrivalRadius*arrivalRadius {
			// XZ alone is not arrival for storey goals: stairs run under
			// the upper-floor slots, so a climber matches the slot's XZ
			// mid-ramp and would freeze a floor below. While the planner
			// still has waypoints, require the Y to be within a storey's
			// reach too; with the path exhausted, pop on XZ as before
			// (ground orders may carry Y=0 vs. terrain at ±2 m) — but never
			// through a wall: a target 0.5 m away in the NEXT room must not
			// count as reached (the walker parks in the wrong room), it
			// must replan through the door.
			dy := finalDiff.Y
			if !morePath || (dy > -arrivalYBand && dy < arrivalYBand) {
				if !wallBlocksApproach(w.pos, action.Target, walls) {
					popAction(w.queue)
					return markerOp
				}
			}
		}
		diff := shortTerm.Sub(*w.pos)
		distSq := diff.X*diff.X + diff.Z*diff.Z
		shortArrive := arrivalRadius
		if morePath && w.microPath.Head+1 < w.microPath.Count &&
			w.microPath.GateMask&(1<<(w.microPath.Head+1)) != 0 {
			// Mouth waypoint before a gate: keep steering until the tight
			// radius MicroPathSystem pops at, or the walker stalls 0.6 m out.
			shortArrive = gateMouthRadius
		}
		if distSq < shortArrive*shortArrive {
			// Reached the short-term waypoint; don't pop (final-goal arrival
			// already handled). MicroPathSystem advances Head next tick.
			return markerOp
		}
		dist := float32(math.Sqrt(float64(distSq)))
		invDist := 1 / dist
		desiredX := diff.X * invDist
		desiredZ := diff.Z * invDist

		// ORCA agent-agent avoidance via SpatialHash neighbours; walls go
		// through reflectAgainstWalls.
		var neighbours []orcaAgent
		if hash != nil {
			// Snapshot-only: live neighbour map reads race with owner workers.
			hash.ForEachEntryInRadius(selfX, selfZ, orcaNeighbourRadius, func(e *core.SpatialEntry, _ float32) {
				if e.Ent == w.ent {
					return
				}
				// Storey filter: XZ-only hash — a mate parked one floor up
				// (stair exit, balcony) is not an obstacle down here; without
				// this the climber decelerates into a phantom and stalls.
				if dy := e.Y - w.pos.Local.Y; dy > neighbourStoreyBand || dy < -neighbourStoreyBand {
					return
				}
				neighbours = append(neighbours, orcaAgent{
					Pos:    orcaVec2{X: e.X, Z: e.Z},
					Vel:    orcaVec2{X: e.VelX, Z: e.VelZ},
					Radius: e.Radius,
					Resp:   0.5,
				})
			})
		}
		if vehHash != nil {
			// Hull neighbours enter with full responsibility on the unit —
			// the vehicle never reciprocates (P5).
			vehHash.ForEachEntryInRadius(selfX, selfZ, orcaVehNeighbourRadius, func(e *core.SpatialEntry, _ float32) {
				if dy := e.Y - w.pos.Local.Y; dy > neighbourStoreyBand || dy < -neighbourStoreyBand {
					return
				}
				neighbours = append(neighbours, orcaAgent{
					Pos:    orcaVec2{X: e.X, Z: e.Z},
					Vel:    orcaVec2{X: e.VelX, Z: e.VelZ},
					Radius: e.Radius + vehOrcaPad,
					Resp:   1,
				})
			})
		}
		// Current self velocity for ORCA's reciprocal share.
		selfVx, selfVz := float32(0), float32(0)
		if w.mot.Speed > 0 {
			selfVx = float32(math.Sin(float64(w.mot.VelocityYaw))) * w.mot.Speed
			selfVz = float32(math.Cos(float64(w.mot.VelocityYaw))) * w.mot.Speed
		}
		prefVel := orcaVec2{X: desiredX * maxSpeed, Z: desiredZ * maxSpeed}
		self := orcaAgent{
			Pos:    orcaVec2{X: selfX, Z: selfZ},
			Vel:    orcaVec2{X: selfVx, Z: selfVz},
			Radius: selfRadius,
		}
		adjusted, orcaFeasible := orcaAdjust(self, neighbours, prefVel, maxSpeed)
		vx := adjusted.X
		vz := adjusted.Z
		desiredSpeed := float32(math.Sqrt(float64(vx*vx + vz*vz)))

		// Replan triggers: two blackboard counters accumulate dt under
		// stalling conditions; crossing threshold flips MicroPath.Dirty.
		//   OvercrowdedSince — ORCA infeasible (unit in inescapable crowd).
		//   StuckSince       — Speed stays low while prefVel wants movement
		//                      (every direction half-blocked).
		const stallReplanThresh float32 = 0.5
		if bb := sys.blackboardMap.Get(w.ent); bb != nil {
			if !orcaFeasible {
				bb.OvercrowdedSince += dt
			} else {
				bb.OvercrowdedSince = 0
			}
			wantsMove := prefVel.lenSq() > 1
			if wantsMove && w.mot.Speed < 0.3 {
				bb.StuckSince += dt
			} else {
				bb.StuckSince = 0
			}
			if (bb.OvercrowdedSince > stallReplanThresh || bb.StuckSince > stallReplanThresh) &&
				w.microPath != nil {
				w.microPath.Dirty = true
				bb.OvercrowdedSince = 0
				bb.StuckSince = 0
			}
		}

		// Acceleration ramp: speed approaches desired magnitude at most
		// stanceAccel[Stance] m/s² per tick, so units visibly spin up and
		// brake instead of snapping.
		accel := stanceAccel[w.stance.Code]
		maxDelta := accel * dt
		speed := w.mot.Speed
		switch {
		case desiredSpeed > speed+maxDelta:
			speed += maxDelta
		case desiredSpeed < speed-maxDelta:
			speed -= maxDelta
		default:
			speed = desiredSpeed
		}
		if desiredSpeed > 1e-3 {
			vx *= speed / desiredSpeed
			vz *= speed / desiredSpeed
		} else {
			vx, vz = 0, 0
		}

		// Slide along wall tangent when the predicted XZ step would cross
		// a wall in the 3×3 chunk window.
		if walls != nil {
			vx, vz = reflectAgainstWalls(selfX, selfZ, w.pos.Local.Y, vx, vz, dt, walls, w.pos.Chunk)
		}

		// Y lerp lets units climb stairs / drop into bunkers without
		// teleporting; GroundStick picks the closest-Y floor next tick.
		// Final-target Y (a replanned path can re-root at the stair BOTTOM —
		// a short-term-waypoint lerp would drag a mid-climb walker back
		// down), but rate-clamped: the unclamped lerp walked a chord through
		// terrain relief on long surface orders (ISSUES #13). 3 m/s tracks
		// any stair ramp; the surface residual is one tick's worth (~5 cm).
		dy := action.Target.Local.Y - w.pos.Local.Y
		progress := float32(0)
		if dist > 0 {
			progress = dt * speed / dist
			if progress > 1 {
				progress = 1
			}
		}
		yStep := dy * progress
		// 8 m/s: enough to hook onto a 45-deg stair ramp at sprint (the
		// GroundStick closest-Y capture needs pos.Y raised to the ramp fast),
		// while capping the #13 surface chord-dive at ~0.13 m per tick.
		const yLerpMaxRate float32 = 8.0
		if maxY := yLerpMaxRate * dt; yStep > maxY {
			yStep = maxY
		} else if yStep < -maxY {
			yStep = -maxY
		}
		// Combat-move: under Alerted/Threatened the body faces the threat
		// (weapons on target) while legs walk along VelocityYaw. Throttle
		// to 0.3× when body > 90° off motion and > 3 m to cover, so the
		// turn lands before the sprint.
		velocityYaw := w.mot.VelocityYaw
		if speed > 0.01 {
			velocityYaw = float32(math.Atan2(float64(vx), float64(vz)))
		}
		desiredFacingYaw := velocityYaw
		if w.threat != nil && w.threat.State >= components.ThreatAlerted &&
			(w.threat.ThreatDir.X != 0 || w.threat.ThreatDir.Z != 0) {
			desiredFacingYaw = float32(math.Atan2(
				float64(-w.threat.ThreatDir.X),
				float64(-w.threat.ThreatDir.Z),
			))
		}
		if dist > 3.0 && speed > 0.5 {
			bodyDelta := wrapAngle(velocityYaw - w.mot.Yaw)
			if bodyDelta > math.Pi/2 || bodyDelta < -math.Pi/2 {
				speed *= 0.3
				if desiredSpeed > 1e-3 {
					rescale := speed / desiredSpeed
					vx *= rescale
					vz *= rescale
				}
			}
		}

		move := rl.Vector3{X: vx * dt, Y: yStep, Z: vz * dt}
		*w.pos = w.pos.Add(move)
		w.mot.Speed = speed
		w.mot.VelocityYaw = velocityYaw

		// Cap yaw rate so units don't snap-spin. wrapAngle folds 350° into
		// -10° so the short-way turn lands.
		delta := wrapAngle(desiredFacingYaw - w.mot.Yaw)
		maxYawDelta := maxYawRate * dt
		if delta > maxYawDelta {
			delta = maxYawDelta
		} else if delta < -maxYawDelta {
			delta = -maxYawDelta
		}
		w.mot.Yaw += delta

	case components.ActionStop:
		w.mot.Speed = 0
		if w.queue.StopUntil == 0 {
			w.queue.StopUntil = now + stopDuration
		} else if now >= w.queue.StopUntil {
			w.queue.StopUntil = 0
			popAction(w.queue)
		}

	case components.ActionStance:
		w.stance.Code = components.StanceCode(action.Target.Local.Y)
		popAction(w.queue)
	}
	return markerOp
}

// wrapAngle folds an angle into [-π, π].
func wrapAngle(a float32) float32 {
	for a > math.Pi {
		a -= 2 * math.Pi
	}
	for a < -math.Pi {
		a += 2 * math.Pi
	}
	return a
}

func popAction(q *components.ActionQueue) {
	if q.Count == 0 {
		return
	}
	q.Actions[q.Head] = components.Action{}
	q.Head = (q.Head + 1) % components.ActionQueueSize
	q.Count--
	q.StopUntil = 0
}

// PushAction appends an action; drops the oldest when full (preserves most
// recent intent).
func PushAction(q *components.ActionQueue, a components.Action) {
	if q.Count == components.ActionQueueSize {
		q.Head = (q.Head + 1) % components.ActionQueueSize
		q.Count--
	}
	q.Actions[q.Tail] = a
	q.Tail = (q.Tail + 1) % components.ActionQueueSize
	q.Count++
}

// ClearActions resets the queue to empty.
func ClearActions(q *components.ActionQueue) {
	for i := range q.Actions {
		q.Actions[i] = components.Action{}
	}
	q.Head = 0
	q.Tail = 0
	q.Count = 0
	q.StopUntil = 0
}
