package systems

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// coverSlotSpec is the pure-data result of a cover-slot generator. Local is
// in chunk-local coords of the slot's host chunk; OriginDir is a unit vector
// pointing OUTWARD from cover (the direction a shooter peeks).
type coverSlotSpec struct {
	Local     rl.Vector3
	Host      ecs.Entity
	HostKind  components.CoverHostKind
	OriginDir rl.Vector3
	Quality   uint8
	Stance    components.StanceMask
}

// propCoverSlots emits 8 cover slots radially around a prop with non-zero
// Cover. Slots stand BBoxRadius out on the eight compass points; placeholder
// primitives are convex, so any slot at bbox radius has the prop between it
// and the centre by construction.
//
// Short props (effective height < 1.5 m) are crouch-only; the rest support
// crouch+stand. Prone is reserved for wall-corner slots.
func propCoverSlots(host ecs.Entity, propLocal rl.Vector3, meta components.PropMeta, scale float32) []coverSlotSpec {
	if meta.Cover <= 0 || meta.BBoxRadius <= 0 {
		return nil
	}
	q := int(meta.Cover * 255)
	if q < 1 {
		return nil
	}
	if q > 255 {
		q = 255
	}
	stance := components.StanceMaskCrouch | components.StanceMaskStand

	// Sphere primitives use 2*X (radius); rest use Size.Y; trees count trunk
	// only (canopy doesn't shield).
	height := meta.Size.Y
	if meta.Primitive == components.PrimitiveSphere {
		height = meta.Size.X * 2
	}
	if height*scale < 1.5 {
		stance = components.StanceMaskCrouch
	}

	r := meta.BBoxRadius * scale
	out := make([]coverSlotSpec, 0, 8)
	for d := 0; d < 8; d++ {
		ang := float64(d) * (2 * math.Pi / 8)
		cosA := float32(math.Cos(ang))
		sinA := float32(math.Sin(ang))
		out = append(out, coverSlotSpec{
			Local: rl.Vector3{
				X: propLocal.X + cosA*r,
				Y: propLocal.Y,
				Z: propLocal.Z + sinA*r,
			},
			Host:      host,
			HostKind:  components.CoverHostProp,
			OriginDir: rl.Vector3{X: cosA, Y: 0, Z: sinA},
			Quality:   uint8(q),
			Stance:    stance,
		})
	}
	return out
}

// windowCoverSlots emits one slot at the centre of a window opening. OriginDir
// is the wall's outward normal (already computed at building-spawn time and
// stored as CoverDirection on the wall entity). Quality fixed at 200 - windows
// are good cover for the slot, but the shooter exposes torso-up.
func windowCoverSlots(host ecs.Entity, wallLocal rl.Vector3, w components.WallSegment, outward rl.Vector3) []coverSlotSpec {
	if w.OpeningKind != components.OpeningWindow || w.OpeningWidth <= 0 {
		return nil
	}
	sa := float32(math.Sin(float64(w.Yaw)))
	ca := float32(math.Cos(float64(w.Yaw)))
	centreT := w.OpeningCenterT * w.Length
	return []coverSlotSpec{{
		Local: rl.Vector3{
			X: wallLocal.X + sa*centreT,
			Y: wallLocal.Y + w.OpeningBottom + w.OpeningHeight*0.5,
			Z: wallLocal.Z + ca*centreT,
		},
		Host:      host,
		HostKind:  components.CoverHostWindow,
		OriginDir: outward,
		Quality:   200,
		Stance:    components.StanceMaskCrouch | components.StanceMaskStand,
	}}
}

// wallCornerSrc is per-wall input for the corner pairing pass. The entity id
// becomes the deterministic owner of any corner slot it shares.
type wallCornerSrc struct {
	entity                     ecs.Entity
	startX, startZ, endX, endZ float32
	normX, normZ               float32
	baseY                      float32
	chunk                      components.ChunkCoord
}

// wallCornerCoverSlots returns one slot per pair of walls sharing a world-XZ
// endpoint within tolerance. Each corner is owned by the lower-ID wall (so
// ByHost lookups stay deterministic). OriginDir is the normalized sum of the
// two outward normals — bisector where the shooter peeks around the corner.
// Caller must group walls by building root before calling.
func wallCornerCoverSlots(walls []wallCornerSrc) []coverSlotSpec {
	if len(walls) < 2 {
		return nil
	}
	type endpointPair struct {
		ax, az, bx, bz float32
	}
	var out []coverSlotSpec
	// Dedupe by 1 cm-binned XZ key — multiple pair combos can hit the same
	// corner (3+ walls meeting at one vertex).
	seen := map[uint64]bool{}

	for i := 0; i < len(walls); i++ {
		for j := i + 1; j < len(walls); j++ {
			wi, wj := walls[i], walls[j]
			candidates := [4]endpointPair{
				{wi.startX, wi.startZ, wj.startX, wj.startZ},
				{wi.startX, wi.startZ, wj.endX, wj.endZ},
				{wi.endX, wi.endZ, wj.startX, wj.startZ},
				{wi.endX, wi.endZ, wj.endX, wj.endZ},
			}
			for _, c := range candidates {
				if absDelta(c.ax, c.bx) > 0.1 || absDelta(c.az, c.bz) > 0.1 {
					continue
				}
				keyX := int32(math.Round(float64(c.ax * 100)))
				keyZ := int32(math.Round(float64(c.az * 100)))
				key := uint64(uint32(keyX))<<32 | uint64(uint32(keyZ))
				if seen[key] {
					continue
				}

				nx := wi.normX + wj.normX
				nz := wi.normZ + wj.normZ
				ln := float32(math.Sqrt(float64(nx*nx + nz*nz)))
				// Claim the corner only once a slot is actually emitted.
				// Claiming before this guard let a collinear pair (opposing
				// normals, no bisector) burn the key, so a third wall at the
				// same vertex — with a perfectly good bisector — got nothing.
				if ln <= 1e-3 {
					continue
				}
				seen[key] = true
				nx /= ln
				nz /= ln

				host := wi.entity
				if wj.entity.ID() < host.ID() {
					host = wj.entity
				}

				baseX := float32(wi.chunk.X) * components.ChunkSize
				baseZ := float32(wi.chunk.Z) * components.ChunkSize
				out = append(out, coverSlotSpec{
					Local: rl.Vector3{
						X: c.ax - baseX,
						Y: wi.baseY,
						Z: c.az - baseZ,
					},
					Host:      host,
					HostKind:  components.CoverHostWallCorner,
					OriginDir: rl.Vector3{X: nx, Y: 0, Z: nz},
					Quality:   180,
					Stance:    components.StanceMaskProne | components.StanceMaskCrouch | components.StanceMaskStand,
				})
			}
		}
	}
	return out
}

// coverSlotsForBuilding returns every live cover-slot entity attached to any
// child of `root`. Two-hop walk (root → children → slots) is needed because
// the slot index is host-keyed (wall / window / corner anchor), not
// building-keyed.
func coverSlotsForBuilding(coverIdx *CoverSlotIndex, buildingIdx *BuildingChildIndex, root ecs.Entity) []ecs.Entity {
	if coverIdx == nil || buildingIdx == nil {
		return nil
	}
	var out []ecs.Entity
	for _, child := range buildingIdx.Loaded[root] {
		out = append(out, coverIdx.ByHost[child]...)
	}
	return out
}
