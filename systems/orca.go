package systems

import (
	"math"
)

// ORCA (Optimal Reciprocal Collision Avoidance) local steering for
// agent-agent collision avoidance. Adapted from Van den Berg et al.
// "Reciprocal n-body Collision Avoidance" (RVO2) to float32 / XZ-plane.
//
// Agent-agent only; wall obstacles still go through reflectAgainstWalls.
//
// Use site: unit_movement_step.go computes a preferred velocity from
// MicroPath / formation slot, then calls orcaAdjust which returns a
// velocity that minimises change from prefVel while satisfying every
// collision-avoidance half-plane. If no feasible velocity exists, the 3D
// linear program degrades gracefully.

// Predict collisions this many seconds into the future for agent-agent.
// Higher = earlier evasive action, more cautious flow. RVO2 default.
const orcaTimeHorizon float32 = 2.0

// Query at most this many nearest neighbours per solver call. Caps O(N²) to
// O(N·K); 15 covers typical formation density.
const orcaMaxNeighbours int = 15

// Spatial-hash query radius. Wider than the danger envelope so newly
// accelerating neighbours are caught in time.
const orcaNeighbourRadius float32 = 6.0

// Used by the in-collision fallback; smaller than timeHorizon to make the
// recovery push stronger.
const orcaTimeStep float32 = 1.0 / 60.0

// orcaVec2 is the local 2D vector type. XZ-plane only — no Y.
type orcaVec2 struct {
	X, Z float32
}

func (a orcaVec2) add(b orcaVec2) orcaVec2  { return orcaVec2{a.X + b.X, a.Z + b.Z} }
func (a orcaVec2) sub(b orcaVec2) orcaVec2  { return orcaVec2{a.X - b.X, a.Z - b.Z} }
func (a orcaVec2) scale(k float32) orcaVec2 { return orcaVec2{a.X * k, a.Z * k} }
func (a orcaVec2) dot(b orcaVec2) float32   { return a.X*b.X + a.Z*b.Z }
func (a orcaVec2) lenSq() float32           { return a.X*a.X + a.Z*a.Z }
func (a orcaVec2) length() float32 {
	return float32(math.Sqrt(float64(a.X*a.X + a.Z*a.Z)))
}

// det — 2D cross product. Used to choose which side of a velocity obstacle
// leg to project onto.
func det(a, b orcaVec2) float32 { return a.X*b.Z - a.Z*b.X }

// orcaLine — one ORCA half-plane constraint. The feasible region is the
// half-plane where (vel - Point) · Normal >= 0, with Normal being Dir
// rotated 90° clockwise (Dir.Z, -Dir.X).
type orcaLine struct {
	Point orcaVec2 // a point on the boundary line
	Dir   orcaVec2 // unit-length direction along the line (feasible side
	// is on the left when facing along Dir, i.e. (Dir.Z, -Dir.X) outward)
}

type orcaAgent struct {
	Pos    orcaVec2
	Vel    orcaVec2
	Radius float32
	// Resp is the share of the avoidance vector SELF shoulders against this
	// neighbour: 0.5 between reciprocating units, 1.0 against a vehicle hull
	// (nonholonomic — it never dodges, P5: infantry yields). 0 → 0.5.
	Resp float32
}

