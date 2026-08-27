package main

import (
	"fmt"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/entities"
	"rts-go/systems"
)

// lite_roe_return measures the three fire modes as three DIFFERENT things.
// Until M1 they were two: ReturnFire shared an empty switch arm with FreeFire,
// so a squad told to answer fire opened up on the first thing it saw.
//
// The claim is a PAIR, the way the balance scenes are: silence proves nothing
// on its own — a squad that cannot see, cannot reach or has no ammunition is
// also silent. Lane B is the same setup on FreeFire and must be shooting in
// the same seconds lane A is quiet. Only the two lines together say the mode
// is doing the work.
const (
	// Lanes are far enough apart that neither hears the other: the audio
	// bubble caps at 64 m and the gunfire pass reaches 120 m at the floor.
	roeLaneGap float32 = 600
	// Facing pairs at 30 m — inside 40 m infantry optics, so "did not fire"
	// can never be confused with "did not see".
	roeFacingM float32 = 30
	// Phase 1 runs long enough for a squad on FreeFire to empty a magazine
	// into the opposite lane, then the far squad in lane A opens up. Both
	// numbers sit BEFORE tick 1000 (16.6 s): the save/load gate snapshots
	// there, and a scene that mutates the world afterwards diverges — the
	// loaded process has no scene harness to repeat the change.
	roeProvokeAt float32 = 10.0
	roeVerdictAt float32 = 24.0
	// How long the answer may take once rounds start landing. The chain is
	// long — the far side must acquire, aim, hit near enough, and Threat must
	// climb past the channel floor — so the bound sits far outside the
	// measured 4.9 s rather than on it (PLAYTEST rule 13).
	roeAnswerMaxS float32 = 8.0

	// Suppression lane: the foe sits well beyond 40 m infantry optics, so
	// "fired" can only mean "fired at a place nobody has seen".
	roeSuppressM     float32 = 80
	roeSuppressAt    float32 = 2.0
	roeSuppressEndAt float32 = 30.0
	// Suppression is pressure, not a body count. The bound is deliberately
	// far below what the channel reaches in a real firefight.
	roeSuppressMin float32 = 0.10
	// Hull lanes sit side by side inside comms range; 120 m apart is far
	// enough that neither is anywhere near the other's line.
	roeVehLaneX float32 = 120

	// Pinned lane: does suppressive fire DEGRADE the man it lands on, or does
	// it only make noise? The base of fire sits far enough that its rounds
	// arrive scattered — pressure, not marksmanship — and it cannot see the
	// squad it is shooting at in the first place.
	roePinBaseX   float32 = -120
	roePinVictim  float32 = 22
	roePinOrderAt float32 = 3.0
	roePinEndAt   float32 = 34.0
	// Bound set after measuring, with room — the verdict prints both counts
	// every run so the margin stays visible.
	roePinQuietPct int = 60
	// How much longer the shelled lane's victims must last. Seconds.
	roePinSavedS float32 = 3.0

	// Missile lane: a gunship over a hull it can plainly see. Its cannon is an
	// ordinary barrel and fires by itself; its rocket pod is player-released
	// and must stay cold until a point is named. A launcher with nothing to
	// shoot at would be trivially cold, so the target is well inside reach.
	roeMissileM     float32 = 60
	roeMissileAt    float32 = 6.0
	roeMissileEndAt float32 = 20.0
)

type roeMode uint8

const (
	roeModeReturn roeMode = iota
	roeModeSuppress
	roeModeMissile
	roeModeVehSuppress
	roeModePinned
)

// roePinLane is one half of the pinned pair: an enemy squad shooting at a
// punching bag, plus a base of fire that may or may not be shelling it.
type roePinLane struct {
	foe         ecs.Entity
	foeMen      []ecs.Entity
	victim      ecs.Entity
	victimMen   []ecs.Entity
	base        ecs.Entity
	foeAmmo0    []int
	foeLast     []int
	foeFired    int
	victimWipe  float32
	foePeakSupp float32
	foeAlive    int
	victimAlive int
}

type roeLane struct {
	own      ecs.Entity
	ownMen   []ecs.Entity
	foe      ecs.Entity
	foeMen   []ecs.Entity
	ammo0    int
	fired    int
	sawFoe   bool
	firstAt  float32
	peakSupp float32
	alive    int
}

func roeWP(x, z float32) components.WorldPos {
	p := components.WorldPos{}.Add(rl.Vector3{X: x, Z: z})
	p.Local.Y = systems.GroundHeight(x, z)
	return p
}

