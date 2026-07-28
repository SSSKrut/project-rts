package main

import (
	"fmt"
	"rts-go/core"
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"
)

const (
	screenW = 1280
	screenH = 720

	camSpeed     = 12.0
	camHeight    = 25.0
	camHeightMin = 5.0
	camHeightMax = 60.0
	camZoomStep  = 2.5

	groundSize = 200.0

	teamCount   = 2
	teamSize    = 6
	spawnLineX  = 18.0
	spawnSpread = 14.0

	charRadius = 0.5
	charHeight = 1.6
	charMaxHP  = 100.0
	muzzleY    = 1.1

	bulletSpeed  = 45.0
	bulletDamage = 22.0
	bulletRadius = 0.12
	bulletLife   = 3.0

	npcFireDelay    = 1.1
	npcSpread       = 0.09
	npcRange        = 55.0
	playerFireDelay = 0.18
)

const (
	_ = iota
	WaitingForInput
	Playing
	GameOver
)

var (
	colorGround    = rl.Color{R: 50, G: 50, B: 50, A: 255}
	colorCrosshair = rl.Color{R: 0, G: 255, B: 140, A: 255}
	colorOutline   = rl.Color{R: 10, G: 10, B: 10, A: 190}
	colorText      = rl.Color{R: 235, G: 235, B: 235, A: 255}

	rainbow = []rl.Color{
		{R: 235, G: 30, B: 30, A: 255},
		{R: 255, G: 130, B: 0, A: 255},
		{R: 240, G: 225, B: 40, A: 255},
		{R: 40, G: 210, B: 70, A: 255},
		{R: 40, G: 140, B: 255, A: 255},
		{R: 90, G: 60, B: 210, A: 255},
		{R: 180, G: 70, B: 230, A: 255},
	}

	camBindings = []struct {
		key  int32
		axis rl.Vector3
	}{
		{rl.KeyW, rl.Vector3{Z: -1}},
		{rl.KeyS, rl.Vector3{Z: 1}},
		{rl.KeyA, rl.Vector3{X: -1}},
		{rl.KeyD, rl.Vector3{X: 1}},
	}
)

// Character covers both the player and the NPCs — they share every rule that
// matters here (shoot, bleed, die), only the trigger source differs.
type Character struct {
	ID        int
	Team      int
	Position  rl.Vector3
	Health    float32
	Cooldown  float32
	FireDelay float32
	Spread    float32
	IsPlayer  bool
}

type Bullet struct {
	Position rl.Vector3
	Velocity rl.Vector3
	Color    rl.Color
	Damage   float32
	Owner    int
	Life     float32
}

type World struct {
	TeamColors [teamCount]rl.Color
	Characters []Character
	Bullets    []Bullet

	seed   uint64
	nextID int
}

func main() {
	rl.InitWindow(screenW, screenH, "RTS - Shooter Sandbox")
	defer rl.CloseWindow()
	rl.SetTargetFPS(144)

	app := core.NewApp()
	world := &World{seed: uint64(time.Now().UnixNano())}

	camera := rl.Camera3D{
		Position:   rl.Vector3{X: 0, Y: camHeight, Z: 0},
		Target:     rl.Vector3{X: 0, Y: 0, Z: 0},
		Up:         rl.Vector3{X: 0, Y: 0, Z: -1},
		Fovy:       50,
		Projection: rl.CameraPerspective,
	}

	lastTick := time.Now()
	gameState := WaitingForInput

	for !rl.WindowShouldClose() {
		now := time.Now()
		dt := float32(now.Sub(lastTick).Seconds())
		lastTick = now
		if dt > 0.25 {
			dt = 0.25
		}

		app.Tick(time.Duration(dt * float32(time.Second)))

		var aim rl.Vector3
		aiming := false

		switch gameState {
		case Playing:
			moveCamera(&camera, dt)
			aim, aiming = mouseGroundPoint(camera)
			if aiming && rl.IsMouseButtonDown(rl.MouseButtonLeft) {
				world.playerFire(aim)
			}
			world.update(dt)
			if world.over() {
				gameState = GameOver
			}

		case WaitingForInput, GameOver:
			if rl.IsKeyPressed(rl.KeySpace) {
				world.gameStart()
				gameState = Playing
			}
		}

		rl.BeginDrawing()
		rl.ClearBackground(rl.Color{R: 0, G: 0, B: 0, A: 255})

		rl.BeginMode3D(camera)
		drawGround()
		world.drawUnits()
		if aiming {
			drawCrosshair(aim)
		}
		rl.EndMode3D()

		world.drawHUD(gameState)
		rl.EndDrawing()
	}
}

