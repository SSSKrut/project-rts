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
	followerMap   *ecs.Map[components.RoadFollower]
	graphRes      ecs.Resource[components.RoadGraph]
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
	sys.followerMap = ecs.NewMap[components.RoadFollower](w)
	sys.graphRes = ecs.NewResource[components.RoadGraph](w)
	sys.sampler = NewHeightSampler(w)
}

// stairEdgePad widens the ramp footprint so a unit hugging the stair edge
// (collider radius ~0.3) is still carried.
const stairEdgePad float32 = 0.4

// stairBaseMagnet — the bottom this-many metres of a ramp win the closest-Y
// tie against the plate the stair stands on.
const stairBaseMagnet float32 = 1.0

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
			} else if rampY > bestY && rampY-bestY <= stairBaseMagnet {
				// Base magnetism: the bottom metre of a ramp claims walkers
				// standing on its host plate. A radius-popped waypoint enters
				// the climb from the side, and the plate snap would otherwise
				// re-capture the walker every tick — the ai_main_m1 straggler
				// loop (climb can only engage exactly at the base without this).
				bestY = rampY
			}
		}
		pos.Local.Y = bestY
	}

	// Vehicles never enter buildings: surface clamp, except on a bridge edge
	// where Y comes off the deck (node-height lerp + bridgeYOffset, ramped
	// over the first/last metres so bank→deck has no step). max() keeps the
	// hull from sinking where deck and bank meet.
	qv := sys.vehicleFilter.Query()
	for qv.Next() {
		_, pos := qv.Get()
		wx := float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
		wz := float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z
		y := sys.sampler.Sample(wx, wz)
		if deck, ok := sys.bridgeDeckY(qv.Entity()); ok && deck > y {
			y = deck
		}
		pos.Local.Y = y
	}
}

// bridgeEndRamp — metres of deck at each end over which the bridgeYOffset
// lift fades to bank level.
const bridgeEndRamp float32 = 4.0

func (sys *GroundStickSystem) bridgeDeckY(ent ecs.Entity) (float32, bool) {
	f := sys.followerMap.Get(ent)
	if f == nil || f.Edge < 0 {
		return 0, false
	}
	g := sys.graphRes.Get()
	if g == nil || int(f.Edge) >= len(g.Edges) {
		return 0, false
	}
	e := &g.Edges[f.Edge]
	if e.Kind != components.RoadBridge {
		return 0, false
	}
	ax, az := worldXZ(g.Nodes[e.From].Pos)
	bx, bz := worldXZ(g.Nodes[e.To].Pos)
	t := f.T
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	base := GroundHeight(ax, az) + t*(GroundHeight(bx, bz)-GroundHeight(ax, az))
	end := t
	if 1-t < end {
		end = 1 - t
	}
	ramp := end * dist2D(ax, az, bx, bz) / bridgeEndRamp
	if ramp > 1 {
		ramp = 1
	}
	return base + bridgeYOffset*ramp, true
}