func aiROESpawn(world *ecs.World, squadService *systems.SquadService,
	roleService *systems.RoleService, unitFactory aiUnitSpawn,
	vehicleFactory *entities.VehicleFactory, aircraftFactory *entities.AircraftFactory,
	mode roeMode,
	posMap *ecs.Map[components.WorldPos],
	rosterMap *ecs.Map[components.CommandRoster]) *aiTestState {
	if squadService == nil || roleService == nil {
		fmt.Printf("[ai-test %s] NO SERVICE — aborting\n", aiSceneID())
		return nil
	}
	s := &aiTestState{
		sceneID:      aiSceneID(),
		roeActive:    true,
		roeMode:      mode,
		World:        world,
		SquadService: squadService,
		PosMap:       posMap,
		HPMap:        ecs.NewMap[components.HP](world),
		AwareMap:     ecs.NewMap[components.Awareness](world),
		RulesMap:     ecs.NewMap[components.EngagementRules](world),
		ThreatMap:    ecs.NewMap[components.Threat](world),
		RosterMap:    rosterMap,
		roeEquipMap:  ecs.NewMap[components.Equipment](world),
		roeWeaponMap: ecs.NewMap[components.Weapon](world),
		roeBehaveMap: ecs.NewMap[components.BehaviorRules](world),
		verdictAt:    roeVerdictAt,
	}
	if mode == roeModePinned {
		s.verdictAt = roePinEndAt
		for i, z := range [2]float32{0, roeLaneGap} {
			var l roePinLane
			l.foe, l.foeMen = s.roeSquad(roleService, unitFactory, roeWP(0, z),
				components.Faction{ID: components.FactionEnemyRed}, components.ControllerAI)
			l.victim, l.victimMen = s.roeSquad(roleService, unitFactory,
				roeWP(roePinVictim, z),
				components.Faction{ID: components.FactionPlayer}, components.ControllerLocal)
			l.base, _ = s.roeSquad(roleService, unitFactory, roeWP(roePinBaseX, z),
				components.Faction{ID: components.FactionPlayer}, components.ControllerLocal)
			// The victim is a punching bag on purpose: if it shot back, the
			// enemy's output would fall for two reasons at once and neither
			// number would mean anything.
			if r := s.RulesMap.Get(l.victim); r != nil {
				r.Mode = components.HoldFire
			}
			if b := s.roeBehaveMap.Get(l.victim); b != nil {
				b.AllowReturnFire = false
			}
			// The base of fire is 120 m out and blind to the squad it will
			// shell — anything it does there comes from the order alone.
			if r := s.RulesMap.Get(l.base); r != nil {
				r.Mode = components.HoldFire
			}
			l.foeAmmo0 = s.roeAmmoEach(l.foeMen)
			l.foeLast = append([]int(nil), l.foeAmmo0...)
			l.victimWipe = -1
			s.roePin[i] = l
		}
		fmt.Println("============================================================")
		fmt.Printf("== ROE %s: lane A pinned by a base of fire at %.0fm, lane B left alone\n",
			s.sceneID, -roePinBaseX)
		fmt.Println("============================================================")
		return s
	}
	if mode == roeModeVehSuppress {
		s.verdictAt = roeSuppressEndAt
		// Lane A: a hull on its own. Lane B: the same hull with a friendly
		// squad standing between it and the ordered point. If only A fires,
		// the blocker is the line-of-fire veto and not the order path.
		s.roeCarrier = vehicleFactory.Spawn(roeWP(0, 0), components.VehicleTank,
			components.FactionPlayer, components.ControllerLocal)
		// Both lanes stay near the scene's relay: 600 m out put lane B off the
		// net and its order came back Blocked, which measures comms, not the
		// line of fire. Nothing hostile exists here, so the lanes need no
		// detection separation at all — only room not to shoot each other.
		s.roeVehB = vehicleFactory.Spawn(roeWP(roeVehLaneX, 0), components.VehicleTank,
			components.FactionPlayer, components.ControllerLocal)
		_, s.roeVehBMen = s.roeSquad(roleService, unitFactory,
			roeWP(roeVehLaneX, roeSuppressM*0.5),
			components.Faction{ID: components.FactionPlayer}, components.ControllerLocal)
		fmt.Println("============================================================")
		fmt.Printf("== ROE %s: lane A hull alone, lane B hull with friendlies down-range\n", s.sceneID)
		fmt.Println("============================================================")
		return s
	}
	if mode == roeModeMissile {
		s.verdictAt = roeMissileEndAt
		if vehicleFactory == nil {
			fmt.Printf("[ai-test %s] NO VEHICLE FACTORY — aborting\n", s.sceneID)
			return nil
		}
		s.roeMissileFoe = vehicleFactory.Spawn(roeWP(0, roeMissileM), components.VehicleBTR,
			components.FactionEnemyRed, components.ControllerAI)
		entry := roeWP(0, -40)
		entry.Local.Y = 40
		if aircraftFactory == nil {
			fmt.Printf("[ai-test %s] NO AIRCRAFT FACTORY — aborting\n", s.sceneID)
			return nil
		}
		// Second gunship, same order, GUIDED loadout: a point strike must
		// bounce off it rather than launch. Sending a guided round at bare
		// ground crashed the sim in queueMissile (owner report 2026-08-27) and
		// would have hurt nobody even past that, having no blast of its own.
		guidedEntry := roeWP(roeLaneGap, -40)
		guidedEntry.Local.Y = 40
		aircraftFactory.Arrival(components.AirArrival{
			At: 1.0, Kind: components.AircraftHeliAttack,
			FactionID: components.FactionPlayer, Controller: components.ControllerLocal,
			Entry: guidedEntry, Exit: systems.AirExitFor(guidedEntry, 400),
			AltRef: components.AltAGL, AltSet: 40, SpeedSet: 10,
			Loadout: 0,
			Rules: components.EngagementRules{
				Mode: components.FreeFire, FireOnInf: true, FireOnArm: true,
			},
		})
		aircraftFactory.Arrival(components.AirArrival{
			At: 1.0, Kind: components.AircraftHeliAttack,
			FactionID: components.FactionPlayer, Controller: components.ControllerLocal,
			Entry: entry, Exit: systems.AirExitFor(entry, 400),
			AltRef: components.AltAGL, AltSet: 40, SpeedSet: 10,
			// Loadout 1 = rocket pod + cannon: the pod is the player-released
			// barrel this scene is about.
			Loadout: 1,
			Rules: components.EngagementRules{
				Mode: components.FreeFire, FireOnInf: true, FireOnArm: true,
			},
		})
		s.roeAirFilter = ecs.NewFilter2[components.Aircraft, components.WorldPos](world)
		fmt.Println("============================================================")
		fmt.Printf("== ROE %s: AT carrier vs a hull at %.0fm, missile released only by order\n",
			s.sceneID, roeMissileM)
		fmt.Println("============================================================")
		return s
	}
	if mode == roeModeSuppress {
		s.verdictAt = roeSuppressEndAt
		s.roeLanes[0] = s.roeBuildLane(roleService, unitFactory, 0, components.FreeFire)
		s.roeLanes[1] = s.roeBuildLane(roleService, unitFactory, roeLaneGap, components.FreeFire)
		fmt.Println("============================================================")
		fmt.Printf("== ROE %s: lane A ordered to suppress %.0fm, lane B identical and unordered\n",
			s.sceneID, roeSuppressM)
		fmt.Println("============================================================")
		return s
	}
	s.roeLanes[0] = s.roeBuildLane(roleService, unitFactory, 0, components.ReturnFire)
	s.roeLanes[1] = s.roeBuildLane(roleService, unitFactory, roeLaneGap, components.FreeFire)

	fmt.Println("============================================================")
	fmt.Printf("== ROE %s: lane A=Return lane B=Free, provoke A at %.0fs\n",
		s.sceneID, roeProvokeAt)
	fmt.Println("============================================================")
	return s
}

