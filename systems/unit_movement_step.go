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
// the snapshot's per-unit pointers and never touches shared maps / resources.
// Returns the StaminaExhausted marker toggle decision (caller batches it into
// the per-worker buffer for the serial post-pass).
//
// Phase 14.5 M14.5.2: separation queries the shared SpatialHash (read-only
// during the parallel section).
func (sys *UnitMovementSystem) step(
	w unitWork,
	dt float32,
	hash *core.SpatialHash,
	walls map[components.ChunkCoord][]colWall,
) staminaMarkerOp {
	// Pace selection: StaminaExhausted forces Walk (P3). Otherwise use the
	// effective profile's Pace.
	effectivePace := w.profile.Pace
	if w.exhausted {
		effectivePace = components.PaceWalk
	}

	// Stance auto-transition (P9): when no explicit ActionStance is currently
	// at the queue head, snap toward the squad's standing default. Phase 13
	// transition is instant; Phase 25 may add a time cost.
	//
	// Phase 17 M17.C: respect StanceControllerSystem's animation lock - if it
	// just dropped the unit into Prone under fire, the squad standing default
	// would otherwise pop the unit back up next tick and Stance would flap.
	hasActiveStanceAction := w.queue.Count > 0 && w.queue.Actions[w.queue.Head].Kind == components.ActionStance
	if !hasActiveStanceAction && w.stance.Code != w.profile.Stance && sys.elapsed >= w.stance.LockUntil {
		w.stance.Code = w.profile.Stance
	}

	// Drain / regen Stamina each tick. Recovery only when Pace=Walk AND
	// Stance in {Stand, Crouch} (Prone doesn't recover - P3). Open question 5
	// answered: crouch allows regen.
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
		// Marker decision: set when fully drained, clear once the unit has
		// rested past the regen threshold. Hysteresis keeps the marker from
		// flapping while the unit hovers at zero.
		switch {
		case !w.exhausted && w.stamina.Current <= 0:
			markerOp = staminaMarkerAdd
		case w.exhausted && w.stamina.Current >= staminaRegenThreshold*w.stamina.MaxLevel:
			markerOp = staminaMarkerRemove
		}
	}

	// Speed lookup uses the unit's current Stance x effective Pace. Phase 15
	// M15.B.5 - per-unit SpeedMul derived from a SplitMix hash of the entity
	// ID; ~ +/- 5 % so squad members visibly drift instead of lock-stepping.
	maxSpeed := components.SpecForStance(w.stance.Code).MaxSpeed * components.PaceSpeedMul[effectivePace]
	personalityHash := slotHash32(uint32(w.ent.ID()))
	speedJitter := perUnitSpeedSpread * (2*float32(personalityHash&0xFFFF)/0xFFFF - 1)
	maxSpeed *= 1 + speedJitter

	if w.queue.Count == 0 {
		// Phase 15 M15.B.5 - brake instead of instant zero so the unit
		// decelerates visibly when the queue drains.
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
		// M17.A: if MicroPath has unspent waypoints, steer at the current
		// waypoint instead of the final goal so the unit follows the
		// planner's route through doors / around obstacles. Falls back to
		// action.Target when the path is empty (fresh unit, soloist with no
		// formation, straight-line micro-distance, NavService.FindPath
		// returned []).
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
			// Reached the short-term waypoint; the unit is mid-route so we
			// don't popAction (the final-goal arrival check above handles
			// that). MicroPathSystem advances Head next tick.
			return markerOp
		}
		dist := float32(math.Sqrt(float64(distSq)))
		invDist := 1 / dist
		desiredX := diff.X * invDist
		desiredZ := diff.Z * invDist

		// Phase 14.5 M14.5.2: separation force pulled from the SpatialHash.
		// Callback receives nearby entries inline - no intermediate slice
		// allocation. Self is filtered via ent check; alive-check is cheap
		// here (the hash may carry indices for entities removed since
		// rebuild, but the world.Alive guard skips dead reads).
		selfX := float32(w.pos.Chunk.X)*components.ChunkSize + w.pos.Local.X
		selfZ := float32(w.pos.Chunk.Z)*components.ChunkSize + w.pos.Local.Z
		var sepX, sepZ float32
		if hash != nil {
			hash.ForEachInRadius(selfX, selfZ, separationRadius, func(ent ecs.Entity, dSq float32) {
				if ent == w.ent || dSq <= 1e-4 {
					return
				}
				// Stale-entity guard: SpatialHash invariant - readers MUST
				// alive-check every callback target before further work.
				if !sys.world.Alive(ent) {
					return
				}
				// Recompute dx/dz so the falloff direction is from the live
				// position (SpatialEntry is a 1-tick stale snapshot, but the
				// resolution mismatch is <= a few cm at top speed -
				// irrelevant for separation force direction).
				np := sys.posMap.Get(ent)
				if np == nil {
					return
				}
				nx := float32(np.Chunk.X)*components.ChunkSize + np.Local.X
				nz := float32(np.Chunk.Z)*components.ChunkSize + np.Local.Z
				dx := selfX - nx
				dz := selfZ - nz
				dd := dx*dx + dz*dz
				if dd <= 1e-4 {
					return
				}
				inv := 1 / dd
				sepX += dx * inv
				sepZ += dz * inv
			})
		}

		vx := desiredX*maxSpeed + sepX*separationWeight
		vz := desiredZ*maxSpeed + sepZ*separationWeight
		desiredSpeed := float32(math.Sqrt(float64(vx*vx + vz*vz)))
		if desiredSpeed > maxSpeed {
			inv := maxSpeed / desiredSpeed
			vx *= inv
			vz *= inv
			desiredSpeed = maxSpeed
		}

		// Phase 15 M15.B.5 - acceleration ramp. Speed approaches the desired
		// magnitude at most stanceAccel[Stance] m/s^2 per tick instead of
		// snapping; units visibly spin up out of stop and brake on arrival.
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

		// Wall sliding (M15.B.3). Velocity slides along the wall tangent
		// when the predicted XZ step would cross a wall in the unit's 3x3
		// chunk window.
		if walls != nil {
			vx, vz = reflectAgainstWalls(selfX, selfZ, vx, vz, dt, walls, w.pos.Chunk)
		}

		// Y lerp toward target. Lets units climb stairs / drop into bunkers
		// without teleporting; GroundStick then picks the floor whose Y is
		// closest on the next tick.
		dy := action.Target.Local.Y - w.pos.Local.Y
		progress := float32(0)
		if dist > 0 {
			progress = dt * speed / dist
			if progress > 1 {
				progress = 1
			}
		}
		// Phase 17 M17.B.4 - combat-move: under Alerted/Threatened, the
		// body faces the threat (so weapons stay on target) while the legs
		// walk along VelocityYaw. The throttle below also keeps the unit
		// from running backwards: when the body is > 90 deg off the
		// desired motion direction and there's still > 3 m to cover, we
		// cut speed to 0.3x so the turn lands before the sprint.
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

		// Phase 15 M15.B.5 - cap yaw rate so units don't snap-spin. Wrap the
		// delta into [-pi, pi] before clamping so a 350 deg desired turn
		// folds into -10 deg the short way around.
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

