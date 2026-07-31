package main

import (
	"fmt"
	"math"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// Evolution, not backprop: genomes are dealt into squads, the squads fight, and
// the ones that win get bred. Points are a TEAM property — every member of a
// winning squad banks the same win, whether it did the shooting or held the
// flank that made the shooting possible. Squads are re-dealt every round, so a
// genome is judged across several sets of teammates rather than on one lucky
// roster.
//
// One pool plays itself. Two pools are an arms race: each side only ever meets
// the other's current generation, and neither can settle, because the thing it
// is being measured against is evolving too.

const (
	pointsWin  = 1.0 // what a team victory is worth to each of its members
	archiveCap = 8   // past champions kept per pool
)

// MarginBonus tops up a win by how decisively it was taken — clock left for an
// attacker, squad left standing for a defender. Only winners are ever paid it,
// so victory stays the whole objective; without it, the safest genome is the
// one that runs out the clock, and a symmetric race settles into two squads
// declining to fight (measured: 100 draws in 100 matches).

type TrainCfg struct {
	Pop, Gens       int
	Coevolve        bool // two pools racing each other instead of one playing itself
	Roles           bool // pool 0 attacks, pool 1 defends, the clock favours the defence
	Rounds          int  // rounds against the opposing pool (or itself)
	ScriptRounds    int  // rounds against the hand-written AI
	HofRounds       int  // rounds against the opponent's past champions
	Elite           int
	Tournament      int
	MutRate         float32
	MutSigma        float32
	Shape           float32 // damage traded, as a tie-breaker between equal win counts
	MarginBonus     float32 // how decisively a side won, in the currency its role cares about
	MatchSec        float32
	Step            float32
	BenchmarkRounds int
	Out             string
	Workers         int
}

func defaultTrainCfg() TrainCfg {
	return TrainCfg{
		Pop: 48, Gens: 60, Rounds: 3, ScriptRounds: 3, HofRounds: 1, Roles: true,
		Elite: 4, Tournament: 3,
		MutRate: 0.1, MutSigma: 0.25, Shape: 0.05, MarginBonus: 0.5,
		MatchSec: 40, Step: 1.0 / 30, BenchmarkRounds: 16,
		Out: "bin/brain.json", Workers: runtime.NumCPU(),
	}
}

func (c TrainCfg) maxTicks() int { return int(c.MatchSec / c.Step) }

// pool is one evolving side: its genomes, this generation's brains, the points
// they have banked, and the champions it has already produced.
type pool struct {
	name    string
	genes   [][]float32
	brains  []*Brain
	fit     []float32
	archive []*Brain
	bestWin float32
}

func newPool(name string, n int, rng *uint64) *pool {
	p := &pool{name: name, genes: make([][]float32, n), bestWin: -1}
	for i := range p.genes {
		p.genes[i] = randomGenome(rng)
	}
	return p
}

func (p *pool) refresh() {
	p.brains = make([]*Brain, len(p.genes))
	for i, g := range p.genes {
		p.brains[i] = NewBrain(g)
	}
	p.fit = make([]float32, len(p.genes))
}

func (p *pool) remember(b *Brain) {
	p.archive = append(p.archive, b)
	if len(p.archive) > archiveCap {
		p.archive = p.archive[len(p.archive)-archiveCap:]
	}
}

type squad [teamSize]*Brain

type matchSetup struct {
	seed     uint64
	brains   [teamCount]squad // a nil slot fights on the hand-written script
	attacker int              // side that must break through; -1 = symmetric match
}

type matchResult struct {
	winner int // -1 on a draw
	ticks  int
	alive  [teamCount]int
	smokes [teamCount]int
	stat   [teamCount][teamSize]Stat
}

// seat says who is sitting in a slot. pool < 0 means nobody who can be paid —
// the script, or an archived champion that is only there to be a wall.
type seat struct {
	pool, gene int
}

type roster [teamCount][teamSize]seat

// round is one batch of matches plus who is in them. cross marks the batch
// where two different pools meet — the only one whose result is a head-to-head
// score rather than a measurement against a fixed wall.
type round struct {
	setups  []matchSetup
	rosters []roster
	cross   bool
}

// genStats is the per-generation read-out. A high draw count means the match
// cap is binding — squads are avoiding each other rather than fighting.
type genStats struct {
	matches, wins, draws, ticks int
	crossWins                   [2]int
	crossDraws                  int
}

func playMatch(s matchSetup, cfg TrainCfg) matchResult {
	w := newWorld(s.seed)
	w.Attacker, w.Limit = s.attacker, cfg.MatchSec
	w.reset(false)
	for t := range teamCount {
		for i := range teamSize {
			w.Characters[t*teamSize+i].Brain = s.brains[t][i]
		}
	}

	// The clock is float seconds and the budget is whole ticks, so the budget
	// gets headroom — without it the loop can end just short of the limit and
	// the match is scored a draw when the rule says the defence held.
	res := matchResult{winner: -1}
	for res.ticks = 0; res.ticks < cfg.maxTicks()+4; res.ticks++ {
		if winner, done := w.outcome(); done {
			res.winner = winner
			break
		}
		w.update(cfg.Step)
	}

	for t := range teamCount {
		res.alive[t] = w.alive(t)
		res.smokes[t] = w.SmokesThrown[t]
		for i := range teamSize {
			res.stat[t][i] = w.Stats[t*teamSize+i]
		}
	}
	return res
}

func runMatches(setups []matchSetup, cfg TrainCfg) []matchResult {
	out := make([]matchResult, len(setups))
	jobs := make(chan int)

	var wg sync.WaitGroup
	for range max(cfg.Workers, 1) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for k := range jobs {
				out[k] = playMatch(setups[k], cfg)
			}
		}()
	}
	for k := range setups {
		jobs <- k
	}
	close(jobs)
	wg.Wait()
	return out
}

