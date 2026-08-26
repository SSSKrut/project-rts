package systems

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// MissionSystem owns the mission clock and BOTH victory conditions (block D
// P3). Splitting them across consumers is the reliable way to end up with two
// different answers to "why did I win", so they live in one place and are
// evaluated in one order.
//
// It also keeps the scoreboard: points held, losses, and time on the net. That
// last number is the join with block A — a mission scores how well the player
// kept comms, which is what turns the radio from a nuisance into a resource.
type MissionSystem struct {
	missionRes ecs.Resource[components.Mission]
	stateRes   ecs.Resource[components.MissionState]
	pointMap   *ecs.Map[components.ControlPoint]
	pointFiltr *ecs.Filter1[components.ControlPoint]
	commsFiltr *ecs.Filter1[components.CommsState]
	eventLog   ecs.Resource[components.EventLog]
	world      *ecs.World
}

func NewMissionSystem() *MissionSystem { return &MissionSystem{} }

func (sys *MissionSystem) InitUI(w *ecs.World) {
	sys.missionRes = ecs.NewResource[components.Mission](w)
	sys.stateRes = ecs.NewResource[components.MissionState](w)
	sys.pointMap = ecs.NewMap[components.ControlPoint](w)
	sys.pointFiltr = ecs.NewFilter1[components.ControlPoint](w)
	sys.commsFiltr = ecs.NewFilter1[components.CommsState](w)
	sys.eventLog = ecs.NewResource[components.EventLog](w)
	sys.world = w
}

func (MissionSystem) Name() string { return "mission" }

func (MissionSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

func (sys *MissionSystem) Update(ctx core.UpdateContext) {
	if ctx.Tier != core.LODTierActive {
		return
	}
	m := sys.missionRes.Get()
	st := sys.stateRes.Get()
	if m == nil || st == nil || !m.Loaded || st.Over() {
		return
	}
	dt := float32(ctx.Delta.Seconds())
	st.Elapsed += dt

	st.Held = [components.FactionCount]uint8{}
	q := sys.pointFiltr.Query()
	for q.Next() {
		cp := q.Get()
		// A contested point counts for nobody: you hold ground you are alone
		// on, and the scoreboard has to say the same thing the ground does.
		if cp.Contested || int(cp.Owner) >= len(st.Held) {
			continue
		}
		st.Held[cp.Owner]++
	}

	// Time under a working net, player side only — this is what the outcome
	// screen reports back.
	if sys.playerLinked() {
		st.LinkedSec += dt
	}

	for f := range st.HoldSince {
		if st.Held[f] >= m.HoldPoints && m.HoldPoints > 0 {
			if st.HoldSince[f] == 0 {
				st.HoldSince[f] = st.Elapsed
			}
			if m.ForSec > 0 && st.Elapsed-st.HoldSince[f] >= m.ForSec {
				sys.finish(m, st, outcomeFor(uint8(f)))
				return
			}
			continue
		}
		st.HoldSince[f] = 0
	}

	if m.TimeSec > 0 && st.Elapsed >= m.TimeSec {
		sys.finish(m, st, sys.timeoutOutcome(st))
	}
}

// playerLinked: the player is "on the net" while at least one of their
// commanders is actually reachable. A single squad in contact is enough —
// the number measures whether the net existed, not how many used it.
func (sys *MissionSystem) playerLinked() bool {
	q := sys.commsFiltr.Query()
	for q.Next() {
		if q.Get().Band == components.CommsGreen {
			q.Close()
			return true
		}
	}
	return false
}

// timeoutOutcome: whoever owns the most when the clock runs out. A tie is a
// draw and says so — inventing a tiebreak would hide the fact that neither side
// achieved anything.
func (sys *MissionSystem) timeoutOutcome(st *components.MissionState) components.MissionOutcome {
	best, bestN, tied := uint8(components.FactionNone), 0, false
	for f, n := range st.Held {
		switch {
		case int(n) > bestN:
			best, bestN, tied = uint8(f), int(n), false
		case int(n) == bestN && bestN > 0:
			tied = true
		}
	}
	if bestN == 0 || tied {
		return components.OutcomeDraw
	}
	return outcomeFor(best)
}

func outcomeFor(faction uint8) components.MissionOutcome {
	if faction == components.FactionPlayer {
		return components.OutcomePlayerWin
	}
	return components.OutcomePlayerLoss
}

func (sys *MissionSystem) finish(m *components.Mission, st *components.MissionState,
	outcome components.MissionOutcome) {
	st.Outcome = outcome
	st.EndedAt = st.Elapsed
	if log := sys.eventLog.Get(); log != nil {
		log.Push(components.EventEntry{
			Kind: components.EventOrderCompleted,
			At:   st.Elapsed,
			Text: "Mission " + outcome.String(),
		})
	}
}

// MissionLoss records a casualty on the scoreboard. Called by DamageService so
// the count is deaths, not a filter over survivors — a squad that was wiped and
// respawned would otherwise read as no losses at all.
func MissionLoss(w *ecs.World, faction uint8) {
	res := ecs.NewResource[components.MissionState](w)
	st := res.Get()
	if st == nil || int(faction) >= len(st.Losses) || st.Losses[faction] == ^uint16(0) {
		return
	}
	st.Losses[faction]++
}