// roeBuildLane puts one own squad and one hostile squad nose to nose. The
// hostile side starts on HoldFire in BOTH lanes: the only difference between
// the lanes must be the mode under test.
func (s *aiTestState) roeBuildLane(roleService *systems.RoleService,
	unitFactory aiUnitSpawn, z float32, mode components.EngagementMode) roeLane {
	var lane roeLane
	// Down-range is +Z, the axis a Line formation does NOT spread along. Put
	// the foe along the spread instead and every man stands in his mate's line
	// of fire — measured, not assumed: the first cut of this scene had the
	// squad in a column pointing at the target.
	own, ownMen := s.roeSquad(roleService, unitFactory, roeWP(0, z),
		components.Faction{ID: components.FactionPlayer}, components.ControllerLocal)
	foe, foeMen := s.roeSquad(roleService, unitFactory, roeWP(0, z+s.roeRangeM()),
		components.Faction{ID: components.FactionEnemyRed}, components.ControllerAI)
	lane.own, lane.ownMen, lane.foe, lane.foeMen = own, ownMen, foe, foeMen

	if r := s.RulesMap.Get(own); r != nil {
		r.Mode = mode
	}
	if r := s.RulesMap.Get(foe); r != nil {
		r.Mode = components.HoldFire
	}
	// The far side must stay silent for the measurement to mean anything: one
	// burst of return fire files a gunfire contact and the "blind" claim is
	// gone. Isolating the variable, the same way the ambush scene locks stance.
	if s.roeMode == roeModeSuppress {
		if b := s.roeBehaveMap.Get(foe); b != nil {
			b.AllowReturnFire = false
		}
	}
	lane.ammo0 = s.roeAmmo(ownMen)
	lane.alive = len(ownMen)
	return lane
}