func RunTraining(cfg TrainCfg, seed uint64) error {
	cfg.Pop -= cfg.Pop % teamSize
	if cfg.Pop < 2*teamSize {
		return fmt.Errorf("population must hold at least two squads of %d", teamSize)
	}

	rng := seed
	pools := []*pool{newPool("pop", cfg.Pop, &rng)}
	if cfg.Coevolve {
		a, b := "A", "B"
		if cfg.Roles {
			a, b = "attack", "defence"
		}
		pools = []*pool{newPool(a, cfg.Pop, &rng), newPool(b, cfg.Pop, &rng)}
	}

	fmt.Printf("genome %d genes (obs %d -> hidden %d -> out %d), %d pool(s) of %d, %d workers\n",
		GenomeLen(), obsN, hiddenN, outN, len(pools), cfg.Pop, cfg.Workers)

	for gen := 1; gen <= cfg.Gens; gen++ {
		started := time.Now()
		for _, p := range pools {
			p.refresh()
		}

		st := evaluate(pools, cfg, &rng)
		if err := closeGeneration(pools, cfg, gen, seed, st, time.Since(started)); err != nil {
			return err
		}
		if gen < cfg.Gens {
			for _, p := range pools {
				p.genes = nextGeneration(p.genes, p.fit, cfg, &rng)
			}
		}
	}

	for _, p := range pools {
		fmt.Printf("%s champion (%.0f%% vs script) -> %s\n", p.name, p.bestWin*100, poolOut(cfg.Out, p, len(pools)))
	}
	return nil
}

// closeGeneration scores each pool's champion against the fixed opponent, files
// it, and reports. The script is not selection pressure here — it is the only
// absolute yardstick in a race where everything else is moving.
func closeGeneration(pools []*pool, cfg TrainCfg, gen int, seed uint64, st genStats, took time.Duration) error {
	fmt.Printf("gen %3d", gen)

	roles := cfg.Roles && len(pools) > 1
	for idx, p := range pools {
		top := argmax(p.fit)
		role := roleNone
		if roles {
			role = idx // pool 0 attacks, pool 1 defends
		}
		win := benchmark(p.brains[top], cfg, seed+uint64(gen)*104729, role)
		p.remember(p.brains[top])

		if len(pools) == 1 {
			fmt.Printf("  best %5.2f  mean %5.2f | wins %3d draws %3d of %3d, %4.0f ticks | vs script %4.0f%%",
				p.fit[top], mean(p.fit), st.wins, st.draws, st.matches,
				float32(st.ticks)/float32(max(st.matches, 1)), win*100)
		} else {
			fmt.Printf(" | %s best %5.2f mean %5.2f script %3.0f%%", p.name, p.fit[top], mean(p.fit), win*100)
		}

		// Filed on the fixed opponent, not on fitness: fitness is measured
		// against a moving population and a lucky roster can inflate it.
		if win > p.bestWin {
			p.bestWin = win
			if err := saveGenome(poolOut(cfg.Out, p, len(pools)), p.genes[top], p.fit[top], win); err != nil {
				return fmt.Errorf("save genome: %w", err)
			}
		}
	}

	if len(pools) > 1 {
		total := st.crossWins[0] + st.crossWins[1] + st.crossDraws
		if roles {
			fmt.Printf(" | broke through %2d of %2d", st.crossWins[0], total)
		} else {
			fmt.Printf(" | head to head %d:%d, %d draws", st.crossWins[0], st.crossWins[1], st.crossDraws)
		}
	}
	fmt.Printf("  %4.1fs\n", took.Seconds())
	return nil
}

