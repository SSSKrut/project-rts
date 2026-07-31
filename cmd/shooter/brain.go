package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"

	rl "github.com/gen2brain/raylib-go/raylib"
)

// A bot's nervous system: an egocentric picture of the fight in, a handful of
// drives out. Weights are a flat []float32 — that slice IS the genome the
// trainer breeds, so nothing here may hold per-tick state (scratch buffers live
// on the World, and one Brain is safely shared by a whole squad).

const (
	gridN    = 7   // cells per side of the matrix around the bot
	gridCell = 3.0 // metres per cell — the window reaches ±10.5 m
	gridChan = 7   // cover / allies / enemies / bullets / bullet vx / vz / smoke

	neighbours    = 4 // nearest allies and nearest enemies reported individually
	neighbourFeat = 5 // dx, dz, closeness, health, line of sight

	hiddenN = 16
	outN    = 4 + neighbours // moveX, moveZ, fire, smoke, one pref per enemy slot

	obsGrid = gridN * gridN * gridChan
	obsSelf = 8
	obsN    = obsGrid + obsSelf + 2*neighbours*neighbourFeat

	brainEvery = 3    // ticks between decisions — ~10 Hz at the training step
	senseRange = 45.0 // how far the neighbour scan looks
)

// Grid channels.
const (
	chCover    = iota // 0.5 knee-high, 1.0 head-high
	chAlly            // density
	chEnemy           // density
	chBullet          // density
	chBulletVX        // where those rounds are heading
	chBulletVZ
	chSmoke // cover you can walk through, that hides you while you do
)

type Brain struct {
	w1, b1, w2, b2 []float32
}

func GenomeLen() int { return obsN*hiddenN + hiddenN + hiddenN*outN + outN }

func NewBrain(genes []float32) *Brain {
	if len(genes) != GenomeLen() {
		panic(fmt.Sprintf("genome is %d genes, this brain takes %d", len(genes), GenomeLen()))
	}
	a := obsN * hiddenN
	b := a + hiddenN
	c := b + hiddenN*outN
	return &Brain{w1: genes[:a], b1: genes[a:b], w2: genes[b:c], b2: genes[c:]}
}

// Forward runs the net. The observation is mostly zeros — an empty grid cell
// costs nothing, which is what makes a matrix this wide affordable per tick.
func (n *Brain) Forward(in, hid, out []float32) {
	copy(hid, n.b1)
	for i, v := range in {
		if v == 0 {
			continue
		}
		row := n.w1[i*hiddenN : (i+1)*hiddenN]
		for h, wgt := range row {
			hid[h] += v * wgt
		}
	}
	for h, v := range hid {
		hid[h] = tanh32(v)
	}

	copy(out, n.b2)
	for h, v := range hid {
		row := n.w2[h*outN : (h+1)*outN]
		for o, wgt := range row {
			out[o] += v * wgt
		}
	}
	for o, v := range out {
		out[o] = tanh32(v)
	}
}

// observe paints the world around c into out. Team 1 sees everything negated,
// so one policy fits both sides of a point-symmetric arena and every match
// trains both ends of it.
func (w *World) observe(c *Character, out []float32) {
	clear(out)
	side := float32(1)
	if c.Team == 1 {
		side = -1
	}

	// One sight test per enemy, reused by every pass below: what a bot cannot
	// see, it is not told about — that is the whole point of a smoke screen.
	w.seen = w.seen[:0]
	for i := range w.Characters {
		o := &w.Characters[i]
		w.seen = append(w.seen, o.ID == c.ID || w.canSee(c, o))
	}

	w.observeCover(c, side, out)
	w.observeSmoke(c, side, out)
	w.observeBodies(c, side, out)
	w.observeBullets(c, side, out)

	self := out[obsGrid : obsGrid+obsSelf]
	self[0] = c.Health / charMaxHP
	self[1] = rl.Clamp(1-c.Cooldown/c.FireDelay, 0, 1)
	self[2] = rl.Clamp(side*c.Position.X/spawnLineX, -1, 1)
	self[3] = rl.Clamp(side*c.Position.Z/spawnLineX, -1, 1)
	self[4] = float32(w.alive(c.Team)) / teamSize
	self[5] = float32(w.alive(1-c.Team)) / teamSize
	self[6] = c.Suppression
	if c.SmokeCooldown <= 0 {
		self[7] = 1
	}

	w.observeNeighbours(c, side, out[obsGrid+obsSelf:])
}

