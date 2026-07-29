package main

import (
	"fmt"
	"math"
	"runtime"
	"sort"
	"sync"
	"time"
)

// Evolution, not backprop: a population of genomes is dealt into squads, the
// squads fight, and the ones that win get bred. Points are a TEAM property —
// every member of a winning squad banks the same win, whether it did the
// shooting or held the flank that made the shooting possible. Squads are
// re-dealt every round so a genome is judged across several sets of teammates
// rather than on one lucky roster.

const (
	pointsWin = 1.0 // what a team victory is worth to each of its members
)

// speedBonus tops up a win by how much of the clock was left. Only winners are
// ever paid it, so victory stays the whole objective — but without it the
// safest genome is the one that hides until the match is called a draw, and a
// draw is worth exactly as much as being beaten.

type TrainCfg struct {
	Pop, Gens       int
	Rounds          int // self-play rounds per generation
	ScriptRounds    int // rounds against the hand-written AI
	Elite           int
	Tournament      int
	MutRate         float32
	MutSigma        float32
	Shape           float32 // damage traded, as a tie-breaker between equal win counts
	SpeedBonus      float32 // extra points for a fast win, scaled by the clock left
	MatchSec        float32
	Step            float32
	BenchmarkRounds int
	Out             string
	Workers         int
}

func defaultTrainCfg() TrainCfg {
	return TrainCfg{
		Pop: 48, Gens: 60, Rounds: 3, ScriptRounds: 3,
		Elite: 4, Tournament: 3,
		MutRate: 0.1, MutSigma: 0.25, Shape: 0.05, SpeedBonus: 0.5,
		MatchSec: 40, Step: 1.0 / 30, BenchmarkRounds: 16,
		Out: "bin/brain.json", Workers: runtime.NumCPU(),
	}
}

func (c TrainCfg) maxTicks() int { return int(c.MatchSec / c.Step) }

type squad [teamSize]*Brain

type matchSetup struct {
	seed   uint64
	brains [teamCount]squad // a nil slot fights on the hand-written script
}

type matchResult struct {
	winner int // -1 on a draw
	ticks  int
	stat   [teamCount][teamSize]Stat
}

// genStats is the per-generation read-out. A high draw count means the match
// cap is binding — squads are avoiding each other rather than fighting.
type genStats struct {
	matches, wins, draws, ticks int
}

// roster remembers which genome sat in which seat, so a match result can be
// paid out to the right lineages. -1 means that seat was scripted.
type roster [teamCount][teamSize]int