// evaluate plays every round of a generation. Cross rounds are where the pools
// meet; script and archive rounds are anchors that stop a race from drifting
// somewhere clever and useless.
func evaluate(pools []*pool, cfg TrainCfg, rng *uint64) genStats {
	var st genStats
	run := func(rd round) {
		for k, res := range runMatches(rd.setups, cfg) {
			payOut(pools, rd.rosters[k], res, cfg, rd.setups[k].attacker)
			st.tally(rd.rosters[k], res, rd.cross)
		}
	}

	opponent := func(p int) int { return (p + 1) % len(pools) }
	roles := cfg.Roles && len(pools) > 1

	for r := range cfg.Rounds {
		run(crossRound(pools, 0, opponent(0), r, roles, rng))
	}
	for r := range cfg.ScriptRounds {
		for p := range pools {
			run(scriptRound(pools, p, r, roles, rng))
		}
	}
	for r := range cfg.HofRounds {
		for p := range pools {
			run(archiveRound(pools, p, opponent(p), r, roles, rng))
		}
	}
	return st
}

func (st *genStats) tally(r roster, res matchResult, cross bool) {
	st.matches++
	st.ticks += res.ticks

	if res.winner < 0 {
		st.draws++
		if cross {
			st.crossDraws++
		}
		return
	}
	winner := r[res.winner][0]
	if winner.pool < 0 {
		return // the script or an archived champion took it
	}
	st.wins++
	if cross {
		st.crossWins[winner.pool]++
	}
}

// payOut hands the match result to the lineages that earned it: the win goes to
// every seat on the winning team — the flank that never fired is paid the same
// as the one that landed the last round — and damage traded only breaks ties.
func payOut(pools []*pool, r roster, res matchResult, cfg TrainCfg, attacker int) {
	for t := range teamCount {
		// Each side is paid its margin in the currency its job is measured in:
		// an attacker banks the clock it saved, a defender the squad it kept.
		margin := max(1-float32(res.ticks)/float32(cfg.maxTicks()), 0)
		if attacker >= 0 && t != attacker {
			margin = float32(res.alive[t]) / teamSize
		}
		win := pointsWin + cfg.MarginBonus*margin

		for i := range teamSize {
			s := r[t][i]
			if s.pool < 0 {
				continue
			}
			fit := pools[s.pool].fit
			if res.winner == t {
				fit[s.gene] += win
			}
			fit[s.gene] += cfg.Shape * (res.stat[t][i].Dealt - res.stat[t][i].Taken) / charMaxHP
		}
	}
}

// crossRound pits two pools against each other — or, when a == b, one pool
// against itself.
// attackerSide answers which end of the arena has to break through, given that
// the pool with index p is sitting on `side`. Pool 0 is the attacker by
// definition; with roles off, nobody is.
func attackerSide(roles bool, p, side int) int {
	switch {
	case !roles:
		return -1
	case p == 0:
		return side
	default:
		return 1 - side
	}
}

func crossRound(pools []*pool, a, b, nth int, roles bool, rng *uint64) round {
	rd := round{cross: a != b}

	if a == b {
		teams := dealSquads(len(pools[a].genes), rng)
		for k := 0; k+1 < len(teams); k += 2 {
			s, r := newMatch(rng, k)
			seatSquad(&s, &r, 0, pools, a, teams[k])
			seatSquad(&s, &r, 1, pools, a, teams[k+1])
			rd.add(s, r)
		}
	} else {
		ta, tb := dealSquads(len(pools[a].genes), rng), dealSquads(len(pools[b].genes), rng)
		for k := range min(len(ta), len(tb)) {
			s, r := newMatch(rng, k)
			side := (nth + k) % teamCount // alternate ends of the arena
			s.attacker = attackerSide(roles, a, side)
			seatSquad(&s, &r, side, pools, a, ta[k])
			seatSquad(&s, &r, 1-side, pools, b, tb[k])
			rd.add(s, r)
		}
	}

	pseudoRand(rng) // move the stream on so rounds never share match seeds
	return rd
}

func scriptRound(pools []*pool, p, nth int, roles bool, rng *uint64) round {
	var rd round
	for k, team := range dealSquads(len(pools[p].genes), rng) {
		s, r := newMatch(rng, k)
		side := (nth + k) % teamCount
		s.attacker = attackerSide(roles, p, side)
		seatSquad(&s, &r, side, pools, p, team) // the other side stays nil = script
		rd.add(s, r)
	}

	pseudoRand(rng)
	return rd
}

