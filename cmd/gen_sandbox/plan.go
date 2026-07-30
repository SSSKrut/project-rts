package main

import (
	"fmt"
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
	"rts-go/gen/buildings"
)

const (
	tplHouse = iota
	tplOffice
	tplCompound
	tplCompoundPlus
	tplCourtyard
)

var templateNames = []string{"House", "Office", "Compnd", "Plus", "Court"}

var wallModeNames = []string{"Solid", "Facing", "Wire"}

// genParams is the full input surface of the sandbox — everything a
// GenerateXxx call can be told. Templates that hardcode a field (office fixes
// stories/size, compounds fix everything) mark it via paramLive.
type genParams struct {
	template int
	bunker   bool
	stories  int
	sizeX    float32
	sizeZ    float32
	doors    [4]bool
	interior bool
	seed     uint64

	matrix bool
	cols   int
	rows   int
}

func defaultParams() genParams {
	return genParams{
		template: tplHouse,
		stories:  2,
		sizeX:    10,
		sizeZ:    8,
		doors:    [4]bool{true, false, false, false},
		seed:     0xA1,
		cols:     4,
		rows:     3,
	}
}

// paramLive reports whether the given control actually reaches the generator
// for the current template. The sandbox greys out the rest rather than lying
// about what the knob does.
func (p genParams) paramLive(name string) bool {
	switch name {
	case "kind":
		return p.template == tplHouse
	case "stories":
		return p.template == tplHouse || p.template == tplCourtyard
	case "size":
		return p.template == tplHouse || p.template == tplCourtyard
	case "doors", "interior":
		return p.template == tplHouse
	}
	return true
}

// cellIssue tags a ValidationIssue with the plan it came from so click-to-fly
// can resolve the offending element.
type cellIssue struct {
	planIdx int
	iss     buildings.ValidationIssue
}

// cell is one generated sample: a template invocation at one world origin.
// Compound templates emit several plans per cell.
type cell struct {
	plans  []*components.BuildingPlan
	label  string
	bounds components.AABB2D
	topY   float32
	issues   []cellIssue
	chunks   int
	oversize bool
	errors   int
	warns    int
}

func (p genParams) spacing() float32 {
	switch p.template {
	case tplCompound, tplCompoundPlus:
		return 40
	case tplOffice:
		return 24
	case tplCourtyard:
		return maxF(p.sizeX, p.sizeZ) + 12
	}
	return maxF(p.sizeX, p.sizeZ) + 8
}

func (sb *sandbox) rebuild() {
	sb.cells = sb.cells[:0]
	sb.focused = 0
	p := sb.params

	if !p.matrix {
		sb.cells = append(sb.cells, buildCell(p, p.seed, p.stories, rl.Vector3{}))
		sb.clampLevelFilter()
		return
	}

	sp := p.spacing()
	// Centre the grid on the origin so framing stays symmetric.
	offX := -0.5 * float32(p.cols-1) * sp
	offZ := -0.5 * float32(p.rows-1) * sp
	for r := 0; r < p.rows; r++ {
		for c := 0; c < p.cols; c++ {
			seed := p.seed
			stories := p.stories
			if p.template == tplHouse || p.template == tplCourtyard {
				// Columns walk the seed, rows walk storey count — the two axes
				// that actually change house geometry.
				seed = p.seed + uint64(c)*0x9E37
				stories = 1 + r
			} else {
				seed = p.seed + uint64(r*p.cols+c)*0x9E37
			}
			origin := rl.Vector3{X: offX + float32(c)*sp, Z: offZ + float32(r)*sp}
			sb.cells = append(sb.cells, buildCell(p, seed, stories, origin))
		}
	}
	sb.clampLevelFilter()
}

// sandboxOrigin centres samples inside chunk (0,0) instead of on its corner.
// At the world origin every footprint straddles four chunks and the
// single-chunk warning would fire on everything.
var sandboxOrigin = rl.Vector3{X: components.ChunkSize * 0.5, Z: components.ChunkSize * 0.5}

