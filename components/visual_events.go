package components

import rl "github.com/gen2brain/raylib-go/raylib"

// Phase 14 M14.2 — transient combat visuals. WeaponSystem appends one
// TracerSpec + one ImpactSpec per resolved shot (or a wall-blocked miss);
// the 3D render pass walks both slices, fading alpha by (TTL - age) / TTL.
// Decay() drops expired entries each frame before the draw.
//
// Lives in components/ (not core/) because the resource carries raylib
// types — keeping core raylib-free is a hidden constraint of the package
// layout (only systems/main/ui touch raylib). Plan section M14.2 originally
// suggested core/; moved here so the import graph stays clean.
//
// Phase 14.5 will replace the slice with an ECS-entity-backed particle pool
// so smoke / dust / casings can share the same lifecycle without copying.

// TracerSpec — a single bullet trail. From → To is the visible segment
// (muzzle → impact); Color is the line tint; SpawnTime + TTL gate the fade.
type TracerSpec struct {
	From      rl.Vector3
	To        rl.Vector3
	Color     rl.Color
	SpawnTime float32
	TTL       float32
}

// ImpactSpec — a single point-impact ping at the bullet's terminal position.
// Renderer draws a small sphere; same fade-by-age rule as TracerSpec.
type ImpactSpec struct {
	Pos       rl.Vector3
	Color     rl.Color
	SpawnTime float32
	TTL       float32
}

// visualEventsCap — soft cap on the number of live tracer + impact records.
// At peak (12+12 unit firefight, ~5 shots/sec each, 150 ms tracer TTL) we
// expect ~50 live entries; 256 leaves a 5× headroom. Old entries are
// silently dropped when the buffer overflows — Phase 14.5 particle pool
// will replace this with proper eviction.
const visualEventsCap = 256

// VisualEvents is the singleton resource that bridges WeaponSystem (writer)
// and the 3D render pass (reader). Append* methods are safe to call from
// serial code only — WeaponSystem batches per-worker into local slices and
// merges into the resource in its serial post-pass.
type VisualEvents struct {
	Tracers []TracerSpec
	Impacts []ImpactSpec
}

func NewVisualEvents() VisualEvents {
	return VisualEvents{
		Tracers: make([]TracerSpec, 0, visualEventsCap),
		Impacts: make([]ImpactSpec, 0, visualEventsCap),
	}
}

// AppendTracer / AppendImpact append a record, evicting the oldest if the
// buffer is at capacity. Serial-only — WeaponSystem calls these from its
// post-pass after the parallel raycast workers finish.
func (v *VisualEvents) AppendTracer(t TracerSpec) {
	if len(v.Tracers) >= visualEventsCap {
		copy(v.Tracers, v.Tracers[1:])
		v.Tracers = v.Tracers[:visualEventsCap-1]
	}
	v.Tracers = append(v.Tracers, t)
}

func (v *VisualEvents) AppendImpact(i ImpactSpec) {
	if len(v.Impacts) >= visualEventsCap {
		copy(v.Impacts, v.Impacts[1:])
		v.Impacts = v.Impacts[:visualEventsCap-1]
	}
	v.Impacts = append(v.Impacts, i)
}

// Decay drops every record whose age (now - SpawnTime) exceeds its TTL.
// Stable in-place compaction (preserves draw order). main.go calls this
// once per frame in the render loop before the 3D pass.
func (v *VisualEvents) Decay(now float32) {
	n := 0
	for i := range v.Tracers {
		if now-v.Tracers[i].SpawnTime <= v.Tracers[i].TTL {
			v.Tracers[n] = v.Tracers[i]
			n++
		}
	}
	v.Tracers = v.Tracers[:n]

	n = 0
	for i := range v.Impacts {
		if now-v.Impacts[i].SpawnTime <= v.Impacts[i].TTL {
			v.Impacts[n] = v.Impacts[i]
			n++
		}
	}
	v.Impacts = v.Impacts[:n]
}