// roeRangeM is how far down-range the far side stands, per mode.
func (s *aiTestState) roeRangeM() float32 {
	if s.roeMode == roeModeSuppress {
		return roeSuppressM
	}
	return roeFacingM
}

func (s *aiTestState) roeSquad(roleService *systems.RoleService,
	unitFactory aiUnitSpawn, at components.WorldPos, faction components.Faction,
	ctrl uint8) (ecs.Entity, []ecs.Entity) {
	sq := s.SquadService.CreateFromTemplate(systems.TmplMotorRifle, at,
		components.FormationLine, faction,
		components.Controller{Owner: ctrl}, roleService, unitFactory)
	if sq == (ecs.Entity{}) {
		return sq, nil
	}
	var men []ecs.Entity
	if r := s.RosterMap.Get(sq); r != nil {
		for i := uint8(0); i < r.Count; i++ {
			if m := r.Members[i]; m != (ecs.Entity{}) {
				men = append(men, m)
			}
		}
	}
	return sq, men
}

// roeAmmo totals the rounds left in every man's active weapon. Rounds spent is
// the honest measure of "did it shoot": LastFiredAt only ever says yes once.
func (s *aiTestState) roeAmmo(men []ecs.Entity) int {
	total := 0
	for _, m := range men {
		if m == (ecs.Entity{}) || !s.World.Alive(m) {
			continue
		}
		eq := s.roeEquipMap.Get(m)
		if eq == nil || eq.Active == (ecs.Entity{}) || !s.World.Alive(eq.Active) {
			continue
		}
		if w := s.roeWeaponMap.Get(eq.Active); w != nil {
			total += int(w.Ammo)
		}
	}
	return total
}

// roeSeesFoe is the anti-vacuity check: a lane whose men never acquired the
// other side proves nothing by staying silent.
func (s *aiTestState) roeSeesFoe(men []ecs.Entity, now float32) bool {
	for _, m := range men {
		if m == (ecs.Entity{}) || !s.World.Alive(m) {
			continue
		}
		aware := s.AwareMap.Get(m)
		if aware == nil {
			continue
		}
		for i := range aware.LastSeen {
			e := aware.LastSeen[i]
			if e.Time > 0 && now-e.Time <= 3.0 && e.Target != (ecs.Entity{}) {
				return true
			}
		}
	}
	return false
}

func (s *aiTestState) updateROE(elapsed float32) {
	if s.verdictDone {
		return
	}
	switch s.roeMode {
	case roeModePinned:
		s.updateROEPinned(elapsed)
		return
	case roeModeVehSuppress:
		s.updateROEVehSuppress(elapsed)
		return
	case roeModeSuppress:
		s.updateROESuppress(elapsed)
		return
	case roeModeMissile:
		s.updateROEMissile(elapsed)
		return
	}
	for i := range s.roeLanes {
		lane := &s.roeLanes[i]
		if spent := lane.ammo0 - s.roeAmmo(lane.ownMen); spent > lane.fired {
			lane.fired = spent
			if lane.firstAt == 0 {
				lane.firstAt = elapsed
			}
		}
		if !lane.sawFoe && s.roeSeesFoe(lane.ownMen, elapsed) {
			lane.sawFoe = true
		}
		lane.alive = s.roeAliveCount(lane.ownMen)
	}
	// Freeze what each lane had done while nobody had shot at it — the whole
	// claim lives in this snapshot, and phase 2 overwrites the running totals.
	if !s.roeProvoked && elapsed >= roeProvokeAt {
		s.roeProvoked = true
		s.roeQuietA = s.roeLanes[0].fired
		s.roeQuietB = s.roeLanes[1].fired
		s.roeProvokeAt = elapsed
		if r := s.RulesMap.Get(s.roeLanes[0].foe); r != nil {
			r.Mode = components.FreeFire
		}
	}
	if elapsed >= s.verdictAt {
		s.roeVerdict(elapsed)
	}
}