func buildCell(p genParams, seed uint64, stories int, origin rl.Vector3) cell {
	pos := components.WorldPos{}.Add(rl.Vector3{
		X: origin.X + sandboxOrigin.X,
		Z: origin.Z + sandboxOrigin.Z,
	})
	var plans []*components.BuildingPlan
	label := ""

	switch p.template {
	case tplOffice:
		plans = []*components.BuildingPlan{buildings.GenerateOffice(seed, pos)}
		label = fmt.Sprintf("office %#x", seed)
	case tplCompound:
		plans = buildings.GenerateCompound(seed, pos)
		label = fmt.Sprintf("compound %#x", seed)
	case tplCompoundPlus:
		plans = buildings.GenerateCompoundPlus(seed, pos)
		label = fmt.Sprintf("compound+ %#x", seed)
	case tplCourtyard:
		cp := buildings.DefaultCourtyardParams()
		cp.Stories = uint8(stories)
		cp.SizeX, cp.SizeZ = p.sizeX+10, p.sizeZ+10
		cp.WellX, cp.WellZ = cp.SizeX*0.42, cp.SizeZ*0.42
		plans = []*components.BuildingPlan{buildings.GenerateCourtyard(seed, cp, pos)}
		label = fmt.Sprintf("court %#x %dst", seed, stories)
	default:
		kind := components.BuildingHouse
		if p.bunker {
			kind = components.BuildingBunker
		}
		hp := buildings.HouseParams{
			Stories:   uint8(stories),
			SizeX:     p.sizeX,
			SizeZ:     p.sizeZ,
			DoorSides: doorList(p.doors),
			Interior:  p.interior,
		}
		if len(hp.DoorSides) == 0 {
			hp.DoorSide = 4 // seed-picked, matches map_def's default
		}
		plans = []*components.BuildingPlan{buildings.GenerateHouse(seed, hp, pos, kind)}
		label = fmt.Sprintf("%#x %dst", seed, stories)
	}

	c := cell{plans: plans, label: label}
	c.bounds = components.AABB2D{
		MinX: float32(math.MaxFloat32), MinZ: float32(math.MaxFloat32),
		MaxX: -float32(math.MaxFloat32), MaxZ: -float32(math.MaxFloat32),
	}
	for i, pl := range plans {
		fp := planFootprint(pl)
		c.bounds.MinX = minF(c.bounds.MinX, fp.MinX)
		c.bounds.MinZ = minF(c.bounds.MinZ, fp.MinZ)
		c.bounds.MaxX = maxF(c.bounds.MaxX, fp.MaxX)
		c.bounds.MaxZ = maxF(c.bounds.MaxZ, fp.MaxZ)
		roofH := float32(0)
		for ri := range pl.Roofs {
			roofH = maxF(roofH, pl.Roofs[ri].Roof.Height)
		}
		c.topY = maxF(c.topY, float32(pl.Stories)*components.FloorHeight+roofH)

		for _, iss := range buildings.Validate(pl) {
			c.issues = append(c.issues, cellIssue{planIdx: i, iss: iss})
			if iss.Severity == buildings.SeverityError {
				c.errors++
			} else {
				c.warns++
			}
		}
	}
	c.oversize = (c.bounds.MaxX-c.bounds.MinX) > components.ChunkSize ||
		(c.bounds.MaxZ-c.bounds.MinZ) > components.ChunkSize
	c.chunks = chunkSpan(c.bounds)
	return c
}

func doorList(d [4]bool) []uint8 {
	var out []uint8
	for i, on := range d {
		if on {
			out = append(out, uint8(i))
		}
	}
	return out
}

// planFootprint mirrors spawnWorldRoots' AABB derivation: plan centre +/-
// half Size, in world coords.
func planFootprint(p *components.BuildingPlan) components.AABB2D {
	cx := p.Pos.Local.X + float32(p.Pos.Chunk.X)*components.ChunkSize
	cz := p.Pos.Local.Z + float32(p.Pos.Chunk.Z)*components.ChunkSize
	return components.AABB2D{
		MinX: cx - p.Size.X*0.5, MaxX: cx + p.Size.X*0.5,
		MinZ: cz - p.Size.Y*0.5, MaxZ: cz + p.Size.Y*0.5,
	}
}

// chunkSpan counts how many chunks the footprint touches. >1 violates the
// per-building constraint that children share one Pos.Chunk.
func chunkSpan(b components.AABB2D) int {
	cx0 := int(math.Floor(float64(b.MinX / components.ChunkSize)))
	cx1 := int(math.Floor(float64(b.MaxX / components.ChunkSize)))
	cz0 := int(math.Floor(float64(b.MinZ / components.ChunkSize)))
	cz1 := int(math.Floor(float64(b.MaxZ / components.ChunkSize)))
	return (cx1 - cx0 + 1) * (cz1 - cz0 + 1)
}

// planWorld lifts a chunk-local spec coord into world space.
func planWorld(p *components.BuildingPlan, local rl.Vector3) rl.Vector3 {
	return rl.Vector3{
		X: local.X + float32(p.Pos.Chunk.X)*components.ChunkSize,
		Y: local.Y,
		Z: local.Z + float32(p.Pos.Chunk.Z)*components.ChunkSize,
	}
}

func (sb *sandbox) maxLevels() int {
	n := 0
	for i := range sb.cells {
		for _, p := range sb.cells[i].plans {
			if len(p.Levels) > n {
				n = len(p.Levels)
			}
		}
	}
	return n
}

func (sb *sandbox) clampLevelFilter() {
	if sb.levelFilter >= sb.maxLevels() {
		sb.levelFilter = -1
	}
}

// issueFocus resolves an issue to a world point. Codes whose EntityIdx is
// ambiguous (BadLevelRef indexes into whichever slice raised it) fall back to
// the plan centre rather than flying somewhere wrong.
func issueFocus(p *components.BuildingPlan, iss buildings.ValidationIssue) rl.Vector3 {
	centre := planWorld(p, p.Pos.Local)
	centre.Y += components.FloorHeight * 0.5
	if iss.EntityIdx < 0 {
		return centre
	}
	switch iss.Code {
	case buildings.CodeEmptyLevel, buildings.CodeLevelOverlap:
		if iss.EntityIdx < len(p.Levels) {
			a := p.Levels[iss.EntityIdx].AABB
			return rl.Vector3{X: a.CenterX(), Y: a.CenterY(), Z: a.CenterZ()}
		}
	case buildings.CodeLevelVolumeMissing:
		if iss.EntityIdx < len(p.Walls) {
			return planWorld(p, p.Walls[iss.EntityIdx].Local)
		}
	case buildings.CodeStairWpUnanchored:
		if iss.EntityIdx < len(p.Stairs) {
			return planWorld(p, p.Stairs[iss.EntityIdx].Local)
		}
	}
	return centre
}
