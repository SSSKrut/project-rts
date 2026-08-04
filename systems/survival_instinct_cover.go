package systems

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// pickCover returns the best cover slot for a unit. Score formula:
//
//	score = quality*100 + facing*20 - dist*5 - anglePenalty*30 - capacityPenalty*50
//
// facing = dot(slot.OriginDir, threatDir); slots with facing < -0.3 are
// rejected (kills "cover behind my back" picks while keeping side-cover).
// anglePenalty = 1 - dot(-threatDir, dirToSlot) so running backward to a
// slot gets penalised. capacityPenalty grows with occupancyClaim: 1 if any
// other unit holds it, 20 at the soft cap.
func (sys *SurvivalInstinctSystem) pickCover(
	unit ecs.Entity, pos *components.WorldPos, threatDir rl.Vector3,
	claimed map[ecs.Entity]ecs.Entity,
) (ecs.Entity, components.WorldPos, bool) {
	if threatDir.X == 0 && threatDir.Z == 0 {
		return ecs.Entity{}, components.WorldPos{}, false
	}
	tx, tz := threatDir.X, threatDir.Z
	if l := float32(math.Sqrt(float64(tx*tx + tz*tz))); l > 0 {
		tx /= l
		tz /= l
	}
	// Approach forward = away from threat.
	fx, fz := -tx, -tz

	unitX := float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
	unitZ := float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z

	var bestEnt ecs.Entity
	var bestPos components.WorldPos
	const negInf = float32(-1e9)
	bestScore := negInf

	for i := range sys.slots {
		s := &sys.slots[i]
		dx := s.worldX - unitX
		dz := s.worldZ - unitZ
		distSq := dx*dx + dz*dz
		if distSq > siCoverSearchRadius*siCoverSearchRadius {
			continue
		}
		// A wall inside the beaten zone stops bullets, not shells.
		if sys.zoneAt(s.worldX, s.worldZ) >= 0 {
			continue
		}
		facing := s.originX*tx + s.originZ*tz
		if facing < -0.3 {
			continue
		}
		dist := float32(math.Sqrt(float64(distSq)))
		var dirX, dirZ float32
		if dist > 1e-3 {
			dirX = dx / dist
			dirZ = dz / dist
		}
		align := fx*dirX + fz*dirZ
		anglePenalty := 1 - align // [0, 2]

		var capacityPenalty float32
		claim := sys.occupancyClaim[s.ent]
		if owner, ok := claimed[s.ent]; ok && owner != unit {
			claim++
		}
		switch {
		case claim >= 2:
			capacityPenalty = 20
		case claim >= 1:
			capacityPenalty = 1
		}

		score := s.quality*100 + facing*20 - dist*5 - anglePenalty*30 - capacityPenalty*50
		if score > bestScore {
			bestScore = score
			bestEnt = s.ent
			bestPos = s.worldPos
		}
	}
	if bestEnt == (ecs.Entity{}) {
		return ecs.Entity{}, components.WorldPos{}, false
	}
	return bestEnt, bestPos, true
}

// zoneAt returns the index of the live unsafe area covering (x, z), or -1.
func (sys *SurvivalInstinctSystem) zoneAt(x, z float32) int {
	for i := range sys.unsafe {
		zn := &sys.unsafe[i]
		dx, dz := x-zn.x, z-zn.z
		if dx*dx+dz*dz <= zn.radius*zn.radius {
			return i
		}
	}
	return -1
}

func (sys *SurvivalInstinctSystem) zoneUnder(pos *components.WorldPos) int {
	x, z := worldXZ(*pos)
	return sys.zoneAt(x, z)
}

// evacDir is the threat bearing for a unit standing in a zone: from the zone
// centre toward the unit, i.e. cover picks face the shelling.
func (sys *SurvivalInstinctSystem) evacDir(pos *components.WorldPos, zone int) (rl.Vector3, bool) {
	if zone < 0 || zone >= len(sys.unsafe) {
		return rl.Vector3{}, false
	}
	zn := &sys.unsafe[zone]
	x, z := worldXZ(*pos)
	dx, dz := x-zn.x, z-zn.z
	l := float32(math.Sqrt(float64(dx*dx + dz*dz)))
	if l < 1e-3 {
		return rl.Vector3{}, false
	}
	return rl.Vector3{X: dx / l, Z: dz / l}, true
}

// evacTarget is the nearest point clear of EVERY live zone the unit stands
// in, radially out from the deepest one. Straight out is the shortest way
// through a beaten zone; MicroPath still routes around whatever is in the way.
func (sys *SurvivalInstinctSystem) evacTarget(pos *components.WorldPos) (components.WorldPos, bool) {
	x, z := worldXZ(*pos)
	var bestOut float32
	var bx, bz float32
	found := false
	for i := range sys.unsafe {
		zn := &sys.unsafe[i]
		dx, dz := x-zn.x, z-zn.z
		d := float32(math.Sqrt(float64(dx*dx + dz*dz)))
		if d > zn.radius {
			continue
		}
		out := zn.radius - d + siEvacMargin
		if out <= bestOut {
			continue
		}
		if d < 1e-3 {
			// Dead centre: deterministic bearing, else the unit dithers.
			dx, dz, d = 1, 0, 1
		}
		bestOut, bx, bz = out, dx/d, dz/d
		found = true
	}
	if !found {
		return components.WorldPos{}, false
	}
	return pos.Add(rl.Vector3{X: bx * bestOut, Z: bz * bestOut}), true
}