// archiveRound makes a pool answer for beating its opponent's older selves. It
// is what keeps a race honest: without it two pools can circle each other,
// trading counters forever without either getting better in absolute terms.
func archiveRound(pools []*pool, p, opp, nth int, roles bool, rng *uint64) round {
	var rd round
	arc := pools[opp].archive
	if len(arc) == 0 {
		return rd
	}

	for k, team := range dealSquads(len(pools[p].genes), rng) {
		s, r := newMatch(rng, k)
		side := (nth + k) % teamCount
		s.attacker = attackerSide(roles, p, side)
		seatSquad(&s, &r, side, pools, p, team)
		seatFixed(&s, 1-side, arc[randIndex(rng, len(arc))])
		rd.add(s, r)
	}

	pseudoRand(rng)
	return rd
}

func (rd *round) add(s matchSetup, r roster) {
	rd.setups = append(rd.setups, s)
	rd.rosters = append(rd.rosters, r)
}

func newMatch(rng *uint64, k int) (matchSetup, roster) {
	s := matchSetup{seed: *rng + uint64(k)*7919}
	var r roster
	for t := range r {
		for i := range r[t] {
			r[t][i] = seat{pool: -1}
		}
	}
	return s, r
}

func seatSquad(s *matchSetup, r *roster, side int, pools []*pool, p int, team [teamSize]int) {
	for i, g := range team {
		s.brains[side][i] = pools[p].brains[g]
		r[side][i] = seat{pool: p, gene: g}
	}
}

func seatFixed(s *matchSetup, side int, b *Brain) {
	for i := range teamSize {
		s.brains[side][i] = b
	}
}

func dealSquads(n int, rng *uint64) [][teamSize]int {
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	for i := n - 1; i > 0; i-- {
		j := randIndex(rng, i+1)
		order[i], order[j] = order[j], order[i]
	}

	teams := make([][teamSize]int, 0, n/teamSize)
	for i := 0; i+teamSize <= n; i += teamSize {
		var t [teamSize]int
		copy(t[:], order[i:i+teamSize])
		teams = append(teams, t)
	}
	return teams
}

// poolOut keeps a single pool on the plain -out path and gives each side of a
// race its own file.
func poolOut(base string, p *pool, pools int) string {
	if pools == 1 {
		return base
	}
	ext := filepath.Ext(base)
	return strings.TrimSuffix(base, ext) + "-" + strings.ToLower(p.name) + ext
}

// EvalGenome scores a saved brain over a longer run than the per-generation
// read-out — sixteen matches is a coin toss, a hundred is a number. With no
// genome it plays the script against itself, which is the baseline every other
// number here has to be read against; with -brain-b it is brain against brain.
func EvalGenome(path, pathB string, cfg TrainCfg, seed uint64) error {
	brain, label, err := loadBrain(path, "script")
	if err != nil {
		return err
	}
	other, otherLabel, err := loadBrain(pathB, "script")
	if err != nil {
		return err
	}

	role := roleNone
	if cfg.Roles {
		role = roleAttack // -brain attacks, -brain-b (or the script) holds
		label += " (attack)"
		otherLabel += " (defence)"
	}

	cfg.BenchmarkRounds = max(cfg.BenchmarkRounds, 100)
	d := duel(brain, other, cfg, seed, role)
	n := cfg.BenchmarkRounds
	fmt.Printf("%s vs %s: %d wins, %d losses, %d draws of %d (%.0f%% wins) | damage %.0f dealt, %.0f taken | smoke %.1f vs %.1f | %.0fs mean\n",
		label, otherLabel, d.wins, n-d.wins-d.draws, d.draws, n,
		float32(d.wins)/float32(n)*100, d.dealt, d.taken, d.smoke, d.smokeB, d.ticks*cfg.Step)
	return nil
}

func loadBrain(path, fallback string) (*Brain, string, error) {
	if path == "" {
		return nil, fallback, nil
	}
	genes, err := loadGenome(path)
	if err != nil {
		return nil, "", err
	}
	return NewBrain(genes), filepath.Base(path), nil
}

// benchmark is a read-out, not selection pressure: how often one brain, fielded
// as a whole squad, beats the hand-written AI.
// role says what the brain under test is being asked to do. It is the pool's
// own index whenever roles are on: pool 0 attacks, pool 1 defends.
const (
	roleNone = -1 + iota
	roleAttack
	roleDefend
)

