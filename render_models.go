package main

import (
	"fmt"
	"math"
	"os"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/assets"
	"rts-go/components"
	"rts-go/systems"
)

// modelDir is where the bake's output is checked in. Missing or unreadable is
// not fatal: every draw site falls back to the placeholder box, so the game
// still runs on a tree that has not been baked.
const modelDir = "assets/models"

// modelSet is the render-side model state: the registry plus the per-entity
// wheel angle. Spin is integrated here rather than stored on the entity —
// it is pure cosmetics, so it must not enter a component, save_spec or a hash.
type modelSet struct {
	reg     *assets.Registry
	byKind  [components.VehicleKindCount][components.AssetSideCount]assets.ModelID
	hasKind [components.VehicleKindCount][components.AssetSideCount]bool

	digits    assets.ModelID
	hasDigits bool

	spin     map[ecs.Entity]float32
	spinSeen map[ecs.Entity]float32
}

func newModelSet() *modelSet {
	m := &modelSet{
		spin:     make(map[ecs.Entity]float32),
		spinSeen: make(map[ecs.Entity]float32),
	}
	reg, err := assets.LoadRegistry(modelDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "models: %v (falling back to boxes)\n", err)
		return m
	}
	m.reg = reg
	m.digits, m.hasDigits = reg.ID(digitsAsset)
	for kind, sides := range components.VehicleAssetName {
		for side, name := range sides {
			if name == "" {
				continue
			}
			if id, ok := reg.ID(name); ok {
				m.byKind[kind][side] = id
				m.hasKind[kind][side] = true
			}
		}
	}
	return m
}

func (m *modelSet) unload() {
	if m.reg != nil {
		m.reg.Unload()
	}
}

// beginFrame hands the shader this frame's sun and drops the wheel angles of
// anything that stopped being drawn, so a despawned hull cannot leak an entry.
func (m *modelSet) beginFrame(pal skyPalette, camPos rl.Vector3) {
	if m.reg == nil {
		return
	}
	v3 := func(c [3]float32) rl.Vector3 { return rl.Vector3{X: c[0], Y: c[1], Z: c[2]} }
	m.reg.SetLight(assets.Light{
		SunDir:   v3(pal.lightDir),
		SunColor: v3(pal.lightCol),
		SkyColor: v3(pal.ambSky),
		Bounce:   v3(pal.ambBounce),
		FogColor: v3(pal.horizon),
		FogStart: 260,
		FogEnd:   1400,
	}, camPos)
	m.spin, m.spinSeen = m.spinSeen, m.spin
	clear(m.spinSeen)
}

func (m *modelSet) has(kind components.VehicleKind, side components.AssetSide) bool {
	return m.reg != nil && int(kind) < len(m.hasKind) && m.hasKind[kind][side]
}

// rollWheels integrates the wheel angle from ground speed. Radius comes from
// the hull box rather than the model: it only has to look plausible, and the
// spec is the thing both halves already agree on.
func (m *modelSet) rollWheels(ent ecs.Entity, speed, radius, dt float32) float32 {
	a := m.spin[ent]
	if radius > 0.05 {
		a += speed / radius * dt
		if a > 2*math.Pi || a < -2*math.Pi {
			a = float32(math.Mod(float64(a), 2*math.Pi))
		}
	}
	m.spinSeen[ent] = a
	return a
}

// drawVehicleModel replaces the placeholder box for a class that has an asset.
// Turret yaw is the sim's; wheel spin and steer are derived here — the driver
// never stores them, and neither does the save.
func (g *Game) drawVehicleModel(ent ecs.Entity, pos rl.Vector3, kind components.VehicleKind,
	side components.AssetSide, yaw, turretYaw float32, tint rl.Color) int {
	m := g.Ctx.Models
	id := m.byKind[kind][side]
	spec := components.SpecForVehicle(kind)

	speed, velYaw := float32(0), float32(0)
	if mo := g.Svc.UnitFactory.MotionMap.Get(ent); mo != nil {
		speed, velYaw = mo.Speed, mo.VelocityYaw
	}
	steer := clampF(velYaw*0.6, -0.5, 0.5)
	spin := m.rollWheels(ent, speed, spec.BoxHgt*0.28, rl.GetFrameTime())

	cam := systems.CurrentCamera.Position
	dist := rl.Vector3Distance(cam, pos)
	lod := m.reg.PickLOD(id, dist, systems.CurrentCamera.Fovy, float32(g.UI.ScreenH))

	m.reg.Draw(id, lod, assets.Pose{
		Pos: pos, Yaw: yaw, Turret: turretYaw,
		WheelSpin: spin, Steer: steer, Tint: tint,
	})
	return lod
}

// vehicleMountWorld resolves a named attachment point on a live hull. Callers
// that have no model fall back to their own box-relative guess.
func (g *Game) vehicleMountWorld(ent ecs.Entity, kind components.VehicleKind,
	side components.AssetSide, mount string,
	pos rl.Vector3, yaw, turretYaw float32) (rl.Vector3, rl.Vector3, bool) {
	m := g.Ctx.Models
	if !m.has(kind, side) {
		return rl.Vector3{}, rl.Vector3{}, false
	}
	return m.reg.MountWorld(m.byKind[kind][side], mount,
		assets.Pose{Pos: pos, Yaw: yaw, Turret: turretYaw})
}

func clampF(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
