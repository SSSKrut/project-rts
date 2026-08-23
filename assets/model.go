package assets

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
)

// Pose is everything the renderer knows about one instance. Turret, Gun,
// WheelSpin and Steer are angles in radians about the pivots the manifest
// names — the sim owns Turret.Yaw, the rest is derived at draw time from speed
// and heading, so no component grows a field for cosmetics.
type Pose struct {
	Pos       rl.Vector3
	Yaw       float32
	Turret    float32
	Gun       float32
	WheelSpin float32
	Steer     float32
	// Rotor drives parts on the "spin_y" axis — a mast, which turns about up.
	// A tail rotor is not this: its shaft is lateral, which is what "spin"
	// already means, so it rides WheelSpin instead of growing a third channel.
	Rotor float32
	Tint  rl.Color
}

// Ensure uploads the geometry on first use. Returns nil if the asset does not
// exist or the upload failed; every caller treats that as "draw the box".
func (r *Registry) Ensure(id ModelID) *Asset {
	a := r.Get(id)
	if a == nil {
		return nil
	}
	if !a.loaded {
		a.model = rl.LoadModel(a.glb)
		a.loaded = true
		if a.model.MeshCount == 0 {
			return nil
		}
	}
	if a.model.MeshCount == 0 {
		return nil
	}
	if !r.matReady {
		r.shader = newShader()
		r.material = rl.LoadMaterialDefault()
		if r.shader.ok {
			r.material.Shader = r.shader.shader
		}
		r.matReady = true
	}
	return a
}

// PickLOD chooses a level from projected height in pixels rather than metres:
// a zoomed-in camera must not get the far mesh just because the vehicle is
// distant. Callers own the hysteresis — flipping every frame at a threshold is
// worse than one level too many.
func (r *Registry) PickLOD(id ModelID, dist, fovYDeg, screenH float32) int {
	a := r.Get(id)
	if a == nil || len(a.LODs) == 0 {
		return 0
	}
	if dist < 0.01 {
		dist = 0.01
	}
	f := float32(math.Tan(float64(fovYDeg) * math.Pi / 360.0))
	px := a.Size[1] / dist * (screenH / (2 * f))
	switch {
	case px >= 90:
		return 0
	case px >= 26:
		return min(1, len(a.LODs)-1)
	default:
		return len(a.LODs) - 1
	}
}

// raylib-go's MatrixRotateX/Y are TRANSPOSED relative to raylib C's, and so
// relative to rl.Rotatef — which is what the placeholder-box path uses and what
// the sim's heading maths assumes (forward = sin/cos of Motion.Yaw, see
// VehicleDriver and emitExhaust). Go's MatrixRotateY(t) is C's MatrixRotateY(-t):
//
//	C   m2 = -sin, m8 = +sin        Go   M2 = +sin, M8 = -sin
//
// The matrix is uploaded column-major and used as M*v in the shader, so the
// angle is negated once here rather than at each call site. Symptom when it is
// not: a hull that reads correctly at yaw 0 and 180 and drives backwards at
// +-90 — found on mrap_2's first drive, 2026-08-23.
func rotY(a float32) rl.Matrix { return rl.MatrixRotateY(-a) }
func rotX(a float32) rl.Matrix { return rl.MatrixRotateX(-a) }

// partMatrices resolves every part's world transform for one pose. Parts are
// ordered parent-before-child by prepare(), so one pass suffices.
func (a *Asset) partMatrices(p Pose, out []rl.Matrix) []rl.Matrix {
	out = out[:0]
	base := rl.MatrixMultiply(rotY(p.Yaw),
		rl.MatrixTranslate(p.Pos.X, p.Pos.Y, p.Pos.Z))
	for i := range a.Parts {
		part := &a.Parts[i]
		local := rl.MatrixIdentity()
		switch part.Axis {
		case "yaw":
			local = rotY(p.Turret)
		case "pitch":
			local = rotX(p.Gun)
		case "spin":
			local = rotX(p.WheelSpin)
			if part.Steer {
				local = rl.MatrixMultiply(local, rotY(p.Steer))
			}
		case "spin_y":
			local = rotY(p.Rotor)
		case "spin_y_rev":
			// Counter-rotating pair (tandem, coaxial, twin-mast). One angle
			// drives both, negated here, so the two discs can never drift out
			// of step the way two independent phases would.
			local = rotY(-p.Rotor)
		}
		if part.Axis != "" {
			pv := part.pivot
			local = rl.MatrixMultiply(rl.MatrixTranslate(-pv.X, -pv.Y, -pv.Z), local)
			local = rl.MatrixMultiply(local, rl.MatrixTranslate(pv.X, pv.Y, pv.Z))
		}
		parent := base
		if part.parent >= 0 {
			parent = out[part.parent]
		}
		out = append(out, rl.MatrixMultiply(local, parent))
	}
	return out
}