func (s *aiTestState) updateROESuppress(elapsed float32) {
	for i := range s.roeLanes {
		lane := &s.roeLanes[i]
		if spent := lane.ammo0 - s.roeAmmo(lane.ownMen); spent > lane.fired {
			lane.fired = spent
		}
		if !lane.sawFoe && s.roeSeesFoe(lane.ownMen, elapsed) {
			lane.sawFoe = true
		}
		if sup := s.roePeakSuppression(lane.foeMen); sup > lane.peakSupp {
			lane.peakSupp = sup
		}
		lane.alive = s.roeAliveCount(lane.ownMen)
	}
	if !s.roeProvoked && elapsed >= roeSuppressAt {
		s.roeProvoked = true
		s.SquadService.IssueOrder(s.roeLanes[0].own, components.OrderKindSuppressFire,
			roeWP(0, roeSuppressM), ecs.Entity{}, false, systems.OrderParams{})
	}
	if elapsed >= s.verdictAt {
		s.roeSuppressVerdict(elapsed)
	}
}

// roeAmmoOf is the rounds left in one weapon entity, 0 when it is gone.
func (s *aiTestState) roeAmmoOf(w ecs.Entity) int {
	if w == (ecs.Entity{}) || !s.World.Alive(w) {
		return 0
	}
	if wp := s.roeWeaponMap.Get(w); wp != nil {
		return int(wp.Ammo)
	}
	return 0
}

// roeAmmoEach reports each man's remaining rounds, -1 where he is gone.
func (s *aiTestState) roeAmmoEach(men []ecs.Entity) []int {
	out := make([]int, len(men))
	for i, m := range men {
		out[i] = -1
		if m == (ecs.Entity{}) || !s.World.Alive(m) {
			continue
		}
		eq := s.roeEquipMap.Get(m)
		if eq == nil || eq.Active == (ecs.Entity{}) || !s.World.Alive(eq.Active) {
			continue
		}
		if w := s.roeWeaponMap.Get(eq.Active); w != nil {
			out[i] = int(w.Ammo)
		}
	}
	return out
}

// roeDryMen counts shooters left with an empty weapon. A squad total says
// nothing — one full machine gun hides five empty rifles — and what the ammo
// reserve promises is that NOBODY is dry.
func (s *aiTestState) roeDryMen(men []ecs.Entity) int {
	dry := 0
	for _, m := range men {
		if m == (ecs.Entity{}) || !s.World.Alive(m) {
			continue
		}
		eq := s.roeEquipMap.Get(m)
		if eq == nil || eq.Active == (ecs.Entity{}) || !s.World.Alive(eq.Active) {
			continue
		}
		if w := s.roeWeaponMap.Get(eq.Active); w != nil && w.Ammo == 0 {
			dry++
		}
	}
	return dry
}

func (s *aiTestState) updateROEVehSuppress(elapsed float32) {
	if s.roeMissileAmmo0 == 0 {
		s.roeMissileAmmo0 = s.roeVehAmmo(s.roeCarrier)
		s.roeGunAmmo0 = s.roeVehAmmo(s.roeVehB)
	}
	s.roeMissileFired = s.roeMissileAmmo0 - s.roeVehAmmo(s.roeCarrier)
	s.roeGunFired = s.roeGunAmmo0 - s.roeVehAmmo(s.roeVehB)
	if !s.roeProvoked && elapsed >= roeSuppressAt {
		s.roeProvoked = true
		s.SquadService.IssueOrder(s.roeCarrier, components.OrderKindSuppressFire,
			roeWP(0, roeSuppressM), ecs.Entity{}, false, systems.OrderParams{})
		s.SquadService.IssueOrder(s.roeVehB, components.OrderKindSuppressFire,
			roeWP(roeVehLaneX, roeSuppressM), ecs.Entity{}, false, systems.OrderParams{})
	}
	if elapsed >= s.verdictAt {
		s.verdictDone = true
		// Lane B keeps firing — the scatter finds clear parts of the ordered
		// area — but its own infantry standing in the beaten zone must come
		// through untouched. That pair pins the veto in BOTH directions: read
		// backwards it silences the hull (lane A catches it), removed it
		// shells the squad (lane B catches it).
		alive := s.roeAliveCount(s.roeVehBMen)
		pass := s.roeMissileFired > 0 && s.roeGunFired > 0 && alive == len(s.roeVehBMen)
		fmt.Println("============================================================")
		fmt.Printf("== VERDICT [%s]: %s  (alone fired=%d | over own troops fired=%d, they are %d/%d | t=%.1fs)\n",
			s.sceneID, balVerdictWord(pass), s.roeMissileFired, s.roeGunFired,
			alive, len(s.roeVehBMen), elapsed)
		fmt.Println("============================================================")
	}
}