func (w *World) observeSmoke(c *Character, side float32, out []float32) {
	for i := range w.Smokes {
		s := &w.Smokes[i]
		r := s.Radius()

		i0, i1 := cellSpan(side*(s.Position.X-c.Position.X-r), side*(s.Position.X-c.Position.X+r))
		j0, j1 := cellSpan(side*(s.Position.Z-c.Position.Z-r), side*(s.Position.Z-c.Position.Z+r))
		for j := j0; j <= j1; j++ {
			for i := i0; i <= i1; i++ {
				if k, ok := cellIdx(i, j, chSmoke); ok {
					out[k] = 1
				}
			}
		}
	}
}

// observeCover rasterises each box footprint into the cells it covers — far
// cheaper than testing every cell against every box.
func (w *World) observeCover(c *Character, side float32, out []float32) {
	for _, b := range w.Phys.Boxes {
		val := float32(1)
		if b.Size().Y <= coverLowH+1e-3 {
			val = 0.5
		}

		i0, i1 := cellSpan(side*(b.Min.X-c.Position.X), side*(b.Max.X-c.Position.X))
		j0, j1 := cellSpan(side*(b.Min.Z-c.Position.Z), side*(b.Max.Z-c.Position.Z))
		for j := j0; j <= j1; j++ {
			for i := i0; i <= i1; i++ {
				if k, ok := cellIdx(i, j, chCover); ok {
					out[k] = max(out[k], val)
				}
			}
		}
	}
}

func (w *World) observeBodies(c *Character, side float32, out []float32) {
	for i := range w.Characters {
		o := &w.Characters[i]
		if o.ID == c.ID || hidden(o, c, w.seen[i]) {
			continue
		}
		ch := chEnemy
		if o.Team == c.Team {
			ch = chAlly
		}
		if k, ok := cellAt(side, o.Position, c.Position, ch); ok {
			out[k] += 0.5
		}
	}
}

// observeBullets records both where the rounds are and where they are going —
// without the velocity channels a bot cannot tell incoming fire from outgoing.
func (w *World) observeBullets(c *Character, side float32, out []float32) {
	for i := range w.Bullets {
		b := &w.Bullets[i]
		if b.Owner == c.ID {
			continue
		}
		k, ok := cellAt(side, b.Position, c.Position, chBullet)
		if !ok {
			continue
		}
		out[k] += 0.5
		out[k+chBulletVX-chBullet] += side * b.Velocity.X / bulletSpeed
		out[k+chBulletVZ-chBullet] += side * b.Velocity.Z / bulletSpeed
	}
}

// observeNeighbours fills the detail slots: the nearest allies first, then the
// nearest enemies. The enemy order is the one the fire outputs index into, so
// it is kept on the World for the intent step.
func (w *World) observeNeighbours(c *Character, side float32, out []float32) {
	var ally, enemy [neighbours]int
	for i := range ally {
		ally[i], enemy[i] = -1, -1
	}

	for i := range w.Characters {
		o := &w.Characters[i]
		if o.ID == c.ID || hidden(o, c, w.seen[i]) {
			continue
		}
		slots := &enemy
		if o.Team == c.Team {
			slots = &ally
		}
		insertNearest(slots[:], w.Characters, i, c.Position)
	}
	w.slotEnemy = enemy

	writeSlots := func(slots []int, dst []float32) {
		for s, idx := range slots {
			if idx < 0 {
				continue
			}
			o := &w.Characters[idx]
			d := rl.Vector3Subtract(o.Position, c.Position)
			f := dst[s*neighbourFeat:]
			f[0] = rl.Clamp(side*d.X/senseRange, -1, 1)
			f[1] = rl.Clamp(side*d.Z/senseRange, -1, 1)
			f[2] = rl.Clamp(1-rl.Vector3Length(d)/senseRange, 0, 1)
			f[3] = o.Health / charMaxHP
			if w.seen[idx] {
				f[4] = 1
			}
		}
	}
	writeSlots(ally[:], out)
	writeSlots(enemy[:], out[neighbours*neighbourFeat:])
}

