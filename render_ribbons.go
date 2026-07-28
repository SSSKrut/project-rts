package main

import (
	"fmt"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
	"rts-go/gen/ribbons"
	"rts-go/systems"
)

// ribbonCullDistSq — beyond this the road / water strips are skipped.
const ribbonCullDistSq float32 = 700 * 700

// ribbonSet owns the world's linear-feature geometry: built once from the
// road graph and river polylines, uploaded after the window exists, drawn
// straight from here. Roads never stream, so there is no per-chunk rebuild
// and no eviction path. Roads and water are separate groups because water is
// translucent and has to go down after the opaque world.
type ribbonSet struct {
	Roads ribbonGroup
	Water ribbonGroup
}

type ribbonGroup struct {
	geom []ribbons.Mesh
	gpu  []rl.Mesh
}

// buildRibbons derives everything geometric from RoadGraph + Rivers: the deck
// index the sim queries for height, and the meshes the renderer draws.
func (g *Game) buildRibbons() {
	net := ribbons.BuildRoads(&g.Res.RoadGraph, systems.GroundHeight)
	g.Res.RoadSurface.Build(net.Deck)
	g.Ctx.Ribbons.Roads.geom = net.Meshes
	g.Ctx.Ribbons.Water.geom = ribbons.BuildWater(&g.Res.Rivers, systems.GroundHeight)
	fmt.Printf("ribbons: road=%d water=%d deck-samples=%d\n",
		len(net.Meshes), len(g.Ctx.Ribbons.Water.geom), len(net.Deck))
}

func (rs *ribbonSet) upload() {
	rs.Roads.upload()
	rs.Water.upload()
}

func (rs *ribbonSet) unload() {
	rs.Roads.unload()
	rs.Water.unload()
}

// upload compacts empty runs out of geom so it stays index-aligned with gpu,
// and keeps the vertex slices alive — rl.Mesh holds pointers into them.
func (rg *ribbonGroup) upload() {
	rg.gpu = make([]rl.Mesh, 0, len(rg.geom))
	kept := rg.geom[:0]
	for i := range rg.geom {
		src := &rg.geom[i]
		if len(src.Verts) == 0 || len(src.Indices) == 0 {
			continue
		}
		m := rl.Mesh{
			VertexCount:   int32(len(src.Verts) / 3),
			TriangleCount: src.TriangleCount(),
			Vertices:      &src.Verts[0],
			Normals:       &src.Norms[0],
			Colors:        &src.Colors[0],
			Indices:       &src.Indices[0],
		}
		rl.UploadMesh(&m, false)
		rg.gpu = append(rg.gpu, m)
		kept = append(kept, *src)
	}
	rg.geom = kept
}

func (rg *ribbonGroup) unload() {
	for i := range rg.gpu {
		rl.UnloadMesh(&rg.gpu[i])
	}
	rg.gpu = nil
}

// draw rebases each mesh from its build anchor onto the current render
// origin — the same translation chunk meshes use. Returns the draw count.
func (rg *ribbonGroup) draw(material rl.Material) int {
	originX := float32(systems.CurrentOriginChunk.X) * components.ChunkSize
	originZ := float32(systems.CurrentOriginChunk.Z) * components.ChunkSize
	cam := systems.CurrentCamera.Position
	drawn := 0
	for i := range rg.gpu {
		src := &rg.geom[i]
		dx := src.CullX - originX - cam.X
		dz := src.CullZ - originZ - cam.Z
		if dx*dx+dz*dz > ribbonCullDistSq+src.CullR*src.CullR {
			continue
		}
		rl.DrawMesh(rg.gpu[i], material,
			rl.MatrixTranslate(src.AnchorX-originX, 0, src.AnchorZ-originZ))
		drawn++
	}
	return drawn
}
