package systems

import (
	"math"

	"rts-go/components"
)

// colWall is the movement-collision view of a WallSegment. Windows block
// movement (separate from LOS - losWall flips openingTransparent for windows
// too), open doors allow passage through the opening range, closed doors and
// plain walls fully block. World coords; trig pre-computed.
//
// Phase 17.8 follow-up — yBase/yTop is the storey range the wall occupies.
// Stacked walls (storey-0 door + storey-1 window at the same XZ) share the
// same 2D line; without a Y filter the storey-1 window's "block" would
// override the storey-0 door's "passable", trapping units outside the
// building. reflectAgainstWalls / escape both skip walls whose Y range
// doesn't overlap the unit's foot Y.
type colWall struct {
	fromX, fromZ       float32
	sa, ca             float32
	length             float32
	hasOpening         bool
	openPassable       bool // true <-> open door (window opening still blocks movement)
	openStart, openEnd float32
	yBase, yTop        float32
}

func makeColWall(pos components.WorldPos, w components.WallSegment, doorState components.DoorState) colWall {
	baseX := float32(pos.Chunk.X) * components.ChunkSize
	baseZ := float32(pos.Chunk.Z) * components.ChunkSize
	sa := float32(math.Sin(float64(w.Yaw)))
	ca := float32(math.Cos(float64(w.Yaw)))
	cw := colWall{
		fromX:      baseX + pos.Local.X,
		fromZ:      baseZ + pos.Local.Z,
		sa:         sa,
		ca:         ca,
		length:     w.Length,
		hasOpening: w.OpeningKind != components.OpeningNone,
		openStart:  w.OpeningCenterT*w.Length - w.OpeningWidth*0.5,
		openEnd:    w.OpeningCenterT*w.Length + w.OpeningWidth*0.5,
		yBase:      pos.Local.Y,
		yTop:       pos.Local.Y + w.Height,
	}
	if w.OpeningKind == components.OpeningDoor && doorState == components.DoorOpen {
		cw.openPassable = true
	}
	return cw
}

// unitMatchesWallStorey returns true when the unit's foot Y overlaps the
// wall's storey range. Stacked walls (storey-0 door + storey-1 window at
// the same XZ projection) must NOT collectively block a ground-floor unit
// passing through the door opening — the storey-1 window's blocking should
// only apply when a unit walks on storey-1's floor.
func unitMatchesWallStorey(unitY, wallYBase, wallYTop float32) bool {
	// 0.3 m margin so ground-snap fluctuations don't pop the unit out of
	// the matching range right at the storey boundary.
	const yMargin float32 = 0.3
	return unitY >= wallYBase-yMargin && unitY <= wallYTop+yMargin
}