// hidden gates what a bot is told: an unseen enemy is simply not there, while a
// teammate is always on the net whether or not there is a clear line to them.
func hidden(o, self *Character, seen bool) bool {
	return o.Team != self.Team && !seen
}

// insertNearest keeps slots sorted by distance, dropping the far end.
func insertNearest(slots []int, chars []Character, idx int, from rl.Vector3) {
	d := rl.Vector3DistanceSqr(chars[idx].Position, from)
	for s := range slots {
		if slots[s] < 0 || d < rl.Vector3DistanceSqr(chars[slots[s]].Position, from) {
			copy(slots[s+1:], slots[s:])
			slots[s] = idx
			return
		}
	}
}

// brainIntent reads the net's drives. Movement is free-form; aim is locked to
// whichever of the nearby enemies the net prefers — a GA can learn where to
// stand and when to shoot long before it could learn to lead a 1 m target at
// 20 m, and the trigger still costs it, because a round into a wall is a round
// wasted.
func (w *World) brainIntent(c *Character) Intent {
	w.observe(c, w.obs)
	c.Brain.Forward(w.obs, w.hid, w.act)

	side := float32(1)
	if c.Team == 1 {
		side = -1
	}

	in := Intent{Move: rl.Vector3{X: side * w.act[0], Z: side * w.act[1]}}

	best := -1
	for s, idx := range w.slotEnemy {
		if idx < 0 {
			continue
		}
		if best < 0 || w.act[4+s] > w.act[4+best] {
			best = s
		}
	}
	// Smoke has to be throwable BEFORE contact — a screen is only worth
	// anything while the bot is still unseen, so with nobody in sight it goes
	// out along the line of advance rather than not at all.
	if w.act[3] > 0 {
		in.Smoke, in.SmokeAt = true, c.Position
		switch {
		case best >= 0:
			in.SmokeAt = w.Characters[w.slotEnemy[best]].Position
		case rl.Vector3Length(in.Move) > 1e-3:
			in.SmokeAt = rl.Vector3Add(c.Position, rl.Vector3Scale(flatNormalize(in.Move), smokeThrow))
		}
	}
	if best >= 0 && w.act[2] > 0 {
		in.Aim, in.Fire = w.Characters[w.slotEnemy[best]].Position, true
	}
	return in
}

func cellSpan(a, b float32) (int, int) {
	if a > b {
		a, b = b, a
	}
	return cellOf(a), cellOf(b)
}

func cellOf(v float32) int {
	return int(math.Floor(float64(v/gridCell))) + gridN/2
}

func cellIdx(i, j, ch int) (int, bool) {
	if i < 0 || i >= gridN || j < 0 || j >= gridN {
		return 0, false
	}
	return (j*gridN+i)*gridChan + ch, true
}

func cellAt(side float32, p, origin rl.Vector3, ch int) (int, bool) {
	return cellIdx(cellOf(side*(p.X-origin.X)), cellOf(side*(p.Z-origin.Z)), ch)
}

func tanh32(v float32) float32 { return float32(math.Tanh(float64(v))) }

// brainFile pins the net's shape next to its weights: a genome bred against a
// different observation layout is meaningless, so loading one must fail loudly.
type brainFile struct {
	Version int       `json:"version"`
	Obs     int       `json:"obs"`
	Hidden  int       `json:"hidden"`
	Out     int       `json:"out"`
	Fitness float32   `json:"fitness"`
	WinRate float32   `json:"win_rate_vs_script"`
	Genes   []float32 `json:"genes"`
}

func saveGenome(path string, genes []float32, fitness, winRate float32) error {
	blob, err := json.Marshal(brainFile{
		Version: 1, Obs: obsN, Hidden: hiddenN, Out: outN,
		Fitness: fitness, WinRate: winRate, Genes: genes,
	})
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, blob, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func loadGenome(path string) ([]float32, error) {
	blob, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f brainFile
	if err := json.Unmarshal(blob, &f); err != nil {
		return nil, err
	}
	if f.Obs != obsN || f.Hidden != hiddenN || f.Out != outN || len(f.Genes) != GenomeLen() {
		return nil, fmt.Errorf("genome shape %d-%d-%d does not fit this build's %d-%d-%d",
			f.Obs, f.Hidden, f.Out, obsN, hiddenN, outN)
	}
	return f.Genes, nil
}