// Draw renders one instance at the given LOD. Opaque geometry goes down first
// and glass after it, or the depth write rejects whatever stands behind a
// windscreen.
func (r *Registry) Draw(id ModelID, lod int, p Pose) {
	a := r.Ensure(id)
	if a == nil {
		return
	}
	if lod < 0 {
		lod = 0
	}
	if lod >= len(a.LODs) {
		lod = len(a.LODs) - 1
	}
	r.setEncode(a.ColorSpace != "srgb")
	r.scratch = a.partMatrices(p, r.scratch)
	meshes := a.model.GetMeshes()

	tint := p.Tint
	if tint.A == 0 {
		tint = rl.White
	}
	r.material.GetMap(rl.MapDiffuse).Color = tint
	for pass := 0; pass < 2; pass++ {
		want := "opaque"
		if pass == 1 {
			want = "glass"
			glass := tint
			glass.A = uint8(float32(tint.A) * 0.55)
			r.material.GetMap(rl.MapDiffuse).Color = glass
		}
		for i := range a.Parts {
			for _, ref := range a.Parts[i].meshesAt(lod) {
				if ref.Bucket != want || int(ref.Mesh) >= len(meshes) {
					continue
				}
				rl.DrawMesh(meshes[ref.Mesh], r.material, r.scratch[i])
			}
		}
	}
}

func (p *Part) meshesAt(lod int) []MeshRef {
	if lod < 0 || lod >= len(p.byLOD) {
		return nil
	}
	return p.byLOD[lod]
}

// MountWorld resolves an attachment point into world space for a pose — where
// the exhaust plume starts, where the smoke grenade leaves the launcher. The
// mount rides its part, so a muzzle follows the turret without a special case.
func (r *Registry) MountWorld(id ModelID, mountID string, p Pose) (pos, dir rl.Vector3, ok bool) {
	a := r.Get(id)
	if a == nil {
		return pos, dir, false
	}
	m := a.Mount(mountID)
	if m == nil {
		return pos, dir, false
	}
	r.scratch = a.partMatrices(p, r.scratch)
	mat := rl.MatrixMultiply(rotY(p.Yaw),
		rl.MatrixTranslate(p.Pos.X, p.Pos.Y, p.Pos.Z))
	if i, found := a.partByID[m.Part]; found {
		mat = r.scratch[i]
	}
	pos = rl.Vector3Transform(m.Pos.rl(), mat)
	tip := rl.Vector3Transform(rl.Vector3Add(m.Pos.rl(), m.Dir.rl()), mat)
	dir = rl.Vector3Normalize(rl.Vector3Subtract(tip, pos))
	return pos, dir, true
}

// Unload frees every uploaded model plus the shared material and shader. The
// material borrows the shader, and UnloadMaterial would free what it borrowed
// (ISSUES #27), so the handle is taken back out first.
func (r *Registry) Unload() {
	for _, a := range r.assets {
		if a.loaded && a.model.MeshCount > 0 {
			rl.UnloadModel(a.model)
		}
		a.loaded = false
	}
	if r.matReady {
		r.material.Shader = rl.Shader{}
		rl.UnloadMaterial(r.material)
		if r.shader.ok {
			rl.UnloadShader(r.shader.shader)
		}
		r.matReady = false
	}
}

// PartMatrix is the world transform of one part for a pose — the frame a decal
// or an attached prop has to be placed in.
func (r *Registry) PartMatrix(id ModelID, part string, p Pose) (rl.Matrix, bool) {
	a := r.Get(id)
	if a == nil {
		return rl.MatrixIdentity(), false
	}
	r.scratch = a.partMatrices(p, r.scratch)
	i, ok := a.partByID[part]
	if !ok || i >= len(r.scratch) {
		return rl.MatrixIdentity(), false
	}
	return r.scratch[i], true
}

// DrawPartAt draws one named part at an arbitrary transform, ignoring the pose
// machinery. Used for the digit glyphs stamped onto a hull.
func (r *Registry) DrawPartAt(id ModelID, part string, lod int, m rl.Matrix, tint rl.Color) {
	a := r.Ensure(id)
	if a == nil {
		return
	}
	p := a.Part(part)
	if p == nil {
		return
	}
	r.setEncode(a.ColorSpace != "srgb")
	r.material.GetMap(rl.MapDiffuse).Color = tint
	meshes := a.model.GetMeshes()
	for _, ref := range p.meshesAt(lod) {
		if int(ref.Mesh) < len(meshes) {
			rl.DrawMesh(meshes[ref.Mesh], r.material, m)
		}
	}
}