// orcaAgentConstraint builds the ORCA half-plane between `self` and `other`
// for time horizon `tau`. Reciprocal — `self` shoulders `other.Resp` of the
// avoidance vector; a reciprocating other handles the rest via its own
// solver invocation.
func orcaAgentConstraint(self, other orcaAgent, tau float32) orcaLine {
	relPos := other.Pos.sub(self.Pos)
	relVel := self.Vel.sub(other.Vel)
	distSq := relPos.lenSq()
	combinedR := self.Radius + other.Radius
	combinedRSq := combinedR * combinedR

	var line orcaLine
	var u orcaVec2

	if distSq > combinedRSq {
		// No collision yet. Compute u from velocity obstacle cone.
		w := relVel.sub(relPos.scale(1 / tau))
		wLenSq := w.lenSq()
		dotProd := w.dot(relPos)

		if dotProd < 0 && dotProd*dotProd > combinedRSq*wLenSq {
			// Project on cutoff circle of VO.
			wLen := float32(math.Sqrt(float64(wLenSq)))
			unitW := w.scale(1 / wLen)
			line.Dir = orcaVec2{X: unitW.Z, Z: -unitW.X}
			u = unitW.scale(combinedR/tau - wLen)
		} else {
			// Project on legs of VO.
			leg := float32(math.Sqrt(float64(distSq - combinedRSq)))
			if det(relPos, w) > 0 {
				// Left leg.
				line.Dir = orcaVec2{
					X: (relPos.X*leg - relPos.Z*combinedR) / distSq,
					Z: (relPos.X*combinedR + relPos.Z*leg) / distSq,
				}
			} else {
				// Right leg.
				line.Dir = orcaVec2{
					X: -(relPos.X*leg + relPos.Z*combinedR) / distSq,
					Z: (-relPos.X*combinedR + relPos.Z*leg) / distSq,
				}
			}
			dotProd2 := relVel.dot(line.Dir)
			u = line.Dir.scale(dotProd2).sub(relVel)
		}
	} else {
		// Already colliding — recover on a tighter horizon.
		invTS := 1 / orcaTimeStep
		w := relVel.sub(relPos.scale(invTS))
		wLen := w.length()
		if wLen < 1e-5 {
			// Perfect overlap — push along arbitrary axis.
			line.Dir = orcaVec2{X: 1, Z: 0}
			u = orcaVec2{X: combinedR * invTS, Z: 0}
		} else {
			unitW := w.scale(1 / wLen)
			line.Dir = orcaVec2{X: unitW.Z, Z: -unitW.X}
			u = unitW.scale(combinedR*invTS - wLen)
		}
	}

	resp := other.Resp
	if resp <= 0 {
		resp = 0.5
	}
	line.Point = self.Vel.add(u.scale(resp))
	return line
}

// orcaSolveLP2D solves the 2D linear program: find the velocity nearest
// `prefVel`, magnitude ≤ maxSpeed, satisfying every half-plane in `lines`.
// Returns (result, ok). When the program is infeasible (lines mutually
// exclusive), returns (last best result, false) so the caller can run a 3D
// fallback or use prefVel raw.
func orcaSolveLP2D(lines []orcaLine, maxSpeed float32, prefVel orcaVec2) (orcaVec2, bool) {
	// Start at prefVel, clamped to the speed disc.
	result := prefVel
	if result.lenSq() > maxSpeed*maxSpeed {
		result = result.scale(maxSpeed / result.length())
	}

	for i := range lines {
		// If current result satisfies line i, skip.
		if det(lines[i].Dir, lines[i].Point.sub(result)) <= 0 {
			continue
		}
		// Need to fix — solve 1D LP on line i, constrained by lines 0..i-1
		// and the speed disc.
		saved := result
		newRes, ok := orcaSolveLP1D(lines, i, maxSpeed, prefVel)
		if !ok {
			return saved, false
		}
		result = newRes
	}
	return result, true
}

