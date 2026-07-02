package systems

import (
	"math"

	"rts-go/components"
)

// colWall is the movement-collision view of a WallSegment. Windows block
// movement (LOS treats them transparent), open doors pass through the
// opening, closed doors / walls fully block.
//
// yBase/yTop is the storey range. Stacked walls (storey-0 door + storey-1
// window at the same XZ projection) share the same 2D line; without the Y
// filter, the storey-1 window's "block" would override the storey-0 door's
// "passable" and trap units outside the building. reflectAgainstWalls /
// escape both skip walls whose Y range doesn't overlap the unit's foot Y.
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
// wall's storey range. Critical: without this filter, a storey-1 window
// stacked above a storey-0 door at the same XZ collectively blocks the
// ground-floor unit (see colWall comment).
//
// 0.3 m margin keeps ground-snap fluctuations from popping the unit out of
// the matching range right at the storey boundary.
func unitMatchesWallStorey(unitY, wallYBase, wallYTop float32) bool {
	const yMargin float32 = 0.3
	return unitY >= wallYBase-yMargin && unitY <= wallYTop+yMargin
}

// reflectAgainstWalls projects velocity along walls in the 3×3 chunk window
// the predicted XZ step would cross. Sliding (v - (v·n)n) instead of full
// reflection — units brush past corners and glide along corridor walls.
//
// Escape recovery blends additively into velocity (not override). Override
// caused oscillation: dist < margin → push out → out of margin → unit's
// intent pulls back → re-enters margin → re-push, net velocity ~0. Additive
// blend lets the spring nudge while the main slide loop handles wall-
// crossing. On corners with opposite-normal walls (compound NW junction),
// springs sum to ~0 instead of freezing the unit.
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
				// Storey Y filter (see unitMatchesWallStorey).
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
					// Degenerate: use wall normal direction.
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
	// Additive blend: idle unit → escape becomes sole driver (pops unit
	// free); non-zero velocity → spring adds drift while sliding handles
	// geometry crossings.
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
							continue
						}
						// Door funnel: the walker wants through THIS wall
						// and an open door exists — head for the opening
						// at full speed instead of sliding. The plain
						// slide's tangential remainder is a few percent of
						// speed when the goal sits nearly perpendicular
						// behind the wall; a misaligned unit then takes
						// tens of seconds to creep to the doorway.
						openT := (w.openStart + w.openEnd) * 0.5
						ox := w.fromX + w.sa*openT
						oz := w.fromZ + w.ca*openT
						nx, nz := w.ca, -w.sa
						if (curX-ox)*nx+(curZ-oz)*nz < 0 {
							nx, nz = -nx, -nz
						}
						// Aim slightly before the opening on the unit's
						// side so the approach stays wall-parallel.
						tx := ox + nx*0.45
						tz := oz + nz*0.45
						fdx := tx - curX
						fdz := tz - curZ
						fd := float32(math.Sqrt(float64(fdx*fdx + fdz*fdz)))
						if fd > 1e-4 {
							spd := float32(math.Sqrt(float64(rvx*rvx + rvz*rvz)))
							rvx = fdx / fd * spd
							rvz = fdz / fd * spd
						}
						hit = true
						break
					}
					// Wall direction (sa, ca); normal (ca, -sa) flipped to
					// point toward the moving unit.
					nx, nz := w.ca, -w.sa
					if rvx*nx+rvz*nz > 0 {
						nx, nz = -nx, -nz
					}
					// Slide: subtract only the into-wall component; tangent
					// survives.
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
