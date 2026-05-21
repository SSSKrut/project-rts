package systems

import (
	"math"

	"rts-go/components"
)

// colWall is the movement-collision view of a WallSegment. Windows block
// movement (separate from LOS - losWall flips openingTransparent for windows
// too), open doors allow passage through the opening range, closed doors and
// plain walls fully block. World coords; trig pre-computed.
type colWall struct {
	fromX, fromZ       float32
	sa, ca             float32
	length             float32
	hasOpening         bool
	openPassable       bool // true <-> open door (window opening still blocks movement)
	openStart, openEnd float32
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
	}
	if w.OpeningKind == components.OpeningDoor && doorState == components.DoorOpen {
		cw.openPassable = true
	}
	return cw
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
func reflectAgainstWalls(curX, curZ, velX, velZ, dt float32,
	walls map[components.ChunkCoord][]colWall, home components.ChunkCoord,
) (float32, float32) {
	if velX*velX+velZ*velZ < 1e-6 {
		return velX, velZ
	}
	rvx, rvz := velX, velZ
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
