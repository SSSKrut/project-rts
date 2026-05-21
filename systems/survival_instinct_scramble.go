package systems

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// runScatterProtocol walks every squad, lazy-adds SquadState when missing,
// pushes the latest squad-average Threat.Total into the history ring, and
// runs the Idle / Engaged / Scrambling transition logic. squadInfo is
// repopulated from scratch each tick so Pass 2 reads a consistent view.
//
// Phase 17 M17.0.3: history tracks Total instead of raw Suppression, so a
// far-fire-only spike (ShotsFired channel) can also flip a squad into
// Scrambling once that producer ships.
func (sys *SurvivalInstinctSystem) runScatterProtocol(now float32) {
	clear(sys.squadInfo)
	sys.stateAdds = sys.stateAdds[:0]

	q := sys.squadFilter.Query()
	for q.Next() {
		squad := q.Entity()
		_, roster := q.Get()
		if roster.Count == 0 {
			continue
		}

		avgSupp, threatDir := sys.aggregateSquadThreat(roster)

		state := sys.squadStateMap.Get(squad)
		if state == nil {
			// Lazy add - the squad was spawned before this system existed.
			init := components.SquadState{Code: components.SquadStateIdle}
			sys.stateAdds = append(sys.stateAdds, siStateAdd{squad: squad, init: init})
			sys.squadInfo[squad] = siSquadInfo{code: components.SquadStateIdle, threatDir: threatDir}
			continue
		}

		// Push current avg into history ring.
		state.SuppHistory[state.HistHead] = avgSupp
		state.HistHead = (state.HistHead + 1) % components.SquadSuppressionWindow
		if state.HistCount < components.SquadSuppressionWindow {
			state.HistCount++
		}

		delta := sys.windowDelta(state)

		// State transitions.
		switch state.Code {
		case components.SquadStateIdle:
			if avgSupp > 0 {
				state.Code = components.SquadStateEngaged
			}
		case components.SquadStateEngaged:
			if delta >= scrambleDeltaTrigger {
				state.Code = components.SquadStateScrambling
				state.ScramblingSince = now
				state.LowDeltaSince = 0
				sys.pushSuppressionEvent(squad, now)
			} else if avgSupp <= 0 {
				state.Code = components.SquadStateIdle
			}
		case components.SquadStateScrambling:
			if now-state.ScramblingSince >= scrambleSafetyDuration {
				state.Code = components.SquadStateEngaged
				state.LowDeltaSince = 0
			} else if delta < scrambleRecoveryDelta {
				if state.LowDeltaSince == 0 {
					state.LowDeltaSince = now
				} else if now-state.LowDeltaSince >= scrambleRecoveryDuration {
					state.Code = components.SquadStateEngaged
					state.LowDeltaSince = 0
				}
			} else {
				state.LowDeltaSince = 0
			}
		}

		sys.squadInfo[squad] = siSquadInfo{code: state.Code, threatDir: threatDir}
	}

	for _, add := range sys.stateAdds {
		if !sys.world.Alive(add.squad) {
			continue
		}
		if !sys.squadStateMap.Has(add.squad) {
			cpy := add.init
			sys.squadStateMap.Add(add.squad, &cpy)
		}
	}
}

// aggregateSquadThreat walks the roster, returns the mean Threat.Total
// across live members + the unit-length Total-weighted ThreatDir (XZ).
// Members without a Threat component count as zero contribution.
func (sys *SurvivalInstinctSystem) aggregateSquadThreat(roster *components.CommandRoster) (float32, rl.Vector3) {
	var sum, weight float32
	var tx, tz float32
	var live float32
	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) || !sys.world.Alive(mem) {
			continue
		}
		live++
		threat := sys.threatMap.Get(mem)
		if threat == nil {
			continue
		}
		sum += threat.Total
		tx += threat.ThreatDir.X * threat.Total
		tz += threat.ThreatDir.Z * threat.Total
		weight += threat.Total
	}
	if live == 0 {
		return 0, rl.Vector3{}
	}
	avg := sum / live
	dir := rl.Vector3{}
	if weight > 0 {
		l := float32(math.Sqrt(float64(tx*tx + tz*tz)))
		if l > 0 {
			dir = rl.Vector3{X: tx / l, Z: tz / l}
		}
	}
	return avg, dir
}

// windowDelta returns history[newest] - history[oldest]. Both indices are
// computed from HistHead with wrap-around. While the buffer is filling
// (HistCount < window) the "oldest" sample is index 0.
func (sys *SurvivalInstinctSystem) windowDelta(state *components.SquadState) float32 {
	if state.HistCount < 2 {
		return 0
	}
	newestIdx := (state.HistHead + components.SquadSuppressionWindow - 1) % components.SquadSuppressionWindow
	var oldestIdx uint8
	if state.HistCount < components.SquadSuppressionWindow {
		oldestIdx = 0
	} else {
		oldestIdx = state.HistHead
	}
	return state.SuppHistory[newestIdx] - state.SuppHistory[oldestIdx]
}
