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