// reflectAgainstWalls runs an XZ ray cast from `(curX, curZ)` along velocity
// `(velX, velZ) * dt` and adjusts the velocity vector to glide along the
// normal of every wall the predicted segment would cross this tick. Walls in
// the 3x3 chunk window around `home` are considered; open-door openings pass
// through.
//
// Phase 15 M15.B.3 - velocity adjustment switched from full reflection
// (v - 2*(v.n)*n, bouncy) to projection along the wall (v - (v.n)*n, slide).
// Steep impacts now stop perpendicular to the wall while keeping any tangent
// component, so units brush past corners and glide along corridor walls
// instead of zig-zagging. Multiple wall hits chain: the first slide updates
// the prediction, the next wall is tested against the new direction. Bound
// the loop at 4 passes to avoid pathological corners (two walls meeting at
// an acute angle).
//
// Phase 17.9 M1 — escape recovery now ADDITIVELY blends into velocity instead
// of fully overriding it. Old override caused oscillation: unit approaches
// wall to dist=0.05 < escapeMargin → override pushes (0, 2) north → out of
// margin at 0.30 → ORCA wants south → re-enters margin → re-push. Net
// forward velocity ~0. With additive blend, the small spring force away from
// the wall combines with the unit's intent; the main slide loop then handles
// the wall-crossing case correctly (removing only the into-wall component).
// On a corner where two walls have opposite normals (compound NW junction:
// M north + N south both at Z=38), the springs sum to ~0 and don't freeze
// the unit any more — the velocity simply passes through unmodified, and
// sliding takes over.
func reflectAgainstWalls(curX, curZ, curY, velX, velZ, dt float32,
	walls map[components.ChunkCoord][]colWall, home components.ChunkCoord,
) (float32, float32) {
	const escapeMargin float32 = 0.30
	const escapeSpeed float32 = 2.0
	const escapeBlendWeight float32 = 0.5 // additive contribution into rvx/rvz
	var escapeX, escapeZ float32
	escapeActive := false
	for dcZ := int32(-1); dcZ <= 1; dcZ++ {
		for dcX := int32(-1); dcX <= 1; dcX++ {
			cc := components.ChunkCoord{X: home.X + dcX, Z: home.Z + dcZ}
			for i := range walls[cc] {
				w := &walls[cc][i]
				// Phase 17.8 follow-up — skip walls outside the unit's
				// storey Y range so a 1st-floor window doesn't block a
				// ground-floor unit walking through the ground door at
				// the same XZ projection.
				if !unitMatchesWallStorey(curY, w.yBase, w.yTop) {
					continue
				}
				toX := w.fromX + w.sa*w.length
				toZ := w.fromZ + w.ca*w.length
				// Closest point on segment (curX, curZ) -> wall segment.
				wdx := toX - w.fromX
				wdz := toZ - w.fromZ
				lenSq := wdx*wdx + wdz*wdz
				if lenSq < 1e-6 {
					continue
				}
				t := ((curX-w.fromX)*wdx + (curZ-w.fromZ)*wdz) / lenSq
				if t < 0 {
					t = 0
				} else if t > 1 {
					t = 1
				}
				if w.hasOpening && w.openPassable {
					wallT := t * w.length
					if wallT >= w.openStart && wallT <= w.openEnd {
						continue
					}
				}
				cx := w.fromX + wdx*t
				cz := w.fromZ + wdz*t
				dx := curX - cx
				dz := curZ - cz
				distSq := dx*dx + dz*dz
				if distSq >= escapeMargin*escapeMargin {
					continue
				}
				dist := float32(math.Sqrt(float64(distSq)))
				if dist < 1e-4 {
					// Degenerate — pick wall normal direction.
					nx, nz := w.ca, -w.sa
					escapeX += nx * escapeSpeed
					escapeZ += nz * escapeSpeed
				} else {
					escapeX += dx / dist * escapeSpeed
					escapeZ += dz / dist * escapeSpeed
				}
				escapeActive = true
			}
		}
	}
	// Phase 17.9 M1 — additive blend instead of override. If the unit is
	// completely idle (velocity ~ 0) escape becomes the sole driver, which
	// matches the original "pop unit free" intent. With non-zero velocity,
	// the spring adds a small drift away from the wall while sliding stays
	// in charge of crossing geometry.
	rvx, rvz := velX, velZ
	if escapeActive {
		rvx += escapeX * escapeBlendWeight
		rvz += escapeZ * escapeBlendWeight
	}
	if rvx*rvx+rvz*rvz < 1e-6 {
		return rvx, rvz
	}
	for pass := 0; pass < 4; pass++ {
		predX := curX + rvx*dt
		predZ := curZ + rvz*dt
		hit := false
		for dcZ := int32(-1); dcZ <= 1; dcZ++ {
			for dcX := int32(-1); dcX <= 1; dcX++ {
				cc := components.ChunkCoord{X: home.X + dcX, Z: home.Z + dcZ}
				bucket := walls[cc]
				for i := range bucket {
					w := &bucket[i]
					// Phase 17.8 follow-up — storey Y filter (see colWall).
					if !unitMatchesWallStorey(curY, w.yBase, w.yTop) {
						continue
					}
					toX := w.fromX + w.sa*w.length
					toZ := w.fromZ + w.ca*w.length
					t1, t2, ok := segmentSegmentIntersect2D(curX, curZ, predX, predZ,
						w.fromX, w.fromZ, toX, toZ)
					if !ok || t1 < 0 || t1 > 1 || t2 < 0 || t2 > 1 {
						continue
					}
					if w.hasOpening && w.openPassable {
						wallT := t2 * w.length
						if wallT >= w.openStart && wallT <= w.openEnd {
							continue // pass through open door
						}
					}
					// Wall direction (sa, ca); normal perpendicular = (ca,
					// -sa) or its negation, whichever points toward the
					// moving unit.
					nx, nz := w.ca, -w.sa
					if rvx*nx+rvz*nz > 0 {
						nx, nz = -nx, -nz
					}
					// Sliding: remove only the component of velocity that
					// points into the wall. Tangent component survives so the
					// unit keeps moving along the wall.
					dot := rvx*nx + rvz*nz
					rvx -= dot * nx
					rvz -= dot * nz
					hit = true
					break
				}
				if hit {
					break
				}
			}
			if hit {
				break
			}
		}
		if !hit {
			return rvx, rvz
		}
	}
	return rvx, rvz
}
