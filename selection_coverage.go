package main

import (
	"math"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/systems"
	"rts-go/ui"
)

// coverageState drives the persistent selection-coverage overlay (Phase 20.7
// L1): what the selected group detects, how far it is heard, what its weapons
// reach — from its own position, no key held. Hold-V stays the
// hypothetical-point tool; this is the state indicator.
//
// Profile = first selected entity with Sensors (squads via roster), same rule
// as hold-V: one fan and one ring set per selection. Emission is the
// exception — the group's danger radius is its loudest member, wherever the
// profile happens to be.
type coverageState struct {
	probe   *systems.LOSProbe
	sampler *systems.HeightSampler

	sensorsMap   *ecs.Map[components.Sensors]
	equipmentMap *ecs.Map[components.Equipment]
	weaponMap    *ecs.Map[components.Weapon]
	rosterMap    *ecs.Map[components.CommandRoster]
	posMap       *ecs.Map[components.WorldPos]
	aircraftMap  *ecs.Map[components.Aircraft]

	View ui.CoverageView

	runs           [][]components.VisRun
	haveSweep      bool
	lastCX, lastCZ int32
	lastProfile    ecs.Entity
	lastVisR       float32
}

func newCoverageState(w *ecs.World) *coverageState {
	return &coverageState{
		probe:        systems.NewLOSProbe(w),
		sampler:      systems.NewHeightSampler(w),
		sensorsMap:   ecs.NewMap[components.Sensors](w),
		equipmentMap: ecs.NewMap[components.Equipment](w),
		weaponMap:    ecs.NewMap[components.Weapon](w),
		rosterMap:    ecs.NewMap[components.CommandRoster](w),
		posMap:       ecs.NewMap[components.WorldPos](w),
		aircraftMap:  ecs.NewMap[components.Aircraft](w),
	}
}

// coverageSweepCell is the fan's recompute granularity. Coarser than hold-V's
// 1 m: a state indicator may lag half a stride, a cursor tool may not.
const coverageSweepCell = 2.0

func (cs *coverageState) update(world *ecs.World, selected []ecs.Entity) {
	v := &cs.View
	v.Active = false
	v.Runs = nil
	v.Rings = v.Rings[:0]
	v.WeaponRs = v.WeaponRs[:0]
	v.EmitR = 0

	profile := ecs.Entity{}
	addWeapon := func(unit ecs.Entity) {
		eq := cs.equipmentMap.Get(unit)
		if eq == nil || eq.Primary == (ecs.Entity{}) || !world.Alive(eq.Primary) {
			return
		}
		wp := cs.weaponMap.Get(eq.Primary)
		if wp == nil {
			return
		}
		for _, r := range v.WeaponRs {
			if r == wp.RangeM {
				return
			}
		}
		v.WeaponRs = append(v.WeaponRs, wp.RangeM)
	}
	consider := func(m ecs.Entity) {
		if sn := cs.sensorsMap.Get(m); sn != nil {
			if profile == (ecs.Entity{}) {
				profile = m
			}
			if r := sn.EmitRangeM(); r > v.EmitR {
				if pos := cs.posMap.Get(m); pos != nil {
					v.EmitR = r
					v.EmitOrigin = *pos
				}
			}
		}
		addWeapon(m)
	}
	for _, e := range selected {
		if !world.Alive(e) {
			continue
		}
		if roster := cs.rosterMap.Get(e); roster != nil {
			for i := uint8(0); i < roster.Count; i++ {
				if m := roster.Members[i]; m != (ecs.Entity{}) && world.Alive(m) {
					consider(m)
				}
			}
			continue
		}
		consider(e)
	}
	if profile == (ecs.Entity{}) {
		cs.haveSweep = false
		return
	}
	pos := cs.posMap.Get(profile)
	sn := cs.sensorsMap.Get(profile)
	if pos == nil || sn == nil {
		cs.haveSweep = false
		return
	}
	v.Active = true
	v.Origin = *pos
	v.AirProfile = cs.aircraftMap.Has(profile)
	for i := uint8(0); i < sn.Count; i++ {
		if !sn.ChannelOn(i) || sn.Channels[i].BaseRangeM <= 0 {
			continue
		}
		v.Rings = append(v.Rings, ui.CoverageRing{
			Kind: sn.Channels[i].Kind, RadiusM: sn.Channels[i].BaseRangeM,
		})
	}
	v.VisR, v.Falloff = systems.PassiveSensorProfile(sn)
	// No fan for an airframe: from working altitude a terrain sweep collapses
	// into an information-free disc, and Sweep's standing-man eye height would
	// misreport it anyway. The rings carry its story.
	if v.AirProfile || v.VisR <= 0 {
		cs.haveSweep = false
		return
	}

	wx := float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
	wz := float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z
	cx := int32(math.Floor(float64(wx) / coverageSweepCell))
	cz := int32(math.Floor(float64(wz) / coverageSweepCell))
	if !cs.haveSweep || cx != cs.lastCX || cz != cs.lastCZ ||
		profile != cs.lastProfile || v.VisR != cs.lastVisR {
		cs.runs = cs.probe.Sweep(wx, wz, v.VisR, cs.runs)
		cs.haveSweep = true
		cs.lastCX, cs.lastCZ = cx, cz
		cs.lastProfile = profile
		cs.lastVisR = v.VisR
	}
	v.Runs = cs.runs
}
