package systems

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// AnchorEyeHeight: offset above the terrain surface for the anchor.
// Roughly average human eye level.
const AnchorEyeHeight float32 = 1.5

// GroundStickSystem clamps anchor + every unit Y to the terrain surface
// using GroundHeight(). Anchor uses eye-height offset; units have foot at
// surface (the unit cube draws upward from WorldPos). A unit whose WorldPos
// lies on a building Floor uses the floor Y instead.
type GroundStickSystem struct {
	anchorFilter *ecs.Filter2[components.LODAnchor, components.WorldPos]
	unitFilter   *ecs.Filter2[components.Unit, components.WorldPos]
	floorFilter  *ecs.Filter2[components.WorldPos, components.Floor]
}

func (sys *GroundStickSystem) InitUI(w *ecs.World) {
	sys.anchorFilter = ecs.NewFilter2[components.LODAnchor, components.WorldPos](w)
	sys.unitFilter = ecs.NewFilter2[components.Unit, components.WorldPos](w)
	sys.floorFilter = ecs.NewFilter2[components.WorldPos, components.Floor](w)
}

func (GroundStickSystem) Name() string { return "ground_stick" }

func (GroundStickSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

func (sys GroundStickSystem) Update(ctx core.UpdateContext) {
	if ctx.Tier != core.LODTierActive {
		return
	}
	type floorRec struct {
		minX, maxX, minZ, maxZ float32
		y                      float32
	}
	var floors []floorRec
	qf := sys.floorFilter.Query()
	for qf.Next() {
		pos, f := qf.Get()
		baseX := float32(pos.Chunk.X) * components.ChunkSize
		baseZ := float32(pos.Chunk.Z) * components.ChunkSize
		floors = append(floors, floorRec{
			minX: baseX + pos.Local.X - f.SizeX*0.5,
			maxX: baseX + pos.Local.X + f.SizeX*0.5,
			minZ: baseZ + pos.Local.Z - f.SizeZ*0.5,
			maxZ: baseZ + pos.Local.Z + f.SizeZ*0.5,
			y:    pos.Local.Y,
		})
	}

	// Closest-Y rule lets a path walker drive Y up stairs and have GS keep
	// the anchor on the new floor next tick.
	qa := sys.anchorFilter.Query()
	for qa.Next() {
		_, pos := qa.Get()
		wx := float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
		wz := float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z
		surfaceY := GroundHeight(wx, wz) + AnchorEyeHeight
		bestY := surfaceY
		bestD := absDelta(pos.Local.Y, surfaceY)
		for _, fr := range floors {
			if wx < fr.minX || wx > fr.maxX || wz < fr.minZ || wz > fr.maxZ {
				continue
			}
			candidateY := fr.y + AnchorEyeHeight
			d := absDelta(pos.Local.Y, candidateY)
			if d < bestD {
				bestD = d
				bestY = candidateY
			}
		}
		pos.Local.Y = bestY
	}

	qu := sys.unitFilter.Query()
	for qu.Next() {
		_, pos := qu.Get()
		wx := float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
		wz := float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z
		surfaceY := GroundHeight(wx, wz)
		bestY := surfaceY
		bestD := absDelta(pos.Local.Y, surfaceY)
		for _, fr := range floors {
			if wx < fr.minX || wx > fr.maxX || wz < fr.minZ || wz > fr.maxZ {
				continue
			}
			d := absDelta(pos.Local.Y, fr.y)
			if d < bestD {
				bestD = d
				bestY = fr.y
			}
		}
		pos.Local.Y = bestY
	}
}