func (w *World) gameStart() {
	w.TeamColors = pickTeamColors(&w.seed)
	w.Characters = w.Characters[:0]
	w.Bullets = w.Bullets[:0]

	for team := range teamCount {
		lineX := float32(spawnLineX)
		if team == 0 {
			lineX = -spawnLineX
		}
		for i := range teamSize {
			w.spawn(team, rl.Vector3{
				X: lineX + (pseudoRand(&w.seed)-0.5)*spawnSpread*0.4,
				Z: (pseudoRand(&w.seed) - 0.5) * spawnSpread * 2,
			}, team == 0 && i == 0)
		}
	}
}

func (w *World) spawn(team int, pos rl.Vector3, isPlayer bool) {
	w.nextID++
	c := Character{
		ID:        w.nextID,
		Team:      team,
		Position:  pos,
		Health:    charMaxHP,
		Cooldown:  pseudoRand(&w.seed) * npcFireDelay, // stagger the opening volley
		FireDelay: npcFireDelay,
		Spread:    npcSpread,
		IsPlayer:  isPlayer,
	}
	if isPlayer {
		c.FireDelay, c.Spread = playerFireDelay, 0
	}
	w.Characters = append(w.Characters, c)
}

func (w *World) update(dt float32) {
	for i := range w.Characters {
		c := &w.Characters[i]
		c.Cooldown -= dt
		if c.IsPlayer || c.Cooldown > 0 {
			continue
		}
		if target, ok := w.nearestEnemy(c); ok {
			w.fire(c, target)
		}
	}

	w.stepBullets(dt)
	w.removeDead()
}

func (w *World) player() *Character {
	for i := range w.Characters {
		if w.Characters[i].IsPlayer {
			return &w.Characters[i]
		}
	}
	return nil
}

func (w *World) playerFire(at rl.Vector3) {
	if p := w.player(); p != nil && p.Cooldown <= 0 {
		w.fire(p, at)
	}
}

func (w *World) nearestEnemy(c *Character) (rl.Vector3, bool) {
	best := float32(npcRange * npcRange)
	var pos rl.Vector3
	found := false

	for i := range w.Characters {
		o := &w.Characters[i]
		if o.Team == c.Team {
			continue
		}
		if d := rl.Vector3DistanceSqr(c.Position, o.Position); d < best {
			best, pos, found = d, o.Position, true
		}
	}
	return pos, found
}

// fire aims from the muzzle at `at` lifted to muzzle height, so shots fly level
// whether the target is a character or a point on the ground under the cursor.
func (w *World) fire(c *Character, at rl.Vector3) {
	c.Cooldown = c.FireDelay

	muzzle := rl.Vector3{X: c.Position.X, Y: c.Position.Y + muzzleY, Z: c.Position.Z}
	dir := rl.Vector3Normalize(rl.Vector3Subtract(rl.Vector3{X: at.X, Y: at.Y + muzzleY, Z: at.Z}, muzzle))
	if c.Spread > 0 {
		dir = rl.Vector3Normalize(rl.Vector3Add(dir, rl.Vector3{
			X: (pseudoRand(&w.seed) - 0.5) * c.Spread,
			Z: (pseudoRand(&w.seed) - 0.5) * c.Spread,
		}))
	}

	w.Bullets = append(w.Bullets, Bullet{
		Position: muzzle,
		Velocity: rl.Vector3Scale(dir, bulletSpeed),
		Color:    w.TeamColors[c.Team],
		Damage:   bulletDamage,
		Owner:    c.ID,
		Life:     bulletLife,
	})
}

func (w *World) stepBullets(dt float32) {
	for i := len(w.Bullets) - 1; i >= 0; i-- {
		b := &w.Bullets[i]
		b.Position = rl.Vector3Add(b.Position, rl.Vector3Scale(b.Velocity, dt))
		b.Life -= dt

		hit := w.hitCharacter(b)
		if hit != nil {
			hit.Health -= b.Damage
		}
		if hit == nil && b.Life > 0 && inField(b.Position) {
			continue
		}

		w.Bullets[i] = w.Bullets[len(w.Bullets)-1]
		w.Bullets = w.Bullets[:len(w.Bullets)-1]
	}
}

