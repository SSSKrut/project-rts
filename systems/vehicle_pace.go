package systems

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// vehicle_pace.go — Phase 19 M7 convoy pacing. A squad-member vehicle's
// cruise is capped to the column speed: the slowest mate's currently
// achievable speed (road spec when riding an edge, offroad otherwise;
// infantry mates count as foot pace). Everyone keeps ×1.3 catch-up headroom;
// a leader whose column has stretched past the engage distance drops to the
// bottleneck speed exactly — the frontier waits by slowing, not stopping.
// Stateless: derived from the roster every tick, so leaving a squad frees
// the vehicle instantly and nothing rides save/load.

const (
	vehFootPace       float32 = 3.0
	vehPaceHeadroom   float32 = 1.3
	vehPaceStretchMul float32 = 0.6
	vehPaceMinSpacing float32 = 4.0
)

// squadPaceCap returns the cruise cap for ent, 0 = uncapped (soloist or
// single-member squad).
func (sys *VehicleDriverSystem) squadPaceCap(ent ecs.Entity, pos *components.WorldPos) float32 {
	sm := sys.squadMemberMap.Get(ent)
	if sm == nil || sm.Squad == (ecs.Entity{}) || !sys.worldRef.Alive(sm.Squad) {
		return 0
	}
	roster := sys.rosterMap.Get(sm.Squad)
	if roster == nil || roster.Count < 2 {
		return 0
	}

	spacing := vehPaceMinSpacing
	if fd := sys.formationMap.Get(sm.Squad); fd != nil && fd.Spacing > spacing {
		spacing = fd.Spacing
	}

	minSpeed := float32(0)
	var leader ecs.Entity
	var leaderPos components.WorldPos
	stretched := false
	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) || !sys.worldRef.Alive(mem) {
			continue
		}
		mp := sys.posMap.Get(mem)
		if mp == nil {
			continue
		}
		if leader == (ecs.Entity{}) {
			leader = mem
			leaderPos = *mp
		}
		sp := vehFootPace
		if veh := sys.vehicleMap.Get(mem); veh != nil {
			spec := components.SpecForVehicle(veh.Kind)
			sp = spec.MaxSpeedOffroad
			if f := sys.followerMap.Get(mem); f != nil && f.Edge >= 0 {
				sp = spec.MaxSpeedRoad
			}
		}
		if minSpeed == 0 || sp < minSpeed {
			minSpeed = sp
		}
		if mem != leader {
			// Stretch = farther from the leader than the nominal column
			// depth for this roster index plus 1.5×spacing of slack. NOT
			// distance-to-slot: slots hang off the waypoint frontier ahead
			// of everyone, which reads as permanent lag mid-march.
			d := mp.Sub(leaderPos)
			nominal := spacing*float32(i) + 1.5*spacing
			if d.X*d.X+d.Z*d.Z > nominal*nominal {
				stretched = true
			}
		}
	}
	if minSpeed == 0 {
		return 0
	}

	if ent == leader {
		// The frontier has nobody to catch up to: it holds the bottleneck
		// speed, and drops further while the column is stretched so the
		// laggards actually close the gap.
		if stretched {
			return minSpeed * vehPaceStretchMul
		}
		return minSpeed
	}
	return minSpeed * vehPaceHeadroom
}