func playMatch(s matchSetup, step float32, maxTicks int) matchResult {
	w := newWorld(s.seed)
	w.reset(false)
	for t := range teamCount {
		for i := range teamSize {
			w.Characters[t*teamSize+i].Brain = s.brains[t][i]
		}
	}

	res := matchResult{winner: -1}
	for res.ticks = 0; res.ticks < maxTicks && !w.over(); res.ticks++ {
		w.update(step)
	}

	switch {
	case w.alive(0) > 0 && w.alive(1) == 0:
		res.winner = 0
	case w.alive(1) > 0 && w.alive(0) == 0:
		res.winner = 1
	}
	for t := range teamCount {
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
				out[k] = playMatch(setups[k], cfg.Step, cfg.maxTicks())
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
	pop := make([][]float32, cfg.Pop)
	for i := range pop {
		pop[i] = randomGenome(&rng)
	}

	fmt.Printf("genome %d genes (obs %d -> hidden %d -> out %d), pop %d, %d workers\n",
		GenomeLen(), obsN, hiddenN, outN, cfg.Pop, cfg.Workers)

	bestWin := float32(-1)
	for gen := 1; gen <= cfg.Gens; gen++ {
		started := time.Now()
		fit, st := evaluate(pop, cfg, &rng)

		top := argmax(fit)
		win := benchmark(NewBrain(pop[top]), cfg, seed+uint64(gen)*104729)
		fmt.Printf("gen %3d  best %6.2f  mean %6.2f | wins %3d draws %3d of %3d, %4.0f ticks | vs script %4.0f%%  %4.1fs\n",
			gen, fit[top], mean(fit), st.wins, st.draws, st.matches,
			float32(st.ticks)/float32(max(st.matches, 1)), win*100, time.Since(started).Seconds())

		// Saving on the fixed opponent, not on fitness: fitness is measured
		// against a moving population and a lucky roster can inflate it.
		if win > bestWin {
			bestWin = win
			if err := saveGenome(cfg.Out, pop[top], fit[top], win); err != nil {
				return fmt.Errorf("save genome: %w", err)
			}
		}
		if gen < cfg.Gens {
			pop = nextGeneration(pop, fit, cfg, &rng)
		}
	}

	fmt.Printf("best genome (%.0f%% vs script) written to %s — watch it with -brain=%s\n",
		bestWin*100, cfg.Out, cfg.Out)
	return nil
}

// evaluate deals the population into squads and plays out the rounds. Self-play
// rounds sharpen the population against itself; script rounds keep it honest
// against a fixed opponent, so a generation cannot "win" by all agreeing to be
// worse together.
func evaluate(pop [][]float32, cfg TrainCfg, rng *uint64) ([]float32, genStats) {
	brains := make([]*Brain, len(pop))
	for i := range pop {
		brains[i] = NewBrain(pop[i])
	}

	fit := make([]float32, len(pop))
	var st genStats
	for r := range cfg.Rounds + cfg.ScriptRounds {
		teams := dealSquads(len(pop), rng)
		setups, rosters := buildRound(teams, brains, r >= cfg.Rounds, r, rng)

		for k, res := range runMatches(setups, cfg) {
			payOut(fit, rosters[k], res, cfg)

			st.matches++
			st.ticks += res.ticks
			switch {
			case res.winner < 0:
				st.draws++
			case rosters[k][res.winner][0] >= 0:
				st.wins++ // a population squad took it, not the script
			}
		}
	}
	return fit, st
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

func buildRound(teams [][teamSize]int, brains []*Brain, vsScript bool, round int, rng *uint64) ([]matchSetup, []roster) {
	var setups []matchSetup
	var rosters []roster

	seat := func(s *matchSetup, r *roster, side int, team [teamSize]int) {
		for i, g := range team {
			s.brains[side][i] = brains[g]
			r[side][i] = g
		}
	}

	if vsScript {
		for k, team := range teams {
			var s matchSetup
			var r roster
			blank(&r)
			s.seed = *rng + uint64(k)*7919
			seat(&s, &r, (round+k)%teamCount, team) // alternate sides, no side bias
			setups, rosters = append(setups, s), append(rosters, r)
		}
	} else {
		for k := 0; k+1 < len(teams); k += 2 {
			var s matchSetup
			var r roster
			blank(&r)
			s.seed = *rng + uint64(k)*7919
			seat(&s, &r, 0, teams[k])
			seat(&s, &r, 1, teams[k+1])
			setups, rosters = append(setups, s), append(rosters, r)
		}
	}
	pseudoRand(rng) // advance the stream so rounds don't share match seeds
	return setups, rosters
}

func blank(r *roster) {
	for t := range r {
		for i := range r[t] {
			r[t][i] = -1
		}
	}
}

// payOut hands the match result to the lineages that earned it: the win goes to
// every seat on the winning team — the flank that never fired is paid the same
// as the one that landed the last round — and damage traded only breaks ties.
func payOut(fit []float32, r roster, res matchResult, cfg TrainCfg) {
	win := pointsWin + cfg.SpeedBonus*(1-float32(res.ticks)/float32(cfg.maxTicks()))

	for t := range teamCount {
		for i := range teamSize {
			g := r[t][i]
			if g < 0 {
				continue
			}
			if res.winner == t {
				fit[g] += win
			}
			fit[g] += cfg.Shape * (res.stat[t][i].Dealt - res.stat[t][i].Taken) / charMaxHP
		}
	}
}

// EvalGenome scores a saved brain against the script over a longer run than the
// per-generation read-out — sixteen matches is a coin toss, a hundred is a
// number. With no genome it plays the script against itself, which is the
// baseline every other number here has to be read against.
func EvalGenome(path string, cfg TrainCfg, seed uint64) error {
	var brain *Brain
	label := "script vs script (baseline)"
	if path != "" {
		genes, err := loadGenome(path)
		if err != nil {
			return err
		}
		brain, label = NewBrain(genes), path
	}

	cfg.BenchmarkRounds = max(cfg.BenchmarkRounds, 100)
	wins, draws := benchmarkFull(brain, cfg, seed)
	n := cfg.BenchmarkRounds
	fmt.Printf("%s: %d wins, %d losses, %d draws of %d (%.0f%% wins)\n",
		label, wins, n-wins-draws, draws, n, float32(wins)/float32(n)*100)
	return nil
}

// benchmark is a read-out, not selection pressure: how often one brain, fielded
// as a whole squad, beats the hand-written AI. Sides alternate so a squad is
// judged on both ends of the arena.
func benchmark(b *Brain, cfg TrainCfg, seed uint64) float32 {
	wins, _ := benchmarkFull(b, cfg, seed)
	return float32(wins) / float32(cfg.BenchmarkRounds)
}

func benchmarkFull(b *Brain, cfg TrainCfg, seed uint64) (wins, draws int) {
	setups := make([]matchSetup, cfg.BenchmarkRounds)
	sides := make([]int, cfg.BenchmarkRounds)
	for m := range setups {
		sides[m] = m % teamCount
		setups[m].seed = seed + uint64(m)*7919
		for i := range teamSize {
			setups[m].brains[sides[m]][i] = b
		}
	}

	for m, res := range runMatches(setups, cfg) {
		switch {
		case res.winner < 0:
			draws++
		case res.winner == sides[m]:
			wins++
		}
	}
	return wins, draws
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
