package systems

import (
	"math"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// AnchorEyeHeight: offset above the terrain surface for the anchor.
// Roughly average human eye level.
const AnchorEyeHeight float32 = 1.5

// GroundStickSystem clamps anchor + every unit Y to the terrain surface
// using the live heightmap (HeightSampler — trenches, craters and bunker
// cuts count). Anchor uses eye-height offset; units have foot at surface
// (the unit cube draws upward from WorldPos). A unit whose WorldPos
// lies on a building Floor uses the floor Y instead; inside a Stairs
// footprint the ramp Y (bottom→top along the stair axis) joins the
// closest-Y pool — that's what physically carries a walker between
// storeys. Floor plates alone can't: the storey midpoint is ~1.5 m away
// and per-tick Y-lerp gains are an order of magnitude smaller, so without
// the ramp every climb snaps back to the lower plate.
type GroundStickSystem struct {
	anchorFilter  *ecs.Filter2[components.LODAnchor, components.WorldPos]
	unitFilter    *ecs.Filter2[components.Unit, components.WorldPos]
	vehicleFilter *ecs.Filter2[components.Vehicle, components.WorldPos]
	floorFilter   *ecs.Filter2[components.WorldPos, components.Floor]
	stairsFilter  *ecs.Filter2[components.WorldPos, components.Stairs]
	sampler       *HeightSampler
}

func (sys *GroundStickSystem) InitUI(w *ecs.World) {
	sys.anchorFilter = ecs.NewFilter2[components.LODAnchor, components.WorldPos](w)
	// OnGround gates the clamp: aircraft (Phase 20) omit the marker and
	// keep their own Y.
	sys.unitFilter = ecs.NewFilter2[components.Unit, components.WorldPos](w).
		With(ecs.C[components.OnGround]())
	sys.vehicleFilter = ecs.NewFilter2[components.Vehicle, components.WorldPos](w).
		With(ecs.C[components.OnGround]())
	sys.floorFilter = ecs.NewFilter2[components.WorldPos, components.Floor](w)
	sys.stairsFilter = ecs.NewFilter2[components.WorldPos, components.Stairs](w)
	sys.sampler = NewHeightSampler(w)
}

// stairEdgePad widens the ramp footprint so a unit hugging the stair edge
// (collider radius ~0.3) is still carried.
const stairEdgePad float32 = 0.4

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

	type stairRec struct {
		bx, bz   float32 // bottom-anchor world XZ
		sa, ca   float32 // stair axis (sin/cos yaw)
		length   float32
		halfW    float32
		y0, rise float32
	}
	var stairs []stairRec
	qs := sys.stairsFilter.Query()
	for qs.Next() {
		pos, s := qs.Get()
		if s.Length <= 0 {
			continue
		}
		stairs = append(stairs, stairRec{
			bx:     float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X,
			bz:     float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z,
			sa:     float32(math.Sin(float64(s.Yaw))),
			ca:     float32(math.Cos(float64(s.Yaw))),
			length: s.Length,
			halfW:  s.Width*0.5 + stairEdgePad,
			y0:     pos.Local.Y,
			rise:   s.Rise,
		})
	}
	// stairRampY returns the ramp height under (wx, wz), ok=false outside
	// every stair footprint.
	stairRampY := func(wx, wz float32) (float32, bool) {
		for i := range stairs {
			sr := &stairs[i]
			dx := wx - sr.bx
			dz := wz - sr.bz
			t := dx*sr.sa + dz*sr.ca
			if t < -stairEdgePad || t > sr.length+stairEdgePad {
				continue
			}
			n := dx*sr.ca - dz*sr.sa
			if n < -sr.halfW || n > sr.halfW {
				continue
			}
			tc := t
			if tc < 0 {
				tc = 0
			} else if tc > sr.length {
				tc = sr.length
			}
			return sr.y0 + sr.rise*tc/sr.length, true
		}
		return 0, false
	}

	// Closest-Y rule lets a path walker drive Y up stairs and have GS keep
	// the anchor on the new floor next tick.
	qa := sys.anchorFilter.Query()
	for qa.Next() {
		_, pos := qa.Get()
		wx := float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
		wz := float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z
		surfaceY := sys.sampler.Sample(wx, wz) + AnchorEyeHeight
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
		if rampY, ok := stairRampY(wx, wz); ok {
			candidateY := rampY + AnchorEyeHeight
			if d := absDelta(pos.Local.Y, candidateY); d < bestD {
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
		surfaceY := sys.sampler.Sample(wx, wz)
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
		if rampY, ok := stairRampY(wx, wz); ok {
			if d := absDelta(pos.Local.Y, rampY); d < bestD {
				bestD = d
				bestY = rampY
			}
		}
		pos.Local.Y = bestY
	}

	// Vehicles never enter buildings: surface clamp only. Bridge-deck Y is
	// RoadFollower's job (Phase 19 M2).
	qv := sys.vehicleFilter.Query()
	for qv.Next() {
		_, pos := qv.Get()
		wx := float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
		wz := float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z
		pos.Local.Y = sys.sampler.Sample(wx, wz)
	}
}
