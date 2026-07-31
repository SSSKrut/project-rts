package main

import (
	rl "github.com/gen2brain/raylib-go/raylib"
)

// Suppression and smoke — the two things that make crossing open ground
// survivable, and the whole reason an attack can be more than a suicide. Rounds
// cracking past a fighter shake its aim and slow it down; a cloud takes both
// sides' eyes away, so the ground under it can be crossed.

const (
	suppressRadius   = 3.0  // how close a round has to pass to be felt
	suppressPerMetre = 0.15 // built up per metre of nearby flight, not per tick
	suppressDecay    = 0.45 // per second, once the rounds stop
	suppressSpread   = 0.30 // extra spread at full suppression
	suppressSlow     = 0.55 // fraction of speed lost at full suppression

	smokeRadius = 6.0
	smokeHeight = 4.0
	smokeLife   = 12.0
	smokeGrow   = 1.5
	smokeThrow  = 14.0
	smokeReload = 18.0
)

var colorSmoke = rl.Color{R: 205, G: 205, B: 210, A: 70}

type Smoke struct {
	Position rl.Vector3
	Age      float32
}

// Radius grows to full over smokeGrow seconds — a cloud is not a wall the
// instant it lands.
func (s *Smoke) Radius() float32 { return smokeRadius * min(s.Age/smokeGrow, 1) }

// suppress shakes everyone the round flew past, friend or foe. The build-up is
// per metre of nearby flight, so it does not depend on the tick rate — the
// trainer at 30 Hz and the game at 144 Hz feel the same pressure.
func (w *World) suppress(from, to rl.Vector3, owner int) {
	travel := rl.Vector3Distance(from, to)
	for i := range w.Characters {
		c := &w.Characters[i]
		if c.ID == owner {
			continue
		}
		d := SegDistXZ(from, to, c.Position)
		if d > suppressRadius {
			continue
		}
		c.Suppression = min(c.Suppression+suppressPerMetre*travel*(1-d/suppressRadius), 1)
	}
}

func (w *World) stepCombatState(dt float32) {
	for i := range w.Characters {
		c := &w.Characters[i]
		c.Suppression = max(c.Suppression-suppressDecay*dt, 0)
		c.SmokeCooldown -= dt
	}

	for i := len(w.Smokes) - 1; i >= 0; i-- {
		w.Smokes[i].Age += dt
		if w.Smokes[i].Age >= smokeLife {
			w.Smokes[i] = w.Smokes[len(w.Smokes)-1]
			w.Smokes = w.Smokes[:len(w.Smokes)-1]
		}
	}
}

// throwSmoke lands a cloud at `at`, capped to throwing range along that bearing.
func (w *World) throwSmoke(c *Character, at rl.Vector3) {
	if c.SmokeCooldown > 0 {
		return
	}
	c.SmokeCooldown = smokeReload
	w.SmokesThrown[c.Team]++

	d := rl.Vector3{X: at.X - c.Position.X, Z: at.Z - c.Position.Z}
	if l := rl.Vector3Length(d); l > smokeThrow {
		d = rl.Vector3Scale(d, smokeThrow/l)
	}
	w.Smokes = append(w.Smokes, Smoke{Position: rl.Vector3Add(c.Position, d)})
}

// sighted is the one sight test in the game: walls stop it, and so does smoke —
// including smoke you are standing in, which is what hides you.
func (w *World) sighted(from, to rl.Vector3) bool {
	if w.Phys.Blocked(from, to) {
		return false
	}

	d := rl.Vector3Subtract(to, from)
	dist := rl.Vector3Length(d)
	if dist < 1e-4 {
		return true
	}
	dir := rl.Vector3Scale(d, 1/dist)

	for i := range w.Smokes {
		s := &w.Smokes[i]
		base := rl.Vector3{X: s.Position.X, Y: s.Position.Y, Z: s.Position.Z}
		if _, hit := RayCylinderT(from, dir, dist, base, s.Radius(), smokeHeight); hit {
			return false
		}
	}
	return true
}

func (w *World) canSee(seer, target *Character) bool {
	return w.sighted(muzzleOf(seer.Position), muzzleOf(target.Position))
}

func drawSmokes(smokes []Smoke) {
	for i := range smokes {
		s := &smokes[i]
		r := s.Radius()

		colour := colorSmoke
		if left := smokeLife - s.Age; left < 2 {
			colour.A = uint8(float32(colour.A) * left / 2)
		}
		for k := range 3 {
			y := s.Position.Y + smokeHeight*(float32(k)+0.5)/3
			rl.DrawSphere(rl.Vector3{X: s.Position.X, Y: y, Z: s.Position.Z}, r*(1-0.12*float32(k)), colour)
		}
	}
}

// drawSuppression is the thin amber bar under the health bar — without it a
// fighter that suddenly shoots wide and walks slowly just looks broken.
func drawSuppression(c *Character) {
	if c.Suppression <= 0.02 {
		return
	}
	const barW, barD = 1.3, 0.12

	base := rl.Vector3{X: c.Position.X, Y: c.Position.Y + charHeight + 0.2, Z: c.Position.Z}
	fill := base
	fill.X -= barW * (1 - c.Suppression) / 2
	rl.DrawCube(fill, barW*c.Suppression, 0.04, barD, rl.Color{R: 255, G: 190, B: 40, A: 230})
}
