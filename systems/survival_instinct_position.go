package systems

import (
	"fmt"
	"math"
	"os"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// Position scoring (P4). One scorer ranks every kind of place a unit can take
// cover in — emitted slots, trench cells, terrain defilade, the shadow of a
// friendly hull — against ALL live threat bearings, not just the dominant one.
// A slot that stops the front shooter while standing open to the flank one is
// worth less than a trench that stops both.

var posDebug = os.Getenv("RTS_POS_DUMP") != ""

type siCandKind uint8

const (
	siCandSlot siCandKind = iota
	siCandTrench
	siCandDefilade
	siCandHull
)

// siCandidate is one scored place. Protection is described either by an
// outward normal (slot / hull shadow: cover lies opposite the normal) or by an
// 8-way blocked mask (trench / terrain defilade, same encoding as
// CoverCell.DirMask).
type siCandidate struct {
	ent              ecs.Entity // slot entity; zero for cell-derived places
	pos              components.WorldPos
	x, z             float32
	originX, originZ float32
	dirMask          uint8
	quality          float32
	kind             siCandKind
}

const (
	// Weight of one metre of approach distance against a full point of
	// protection. Deliberately low: a good position 20 m away beats a bad one
	// underfoot — the old slot picker inverted this and men hugged the
	// nearest bush while a trench sat 10 m further.
	siDistWeight float32 = 0.012
	// Running INTO the threat is worse than the same distance away from it.
	siApproachWeight float32 = 0.10
	// A place must stop at least this much of the incoming fire to be worth
	// leaving the order for.
	siProtectionFloor float32 = 0.35
	// Occupancy: one other claimant is a nudge, two is a wall.
	siOccupiedPenalty float32 = 0.10
	siCrowdedPenalty  float32 = 0.60

	// A ditch beats a bush (that is the point of a trench) and a hull is
	// nearly a wall; terrain defilade is the weakest of the three — the bake
	// only says "the sightline is broken at observer height within 18 m",
	// which a shooter on higher ground or closer in undoes. At parity with
	// slot quality it out-ranked a tree trunk by 0.02 and men walked TOWARD
	// the shooter to reach it (ai_cover_side).
	siTrenchQuality   float32 = 0.85
	siDefiladeQuality float32 = 0.50
	siHullQuality     float32 = 0.80
	// Stand-off from the hull centre for the shadow ring.
	siHullStandoff float32 = 1.6
	// A hull only shields while it is parked.
	siHullMaxSpeed float32 = 0.5

	// Cell scan radius for trench / defilade candidates (metres). Kept under
	// the slot search radius: walking 30 m to a ditch under fire is worse
	// than going prone where you are.
	siCellScanR float32 = 18.0
	// Sampling stride for the cell scan — every metre is far more candidates
	// than the ranking can distinguish.
	siCellStride = 3
)

// clusterView is the per-tick snapshot of one live threat bearing.
type clusterView struct {
	dirX, dirZ float32 // from the source toward the unit
	weight     float32
}

// gatherClusters copies the live bearings, falling back to the squad-wide
// direction when the unit's own set is empty (scrambling members).
func gatherClusters(threat *components.Threat, fallback rl.Vector3) []clusterView {
	var out []clusterView
	if threat != nil {
		for i := range threat.Clusters {
			c := &threat.Clusters[i]
			if c.Weight <= 0 {
				continue
			}
			out = append(out, clusterView{dirX: c.Dir.X, dirZ: c.Dir.Z, weight: c.Weight})
		}
	}
	if len(out) == 0 && (fallback.X != 0 || fallback.Z != 0) {
		out = append(out, clusterView{dirX: fallback.X, dirZ: fallback.Z, weight: 1})
	}
	return out
}

// protection returns how much of one bearing this candidate stops, in [0, 1].
func (c *siCandidate) protection(dirX, dirZ float32) float32 {
	if c.dirMask != 0 {
		// The threat sits along -dir (dir points source → unit): a blocked
		// compass bit toward the source is cover.
		return maskProtection(c.dirMask, -dirX, -dirZ)
	}
	// The normal points out of cover toward the shooter; alignment with the
	// incoming bearing is protection.
	dot := c.originX*dirX + c.originZ*dirZ
	if dot <= 0 {
		return 0
	}
	return dot
}

// maskProtection interpolates the 8-way mask along an arbitrary bearing: the
// two compass sectors bracketing it vote by angular proximity, so a ridge
// covering N and NE protects a NNE bearing fully and an E one not at all.
//
// Only those two vote. Letting the whole forward hemisphere vote by cos²
// (what this did until 19.8) caps a wall that squarely blocks the bearing at
// 0.5, because the two unmasked flanking sectors always dilute it — half the
// scale of the sibling normal-dot branch in siCandidate.protection, so
// mask-derived cover lost every ranking to direction-derived cover.
func maskProtection(mask uint8, tx, tz float32) float32 {
	if mask == 0 || tx*tx+tz*tz < 1e-8 {
		return 0
	}
	// coverDirs[0] is N (-Z) and the index runs clockwise, one sector per 45°.
	ang := math.Atan2(float64(tx), float64(-tz))
	if ang < 0 {
		ang += 2 * math.Pi
	}
	s := ang / (math.Pi / 4)
	lo := int(s) % 8
	hi := (lo + 1) % 8
	f := float32(s - math.Floor(s))

	var prot float32
	if mask&(1<<uint(lo)) != 0 {
		prot += 1 - f
	}
	if mask&(1<<uint(hi)) != 0 {
		prot += f
	}
	return prot
}

// scoreCandidate ranks one place. Returns (score, ok) — ok is false when the
// place does not stop enough fire to be worth moving to.
func (sys *SurvivalInstinctSystem) scoreCandidate(c *siCandidate, unitX, unitZ float32,
	clusters []clusterView, claim uint8) (float32, bool) {
	if len(clusters) == 0 {
		return 0, false
	}
	var prot, wsum float32
	for i := range clusters {
		cl := &clusters[i]
		prot += c.protection(cl.dirX, cl.dirZ) * cl.weight
		wsum += cl.weight
	}
	if wsum <= 0 {
		return 0, false
	}
	prot /= wsum
	if prot < siProtectionFloor {
		return 0, false
	}

	dx, dz := c.x-unitX, c.z-unitZ
	dist := float32(math.Sqrt(float64(dx*dx + dz*dz)))
	score := prot*c.quality - dist*siDistWeight

	// Moving toward the dominant threat costs extra — same distance, worse
	// idea.
	if dist > 1e-3 {
		lead := clusters[0]
		toward := (dx/dist)*(-lead.dirX) + (dz/dist)*(-lead.dirZ)
		if toward > 0 {
			score -= toward * siApproachWeight
		}
	}
	switch {
	case claim >= 2:
		score -= siCrowdedPenalty
	case claim >= 1:
		score -= siOccupiedPenalty
	}
	return score, true
}

// pickPosition ranks every candidate source and returns the best place. The
// returned entity is the claimed cover slot (zero for cell-derived places, on
// which no occupancy is booked).
func (sys *SurvivalInstinctSystem) pickPosition(
	unit ecs.Entity, pos *components.WorldPos, threat *components.Threat,
	fallbackDir rl.Vector3, claimed map[ecs.Entity]ecs.Entity,
) (ecs.Entity, components.WorldPos, bool) {
	clusters := gatherClusters(threat, fallbackDir)
	if len(clusters) == 0 {
		return ecs.Entity{}, components.WorldPos{}, false
	}
	unitX, unitZ := worldXZ(*pos)

	sys.cands = sys.cands[:0]
	sys.gatherSlotCandidates(unitX, unitZ)
	sys.gatherTrenchCandidates(unitX, unitZ)
	sys.gatherCellCandidates(*pos, unitX, unitZ, clusters)
	sys.gatherHullCandidates(unitX, unitZ, clusters)

	best := float32(-1e9)
	var bestKind siCandKind
	var bestEnt ecs.Entity
	var bestPos components.WorldPos
	found := false
	for i := range sys.cands {
		c := &sys.cands[i]
		// A place inside a beaten zone is not a place (MC1).
		if sys.zoneAt(c.x, c.z) >= 0 {
			continue
		}
		// Nor is a place outside the ground a cut-off squad is holding. Cover
		// hops are 18 m each and there is no limit on how many; without this
		// a squad under sustained fire walks off the map one good rock at a
		// time, with nobody able to call it back.
		if !sys.withinLeash(c.x, c.z) {
			continue
		}
		claim := uint8(0)
		if c.ent != (ecs.Entity{}) {
			claim = sys.occupancyClaim[c.ent]
			if owner, ok := claimed[c.ent]; ok && owner != unit {
				claim++
			}
		}
		score, ok := sys.scoreCandidate(c, unitX, unitZ, clusters, claim)
		if !ok || score <= best {
			continue
		}
		best, bestEnt, bestPos, found = score, c.ent, c.pos, true
		if posDebug {
			bestKind = c.kind
		}
	}
	if posDebug {
		fmt.Printf("[pos] unit=%d cands=%d best=%.3f kind=%d at=(%.1f,%.1f)\n",
			unit.ID(), len(sys.cands), best, bestKind, bestPos.Local.X, bestPos.Local.Z)
		for i := range sys.cands {
			c := &sys.cands[i]
			sc, ok := sys.scoreCandidate(c, unitX, unitZ, clusters, 0)
			if ok && sc > best-0.25 {
				fmt.Printf("     kind=%d q=%.2f mask=%02x at=(%.1f,%.1f) score=%.3f\n",
					c.kind, c.quality, c.dirMask, c.x, c.z, sc)
			}
		}
	}
	return bestEnt, bestPos, found
}

func (sys *SurvivalInstinctSystem) gatherSlotCandidates(unitX, unitZ float32) {
	for i := range sys.slots {
		s := &sys.slots[i]
		dx, dz := s.worldX-unitX, s.worldZ-unitZ
		if dx*dx+dz*dz > siCoverSearchRadius*siCoverSearchRadius {
			continue
		}
		sys.cands = append(sys.cands, siCandidate{
			ent:     s.ent,
			pos:     s.worldPos,
			x:       s.worldX,
			z:       s.worldZ,
			originX: s.originX,
			originZ: s.originZ,
			quality: s.quality,
			kind:    siCandSlot,
		})
	}
}

// gatherCellCandidates walks the nav / cover bake around the unit: trench
// cells (the bake already knows where the ditches are) and terrain defilade
// (CoverCell.DirMask — its first consumer). Chunks are visited in a fixed
// window order and cells by index, so the candidate list is deterministic.
func (sys *SurvivalInstinctSystem) gatherCellCandidates(pos components.WorldPos,
	unitX, unitZ float32, clusters []clusterView) {
	idx := sys.chunkIndexRes.Get()
	if idx == nil {
		return
	}
	for dcz := int32(-1); dcz <= 1; dcz++ {
		for dcx := int32(-1); dcx <= 1; dcx++ {
			cc := components.ChunkCoord{X: pos.Chunk.X + dcx, Z: pos.Chunk.Z + dcz}
			chunkEnt, ok := idx.Loaded[cc]
			if !ok || !sys.world.Alive(chunkEnt) {
				continue
			}
			nav := sys.navGridMap.Get(chunkEnt)
			cov := sys.coverMapMap.Get(chunkEnt)
			if nav == nil {
				continue
			}
			baseX := float32(cc.X) * components.ChunkSize
			baseZ := float32(cc.Z) * components.ChunkSize
			for cj := 0; cj < components.NavGridSide; cj += siCellStride {
				for ci := 0; ci < components.NavGridSide; ci += siCellStride {
					cell := &nav.Cells[cj*components.NavGridSide+ci]
					if cell.Cost == 0 || cell.Flags&components.NavInBuilding != 0 {
						continue
					}
					cx := baseX + float32(ci) + 0.5
					cz := baseZ + float32(cj) + 0.5
					dx, dz := cx-unitX, cz-unitZ
					if dx*dx+dz*dz > siCellScanR*siCellScanR {
						continue
					}
					wp := components.WorldPos{}.Add(rl.Vector3{X: cx, Z: cz})
					if cov == nil {
						continue
					}
					mask := cov.Cells[cj*components.NavGridSide+ci].DirMask
					if mask == 0 {
						continue
					}
					sys.cands = append(sys.cands, siCandidate{
						pos: wp, x: cx, z: cz,
						dirMask: mask,
						quality: siDefiladeQuality,
						kind:    siCandDefilade,
					})
				}
			}
		}
	}
}

// gatherTrenchCandidates walks the trench polylines rather than the nav
// cells: a 2 m ditch is one or two cells wide and falls straight through the
// sampling stride the defilade scan uses — the men then never saw the trench
// the scene was built around.
func (sys *SurvivalInstinctSystem) gatherTrenchCandidates(unitX, unitZ float32) {
	tn := sys.trenchRes.Get()
	if tn == nil {
		return
	}
	for ti := range tn.Lines {
		t := &tn.Lines[ti]
		for si := 0; si+1 < len(t.Points); si++ {
			ax, az := worldXZ(t.Points[si])
			bx, bz := worldXZ(t.Points[si+1])
			cx, cz := nearestOnSegment(unitX, unitZ, ax, az, bx, bz)
			dx, dz := cx-unitX, cz-unitZ
			if dx*dx+dz*dz > siCellScanR*siCellScanR {
				continue
			}
			sys.cands = append(sys.cands, siCandidate{
				pos:     components.WorldPos{}.Add(rl.Vector3{X: cx, Z: cz}),
				x:       cx,
				z:       cz,
				dirMask: 0xFF, // a ditch shields every bearing
				quality: siTrenchQuality,
				kind:    siCandTrench,
			})
		}
	}
}

func nearestOnSegment(px, pz, ax, az, bx, bz float32) (float32, float32) {
	vx, vz := bx-ax, bz-az
	l2 := vx*vx + vz*vz
	if l2 < 1e-6 {
		return ax, az
	}
	t := ((px-ax)*vx + (pz-az)*vz) / l2
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	return ax + vx*t, az + vz*t
}

// gatherHullCandidates puts one position in the shadow of each parked
// friendly hull — the v0 of dynamic cover (the full emitter is Phase 20).
func (sys *SurvivalInstinctSystem) gatherHullCandidates(unitX, unitZ float32,
	clusters []clusterView) {
	if len(clusters) == 0 {
		return
	}
	lead := clusters[0]
	q := sys.vehicleFilter.Query()
	for q.Next() {
		veh, vpos, mot := q.Get()
		if mot.Speed > siHullMaxSpeed || mot.Speed < -siHullMaxSpeed {
			continue
		}
		vx, vz := worldXZ(*vpos)
		dx, dz := vx-unitX, vz-unitZ
		if dx*dx+dz*dz > siCoverSearchRadius*siCoverSearchRadius {
			continue
		}
		spec := components.SpecForVehicle(veh.Kind)
		off := spec.ColliderR + siHullStandoff
		// Shadow lies along the threat bearing, past the hull.
		sx := vx + lead.dirX*off
		sz := vz + lead.dirZ*off
		sys.cands = append(sys.cands, siCandidate{
			ent: q.Entity(),
			pos: components.WorldPos{}.Add(rl.Vector3{X: sx, Z: sz}),
			x:   sx,
			z:   sz,
			// Same convention as CoverSlot.OriginDir: the normal points the
			// way the bullet travels (away from the shooter), so a place
			// whose normal aligns with the incoming bearing is protected.
			originX: lead.dirX,
			originZ: lead.dirZ,
			quality: siHullQuality,
			kind:    siCandHull,
		})
	}
}

// relocateTarget is the mandatory fallback (P4): nothing worth taking cover
// in, so crawl out of the fire lane — perpendicular to the dominant bearing,
// which leaves the beaten zone faster than running directly away from a
// shooter who simply tracks you.
func (sys *SurvivalInstinctSystem) relocateTarget(pos *components.WorldPos,
	threat *components.Threat, fallbackDir rl.Vector3) (components.WorldPos, bool) {
	clusters := gatherClusters(threat, fallbackDir)
	if len(clusters) == 0 {
		return components.WorldPos{}, false
	}
	lead := clusters[0]
	// Perpendicular, deterministic side: the one that also carries the unit
	// away from the shooter rather than across its front.
	px, pz := -lead.dirZ, lead.dirX
	if px*lead.dirX+pz*lead.dirZ < 0 {
		px, pz = -px, -pz
	}
	x, z := worldXZ(*pos)
	tx := x + px*siRelocateDist + lead.dirX*siRelocateDist*0.5
	tz := z + pz*siRelocateDist + lead.dirZ*siRelocateDist*0.5
	if sys.zoneAt(tx, tz) >= 0 {
		return components.WorldPos{}, false
	}
	return pos.Add(rl.Vector3{X: tx - x, Z: tz - z}), true
}

// planPosition is the single decision point: score every candidate, and when
// nothing is worth taking, fall back to the relocate primitive if the unit is
// standing somewhere it must not stay.
func (sys *SurvivalInstinctSystem) planPosition(unit ecs.Entity, pos *components.WorldPos,
	threat *components.Threat, fallbackDir rl.Vector3, inZone bool,
	claimed map[ecs.Entity]ecs.Entity) (siAcquireOp, bool) {
	dirX, dirZ := leadBearing(threat, fallbackDir)
	sys.setLeash(unit)
	slot, slotPos, found := sys.pickPosition(unit, pos, threat, fallbackDir, claimed)
	if found {
		reason := components.TacticalOverrideUnderFire
		if inZone {
			reason = components.TacticalOverrideShellfire
		}
		return siAcquireOp{unit: unit, slot: slot, pos: slotPos,
			reason: reason, dirX: dirX, dirZ: dirZ}, true
	}
	// Nothing worth moving to. Inside a beaten zone the answer is still
	// "leave"; in the open it is the relocate primitive — get out of the fire
	// lane instead of standing in it (P4 fallback).
	if inZone {
		if evacPos, ok := sys.evacTarget(pos); ok {
			return siAcquireOp{unit: unit, pos: evacPos,
				reason: components.TacticalOverrideShellfire, dirX: dirX, dirZ: dirZ}, true
		}
		return siAcquireOp{}, false
	}
	relocPos, ok := sys.relocateTarget(pos, threat, fallbackDir)
	if !ok {
		return siAcquireOp{}, false
	}
	if !sys.withinLeash(worldXZ(relocPos)) {
		return siAcquireOp{}, false
	}
	return siAcquireOp{unit: unit, pos: relocPos,
		reason: components.TacticalOverrideNoCover, dirX: dirX, dirZ: dirZ}, true
}

// setLeash reads the unit's commander's self-action bound once per plan.
// Leaving a beaten zone is exempt: an anchor is ground to hold, not ground to
// die on, and the evacuation primitive is the one move that must always be
// available.
func (sys *SurvivalInstinctSystem) setLeash(unit ecs.Entity) {
	sys.leashR = 0
	cmd := unit
	if m := sys.memberMap.Get(unit); m != nil && m.Squad != (ecs.Entity{}) {
		cmd = m.Squad
	}
	anchor, r, ok := LeashFor(sys.commsMap.Get(cmd))
	if !ok {
		return
	}
	sys.leashX, sys.leashZ = worldXZ(anchor)
	sys.leashR = r
}

func (sys *SurvivalInstinctSystem) withinLeash(x, z float32) bool {
	if sys.leashR <= 0 {
		return true
	}
	dx, dz := x-sys.leashX, z-sys.leashZ
	return dx*dx+dz*dz <= sys.leashR*sys.leashR
}

// leadBearing is the dominant threat direction (source → unit).
func leadBearing(threat *components.Threat, fallback rl.Vector3) (float32, float32) {
	cl := gatherClusters(threat, fallback)
	if len(cl) == 0 {
		return 0, 0
	}
	return cl[0].dirX, cl[0].dirZ
}

// positionStale reports a held place as worth re-scoring: the dominant
// bearing has swung past a right angle from the one it was chosen against, a
// zone has swallowed it, or it was chosen with no bearing at all.
func (sys *SurvivalInstinctSystem) positionStale(ov *components.TacticalOverride,
	threat *components.Threat) bool {
	if sys.zoneAt(worldXZ(ov.CoverPos)) >= 0 {
		return true
	}
	if ov.ChosenDirX == 0 && ov.ChosenDirZ == 0 {
		return true
	}
	dx, dz := leadBearing(threat, rl.Vector3{})
	if dx == 0 && dz == 0 {
		return false
	}
	return ov.ChosenDirX*dx+ov.ChosenDirZ*dz <= 0
}
