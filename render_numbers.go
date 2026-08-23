package main

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/assets"
	"rts-go/components"
)

// Bort numbers used to be geometry: every generator paints one into the hull,
// so twenty BTRs on a field were all 512. The bake now strips those objects and
// leaves a decal.number mount where they were; the number itself is stamped
// here from ten glyph meshes, so each hull gets its own and the player can name
// the thing he is watching.
//
// LOD0 only. Past that the glyphs are a couple of pixels and cost two draw
// calls per side to say nothing.
const (
	digitsAsset  = "digits"
	digitStep    = 0.23 // metres between glyph centres
	digitStandby = 0.02 // lift off the panel so it does not z-fight
)

// vehicleNumber is a stable three-digit tag per hull. Derived from the entity
// id rather than stored: it is a label, not state, and must not enter a save.
func vehicleNumber(ent ecs.Entity) [3]int {
	h := uint32(ent.ID())*2654435761 + 0x9e3779b9
	h ^= h >> 15
	n := 100 + int(h%900)
	return [3]int{n / 100, (n / 10) % 10, n % 10}
}

func (g *Game) drawVehicleNumber(ent ecs.Entity, kind components.VehicleKind,
	side components.AssetSide, pos rl.Vector3, yaw, turretYaw float32, lod int) {
	m := g.Ctx.Models
	if lod != 0 || m.reg == nil || !m.hasDigits || !m.has(kind, side) {
		return
	}
	host := m.reg.Get(m.byKind[kind][side])
	if host == nil {
		return
	}
	pose := assets.Pose{Pos: pos, Yaw: yaw, Turret: turretYaw}
	num := vehicleNumber(ent)

	for _, face := range [...]struct {
		mount string
		flip  bool
	}{{"decal.number.left", false}, {"decal.number.right", true}} {
		mnt := host.Mount(face.mount)
		if mnt == nil {
			continue
		}
		base, ok := m.reg.PartMatrix(m.byKind[kind][side], mnt.Part, pose)
		if !ok {
			continue
		}
		// Sit just proud of the panel the mount names.
		at := rl.Vector3Add(mnt.Pos.Vec(), rl.Vector3Scale(mnt.Dir.Vec(), digitStandby))
		for k, d := range num {
			// Lay the glyphs out along the hull, then flip the whole run for
			// the far side: one rotation gives both the mirrored facing and
			// the reversed reading order.
			t := rl.MatrixTranslate(0, 0, float32(1-k)*digitStep)
			if face.flip {
				t = rl.MatrixMultiply(t, rl.MatrixRotateY(float32(math.Pi)))
			}
			t = rl.MatrixMultiply(t, rl.MatrixTranslate(at.X, at.Y, at.Z))
			m.reg.DrawPartAt(m.digits, digitPart(d), 0,
				rl.MatrixMultiply(t, base), rl.White)
		}
	}
}

var digitParts = [10]string{"d0", "d1", "d2", "d3", "d4", "d5", "d6", "d7", "d8", "d9"}

func digitPart(d int) string {
	if d < 0 || d > 9 {
		return digitParts[0]
	}
	return digitParts[d]
}