// updateROEPinned: both lanes are identical down to the entity layout; the
// only difference is that lane A's base of fire receives a suppression order.
func (s *aiTestState) updateROEPinned(elapsed float32) {
	for i := range s.roePin {
		l := &s.roePin[i]
		// Per man, frozen on death: a squad-total reading counts a dead man's
		// UNSPENT rounds as fired, which made the shelled lane look like the
		// busier one.
		now := s.roeAmmoEach(l.foeMen)
		spent := 0
		for k := range l.foeAmmo0 {
			if now[k] >= 0 {
				l.foeLast[k] = now[k]
			}
			spent += l.foeAmmo0[k] - l.foeLast[k]
		}
		l.foeFired = spent
		if sup := s.roePeakSuppression(l.foeMen); sup > l.foePeakSupp {
			l.foePeakSupp = sup
		}
		l.foeAlive = s.roeAliveCount(l.foeMen)
		l.victimAlive = s.roeAliveCount(l.victimMen)
		if l.victimAlive == 0 && l.victimWipe < 0 {
			l.victimWipe = elapsed
		}
	}
	if !s.roeProvoked && elapsed >= roePinOrderAt {
		s.roeProvoked = true
		s.SquadService.IssueOrder(s.roePin[0].base, components.OrderKindSuppressFire,
			roeWP(0, 0), ecs.Entity{}, false, systems.OrderParams{})
	}
	if elapsed >= s.verdictAt {
		s.roePinnedVerdict(elapsed)
	}
}

func (s *aiTestState) roePinnedVerdict(elapsed float32) {
	s.verdictDone = true
	a, b := &s.roePin[0], &s.roePin[1]
	landed := a.foePeakSupp >= roeSuppressMin
	// The men it was shooting at are the ones who feel it: still standing
	// when the unshelled lane's are gone, or lasting measurably longer.
	saved := a.victimAlive > b.victimAlive ||
		(a.victimWipe < 0 && b.victimWipe >= 0) ||
		(a.victimWipe > 0 && b.victimWipe > 0 && a.victimWipe-b.victimWipe >= roePinSavedS)

	// VOLUME OF FIRE IS REPORTED, NOT GATED — and the gap is the point. A
	// squad at Suppression 1.00 keeps shooting at very nearly its usual rate:
	// the channel is pressed, accuracy is taxed (dispersion x 1+0.5*supp) and
	// the reactive tier looks for cover, but NOTHING in the model turns "I am
	// being shot at" into "I stop shooting". The `ModeSuppressed` arm of
	// shouldFire is written by UtilityEvaluator, which has no live writer for
	// it. Printed every run the way lite_balance_eyes prints infSeesInf, so
	// the day it closes is visible rather than assumed.
	quiet := "OPEN"
	if a.foeFired*100 < b.foeFired*roePinQuietPct {
		quiet = "met"
	}

	pass := landed && saved
	fmt.Println("============================================================")
	fmt.Printf("== VERDICT [%s]: %s  (supp=%.2f | victims wiped at %.1f vs %.1f, want +%.1fs | enemy rounds A=%d B=%d want<%d%% %s | enemy %d/%d vs %d/%d | alive A=%d B=%d of %d | t=%.1fs)\n",
		s.sceneID, balVerdictWord(pass), a.foePeakSupp,
		a.victimWipe, b.victimWipe, roePinSavedS,
		a.foeFired, b.foeFired, roePinQuietPct, quiet,
		a.foeAlive, len(a.foeMen), b.foeAlive, len(b.foeMen),
		a.victimAlive, b.victimAlive, len(a.victimMen), elapsed)
	fmt.Println("============================================================")
}

