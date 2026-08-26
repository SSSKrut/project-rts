package components

import "github.com/mlange-42/ark/ecs"

// BotVerb is the whole vocabulary of the operational AI (MODEL.md 8). Three
// verbs, and there will not be a fourth: a bot with a wider vocabulary needs a
// planner, and a planner is the subsystem this project pivoted away from.
type BotVerb uint8

const (
	BotIdle   BotVerb = iota
	BotTake           // walk onto a point that is not ours
	BotHold           // stay on one that is
	BotAnswer         // it flipped: go back and take it again
)

var botVerbLabel = [...]string{"Idle", "Take", "Hold", "Answer"}

func (v BotVerb) String() string {
	if int(v) < len(botVerbLabel) {
		return botVerbLabel[v]
	}
	return "?"
}

// BotAssignment lives on a bot-commanded squad: which point it is working and
// under which verb. Keeping it on the SQUAD rather than in a table on the bot
// means it survives save/load with the squad and cannot go stale against a
// roster that changed underneath it.
type BotAssignment struct {
	Point    ecs.Entity
	Verb     BotVerb
	IssuedAt float32
}

// Point valuation (MODEL.md 8). A relay is worth more than plain ground
// because losing it costs the owner orders, not just score — the bot values
// the same thing the player does, which is what keeps the fight over the same
// places.
const (
	BotPointBase     float32 = 1.0
	BotPointRelay    float32 = 0.5
	BotNetProximityM float32 = 400
	// How often one bot side re-decides. Slow on purpose: an operational tier
	// that thinks every tick produces twitching, not planning.
	BotCadenceSec float32 = 12.0
	// Re-issuing the same march every cadence would reset the macro path
	// forever; a squad already working its point is left alone.
	BotArrivedM float32 = 18.0
)
