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
)

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
		if eq := s.roeEquipMap.Get(s.roeCarrier); eq != nil {
			s.roePod = eq.Primary
			s.roeGun = eq.Secondary
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

	pass := cold && saw && gunWorked && ordered
	fmt.Println("============================================================")
	fmt.Printf("== VERDICT [%s]: %s  (pod cold=%d saw=%v | cannon fired=%d | pod after order=%d | t=%.1fs)\n",
		s.sceneID, balVerdictWord(pass), s.roeQuietA, saw, s.roeGunFired,
		s.roeMissileFired, elapsed)
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

	pass := blind && fired && control && pressed && dry == 0 && a.alive == len(a.ownMen)
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
