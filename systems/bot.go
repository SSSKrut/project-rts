package systems

import (
	"math"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// BotSystem is the operational AI: three verbs, one decision every
// BotCadenceSec, no tree search and no neural net (MODEL.md 8).
//
// The load-bearing property is not the decisions — it is that they are ISSUED
// THE SAME WAY the player's are, through SquadService.IssueOrder. That single
// call means the bot's orders are gated by the bot's own comms for free: kill
// its radiomen or take the point carrying its relay and its plans stop
// arriving. Symmetry is not a feature added here; it is what happens when the
// gate is built on the commander instead of on the player.
//
// It runs ONLY under a mission. Scenes are hand-authored situations, and a bot
// that re-tasked their AI squads would rewrite every one of them.
type BotSystem struct {
	missionRes  ecs.Resource[components.Mission]
	pointFilter *ecs.Filter2[components.ControlPoint, components.WorldPos]
	squadFilter *ecs.Filter2[components.Squad, components.CommandRoster]

	assignMap  *ecs.Map[components.BotAssignment]
	factionMap *ecs.Map[components.Faction]
	ctrlMap    *ecs.Map[components.Controller]
	posMap     *ecs.Map[components.WorldPos]
	rulesMap   *ecs.Map[components.EngagementRules]
	commsMap   *ecs.Map[components.CommsState]
	relayMap   *ecs.Map[components.Relay]

	squads *SquadService
	world  *ecs.World

	pts     []botPoint
	crews   []botCrew
	adds    []botAdd
	nextAt  float32
	started bool
}

type botPoint struct {
	ent    ecs.Entity
	pos    components.WorldPos
	x, z   float32
	owner  uint8
	value  float32
	takenB bool // already assigned to one of ours this pass
}

type botCrew struct {
	squad  ecs.Entity
	x, z   float32
	assign *components.BotAssignment
}

type botAdd struct {
	squad ecs.Entity
	a     components.BotAssignment
}

func NewBotSystem(sq *SquadService) *BotSystem { return &BotSystem{squads: sq} }

func (sys *BotSystem) InitUI(w *ecs.World) {
	sys.missionRes = ecs.NewResource[components.Mission](w)
	sys.pointFilter = ecs.NewFilter2[components.ControlPoint, components.WorldPos](w)
	sys.squadFilter = ecs.NewFilter2[components.Squad, components.CommandRoster](w)
	sys.assignMap = ecs.NewMap[components.BotAssignment](w)
	sys.factionMap = ecs.NewMap[components.Faction](w)
	sys.ctrlMap = ecs.NewMap[components.Controller](w)
	sys.posMap = ecs.NewMap[components.WorldPos](w)
	sys.rulesMap = ecs.NewMap[components.EngagementRules](w)
	sys.commsMap = ecs.NewMap[components.CommsState](w)
	sys.relayMap = ecs.NewMap[components.Relay](w)
	sys.world = w
}

func (BotSystem) Name() string { return "bot" }

func (BotSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

func (sys *BotSystem) Update(ctx core.UpdateContext) {
	if ctx.Tier != core.LODTierActive || sys.squads == nil {
		return
	}
	m := sys.missionRes.Get()
	if m == nil || !m.Loaded {
		return
	}
	now := float32(ctx.SimNow)
	if !sys.started {
		// The first decision waits one cadence. Deciding on tick zero would
		// beat CommsSystem to its first pass, and an uncomputed CommsState
		// reads Green by design (M0) — so a bot with no net at all would get
		// exactly one free order, which is all it takes to walk onto a point.
		// It is also just better behaviour: an operational tier that commands
		// in the opening tick is twitching, not planning.
		sys.started = true
		sys.nextAt = now + components.BotCadenceSec
		return
	}
	if now < sys.nextAt {
		return
	}
	sys.nextAt = now + components.BotCadenceSec

	sys.snapshotPoints()
	if len(sys.pts) == 0 {
		return
	}
	sys.decideFor(components.FactionEnemyRed, now)
}

// snapshotPoints values every point once per decision. A relay is worth more
// than plain ground, and ground near your own net is worth more than ground
// deep in theirs — the bot wants what the player wants.
func (sys *BotSystem) snapshotPoints() {
	sys.pts = sys.pts[:0]
	q := sys.pointFilter.Query()
	for q.Next() {
		cp, pos := q.Get()
		x, z := worldXZ(*pos)
		v := components.BotPointBase
		if cp.RelayRangeM > 0 {
			v += components.BotPointRelay
		}
		sys.pts = append(sys.pts, botPoint{
			ent: q.Entity(), pos: *pos, x: x, z: z, owner: cp.Owner, value: v,
		})
	}
}

func (sys *BotSystem) decideFor(side uint8, now float32) {
	sys.crews = sys.crews[:0]
	sys.adds = sys.adds[:0]

	q := sys.squadFilter.Query()
	for q.Next() {
		_, roster := q.Get()
		squad := q.Entity()
		if f := sys.factionMap.Get(squad); f == nil || f.ID != side {
			continue
		}
		if c := sys.ctrlMap.Get(squad); c == nil || c.Owner != components.ControllerAI {
			continue
		}
		pos, ok := SquadAnchorPos(sys.world, roster, sys.posMap)
		if !ok {
			continue
		}
		x, z := worldXZ(pos)
		sys.crews = append(sys.crews, botCrew{
			squad: squad, x: x, z: z, assign: sys.assignMap.Get(squad),
		})
	}
	if len(sys.crews) == 0 {
		return
	}

	// Own points first: a squad already standing on ours holds it, and holding
	// costs nothing. Everything left goes to whoever is nearest.
	for i := range sys.crews {
		c := &sys.crews[i]
		if c.assign == nil {
			continue
		}
		p := sys.pointByEnt(c.assign.Point)
		if p == nil {
			continue
		}
		if p.owner == side && sys.near(c, p) {
			p.takenB = true
			sys.setVerb(c, p, components.BotHold, now, false)
		}
	}

	for i := range sys.crews {
		c := &sys.crews[i]
		if c.assign != nil && c.assign.Verb == components.BotHold {
			continue
		}
		best, bestScore := (*botPoint)(nil), float32(-1e9)
		for j := range sys.pts {
			p := &sys.pts[j]
			if p.takenB {
				continue
			}
			// Distance is a cost in the same units as value, so a rich point
			// far away and a cheap one underfoot are actually comparable.
			d := float32(math.Sqrt(float64((c.x-p.x)*(c.x-p.x) + (c.z-p.z)*(c.z-p.z))))
			score := p.value - d/components.BotNetProximityM
			if p.owner == side {
				score -= 0.4 // ours already: worth less than taking one that is not
			}
			if score > bestScore {
				best, bestScore = p, score
			}
		}
		if best == nil {
			continue
		}
		best.takenB = true
		verb := components.BotTake
		if best.owner == side {
			verb = components.BotHold
		} else if c.assign != nil && c.assign.Point == best.ent {
			// We were working this one and it went the other way.
			verb = components.BotAnswer
		}
		sys.setVerb(c, best, verb, now, true)
	}

	for _, a := range sys.adds {
		if !sys.world.Alive(a.squad) {
			continue
		}
		cpy := a.a
		if sys.assignMap.Has(a.squad) {
			*sys.assignMap.Get(a.squad) = cpy
		} else {
			sys.assignMap.Add(a.squad, &cpy)
		}
	}
}

// setVerb records the assignment and, when the squad has somewhere new to be,
// issues the order — through the player's own path, so the bot's comms gate it.
func (sys *BotSystem) setVerb(c *botCrew, p *botPoint, verb components.BotVerb,
	now float32, march bool) {
	// Free fire: a bot squad that holds its fire is a bot squad that dies
	// holding ground it was told to hold.
	if r := sys.rulesMap.Get(c.squad); r != nil && r.Mode == components.HoldFire {
		r.Mode = components.FreeFire
	}
	same := c.assign != nil && c.assign.Point == p.ent && c.assign.Verb == verb
	sys.adds = append(sys.adds, botAdd{
		squad: c.squad,
		a:     components.BotAssignment{Point: p.ent, Verb: verb, IssuedAt: now},
	})
	if !march || same {
		return
	}
	sys.squads.IssueOrder(c.squad, components.OrderKindMoveTo, p.pos,
		ecs.Entity{}, false, OrderParams{AttackMove: true})
}

func (sys *BotSystem) near(c *botCrew, p *botPoint) bool {
	dx, dz := c.x-p.x, c.z-p.z
	return dx*dx+dz*dz <= components.BotArrivedM*components.BotArrivedM
}

func (sys *BotSystem) pointByEnt(e ecs.Entity) *botPoint {
	for i := range sys.pts {
		if sys.pts[i].ent == e {
			return &sys.pts[i]
		}
	}
	return nil
}