func (s *aiTestState) roeVehAmmo(v ecs.Entity) int {
	if v == (ecs.Entity{}) || !s.World.Alive(v) {
		return 0
	}
	eq := s.roeEquipMap.Get(v)
	if eq == nil || eq.Primary == (ecs.Entity{}) || !s.World.Alive(eq.Primary) {
		return 0
	}
	if w := s.roeWeaponMap.Get(eq.Primary); w != nil {
		return int(w.Ammo)
	}
	return 0
}

// roeAliveCount is the friendly-loss watch. Nobody is shooting at these
// squads, so any drop is a squad that shot itself — which is exactly how the
// missing line-of-fire check was found.
func (s *aiTestState) roeAliveCount(men []ecs.Entity) int {
	n := 0
	for _, m := range men {
		if m != (ecs.Entity{}) && s.World.Alive(m) {
			n++
		}
	}
	return n
}

// roePeakSuppression is the highest Suppression any man on the far side is
// carrying: suppression is pressure on the position, and one pinned man is the
// evidence that rounds are arriving.
func (s *aiTestState) roePeakSuppression(men []ecs.Entity) float32 {
	best := float32(0)
	for _, m := range men {
		if m == (ecs.Entity{}) || !s.World.Alive(m) {
			continue
		}
		if t := s.ThreatMap.Get(m); t != nil && t.Suppression > best {
			best = t.Suppression
		}
	}
	return best
}

// updateROEMissile: the AT carrier sees the hull the whole time and is on
// FreeFire. Everything that would make an ordinary barrel shoot is true, and
// the launcher must still be cold until the player names a point.
func (s *aiTestState) updateROEMissile(elapsed float32) {
	if s.roeCarrier == (ecs.Entity{}) {
		q := s.roeAirFilter.Query()
		for q.Next() {
			s.roeCarrier = q.Entity()
		}
		if s.roeCarrier == (ecs.Entity{}) {
			if elapsed >= s.verdictAt {
				s.roeMissileVerdict(elapsed)
			}
			return
		}
		// Two airframes arrive; the one carrying the rocket pod is the subject,
		// the guided one is the control.
		q2 := s.roeAirFilter.Query()
		for q2.Next() {
			e := q2.Entity()
			eq := s.roeEquipMap.Get(e)
			if eq == nil || eq.Primary == (ecs.Entity{}) {
				continue
			}
			w := s.roeWeaponMap.Get(eq.Primary)
			if w == nil {
				continue
			}
			if components.SpecForWeapon(w.Kind).MissileSpeedM > 0 {
				s.roeGuided, s.roeGuidedTube = e, eq.Primary
				continue
			}
			s.roeCarrier, s.roePod, s.roeGun = e, eq.Primary, eq.Secondary
		}
		if s.roePod == (ecs.Entity{}) {
			s.roeCarrier = ecs.Entity{}
			return
		}
		if s.roeGuidedTube != (ecs.Entity{}) {
			if w := s.roeWeaponMap.Get(s.roeGuidedTube); w != nil {
				s.roeGuidedAmmo0 = int(w.Ammo)
			}
		}
	}
	ammoOf := func(w ecs.Entity) int {
		if w == (ecs.Entity{}) || !s.World.Alive(w) {
			return 0
		}
		if wp := s.roeWeaponMap.Get(w); wp != nil {
			return int(wp.Ammo)
		}
		return 0
	}
	if s.roeMissileAmmo0 == 0 {
		s.roeMissileAmmo0 = ammoOf(s.roePod)
		s.roeGunAmmo0 = ammoOf(s.roeGun)
	}
	s.roeGunFired = s.roeGunAmmo0 - ammoOf(s.roeGun)
	spent := s.roeMissileAmmo0 - ammoOf(s.roePod)
	if !s.roeProvoked {
		s.roeQuietA = spent
		if s.roeSeesFoe([]ecs.Entity{s.roeCarrier}, elapsed) {
			s.roeCarrierSaw = true
		}
		if elapsed >= roeMissileAt {
			s.roeProvoked = true
			// A PLACE, not the live hull: the point of the order is that it
			// needs no target at all, and by now the cannon has usually
			// killed the one that was there.
			s.SquadService.IssueOrder(s.roeCarrier, components.OrderKindMissileStrike,
				roeWP(0, roeMissileM), ecs.Entity{}, false, systems.OrderParams{})
			if s.roeGuided != (ecs.Entity{}) {
				s.SquadService.IssueOrder(s.roeGuided, components.OrderKindMissileStrike,
					roeWP(roeLaneGap, roeMissileM), ecs.Entity{}, false, systems.OrderParams{})
			}
		}
	}
	s.roeMissileFired = spent
	if elapsed >= s.verdictAt {
		s.roeMissileVerdict(elapsed)
	}
}

