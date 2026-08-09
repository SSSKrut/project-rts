package systems

import (
	"math"

	"rts-go/components"
)

// Slot assignment (Phase 19.8 M2). Handing slot i to roster index i is what
// makes a squad cross its own paths in the doorway and reshuffle every
// position the moment somebody dies: the roster compacts on a death and every
// index shifts. Assignment is by delivery cost instead, recomputed each pass
// from live positions, with ownership sticky so an owned slot stays owned.
const (
	// A storey change costs about this much walking: stairs are a detour and
	// a queue, so a man on the right floor should win the position.
	slotStoreyPenalty float32 = 15
	slotStoreyStep    float32 = 3
)

// SlotCandidate is one assignable man: where he is and what he already holds.
type SlotCandidate struct {
	Pos  components.WorldPos
	Held uint8 // index+1, 0 = none (LocalBlackboard.HeldSlot)
	Live bool  // false = skip (dead, or driven by an override)
}

// slotDeliveryCost is what it costs THIS man to reach THIS slot — pure
// geometry, no unit state beyond position.
func slotDeliveryCost(c SlotCandidate, slot BuildingSlot) float32 {
	d := slot.Pos.Sub(c.Pos)
	cost := float32(math.Sqrt(float64(d.X*d.X + d.Z*d.Z)))
	storeys := float32(math.Abs(float64(d.Y))) / slotStoreyStep
	if storeys > 0.5 {
		cost += slotStoreyPenalty * float32(math.Round(float64(storeys)))
	}
	return cost
}

// assignSlots matches men to slots. out[i] is the slot index for candidate i,
// or noSlot. Deterministic: equal costs go to the lower candidate index, never
// to map or float ordering.
func assignSlots(cands []SlotCandidate, slots []BuildingSlot, out []uint8) []uint8 {
	out = out[:0]
	for range cands {
		out = append(out, noSlot)
	}
	if len(slots) == 0 {
		return out
	}
	// The planner emits slots in priority order — one per room first, extras
	// after — so when men are scarce the slots to drop are the TAIL, not the
	// expensive ones. Matching against the whole list instead lets greedy keep
	// the cheap near positions and leave a far room with nobody in it, which
	// is the one thing an occupy order must not do.
	live := 0
	for i := range cands {
		if cands[i].Live {
			live++
		}
	}
	usable := len(slots)
	if live < usable {
		usable = live
	}
	if usable == 0 {
		return out
	}
	takenSlot := make([]bool, usable)
	// Ownership is sticky. Re-matching everyone every pass looks stable on
	// paper and is not: the storey term flips the instant a climber reaches a
	// landing, so two men swap slots mid-stairs and neither arrives. Only
	// slots nobody owns are up for competition.
	for ci := range cands {
		if !cands[ci].Live {
			continue
		}
		if held, ok := heldSlotIndex(cands[ci].Held); ok && int(held) < usable && !takenSlot[held] {
			out[ci] = held
			takenSlot[held] = true
		}
	}
	// Slot-major, in the planner's priority order: each position picks its
	// best available man. Globally-cheapest-pair greedy optimises total cost
	// and produces a poor schedule — it hands the near slots out first and
	// leaves an important far one to whoever is left over, who then arrives
	// late or not at all.
	for si := 0; si < usable; si++ {
		if takenSlot[si] {
			continue
		}
		best := -1
		var bestCost float32
		for ci := range cands {
			if !cands[ci].Live || out[ci] != noSlot {
				continue
			}
			c := slotDeliveryCost(cands[ci], slots[si])
			if best < 0 || c < bestCost {
				best, bestCost = ci, c
			}
		}
		if best < 0 {
			break
		}
		out[best] = uint8(si)
		takenSlot[si] = true
	}
	return out
}

// heldSlotIndex decodes the index+1 encoding LocalBlackboard stores.
func heldSlotIndex(held uint8) (uint8, bool) {
	return (&components.LocalBlackboard{HeldSlot: held}).HeldSlotIndex()
}

// noSlot marks a candidate with no position to go to (more men than slots, or
// not eligible for assignment this pass).
const noSlot uint8 = 0xFF

// facadeSector buckets an outward yaw into one of the four walls.
func facadeSector(yaw float32) int {
	const quarter = math.Pi / 2
	a := math.Mod(float64(yaw)+math.Pi/4, 2*math.Pi)
	if a < 0 {
		a += 2 * math.Pi
	}
	return int(a/quarter) % 4
}

// interleaveByFacade reorders window slots so consecutive entries face
// different walls. Spawn order lists a building's windows facade by facade, so
// a squad smaller than the window count would man one wall and leave the rest
// of the house blind — this is what "all-round defence" has to mean when the
// player gave no facing.
func interleaveByFacade(slots []BuildingSlot) []BuildingSlot {
	if len(slots) < 2 {
		return slots
	}
	var buckets [4][]BuildingSlot
	for _, s := range slots {
		buckets[facadeSector(s.Yaw)] = append(buckets[facadeSector(s.Yaw)], s)
	}
	out := make([]BuildingSlot, 0, len(slots))
	for round := 0; len(out) < len(slots); round++ {
		progressed := false
		for b := 0; b < 4; b++ {
			if round < len(buckets[b]) {
				out = append(out, buckets[b][round])
				progressed = true
			}
		}
		if !progressed {
			break
		}
	}
	return out
}