// hitCharacter tests the bullet against everyone but its own shooter — a stray
// round hurts friend and foe alike.
func (w *World) hitCharacter(b *Bullet) *Character {
	const halfH = charHeight / 2
	hitR := float32(charRadius + bulletRadius)

	for i := range w.Characters {
		c := &w.Characters[i]
		if c.ID == b.Owner {
			continue
		}
		dx, dz := b.Position.X-c.Position.X, b.Position.Z-c.Position.Z
		dy := b.Position.Y - (c.Position.Y + halfH)
		if dx*dx+dz*dz <= hitR*hitR && dy*dy <= (halfH+bulletRadius)*(halfH+bulletRadius) {
			return c
		}
	}
	return nil
}

func (w *World) removeDead() {
	for i := len(w.Characters) - 1; i >= 0; i-- {
		if w.Characters[i].Health > 0 {
			continue
		}
		w.Characters[i] = w.Characters[len(w.Characters)-1]
		w.Characters = w.Characters[:len(w.Characters)-1]
	}
}

func (w *World) alive(team int) int {
	n := 0
	for i := range w.Characters {
		if w.Characters[i].Team == team {
			n++
		}
	}
	return n
}

func (w *World) over() bool {
	return w.alive(0) == 0 || w.alive(1) == 0
}

func (w *World) drawUnits() {
	for i := range w.Characters {
		drawCharacter(&w.Characters[i], w.TeamColors[w.Characters[i].Team])
	}
	for i := range w.Bullets {
		drawBullet(w.Bullets[i].Position, bulletRadius, w.Bullets[i].Color)
	}
}

func (w *World) drawHUD(gameState int) {
	switch gameState {
	case WaitingForInput:
		drawCentered("Press Space Button to Start!", screenH/2, 24, colorText)
		return

	case GameOver:
		winner := "TEAM A WINS"
		if w.alive(0) == 0 {
			winner = "TEAM B WINS"
		}
		drawCentered(winner, screenH/2-20, 32, colorText)
		drawCentered("Press Space to restart", screenH/2+24, 20, colorText)
	}

	rl.DrawRectangle(12, 12, 260, 100, rl.Color{R: 0, G: 0, B: 0, A: 150})
	for team := range teamCount {
		y := int32(22 + team*26)
		rl.DrawRectangle(22, y, 16, 16, w.TeamColors[team])
		rl.DrawText(fmt.Sprintf("TEAM %c   alive %d", 'A'+team, w.alive(team)), 46, y, 18, colorText)
	}

	status := "DEAD"
	if p := w.player(); p != nil {
		status = fmt.Sprintf("%d HP", int(p.Health))
	}
	rl.DrawText("PLAYER   "+status, 22, 78, 18, colorText)
	rl.DrawText("LMB fire   WASD pan   wheel zoom", 12, screenH-26, 16, colorText)
}

func pickTeamColors(seed *uint64) [teamCount]rl.Color {
	i := randIndex(seed, len(rainbow))
	j := randIndex(seed, len(rainbow)-1)
	if j >= i {
		j++ // skip i, so the two teams never draw the same colour
	}
	return [teamCount]rl.Color{rainbow[i], rainbow[j]}
}

func randIndex(seed *uint64, n int) int {
	return min(int(pseudoRand(seed)*float32(n)), n-1)
}

func inField(p rl.Vector3) bool {
	const half = groundSize / 2
	return p.X >= -half && p.X <= half && p.Z >= -half && p.Z <= half
}

// moveCamera pans the top-down camera over the XZ plane and re-sticks it to the
// ground: the focus point always sits on the surface, the eye exactly above it.
func moveCamera(cam *rl.Camera3D, dt float32) {
	height := rl.Clamp(
		cam.Position.Y-cam.Target.Y-rl.GetMouseWheelMove()*camZoomStep,
		camHeightMin, camHeightMax,
	)

	focus := cam.Target
	var dir rl.Vector3
	for _, b := range camBindings {
		if rl.IsKeyDown(b.key) {
			dir = rl.Vector3Add(dir, b.axis)
		}
	}
	if dir != (rl.Vector3{}) {
		speed := camSpeed * height / camHeight
		focus = rl.Vector3Add(focus, rl.Vector3Scale(rl.Vector3Normalize(dir), speed*dt))
	}

	focus.Y = groundHeight(focus.X, focus.Z)
	cam.Target = focus
	cam.Position = rl.Vector3{X: focus.X, Y: focus.Y + height, Z: focus.Z}
}