// orcaSolveLP1D solves the 1D linear program along line `idx`, intersected
// with all earlier lines and the maxSpeed circle. Finds the point closest
// to prefVel along the directed line. Returns (result, ok).
func orcaSolveLP1D(lines []orcaLine, idx int, maxSpeed float32, prefVel orcaVec2) (orcaVec2, bool) {
	line := lines[idx]
	dotProd := line.Point.dot(line.Dir)
	discriminant := dotProd*dotProd + maxSpeed*maxSpeed - line.Point.lenSq()
	if discriminant < 0 {
		// Line doesn't intersect speed disc.
		return orcaVec2{}, false
	}
	sqrtDisc := float32(math.Sqrt(float64(discriminant)))
	tLeft := -dotProd - sqrtDisc
	tRight := -dotProd + sqrtDisc

	for j := 0; j < idx; j++ {
		other := lines[j]
		denom := det(line.Dir, other.Dir)
		num := det(other.Dir, line.Point.sub(other.Point))
		if abs32(denom) <= 1e-5 {
			// Lines nearly parallel. If line i is on the wrong side of
			// other, the program is infeasible.
			if num < 0 {
				return orcaVec2{}, false
			}
			continue
		}
		t := num / denom
		if denom >= 0 {
			tRight = minF(tRight, t)
		} else {
			tLeft = maxF(tLeft, t)
		}
		if tLeft > tRight {
			return orcaVec2{}, false
		}
	}

	t := line.Dir.dot(prefVel.sub(line.Point))
	if t < tLeft {
		t = tLeft
	} else if t > tRight {
		t = tRight
	}
	return line.Point.add(line.Dir.scale(t)), true
}

// orcaSolveLP3D — fallback when 2D LP infeasible. Minimises maximum
// violation (standard RVO2). Used when the agent is hemmed in.
func orcaSolveLP3D(lines []orcaLine, maxSpeed float32, beginIdx int, prevResult orcaVec2) orcaVec2 {
	result := prevResult
	distance := float32(0)

	for i := beginIdx; i < len(lines); i++ {
		if det(lines[i].Dir, lines[i].Point.sub(result)) <= distance {
			continue
		}
		// Build a "projected" line set for the 2D LP: lines 0..i-1 shifted
		// outward by `distance`, plus line i which we project onto.
		proj := make([]orcaLine, 0, i)
		for j := 0; j < i; j++ {
			d := det(lines[i].Dir, lines[j].Dir)
			var pt orcaVec2
			if abs32(d) <= 1e-5 {
				if lines[i].Dir.dot(lines[j].Dir) > 0 {
					continue // same direction — line j subsumed by line i
				}
				pt = lines[i].Point.add(lines[j].Point).scale(0.5)
			} else {
				num := det(lines[j].Dir, lines[i].Point.sub(lines[j].Point))
				pt = lines[i].Point.add(lines[i].Dir.scale(num / d))
			}
			diff := lines[j].Dir.sub(lines[i].Dir)
			dl := diff.length()
			if dl < 1e-5 {
				continue
			}
			proj = append(proj, orcaLine{
				Point: pt,
				Dir:   diff.scale(1 / dl),
			})
		}
		// Optimise along line i against the projected set.
		newDir := orcaVec2{X: -lines[i].Dir.Z, Z: lines[i].Dir.X}
		r2, ok := orcaSolveLP2D(proj, maxSpeed, newDir)
		if ok {
			result = r2
		}
		distance = det(lines[i].Dir, lines[i].Point.sub(result))
	}
	return result
}

// orcaAdjust is the single-call helper unit_movement_step uses. Returns the
// adjusted velocity and a feasibility bool — persistent infeasibility feeds
// the replan-on-stall logic (unit hemmed in → MicroPath should re-route).
func orcaAdjust(self orcaAgent, neighbours []orcaAgent, prefVel orcaVec2, maxSpeed float32) (orcaVec2, bool) {
	if len(neighbours) == 0 {
		if prefVel.lenSq() > maxSpeed*maxSpeed {
			return prefVel.scale(maxSpeed / prefVel.length()), true
		}
		return prefVel, true
	}
	if len(neighbours) > orcaMaxNeighbours {
		neighbours = neighbours[:orcaMaxNeighbours]
	}
	lines := make([]orcaLine, 0, len(neighbours))
	for _, n := range neighbours {
		lines = append(lines, orcaAgentConstraint(self, n, orcaTimeHorizon))
	}
	result, ok := orcaSolveLP2D(lines, maxSpeed, prefVel)
	if !ok {
		result = orcaSolveLP3D(lines, maxSpeed, 0, result)
	}
	return result, ok
}

func abs32(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}