func benchmark(b *Brain, cfg TrainCfg, seed uint64, role int) float32 {
	return float32(duel(b, nil, cfg, seed, role).wins) / float32(cfg.BenchmarkRounds)
}

// duelResult is scored from the first brain's point of view. The damage columns
// are what tell a close fight apart from two squads politely ignoring each
// other until the clock runs out.
type duelResult struct {
	wins, draws   int
	dealt, taken  float32 // per match
	ticks         float32 // per match
	smoke, smokeB float32 // clouds popped per match, by each side
}

// duel fields two brains as whole squads and alternates ends of the arena, so
// neither is judged on a side. A nil brain fights on the script.
func duel(b, other *Brain, cfg TrainCfg, seed uint64, role int) duelResult {
	setups := make([]matchSetup, cfg.BenchmarkRounds)
	sides := make([]int, cfg.BenchmarkRounds)
	for m := range setups {
		sides[m] = m % teamCount
		setups[m].seed = seed + uint64(m)*7919
		setups[m].attacker = attackerSide(role != roleNone, role, sides[m])
		seatFixed(&setups[m], sides[m], b)
		seatFixed(&setups[m], 1-sides[m], other)
	}

	var d duelResult
	for m, res := range runMatches(setups, cfg) {
		switch {
		case res.winner < 0:
			d.draws++
		case res.winner == sides[m]:
			d.wins++
		}
		d.ticks += float32(res.ticks)
		d.smoke += float32(res.smokes[sides[m]])
		d.smokeB += float32(res.smokes[1-sides[m]])
		for i := range teamSize {
			d.dealt += res.stat[sides[m]][i].Dealt
			d.taken += res.stat[sides[m]][i].Taken
		}
	}

	n := float32(len(setups))
	d.dealt, d.taken, d.ticks = d.dealt/n, d.taken/n, d.ticks/n
	d.smoke, d.smokeB = d.smoke/n, d.smokeB/n
	return d
}

func nextGeneration(pop [][]float32, fit []float32, cfg TrainCfg, rng *uint64) [][]float32 {
	ranked := make([]int, len(pop))
	for i := range ranked {
		ranked[i] = i
	}
	sort.SliceStable(ranked, func(a, b int) bool { return fit[ranked[a]] > fit[ranked[b]] })

	next := make([][]float32, 0, len(pop))
	for _, i := range ranked[:min(cfg.Elite, len(ranked))] {
		next = append(next, append([]float32(nil), pop[i]...))
	}
	for len(next) < len(pop) {
		a := pop[tournament(fit, cfg.Tournament, rng)]
		b := pop[tournament(fit, cfg.Tournament, rng)]
		child := crossover(a, b, rng)
		mutate(child, cfg, rng)
		next = append(next, child)
	}
	return next
}

func tournament(fit []float32, k int, rng *uint64) int {
	best := randIndex(rng, len(fit))
	for range max(k, 1) - 1 {
		if c := randIndex(rng, len(fit)); fit[c] > fit[best] {
			best = c
		}
	}
	return best
}

func crossover(a, b []float32, rng *uint64) []float32 {
	child := make([]float32, len(a))
	for i := range child {
		child[i] = a[i]
		if pseudoRand(rng) < 0.5 {
			child[i] = b[i]
		}
	}
	return child
}

func mutate(g []float32, cfg TrainCfg, rng *uint64) {
	for i := range g {
		if pseudoRand(rng) < cfg.MutRate {
			g[i] += gauss(rng) * cfg.MutSigma
		}
	}
}

func randomGenome(rng *uint64) []float32 {
	g := make([]float32, GenomeLen())
	a := obsN * hiddenN
	b := a + hiddenN
	c := b + hiddenN*outN

	fill(g[:a], float32(1/math.Sqrt(obsN)), rng) // biases stay at zero
	fill(g[b:c], float32(1/math.Sqrt(hiddenN)), rng)
	return g
}

func fill(g []float32, scale float32, rng *uint64) {
	for i := range g {
		g[i] = gauss(rng) * scale
	}
}

// gauss is Box-Muller over the same hash stream everything else here uses.
func gauss(rng *uint64) float32 {
	u1 := max(pseudoRand(rng), 1e-7)
	u2 := pseudoRand(rng)
	return float32(math.Sqrt(-2*math.Log(float64(u1))) * math.Cos(2*math.Pi*float64(u2)))
}

func argmax(v []float32) int {
	best := 0
	for i, x := range v {
		if x > v[best] {
			best = i
		}
	}
	return best
}

func mean(v []float32) float32 {
	var sum float32
	for _, x := range v {
		sum += x
	}
	return sum / float32(len(v))
}