// groundHeight is the sandbox's surface sampler - flat for now, swap for a
// heightmap lookup when terrain lands here.
func groundHeight(x, z float32) float32 { return 0 }

// mouseGroundPoint intersects the cursor ray with the ground plane.
func mouseGroundPoint(cam rl.Camera3D) (rl.Vector3, bool) {
	ray := rl.GetScreenToWorldRay(rl.GetMousePosition(), cam)
	if ray.Direction.Y > -1e-4 {
		return rl.Vector3{}, false
	}

	t := (groundHeight(0, 0) - ray.Position.Y) / ray.Direction.Y
	hit := rl.Vector3Add(ray.Position, rl.Vector3Scale(ray.Direction, t))
	hit.Y = groundHeight(hit.X, hit.Z)
	return hit, true
}

func drawGround() {
	rl.DrawPlane(rl.Vector3{X: 0, Y: -0.01, Z: 0}, rl.Vector2{X: groundSize, Y: groundSize}, colorGround)
	rl.DrawGrid(int32(groundSize/5), 5) // sits at y=0, just above the plane
}

func drawCharacter(c *Character, teamColor rl.Color) {
	body := rl.Vector3{X: c.Position.X, Y: c.Position.Y + charHeight/2, Z: c.Position.Z}
	rl.DrawCube(body, charRadius*2, charHeight, charRadius*2, teamColor)
	rl.DrawCubeWires(body, charRadius*2, charHeight, charRadius*2, colorOutline)

	if c.IsPlayer {
		ring := rl.Vector3{X: c.Position.X, Y: c.Position.Y + 0.03, Z: c.Position.Z}
		rl.DrawCircle3D(ring, charRadius*1.9, rl.Vector3{X: 1}, 90, colorText)
	}
	drawHealthBar(c)
}

func drawHealthBar(c *Character) {
	const barW, barD = 1.3, 0.14

	frac := rl.Clamp(c.Health/charMaxHP, 0, 1)
	base := rl.Vector3{X: c.Position.X, Y: c.Position.Y + charHeight + 0.35, Z: c.Position.Z}
	rl.DrawCube(base, barW, 0.02, barD, rl.Color{R: 25, G: 25, B: 25, A: 220})

	fill := base
	fill.X -= barW * (1 - frac) / 2
	rl.DrawCube(fill, barW*frac, 0.04, barD, rl.Color{
		R: uint8(255 * (1 - frac)),
		G: uint8(230 * frac),
		B: 40,
		A: 255,
	})
}

func drawBullet(pos rl.Vector3, size float32, color rl.Color) {
	rl.DrawSphere(pos, size, color)
}

func drawCrosshair(pos rl.Vector3) {
	const (
		radius = 0.5
		arm    = 1.2
	)
	pos.Y += 0.02 // lift off the surface, else the plane z-fights the lines

	rl.DrawCircle3D(pos, radius, rl.Vector3{X: 1}, 90, colorCrosshair)
	rl.DrawLine3D(rl.Vector3{X: pos.X - arm, Y: pos.Y, Z: pos.Z}, rl.Vector3{X: pos.X - radius, Y: pos.Y, Z: pos.Z}, colorCrosshair)
	rl.DrawLine3D(rl.Vector3{X: pos.X + radius, Y: pos.Y, Z: pos.Z}, rl.Vector3{X: pos.X + arm, Y: pos.Y, Z: pos.Z}, colorCrosshair)
	rl.DrawLine3D(rl.Vector3{X: pos.X, Y: pos.Y, Z: pos.Z - arm}, rl.Vector3{X: pos.X, Y: pos.Y, Z: pos.Z - radius}, colorCrosshair)
	rl.DrawLine3D(rl.Vector3{X: pos.X, Y: pos.Y, Z: pos.Z + radius}, rl.Vector3{X: pos.X, Y: pos.Y, Z: pos.Z + arm}, colorCrosshair)
}

func drawCentered(text string, y, size int32, color rl.Color) {
	rl.DrawText(text, (screenW-rl.MeasureText(text, size))/2, y, size, color)
}

func pseudoRand(seed *uint64) float32 {
	*seed += 0x9e3779b97f4a7c15
	z := *seed
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	z ^= z >> 31
	return float32(uint32(z>>33)&0x7fffffff) / float32(0x7fffffff)
}
