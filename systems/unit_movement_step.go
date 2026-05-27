package systems

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

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

// step advances one unit by dt seconds. Race-safe: every write goes through
// the snapshot's per-unit pointers and never touches shared maps. Returns
// the StaminaExhausted marker toggle decision (caller batches it for the
// serial post-pass).
func (sys *UnitMovementSystem) step(
	w unitWork,
	dt float32,
	hash *core.SpatialHash,
	walls map[components.ChunkCoord][]colWall,
) staminaMarkerOp {
	// StaminaExhausted forces Walk; otherwise use the profile's Pace.
	effectivePace := w.profile.Pace
	if w.exhausted {
		effectivePace = components.PaceWalk
	}

	// Stance auto-transition when no ActionStance is at the queue head; the
	// LockUntil gate prevents flapping with StanceControllerSystem (which
	// drops the unit into Prone under fire).
	hasActiveStanceAction := w.queue.Count > 0 && w.queue.Actions[w.queue.Head].Kind == components.ActionStance
	if !hasActiveStanceAction && w.stance.Code != w.profile.Stance && sys.elapsed >= w.stance.LockUntil {
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
		if w.microPath != nil && w.microPath.Count > 0 && w.microPath.Head < w.microPath.Count {
			shortTerm = w.microPath.Waypoints[w.microPath.Head]
		}
		finalDiff := action.Target.Sub(*w.pos)
		if finalDiff.X*finalDiff.X+finalDiff.Z*finalDiff.Z < arrivalRadius*arrivalRadius {
			popAction(w.queue)
			return markerOp
		}
		diff := shortTerm.Sub(*w.pos)
		distSq := diff.X*diff.X + diff.Z*diff.Z
		if distSq < arrivalRadius*arrivalRadius {
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
		selfX := float32(w.pos.Chunk.X)*components.ChunkSize + w.pos.Local.X
		selfZ := float32(w.pos.Chunk.Z)*components.ChunkSize + w.pos.Local.Z
		var neighbours []orcaAgent
		if hash != nil {
			hash.ForEachInRadius(selfX, selfZ, orcaNeighbourRadius, func(ent ecs.Entity, _ float32) {
				if ent == w.ent {
					return
				}
				if !sys.world.Alive(ent) {
					return
				}
				np := sys.posMap.Get(ent)
				if np == nil {
					return
				}
				nMot := sys.motionMap.Get(ent)
				nx := float32(np.Chunk.X)*components.ChunkSize + np.Local.X
				nz := float32(np.Chunk.Z)*components.ChunkSize + np.Local.Z
				var nVx, nVz float32
				if nMot != nil && nMot.Speed > 0 {
					nVx = float32(math.Sin(float64(nMot.VelocityYaw))) * nMot.Speed
					nVz = float32(math.Cos(float64(nMot.VelocityYaw))) * nMot.Speed
				}
				nRadius := float32(0.4)
				if col := sys.colliderMap.Get(ent); col != nil && col.Radius > 0 {
					nRadius = col.Radius
				}
				neighbours = append(neighbours, orcaAgent{
					Pos:    orcaVec2{X: nx, Z: nz},
					Vel:    orcaVec2{X: nVx, Z: nVz},
					Radius: nRadius,
				})
			})
		}

		selfRadius := float32(0.4)
		if col := sys.colliderMap.Get(w.ent); col != nil && col.Radius > 0 {
			selfRadius = col.Radius
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
		dy := action.Target.Local.Y - w.pos.Local.Y
		progress := float32(0)
		if dist > 0 {
			progress = dt * speed / dist
			if progress > 1 {
				progress = 1
			}
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

		move := rl.Vector3{X: vx * dt, Y: dy * progress, Z: vz * dt}
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
			w.queue.StopUntil = sys.elapsed + stopDuration
		} else if sys.elapsed >= w.queue.StopUntil {
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
