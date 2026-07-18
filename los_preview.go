package main

import (
	"math"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/systems"
)

// losPreviewState drives the hold-V hypothetical-position LOS overlay: the
// visibility fan is swept by systems.LOSProbe (same predicates as the sim)
// and cached until the cursor leaves its 1 m cell or the profile changes.
type losPreviewState struct {
	probe   *systems.LOSProbe
	sampler *systems.HeightSampler

	sensorsMap   *ecs.Map[components.Sensors]
	equipmentMap *ecs.Map[components.Equipment]
	weaponMap    *ecs.Map[components.Weapon]
	rosterMap    *ecs.Map[components.CommandRoster]

	active   bool
	origin   components.WorldPos
	runs     [][]components.VisRun
	sensorR  float32
	falloff  components.FalloffKind
	weaponRs []float32

	haveSweep      bool
	lastCX, lastCZ int32
	lastProfile    ecs.Entity
}

func newLOSPreview(w *ecs.World) *losPreviewState {
	return &losPreviewState{
		probe:        systems.NewLOSProbe(w),
		sampler:      systems.NewHeightSampler(w),
		sensorsMap:   ecs.NewMap[components.Sensors](w),
		equipmentMap: ecs.NewMap[components.Equipment](w),
		weaponMap:    ecs.NewMap[components.Weapon](w),
		rosterMap:    ecs.NewMap[components.CommandRoster](w),
		sensorR:      40,
		falloff:      components.FalloffLinear,
	}
}

// fanRuns gates the cached fan for map mirroring: nil while V is not held.
func (lp *losPreviewState) fanRuns() [][]components.VisRun {
	if lp.active && lp.haveSweep {
		return lp.runs
	}
	return nil
}

func (lp *losPreviewState) update(world *ecs.World, held, targetOK bool, target components.WorldPos, selected []ecs.Entity) {
	lp.active = held && targetOK
	if !lp.active {
		lp.haveSweep = false
		return
	}

	profile := ecs.Entity{}
	lp.weaponRs = lp.weaponRs[:0]
	addWeapon := func(unit ecs.Entity) {
		eq := lp.equipmentMap.Get(unit)
		if eq == nil || eq.Primary == (ecs.Entity{}) || !world.Alive(eq.Primary) {
			return
		}
		wp := lp.weaponMap.Get(eq.Primary)
		if wp == nil {
			return
		}
		for _, r := range lp.weaponRs {
			if r == wp.RangeM {
				return
			}
		}
		lp.weaponRs = append(lp.weaponRs, wp.RangeM)
	}
	consider := func(unit ecs.Entity) {
		if profile == (ecs.Entity{}) && lp.sensorsMap.Get(unit) != nil {
			profile = unit
		}
		addWeapon(unit)
	}
	for _, e := range selected {
		if !world.Alive(e) {
			continue
		}
		if roster := lp.rosterMap.Get(e); roster != nil {
			for i := uint8(0); i < roster.Count; i++ {
				if m := roster.Members[i]; m != (ecs.Entity{}) && world.Alive(m) {
					consider(m)
				}
			}
			continue
		}
		consider(e)
	}

	sensorR := float32(0)
	falloff := components.FalloffLinear
	if profile != (ecs.Entity{}) {
		sn := lp.sensorsMap.Get(profile)
		for i := uint8(0); i < sn.Count; i++ {
			if c := &sn.Channels[i]; c.BaseRangeM > sensorR {
				sensorR = c.BaseRangeM
				falloff = c.FalloffKind
			}
		}
	}
	if sensorR <= 0 {
		sensorR = 40
		falloff = components.FalloffLinear
	}

	wx := float32(target.Chunk.X)*components.ChunkSize + target.Local.X
	wz := float32(target.Chunk.Z)*components.ChunkSize + target.Local.Z
	cx := int32(math.Floor(float64(wx)))
	cz := int32(math.Floor(float64(wz)))
	if !lp.haveSweep || cx != lp.lastCX || cz != lp.lastCZ ||
		profile != lp.lastProfile || sensorR != lp.sensorR {
		lp.sensorR = sensorR
		lp.falloff = falloff
		lp.runs = lp.probe.Sweep(wx, wz, sensorR, lp.runs)
		lp.origin = target
		lp.haveSweep = true
		lp.lastCX, lp.lastCZ, lp.lastProfile = cx, cz, profile
	}
}
