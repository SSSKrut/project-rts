package systems

import (
	"fmt"
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
	// A blown position otherwise turns into an unbounded spatial-hash query —
	// an infinite HANG the panic guard can't catch (found via the throttle
	// double-rescale runaway). Panic while the numbers still say who and why.
	if selfX != selfX || selfZ != selfZ || selfX < -1e5 || selfX > 1e5 || selfZ < -1e5 || selfZ > 1e5 {
		panic(fmt.Sprintf("unit pos blown ent=%v x=%v z=%v speed=%v vyaw=%v",
			w.ent, selfX, selfZ, w.mot.Speed, w.mot.VelocityYaw))
	}
	selfRadius := float32(0.4)
	if col := sys.colliderMap.Get(w.ent); col != nil && col.Radius > 0 {
		selfRadius = col.Radius
	}
	// Single-accounting (MA3): a unit that will run ORCA this tick already
	// carries the hull as a Resp=1 neighbour — the corridor sidestep on top
	// double-counts the avoidance and jerks the walker sideways. Idle units
	// never solve ORCA, so the corridor stays their only warning.
	willOrca := w.queue.Count > 0 && w.queue.Actions[w.queue.Head].Kind == components.ActionMoveTo
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
				sx, sz := clipStepAgainstWalls(selfX, selfZ, w.pos.Local.Y,
					nx*push, nz*push, walls, w.pos.Chunk)
				*w.pos = w.pos.Add(rl.Vector3{X: sx, Z: sz})
				selfX = float32(w.pos.Chunk.X)*components.ChunkSize + w.pos.Local.X
				selfZ = float32(w.pos.Chunk.Z)*components.ChunkSize + w.pos.Local.Z
				return
			}
			if willOrca {
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
			sx, sz := clipStepAgainstWalls(selfX, selfZ, w.pos.Local.Y,
				px*push, pz*push, walls, w.pos.Chunk)
			*w.pos = w.pos.Add(rl.Vector3{X: sx, Z: sz})
			selfX = float32(w.pos.Chunk.X)*components.ChunkSize + w.pos.Local.X
			selfZ = float32(w.pos.Chunk.Z)*components.ChunkSize + w.pos.Local.Z
		})
	}
	// Unit-unit overlap resolve — idle units included, so it precedes the
	// empty-queue early-out. Symmetric halves: both parties compute their own
	// push from the same frozen snapshot. The deepest overlap's normal is
	// kept: the steering pass slides velocity along that body (bodies are
	// hard — pressing INTO one holds a shove-vs-drive equilibrium forever).
	var bodyNX, bodyNZ, bodyDepth float32
	// Whether the body pressing hardest is actually going somewhere: make-way
	// is for a walker trying to get past, not for a parked neighbour touching
	// shoulders (the shove already separates those, and stepping off the slot
	// only makes formation walk the man back into him).
	bodyMoving := false
	if hash != nil {
		hash.ForEachEntryInRadius(selfX, selfZ, unitShoveQueryR, func(e *core.SpatialEntry, dSq float32) {
			if e.Ent == w.ent {
				return
			}
			if dy := e.Y - w.pos.Local.Y; dy > neighbourStoreyBand || dy < -neighbourStoreyBand {
				return
			}
			minD := e.Radius + selfRadius
			if dSq >= minD*minD {
				return
			}
			d := float32(math.Sqrt(float64(dSq)))
			var nx, nz float32
			switch {
			case d > 1e-4:
				nx, nz = (selfX-e.X)/d, (selfZ-e.Z)/d
			case w.ent.ID() < e.Ent.ID():
				nx, nz = 1, 0
			default:
				nx, nz = -1, 0
			}
			if depth := minD - d; depth > bodyDepth {
				bodyDepth = depth
				bodyNX, bodyNZ = nx, nz
				bodyMoving = e.VelX*e.VelX+e.VelZ*e.VelZ > 0.25
			}
			push := (minD - d) * 0.5
			if lim := unitShoveCap * dt; push > lim {
				push = lim
			}
			sx, sz := clipStepAgainstWalls(selfX, selfZ, w.pos.Local.Y,
				nx*push, nz*push, walls, w.pos.Chunk)
			*w.pos = w.pos.Add(rl.Vector3{X: sx, Z: sz})
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
		// Brake instead of instant zero so the unit decelerates visibly when
		// the queue drains. (Phase 19.8 tried zeroing here to kill the phantom
		// velocity the spatial hash publishes for ~2 s after a sprint; it costs
		// column cohesion — ai_march_column clusterHold 0.73 -> 2.08 s — for a
		// crowd win the ORCA work already delivers.)
		brake := stanceAccel[w.stance.Code] * dt
		if w.mot.Speed > brake {
			w.mot.Speed -= brake
		} else {
			w.mot.Speed = 0
		}
		// Make way. A man parked in the only doorway blocks the storey: nav is
		// blind to bodies, the walker's blocked-pop is disabled at gates on
		// purpose, and a replan hands back the identical route forever. Steady
		// pressure from another body is the signal; formation walks him back
		// to his slot once the pressure is gone.
		if stepBB != nil {
			if bodyDepth > 0 && bodyMoving && !w.hasOverride {
				stepBB.YieldPressure += dt
			} else {
				stepBB.YieldPressure = 0
			}
			if sx, sz, ok := yieldStep(stepBB.YieldPressure, dt, bodyNX, bodyNZ); ok {
				cx, cz := clipStepAgainstWalls(selfX, selfZ, w.pos.Local.Y,
					sx, sz, walls, w.pos.Chunk)
				*w.pos = w.pos.Add(rl.Vector3{X: cx, Z: cz})
			}
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
				// Inside the arrival ring with the straight line walled off —
				// a dead band no other watchdog covers: the exhausted-path
				// replan wants > 1 m, and the stuck timers want either
				// waypoints or an honest low speed the escape spring inflates
				// past. Plan instead of grinding along the facade forever.
				if w.microPath != nil {
					w.microPath.Dirty = true
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
		// Nearest-K bounded insertion: hash iteration is bucket order, and
		// a blind cut under crowding can drop the closest bodies. The sorted
		// prefix also feeds the predicted-step contact clamp below.
		type nb struct {
			a   orcaAgent
			dSq float32
		}
		var near [orcaMaxNeighbours]nb
		nearN := 0
		if hash != nil {
			// Snapshot-only: live neighbour map reads race with owner workers.
			hash.ForEachEntryInRadius(selfX, selfZ, orcaNeighbourRadius, func(e *core.SpatialEntry, dSq float32) {
				if e.Ent == w.ent {
					return
				}
				// Storey filter: XZ-only hash — a mate parked one floor up
				// (stair exit, balcony) is not an obstacle down here; without
				// this the climber decelerates into a phantom and stalls.
				if dy := e.Y - w.pos.Local.Y; dy > neighbourStoreyBand || dy < -neighbourStoreyBand {
					return
				}
				resp := float32(0.5)
				if e.VelX*e.VelX+e.VelZ*e.VelZ < 0.01 {
					// A standing body never reciprocates (no solver call while
					// idle) — the mover shoulders the whole avoidance.
					resp = 1
				}
				i := nearN
				if i == orcaMaxNeighbours {
					if dSq >= near[i-1].dSq {
						return
					}
					i--
				} else {
					nearN++
				}
				for i > 0 && near[i-1].dSq > dSq {
					near[i] = near[i-1]
					i--
				}
				near[i] = nb{a: orcaAgent{
					Pos:    orcaVec2{X: e.X, Z: e.Z},
					Vel:    orcaVec2{X: e.VelX, Z: e.VelZ},
					Radius: e.Radius,
					Resp:   resp,
				}, dSq: dSq}
			})
			for i := 0; i < nearN; i++ {
				neighbours = append(neighbours, near[i].a)
			}
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
		// Bounded steering rotation from the current velocity direction —
		// see velTurnRate. Skipped from near-rest (a starting unit picks any
		// direction freely) and on gate/mouth approaches: stairs demand a
		// tight 180° at the flight base, and the slew's turn arc at sprint
		// (~0.6 m) sweeps the walker off the ramp band forever (office
		// cascade). Precision beats smoothness at openings.
		gateApproach := morePath && (w.microPath.GateMask&(1<<w.microPath.Head) != 0 ||
			(w.microPath.Head+1 < w.microPath.Count &&
				w.microPath.GateMask&(1<<(w.microPath.Head+1)) != 0))
		if !gateApproach && w.mot.Speed > 0.5 && desiredSpeed > 1e-3 {
			wantYaw := float32(math.Atan2(float64(vx), float64(vz)))
			delta := wrapAngle(wantYaw - w.mot.VelocityYaw)
			if maxRot := velTurnRate * dt; delta > maxRot || delta < -maxRot {
				if delta > 0 {
					delta = maxRot
				} else {
					delta = -maxRot
				}
				newYaw := w.mot.VelocityYaw + delta
				vx = float32(math.Sin(float64(newYaw))) * desiredSpeed
				vz = float32(math.Cos(float64(newYaw))) * desiredSpeed
			}
		}

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
		if desiredSpeed < 0.5 {
			// Emergency brake: ORCA wants a (near-)stop — 3× decel keeps the
			// halt visibly abrupt without the one-tick velocity teleport.
			maxDelta *= 3
		}
		speed := w.mot.Speed
		switch {
		case desiredSpeed > speed+maxDelta:
			speed += maxDelta
		case desiredSpeed < speed-maxDelta:
			speed -= maxDelta
		default:
			speed = desiredSpeed
		}
		switch {
		case desiredSpeed > 1e-3:
			vx *= speed / desiredSpeed
			vz *= speed / desiredSpeed
		case speed > 1e-3:
			// Dead-stop request: coast down through the ramp along the
			// current direction instead of zeroing v in one tick.
			vx = float32(math.Sin(float64(w.mot.VelocityYaw))) * speed
			vz = float32(math.Cos(float64(w.mot.VelocityYaw))) * speed
		default:
			vx, vz = 0, 0
		}

		// Slide along wall tangent when the predicted XZ step would cross
		// a wall in the 3×3 chunk window.
		escape := false
		if walls != nil {
			vx, vz, escape = reflectAgainstWalls(selfX, selfZ, w.pos.Local.Y, vx, vz, dt, walls, w.pos.Chunk)
		}
		// Body brake: bleed the into-body velocity component at a bounded
		// rate — a one-tick projection is a |Δv| spike (crowd jerk metric),
		// and the contact clamp already guarantees the position never enters
		// the body while the approach decays.
		if bodyDepth > 0 {
			if dot := vx*bodyNX + vz*bodyNZ; dot < 0 {
				c := -dot
				if lim := bodyBrakeRate * dt; c > lim {
					c = lim
				}
				vx += c * bodyNX
				vz += c * bodyNZ
			}
		}
		if w.microPath != nil {
			if w.microPath.MoveTicks < math.MaxUint16 {
				w.microPath.MoveTicks++
			}
			if escape && w.microPath.EscapeTicks < math.MaxUint16 {
				w.microPath.EscapeTicks++
			}
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
		// Must cover a 45-deg stair ramp at MAX sprint (8.4 m/s = pace mul
		// 1.6 × jitter — a CatchUp sprinter under 8.0 failed to hook the
		// cascade and orbited the office stairs forever), while capping the
		// #13 surface chord-dive at ~0.17 m per tick.
		const yLerpMaxRate float32 = 10.0
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
		threatFacing := false
		if !w.evacuating && w.threat != nil && w.threat.State >= components.ThreatAlerted &&
			(w.threat.ThreatDir.X != 0 || w.threat.ThreatDir.Z != 0) {
			desiredFacingYaw = float32(math.Atan2(
				float64(-w.threat.ThreatDir.X),
				float64(-w.threat.ThreatDir.Z),
			))
			threatFacing = true
		}
		// Threat-held bodies throttle only under REAL threat: an Alerted
		// CatchUp runner otherwise crawls at 0.3× forever while its body
		// tracks a stale contact (P7-f). Plain turn lag keeps the throttle —
		// the turn should land before the sprint.
		if dist > 3.0 && speed > 0.5 && !w.interiorMove &&
			(!threatFacing || w.threat.State >= components.ThreatThreatened) {
			bodyDelta := wrapAngle(velocityYaw - w.mot.Yaw)
			if bodyDelta > math.Pi/2 || bodyDelta < -math.Pi/2 {
				// Direct ×0.3: (vx, vz) already carry the ramped magnitude —
				// re-dividing by desiredSpeed double-scaled on transients and
				// inflated |v| by speed/desired whenever ORCA wanted a
				// near-stop (position jumps; with honest Speed it compounded
				// to a runaway).
				speed *= 0.3
				vx *= 0.3
				vz *= 0.3
			}
		}

		move := rl.Vector3{X: vx * dt, Y: yStep, Z: vz * dt}
		// Contact clamp: project the predicted step out of the nearest bodies'
		// contact rings (snapshot positions). One tick of late ORCA at sprint
		// is 0.12 m of intrusion otherwise — bodies are hard, steps stop at
		// the ring.
		if nearN > 0 {
			predX := selfX + move.X
			predZ := selfZ + move.Z
			clamped := false
			for i := 0; i < nearN && i < 3; i++ {
				b := &near[i].a
				minD := b.Radius + selfRadius
				dx := predX - b.Pos.X
				dz := predZ - b.Pos.Z
				dSq := dx*dx + dz*dz
				if dSq >= minD*minD || dSq < 1e-8 {
					continue
				}
				d := float32(math.Sqrt(float64(dSq)))
				predX = b.Pos.X + dx/d*minD
				predZ = b.Pos.Z + dz/d*minD
				clamped = true
			}
			if clamped {
				// The clamp pushes out of a body without knowing about walls;
				// re-clip so a crowd can't press anyone through a facade.
				move.X, move.Z = clipStepAgainstWalls(selfX, selfZ, w.pos.Local.Y,
					predX-selfX, predZ-selfZ, walls, w.pos.Chunk)
			}
		}
		*w.pos = w.pos.Add(move)
		// Honest speed: the wall slide reshaped (vx, vz) after the ramp —
		// neighbours (hash snapshot) and the next tick's ramp read Motion,
		// and an inflated value makes ORCA dodge phantom momentum. Capped at
		// the ramp value so the escape-spring's additive boost doesn't bank
		// into the next tick's ramp (+~1 m/s per wall-pinned tick = runaway).
		if out := float32(math.Sqrt(float64(vx*vx + vz*vz))); out < speed {
			w.mot.Speed = out
		} else {
			w.mot.Speed = speed
		}
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
