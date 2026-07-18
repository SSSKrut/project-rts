package systems

import (
	"math"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// LOSProbe answers "what would a standing observer see from there" for the
// hypothetical-position preview. Verdicts come from the same predicates the
// sim uses (terrainBlocksLOS + losWall opening semantics), so the overlay
// cannot disagree with ContactSystem / WeaponSystem.
type LOSProbe struct {
	terrainRes ecs.Resource[TerrainChunkIndex]
	hmMap      *ecs.Map[components.Heightmap]
	wallFilter *ecs.Filter2[components.WorldPos, components.WallSegment]
	doorMap    *ecs.Map[components.Door]

	heightmaps map[components.ChunkCoord][]float32
	walls      []losWall
}

func NewLOSProbe(w *ecs.World) *LOSProbe {
	return &LOSProbe{
		terrainRes: ecs.NewResource[TerrainChunkIndex](w),
		hmMap:      ecs.NewMap[components.Heightmap](w),
		wallFilter: ecs.NewFilter2[components.WorldPos, components.WallSegment](w),
		doorMap:    ecs.NewMap[components.Door](w),
		heightmaps: make(map[components.ChunkCoord][]float32, 256),
	}
}

const (
	LOSPreviewRays = 240
	losPreviewStep = 1.0
)

// Sweep recomputes the visibility fan around world point (wx, wz): for each
// azimuth, the visible runs up to maxRange (clamped below ChunkSize so the
// 3x3 wall window always covers the fan). Stand-vs-stand heights.
func (p *LOSProbe) Sweep(wx, wz, maxRange float32, out [][]components.VisRun) [][]components.VisRun {
	if maxRange > components.ChunkSize-1 {
		maxRange = components.ChunkSize - 1
	}
	idx := p.terrainRes.Get()
	clear(p.heightmaps)
	snapshotHeightmaps(idx, p.hmMap, p.heightmaps)

	ocx := int32(math.Floor(float64(wx) / float64(components.ChunkSize)))
	ocz := int32(math.Floor(float64(wz) / float64(components.ChunkSize)))
	p.walls = p.walls[:0]
	qW := p.wallFilter.Query()
	for qW.Next() {
		pos, w := qW.Get()
		if pos.Chunk.X < ocx-1 || pos.Chunk.X > ocx+1 || pos.Chunk.Z < ocz-1 || pos.Chunk.Z > ocz+1 {
			continue
		}
		doorState := components.DoorClosed
		if d := p.doorMap.Get(qW.Entity()); d != nil {
			doorState = d.State
		}
		p.walls = append(p.walls, makeLosWall(*pos, *w, doorState))
	}

	spec := components.SpecForStance(components.StanceStand)
	eyeY := terrainHeightAt(p.heightmaps, wx, wz) + spec.EyeHeight
	tgtH := spec.TargetCenterY

	if cap(out) < LOSPreviewRays {
		out = make([][]components.VisRun, LOSPreviewRays)
	}
	out = out[:LOSPreviewRays]
	steps := int(maxRange / losPreviewStep)
	for r := 0; r < LOSPreviewRays; r++ {
		runs := out[r][:0]
		ang := float64(r) * (2 * math.Pi / LOSPreviewRays)
		dirX := float32(math.Sin(ang))
		dirZ := float32(math.Cos(ang))
		wallLimit := maxRange
		if t, hit := segmentToWallsT(p.walls, wx, wz, wx+dirX*maxRange, wz+dirZ*maxRange); hit {
			wallLimit = t * maxRange
		}
		open := false
		var start float32
		for s := 1; s <= steps; s++ {
			d := float32(s) * losPreviewStep
			vis := d <= wallLimit
			if vis {
				tx := wx + dirX*d
				tz := wz + dirZ*d
				ty := terrainHeightAt(p.heightmaps, tx, tz) + tgtH
				vis = !terrainBlocksLOS(p.heightmaps, wx, wz, eyeY, tx, tz, ty)
			}
			if vis && !open {
				open = true
				start = d - losPreviewStep
			} else if !vis && open {
				runs = append(runs, components.VisRun{T0: start, T1: d - losPreviewStep})
				open = false
			}
		}
		if open {
			runs = append(runs, components.VisRun{T0: start, T1: maxRange})
		}
		out[r] = runs
	}
	return out
}