// wrapAngle folds an angle in radians into [-pi, pi]. Used by yaw delta math
// so a 350 deg "desired" turn becomes a -10 deg short-way turn.
func wrapAngle(a float32) float32 {
	for a > math.Pi {
		a -= 2 * math.Pi
	}
	for a < -math.Pi {
		a += 2 * math.Pi
	}
	return a
}

// popAction advances the queue head past the current action.
func popAction(q *components.ActionQueue) {
	if q.Count == 0 {
		return
	}
	q.Actions[q.Head] = components.Action{}
	q.Head = (q.Head + 1) % components.ActionQueueSize
	q.Count--
	q.StopUntil = 0
}

// PushAction appends an action to the queue. If the queue is full, the
// oldest entry is dropped to make room (preserves the most recent intent).
// Exposed so main.go can wire orders without re-implementing the ring-buffer
// math.
func PushAction(q *components.ActionQueue, a components.Action) {
	if q.Count == components.ActionQueueSize {
		// Drop oldest.
		q.Head = (q.Head + 1) % components.ActionQueueSize
		q.Count--
	}
	q.Actions[q.Tail] = a
	q.Tail = (q.Tail + 1) % components.ActionQueueSize
	q.Count++
}

// ClearActions resets the queue to empty. Used by RMB immediate override.
func ClearActions(q *components.ActionQueue) {
	for i := range q.Actions {
		q.Actions[i] = components.Action{}
	}
	q.Head = 0
	q.Tail = 0
	q.Count = 0
	q.StopUntil = 0
}