func (s *aiTestState) roeMissileVerdict(elapsed float32) {
	s.verdictDone = true
	cold := s.roeQuietA == 0 // the AI never touched the pod
	saw := s.roeCarrierSaw   // ...although the target was plainly there
	// The other half of the claim, and the reason it is not vacuous: the
	// ORDINARY barrel on the SAME airframe was firing the whole time. Without
	// it, a cold pod could just mean a gunship that never got into the fight.
	gunWorked := s.roeGunFired > 0
	ordered := s.roeMissileFired > 0
	// The guided tube must not have moved: a point is not a target for it.
	guidedHeld := s.roeGuidedTube == (ecs.Entity{}) ||
		s.roeGuidedAmmo0 == s.roeAmmoOf(s.roeGuidedTube)

	pass := cold && saw && gunWorked && ordered && guidedHeld
	fmt.Println("============================================================")
	fmt.Printf("== VERDICT [%s]: %s  (pod cold=%d saw=%v | cannon fired=%d | pod after order=%d | guided fired=%d want 0 | t=%.1fs)\n",
		s.sceneID, balVerdictWord(pass), s.roeQuietA, saw, s.roeGunFired,
		s.roeMissileFired, s.roeGuidedAmmo0-s.roeAmmoOf(s.roeGuidedTube), elapsed)
	fmt.Println("============================================================")
}

func (s *aiTestState) roeSuppressVerdict(elapsed float32) {
	s.verdictDone = true
	a, b := &s.roeLanes[0], &s.roeLanes[1]

	blind := !a.sawFoe && !b.sawFoe // the whole point: fire at an unseen place
	fired := a.fired > 0            // the order put rounds downrange
	control := b.fired == 0         // ...and only the order did
	pressed := a.peakSupp >= roeSuppressMin
	dry := s.roeDryMen(a.ownMen)

	// Own losses are REPORTED, not gated. With explosives kept out of area
	// fire, the only way a man dies here is a rifle round from the rank
	// behind him — the game has no friendly-fire filter at all (block F), and
	// whether one of ~165 rounds clips a mate flips with any change to the
	// scatter. The guard against the bug that mattered — an explosive going
	// off inside the formation — moved to lite_roe_veh_suppress, where a
	// splash barrel fires over its own infantry and they must come through.
	pass := blind && fired && control && pressed && dry == 0
	fmt.Println("============================================================")
	fmt.Printf("== VERDICT [%s]: %s  (blind=%v | rounds A=%d B=%d | supp=%.2f want>=%.2f | dry=%d own %d/%d | t=%.1fs)\n",
		s.sceneID, balVerdictWord(pass), blind, a.fired, b.fired,
		a.peakSupp, roeSuppressMin, dry, a.alive, len(a.ownMen), elapsed)
	fmt.Println("============================================================")
}

func (s *aiTestState) roeVerdict(elapsed float32) {
	s.verdictDone = true
	a, b := &s.roeLanes[0], &s.roeLanes[1]

	held := s.roeQuietA == 0          // Return held its fire while unengaged
	control := s.roeQuietB > 0        // ...and the same setup on Free did not
	saw := a.sawFoe && b.sawFoe       // both lanes actually had a target
	answered := a.fired > s.roeQuietA // Return answered once fired upon
	answerLag := float32(0)
	if answered && a.firstAt > s.roeProvokeAt {
		answerLag = a.firstAt - s.roeProvokeAt
	}
	quick := answered && answerLag <= roeAnswerMaxS

	// Lane B is in a real firefight — its foe answers — so its losses are
	// combat, not self-inflicted, and must not be gated on. Reported because
	// the number is worth seeing.
	pass := held && control && saw && quick
	fmt.Println("============================================================")
	fmt.Printf("== VERDICT [%s]: %s  (quiet: A=%d B=%d | saw=%v | answered=%d rounds in %.1fs want<=%.1f | B own %d/%d | t=%.1fs)\n",
		s.sceneID, balVerdictWord(pass), s.roeQuietA, s.roeQuietB, saw,
		a.fired-s.roeQuietA, answerLag, roeAnswerMaxS, b.alive, len(b.ownMen), elapsed)
	fmt.Println("============================================================")
}
