package systems

import (
	"math"
	"sort"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// stanceConcealmentMul - hand-picked. Prone is the hardest to spot, then
// crouch, stand at full visibility (mul = 1.0 means no reduction).
func stanceConcealmentMul(code components.StanceCode) float32 {
	switch code {
	case components.StanceProne:
		return 0.5
	case components.StanceCrouch:
		return 0.75
	}
	return 1.0
}

// Vehicle audio signature: full NoiseRadiusM on the move, a quarter while
// idling (engine running). Deterministic, no per-vehicle state.
const vehIdleNoiseMul float32 = 0.25

// runDetectPass collects unit/seer snapshots, buckets walls + heightmaps by
// chunk, groups seers by squad, then runs per-group detection in parallel.
// A group's worker owns all its members' Awareness writes; contactRecs go to
// per-worker slices drained in order for the serial upsert.
func (sys *ContactSystem) runDetectPass(dt float32) {
	sys.unitsBuf = sys.unitsBuf[:0]
	sys.seersBuf = sys.seersBuf[:0]
	sys.contactsBuf = sys.contactsBuf[:0]

	// Snapshot live smoke fields — targets standing inside are harder to spot.
	sys.smokeBuf = sys.smokeBuf[:0]
	qSm := sys.smokeFilter.Query()
	for qSm.Next() {
		sf, sp := qSm.Get()
		if float64(sys.elapsed) >= sf.ExpiresAt {
			continue
		}
		sx := float32(sp.Chunk.X)*components.ChunkSize + sp.Local.X
		sz := float32(sp.Chunk.Z)*components.ChunkSize + sp.Local.Z
		sys.smokeBuf = append(sys.smokeBuf, smokeVol{x: sx, z: sz, rSq: sf.Radius * sf.Radius})
	}
	qSm.Close()

	q := sys.unitFilter.Query()
	for q.Next() {
		ent := q.Entity()
		_, pos, mot, sensors, aware, faction := q.Get()
		audio := sys.audioEmissionRadius(ent, mot.Speed)
		stance := components.StanceStand
		if st := sys.stanceMap.Get(ent); st != nil {
			stance = st.Code
		}
		spec := components.SpecForStance(stance)
		wx := float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
		wz := float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z
		conceal := stanceConcealmentMul(stance)
		switch {
		case mot.Speed < detectStillSpeed:
			conceal *= detectStillMul
		case mot.Speed > detectRunSpeed:
			conceal *= detectRunMul
		}
		conceal *= sys.smokeMulAt(wx, wz)
		det := sys.dtMap.Get(ent)
		meter := [components.FactionCount]float32{1, 1, 1, 1}
		if det != nil {
			meter = det.Meter
		}
		sys.unitsBuf = append(sys.unitsBuf, contactUnit{
			ent: ent, pos: *pos, chunk: pos.Chunk,
			x: wx, z: wz, targetY: pos.Local.Y + spec.TargetCenterY,
			faction: faction.ID, dimMask: components.DimInfantry,
			concealment: conceal, audioRadius: audio, emitRange: sensors.EmitRangeM(),
			shotHeardM: sys.gunshotReach(ent),
			meter:      meter, det: det,
		})
		sys.seersBuf = append(sys.seersBuf, contactSeer{
			ent: ent, pos: *pos,
			x: wx, z: wz, eyeY: pos.Local.Y + spec.EyeHeight,
			fwdX: float32(math.Sin(float64(mot.Yaw))),
			fwdZ: float32(math.Cos(float64(mot.Yaw))),
			yaw:  mot.Yaw, faction: faction.ID,
			sensors: sensors, aware: aware, maxRange: passiveMaxRange(sensors),
			netOK: sys.netOK(ent),
		})
	}
	q.Close()

	// Vehicles: big, loud, easy to spot (DetectMul > 1 widens the effective
	// sensor range against them) — and they see through their own Sensors
	// like any seer.
	qV := sys.vehFilter.Query()
	for qV.Next() {
		ent := qV.Entity()
		veh, pos, mot, sensors, aware, faction := qV.Get()
		vspec := components.SpecForVehicle(veh.Kind)
		wx := float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
		wz := float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z
		audio := vspec.NoiseRadiusM * vehIdleNoiseMul
		if mot.Speed > detectStillSpeed || mot.Speed < -detectStillSpeed {
			audio = vspec.NoiseRadiusM
		}
		det := sys.dtMap.Get(ent)
		meter := [components.FactionCount]float32{1, 1, 1, 1}
		if det != nil {
			meter = det.Meter
		}
		sys.unitsBuf = append(sys.unitsBuf, contactUnit{
			ent: ent, pos: *pos, chunk: pos.Chunk,
			x: wx, z: wz, targetY: pos.Local.Y + vspec.BoxHgt*0.6,
			faction: faction.ID, dimMask: components.DimVehicle,
			concealment: vspec.DetectMul * sys.smokeMulAt(wx, wz), audioRadius: audio,
			emitRange:  sensors.EmitRangeM(),
			shotHeardM: sys.gunshotReach(ent),
			meter:      meter, det: det,
		})
		sys.seersBuf = append(sys.seersBuf, contactSeer{
			ent: ent, pos: *pos,
			x: wx, z: wz, eyeY: pos.Local.Y + vspec.BoxHgt,
			fwdX: float32(math.Sin(float64(mot.Yaw))),
			fwdZ: float32(math.Cos(float64(mot.Yaw))),
			yaw:  mot.Yaw, faction: faction.ID,
			sensors: sensors, aware: aware, maxRange: passiveMaxRange(sensors),
			netOK: sys.netOK(ent),
		})
	}
	qV.Close()

	clear(sys.wallsByChunk)
	qW := sys.wallFilter.Query()
	for qW.Next() {
		pos, w := qW.Get()
		doorState := components.DoorClosed
		if d := sys.doorMap.Get(qW.Entity()); d != nil {
			doorState = d.State
		}
		sys.wallsByChunk[pos.Chunk] = append(sys.wallsByChunk[pos.Chunk], makeLosWall(*pos, *w, doorState))
	}
	qW.Close()

	snapshotHeightmaps(sys.indexRes.Get(), sys.hmMap, sys.heightmaps)
	sys.snapshotAircraft()
	sys.buildDetectGroups()

	units := sys.unitsBuf
	seers := sys.seersBuf
	groups := sys.groupsBuf
	wallsByChunk := sys.wallsByChunk
	hm := sys.heightmaps
	elapsed := sys.elapsed

	// Drained in worker order so contact creation order is deterministic.
	for i := range sys.workerContacts {
		sys.workerContacts[i] = sys.workerContacts[i][:0]
		sys.workerMeter[i] = sys.workerMeter[i][:0]
	}
	sys.pool.ParallelForIndexed(len(groups), func(chunkIdx, start, end int) {
		if chunkIdx >= len(sys.workerContacts) {
			chunkIdx = 0
		}
		local := sys.workerContacts[chunkIdx]
		for i := start; i < end; i++ {
			local = processDetectGroup(int32(i), &groups[i], seers, units, wallsByChunk, hm, elapsed, dt, local, &sys.workerMeter[chunkIdx])
		}
		sys.workerContacts[chunkIdx] = local
	})
	for i := range sys.workerContacts {
		sys.contactsBuf = append(sys.contactsBuf, sys.workerContacts[i]...)
	}
	sys.applyDetectMeters(dt)
}

// applyDetectMeters is the serial meter pass: max fill per (target, faction)
// wins, a 0→1 crossing fires the sighting, unobserved slots decay.
func (sys *ContactSystem) applyDetectMeters(dt float32) {
	units := sys.unitsBuf
	seers := sys.seersBuf
	groups := sys.groupsBuf
	n := len(units)
	if cap(sys.meterFill) < n {
		sys.meterFill = make([][components.FactionCount]float32, n)
		sys.meterSrc = make([][components.FactionCount]int32, n)
	}
	sys.meterFill = sys.meterFill[:n]
	sys.meterSrc = sys.meterSrc[:n]
	for i := 0; i < n; i++ {
		sys.meterFill[i] = [components.FactionCount]float32{}
		sys.meterSrc[i] = [components.FactionCount]int32{-1, -1, -1, -1}
	}
	sys.meterEvents = sys.meterEvents[:0]
	for w := range sys.workerMeter {
		sys.meterEvents = append(sys.meterEvents, sys.workerMeter[w]...)
	}
	for ei := range sys.meterEvents {
		ev := &sys.meterEvents[ei]
		if ev.fill > sys.meterFill[ev.target][ev.faction] {
			sys.meterFill[ev.target][ev.faction] = ev.fill
			sys.meterSrc[ev.target][ev.faction] = int32(ei)
		}
	}
	for i := 0; i < n; i++ {
		det := units[i].det
		if det == nil {
			continue
		}
		for fac := 0; fac < components.FactionCount; fac++ {
			fill := sys.meterFill[i][fac]
			m := det.Meter[fac]
			if fill > 0 {
				nm := m + fill
				if nm > 1 {
					nm = 1
				}
				det.Meter[fac] = nm
				if m < 1 && nm >= 1 {
					ev := &sys.meterEvents[sys.meterSrc[i][fac]]
					g := &groups[ev.group]
					sp := &seers[ev.spotter]
					if units[i].faction != sp.faction {
						recordSighting(sp.aware, units[i].ent, units[i].pos, sys.elapsed, components.AwareDirect)
						for _, mi := range g.members {
							if mi == ev.spotter || seers[mi].ent == units[i].ent {
								continue
							}
							if !seers[mi].netOK && !inVoice(sp, &seers[mi]) {
								continue
							}
							recordSighting(seers[mi].aware, units[i].ent, units[i].pos, sys.elapsed, 0)
						}
					}
					sys.contactsBuf = appendContactIfHostile(sys.contactsBuf, *sp, units[i], ev.closeLOS)
				}
			} else if m > 0 {
				m -= detectDecayPerSec * dt
				if m < 0 {
					m = 0
				}
				det.Meter[fac] = m
			}
		}
	}
}

// Altitude shapes an airframe's signature in opposite directions, which is the
// whole point of flying low. Down among the clutter it is hard to pick out and
// deafening; up in clear air it is conspicuous and only a distant drone.
func airConcealMul(band components.AltBand) float32 {
	switch band {
	case components.AltBandNOE:
		return 0.55
	case components.AltBandLow:
		return 1.0
	case components.AltBandMedium:
		return 1.25
	}
	return 1.4
}

func airNoiseMul(band components.AltBand) float32 {
	switch band {
	case components.AltBandNOE:
		return 1.0
	case components.AltBandLow:
		return 0.6
	}
	return 0.35
}

// snapshotAircraft adds every airframe to both pools. Runs after the heightmap
// snapshot because the altitude band — which drives both signature halves — is
// AGL, and AGL needs the ground under the airframe.
func (sys *ContactSystem) snapshotAircraft() {
	q := sys.airFilter.Query()
	for q.Next() {
		ent := q.Entity()
		ac, pos, mot, sensors, aware, faction := q.Get()
		spec := components.SpecForAircraft(ac.Kind)
		wx := float32(pos.Chunk.X)*components.ChunkSize + pos.Local.X
		wz := float32(pos.Chunk.Z)*components.ChunkSize + pos.Local.Z
		band := components.AltBandOf(pos.Local.Y - terrainHeightAt(sys.heightmaps, wx, wz))
		det := sys.dtMap.Get(ent)
		meter := [components.FactionCount]float32{1, 1, 1, 1}
		if det != nil {
			meter = det.Meter
		}
		sys.unitsBuf = append(sys.unitsBuf, contactUnit{
			ent: ent, pos: *pos, chunk: pos.Chunk,
			x: wx, z: wz, targetY: pos.Local.Y,
			faction: faction.ID, dimMask: components.DimAir,
			concealment: spec.DetectMul * airConcealMul(band) * sys.smokeMulAt(wx, wz),
			audioRadius: spec.NoiseRadiusM * airNoiseMul(band),
			emitRange:   sensors.EmitRangeM(),
			shotHeardM:  sys.gunshotReach(ent),
			meter:       meter, det: det,
		})
		sys.seersBuf = append(sys.seersBuf, contactSeer{
			ent: ent, pos: *pos,
			x: wx, z: wz, eyeY: pos.Local.Y,
			fwdX: float32(math.Sin(float64(mot.Yaw))),
			fwdZ: float32(math.Cos(float64(mot.Yaw))),
			yaw:  mot.Yaw, faction: faction.ID,
			sensors: sensors, aware: aware, maxRange: passiveMaxRange(sensors),
			netOK: sys.netOK(ent),
		})
	}
	q.Close()
}

// PassiveSensorProfile is the IMAGING reach of a sensor set: the longest LIVE
// non-ESM channel, plus its curve. ESM is excluded because it detects
// emissions, not bodies — letting its kilometres count would drag every
// candidate on the map through a raycast the optics could never make, and
// would draw the hold-V fan a visibility arc the hull does not have.
//
// Exported because the LOS preview promises "the same predicates as
// ContactSystem" and can only keep that promise by asking the same function.
func PassiveSensorProfile(s *components.Sensors) (float32, components.FalloffKind) {
	best := float32(0)
	falloff := components.FalloffLinear
	for i := uint8(0); i < s.Count; i++ {
		c := &s.Channels[i]
		if c.Kind == components.SensorESM || !s.ChannelOn(i) {
			continue
		}
		if c.BaseRangeM > best {
			best = c.BaseRangeM
			falloff = c.FalloffKind
		}
	}
	return best, falloff
}

func passiveMaxRange(s *components.Sensors) float32 {
	r, _ := PassiveSensorProfile(s)
	return r
}

// buildDetectGroups groups seers by squad (solo unit = its own group), in
// seer order so processing stays deterministic.
func (sys *ContactSystem) buildDetectGroups() {
	sys.groupsBuf = sys.groupsBuf[:0]
	clear(sys.groupIdx)
	seers := sys.seersBuf
	for i := range seers {
		var sq ecs.Entity
		if sm := sys.squadMemberMap.Get(seers[i].ent); sm != nil {
			sq = sm.Squad
		}
		if sq == (ecs.Entity{}) {
			sys.groupsBuf = append(sys.groupsBuf, detectGroup{members: []int32{int32(i)}})
			continue
		}
		gi, ok := sys.groupIdx[sq]
		if !ok {
			gi = int32(len(sys.groupsBuf))
			sys.groupIdx[sq] = gi
			sys.groupsBuf = append(sys.groupsBuf, detectGroup{})
		}
		g := &sys.groupsBuf[gi]
		g.members = append(g.members, int32(i))
	}
	// The cull gate has to clear the loudest thing on the field, not just the
	// sharpest pair of eyes. Audio ignores range and facing, so a candidate
	// culled before the audio test is a candidate that cannot be heard at all —
	// which silently deleted a helicopter's whole signature (400 m of rotor
	// noise against a 64 m gate) and had already been eating most of a hull's.
	loudest := float32(0)
	for i := range sys.unitsBuf {
		if r := sys.unitsBuf[i].audioRadius; r > loudest {
			loudest = r
		}
	}
	for gi := range sys.groupsBuf {
		g := &sys.groupsBuf[gi]
		var sx, sz float32
		for k, mi := range g.members {
			s := &seers[mi]
			sx += s.x
			sz += s.z
			if k == 0 {
				g.minCX, g.maxCX = s.pos.Chunk.X, s.pos.Chunk.X
				g.minCZ, g.maxCZ = s.pos.Chunk.Z, s.pos.Chunk.Z
				continue
			}
			g.minCX = min(g.minCX, s.pos.Chunk.X)
			g.maxCX = max(g.maxCX, s.pos.Chunk.X)
			g.minCZ = min(g.minCZ, s.pos.Chunk.Z)
			g.maxCZ = max(g.maxCZ, s.pos.Chunk.Z)
		}
		inv := 1 / float32(len(g.members))
		g.cx = sx * inv
		g.cz = sz * inv
		reach := loudest
		if reach < visionMaxRange {
			reach = visionMaxRange
		}
		visual := float32(0)
		spreadSq := float32(0)
		for _, mi := range g.members {
			s := &seers[mi]
			dx := s.x - g.cx
			dz := s.z - g.cz
			if d := dx*dx + dz*dz; d > spreadSq {
				spreadSq = d
			}
			if s.maxRange > reach {
				reach = s.maxRange
			}
			if s.maxRange > visual {
				visual = s.maxRange
			}
		}
		// The wall window and the per-pair chunk gate are ONE number, because
		// a pair may only be tested against walls that were gathered. Fixed at
		// one chunk it was a silent ceiling on range itself: everything in the
		// game saw 40-65 m, so a 64 m window never bit, and the first sensor
		// that outreached it (a 420 m radar) was quietly clipped to 76 m.
		// Sized from the VISUAL reach only — audio needs no walls, and letting
		// a helicopter's 400 m of noise set this would widen the window for
		// every rifle squad on the map.
		g.span = int32(math.Ceil(float64(visual / components.ChunkSize)))
		if g.span < 1 {
			g.span = 1
		}
		g.cullR = float32(math.Sqrt(float64(spreadSq))) + reach
	}
}

// processDetectGroup is the per-group hot loop: one cull gate per candidate,
// audio per member, then up to two best-positioned members attempt the full
// visual pipeline; a success is shared to every member's Awareness. The
// group's worker owns all member Awareness writes.
func processDetectGroup(
	gIdx int32,
	g *detectGroup,
	seers []contactSeer,
	units []contactUnit,
	wallsByChunk map[components.ChunkCoord][]losWall,
	hm map[components.ChunkCoord][]float32,
	elapsed, dt float32,
	out []contactRec,
	meterOut *[]meterEvent,
) []contactRec {
	var localWalls []losWall
	for cz := g.minCZ - g.span; cz <= g.maxCZ+g.span; cz++ {
		for cx := g.minCX - g.span; cx <= g.maxCX+g.span; cx++ {
			localWalls = append(localWalls, wallsByChunk[components.ChunkCoord{X: cx, Z: cz}]...)
		}
	}
	cullSq := g.cullR * g.cullR

	for ci := range units {
		cand := &units[ci]
		dxc := cand.x - g.cx
		dzc := cand.z - g.cz
		if dxc*dxc+dzc*dzc > cullSq {
			continue
		}

		// Audio bubble — personal, ignores FOV / LOS.
		if cand.audioRadius > 0 {
			audSq := cand.audioRadius * cand.audioRadius
			for _, mi := range g.members {
				s := &seers[mi]
				if s.ent == cand.ent {
					continue
				}
				dx := cand.x - s.x
				dz := cand.z - s.z
				dSq := dx*dx + dz*dz
				if dSq < 1e-4 || dSq > audSq {
					continue
				}
				if cand.faction != s.faction {
					recordSighting(s.aware, cand.ent, cand.pos, elapsed, components.AwareDirect)
				}
				out = appendContactIfHostile(out, *s, *cand, false)
			}
		}

		// Visual: pick up to two best origins (near + roughly facing wins).
		var b0, b1 int32 = -1, -1
		s0, s1 := float32(math.MaxFloat32), float32(math.MaxFloat32)
		for _, mi := range g.members {
			s := &seers[mi]
			if s.ent == cand.ent {
				continue
			}
			dcx := cand.chunk.X - s.pos.Chunk.X
			dcz := cand.chunk.Z - s.pos.Chunk.Z
			if dcx < -g.span || dcx > g.span || dcz < -g.span || dcz > g.span {
				continue
			}
			dx := cand.x - s.x
			dz := cand.z - s.z
			dSq := dx*dx + dz*dz
			if dSq < 1e-4 || dSq > s.maxRange*s.maxRange {
				continue
			}
			score := dSq
			if dx*s.fwdX+dz*s.fwdZ < 0 {
				score *= 4
			}
			if score < s0 {
				b1, s1 = b0, s0
				b0, s0 = mi, score
			} else if score < s1 {
				b1, s1 = mi, score
			}
		}
		spotter := int32(-1)
		var mag float32
		for _, mi := range [2]int32{b0, b1} {
			if mi < 0 {
				continue
			}
			if m, ok := visualMagnitude(&seers[mi], cand, localWalls, hm); ok {
				spotter, mag = mi, m
				break
			}
		}
		if spotter < 0 {
			continue
		}
		sp := &seers[spotter]
		dx := cand.x - sp.x
		dz := cand.z - sp.z
		closeLOS := dx*dx+dz*dz <= contactCloseRangeM*contactCloseRangeM
		if cand.meter[sp.faction] >= 1 {
			if cand.faction != sp.faction {
				recordSighting(sp.aware, cand.ent, cand.pos, elapsed, components.AwareDirect)
				for _, mi := range g.members {
					if mi == spotter {
						continue
					}
					s := &seers[mi]
					// A sighting somebody else made travels by RADIO, or by
					// shouting if he is close enough. Off the net a strung-out
					// squad loses the shared picture; a tight one keeps it.
					if s.ent == cand.ent {
						continue
					}
					if !s.netOK && !inVoice(sp, s) {
						continue
					}
					recordSighting(s.aware, cand.ent, cand.pos, elapsed, 0)
				}
			}
			out = appendContactIfHostile(out, *sp, *cand, closeLOS)
		}
		*meterOut = append(*meterOut, meterEvent{
			group: gIdx, spotter: spotter, target: int32(ci),
			faction: sp.faction, closeLOS: closeLOS,
			fill: detectFillRate * mag * mag * dt,
		})
	}
	return out
}

// visualMagnitude runs the per-pair sensor pipeline (channel gates → walls
// → terrain) and returns the best channel magnitude.
func visualMagnitude(s *contactSeer, cand *contactUnit, walls []losWall, hm map[components.ChunkCoord][]float32) (float32, bool) {
	dx := cand.x - s.x
	dz := cand.z - s.z
	d := float32(math.Sqrt(float64(dx*dx + dz*dz)))
	if d < 1e-2 {
		return 0, false
	}
	dotF := (dx*s.fwdX + dz*s.fwdZ) / d
	angle := float32(math.Acos(float64(clamp32(dotF, -1, 1))))
	best := float32(0)
	for i := uint8(0); i < s.sensors.Count; i++ {
		c := &s.sensors.Channels[i]
		// ESM hears emissions, not bodies — it has its own pass.
		if c.Kind == components.SensorESM || !s.sensors.ChannelOn(i) {
			continue
		}
		if c.DetectMask&cand.dimMask == 0 {
			continue
		}
		if d > c.BaseRangeM {
			continue
		}
		strength := components.Falloff(c.FalloffKind, d, c.BaseRangeM)
		strength *= components.FacingMul(angle, c.Facing)
		strength *= cand.concealment
		if strength > best {
			best = strength
		}
	}
	if best < detectMinMagnitude {
		return 0, false
	}
	if best > 1 {
		best = 1
	}
	// Walls are XZ segments with no top. Once the pair is separated vertically
	// by more than any building is tall, one of the two is looking over them.
	dy := cand.targetY - s.eyeY
	if dy < 0 {
		dy = -dy
	}
	if dy <= airWallClearM && anyLosWallBlocks(walls, s.x, s.z, cand.x, cand.z) {
		return 0, false
	}
	if terrainBlocksLOS(hm, s.x, s.z, s.eyeY, cand.x, cand.z, cand.targetY) {
		return 0, false
	}
	return best, true
}

// appendContactIfHostile records a hostile-side detection for serial upsert.
// PlayerFaction observers feed the registry; other observers' awareness was
// already written by recordSighting but we don't track per-faction contacts
// in Phase 18.5 (single-player).
func appendContactIfHostile(out []contactRec, s contactSeer, cand contactUnit, closeLOS bool) []contactRec {
	if s.faction != components.FactionPlayer {
		return out
	}
	if cand.faction == components.FactionPlayer {
		return out
	}
	return append(out, contactRec{
		observer:  s.ent,
		target:    cand.ent,
		pos:       cand.pos,
		dim:       components.DimensionFromMask(cand.dimMask),
		closeLOS:  closeLOS,
		obsFactID: s.faction,
	})
}

// applyContactUpsert is the serial post-pass — drains contactsBuf, creates
// new Contact entities or refreshes existing ones via ContactRegistry.
// Source promotion: Sensor on first creation, CloseRangeID on close-LOS pass
// (skipped if PlayerSet wins). CombatEvidence happens in applyCombatEvidence.
func (sys *ContactSystem) applyContactUpsert() {
	reg := sys.registryRes.Get()
	if reg.Tracked == nil {
		// Resource was added by main but never initialized — guard.
		*reg = components.NewContactRegistry()
	}
	for i := range sys.contactsBuf {
		rec := &sys.contactsBuf[i]
		if !sys.worldRef.Alive(rec.target) {
			delete(reg.Tracked, rec.target)
			continue
		}
		ent, ok := reg.Tracked[rec.target]
		if ok && sys.worldRef.Alive(ent) {
			c := sys.contactMap.Get(ent)
			if c == nil {
				continue
			}
			c.EstimatedPos = rec.pos
			c.LastSeenTime = sys.elapsed
			// The second source ESM was waiting for: a direction becomes a
			// point, and stays one.
			c.BearingOnly = false
			sys.promoteOnRefresh(ent, c, rec)
			continue
		}
		newEnt := sys.worldRef.NewEntity()
		affil := components.AffilUnknown
		dim := components.DimUnknownClass
		source := components.SourceSensor
		if rec.closeLOS {
			affil = sys.affilFromTargetFaction(rec.target)
			dim = rec.dim
			source = components.SourceCloseRangeID
		}
		c := components.Contact{
			Tracked:        rec.target,
			Observer:       rec.obsFactID,
			EstimatedPos:   rec.pos,
			LastSeenTime:   sys.elapsed,
			PerceivedAffil: affil,
			PerceivedDim:   dim,
			Source:         source,
		}
		sys.contactMap.Add(newEnt, &c)
		reg.Tracked[rec.target] = newEnt
		sys.pushContactEvent(rec)
	}
	sys.contactsBuf = sys.contactsBuf[:0]
	sys.enforceContactCap(reg)
}

// pushContactEvent logs a fresh track the player's own sensors just made.
// Write-only: nothing in the sim reads the log, so the attention layer gets
// its first-contact signal without any behaviour riding on it. Other
// factions' tracks are their business, not the player's notification feed.
func (sys *ContactSystem) pushContactEvent(rec *contactRec) {
	if rec.obsFactID != components.FactionPlayer {
		return
	}
	log := sys.eventLogRes.Get()
	if log == nil {
		return
	}
	text := "New contact"
	if rec.closeLOS {
		text = "Enemy spotted"
	}
	log.Push(components.EventEntry{
		Kind: components.EventEnemyContact,
		At:   sys.elapsed,
		Pos:  rec.pos,
		Text: text,
	})
}

// enforceContactCap evicts contacts past contactCap: oldest Unknown first,
// then oldest others; PlayerSet pins survive. Candidates come in Filter
// order and the sort is stable, so eviction is deterministic.
func (sys *ContactSystem) enforceContactCap(reg *components.ContactRegistry) {
	over := len(reg.Tracked) - contactCap
	if over <= 0 {
		return
	}
	type victim struct {
		ent     ecs.Entity
		tracked ecs.Entity
		seen    float32
		unknown bool
	}
	victims := make([]victim, 0, len(reg.Tracked))
	q := sys.contactFilter.Query()
	for q.Next() {
		ent := q.Entity()
		if sys.contactPlayerSet.Has(ent) {
			continue
		}
		c := q.Get()
		victims = append(victims, victim{
			ent: ent, tracked: c.Tracked, seen: c.LastSeenTime,
			unknown: c.PerceivedAffil == components.AffilUnknown,
		})
	}
	sort.SliceStable(victims, func(a, b int) bool {
		if victims[a].unknown != victims[b].unknown {
			return victims[a].unknown
		}
		return victims[a].seen < victims[b].seen
	})
	if over > len(victims) {
		over = len(victims)
	}
	for i := 0; i < over; i++ {
		delete(reg.Tracked, victims[i].tracked)
		sys.worldRef.RemoveEntity(victims[i].ent)
	}
}

// promoteOnRefresh upgrades an existing contact when a close-LOS observation
// arrives. PlayerSet wins always; CloseRangeID otherwise dominates Sensor /
// CombatEvidence.
func (sys *ContactSystem) promoteOnRefresh(ent ecs.Entity, c *components.Contact, rec *contactRec) {
	if sys.contactPlayerSet.Has(ent) {
		return
	}
	if rec.closeLOS && c.Source < components.SourceCloseRangeID {
		c.PerceivedAffil = sys.affilFromTargetFaction(rec.target)
		c.PerceivedDim = rec.dim
		c.Source = components.SourceCloseRangeID
		return
	}
	// Sensor refresh only updates dim if unknown.
	if c.PerceivedDim == components.DimUnknownClass {
		c.PerceivedDim = rec.dim
	}
}

// affilFromTargetFaction maps ground-truth faction → Affiliation (used by
// CloseRangeID / CombatEvidence). Player never gets contacted from his own
// side; this returns Friend just in case for completeness.
func (sys *ContactSystem) affilFromTargetFaction(target ecs.Entity) components.Affiliation {
	f := sys.factionMap.Get(target)
	if f == nil {
		return components.AffilUnknown
	}
	switch f.ID {
	case components.FactionPlayer:
		return components.AffilFriend
	case components.FactionEnemyRed:
		return components.AffilHostile
	case components.FactionNeutral:
		return components.AffilNeutral
	case components.FactionWildlife:
		return components.AffilNeutral
	}
	return components.AffilUnknown
}

// audioEmissionRadius - moving units broadcast their position regardless of
// FOV/LOS within the bubble (legacy behaviour from VisionSystem).
func (sys *ContactSystem) audioEmissionRadius(unit ecs.Entity, motionSpeed float32) float32 {
	if motionSpeed < 0.2 {
		return 0
	}
	pace := components.PaceWalk
	posture := components.PostureStandard
	if sm := sys.squadMemberMap.Get(unit); sm != nil && sm.Squad != (ecs.Entity{}) {
		if mp := sys.movementProfileMap.Get(sm.Squad); mp != nil {
			pace = mp.Pace
			posture = mp.Posture
		}
	}
	mul := audioPaceMul[pace]
	if posture == components.PostureQuiet {
		mul *= audioPostureQuiet
	}
	r := audioBaseRadius * mul
	if r > visionMaxRange {
		r = visionMaxRange
	}
	return r
}

// smokeMulAt returns smokeConcealMul if (wx, wz) sits inside any live smoke
// field this tick, else 1.0. Multiple overlapping fields do not stack.
func (sys *ContactSystem) smokeMulAt(wx, wz float32) float32 {
	for i := range sys.smokeBuf {
		s := &sys.smokeBuf[i]
		dx := s.x - wx
		dz := s.z - wz
		if dx*dx+dz*dz <= s.rSq {
			return smokeConcealMul
		}
	}
	return 1
}

func clamp32(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// recordSighting refreshes the FIFO entry for `target`, evicting the oldest
// slot for new targets. A Direct flag never downgrades to Shared within the
// same timestamp.
// voiceShareM is how far a sighting carries without any radio at all. Without
// it the gate below is a cliff: a squad whose radioman dies is capped at half
// quality, which is under Green at ANY distance from a relay, so one casualty
// would blind the squad permanently. "Только голосом" is the model's own phrase
// for what is left — this is that phrase as a number.
const voiceShareM float32 = 25.0

// netOK: shared sightings ride the radio, so they need one. Green only —
// a degraded net carries orders late but not a live picture (MODEL.md 1).
func (sys *ContactSystem) netOK(ent ecs.Entity) bool {
	cmd := ent
	if m := sys.squadMemberMap.Get(ent); m != nil && m.Squad != (ecs.Entity{}) {
		cmd = m.Squad
	}
	cs := sys.commsMap.Get(cmd)
	return cs == nil || cs.Band == components.CommsGreen
}

func inVoice(a, b *contactSeer) bool {
	dx, dz := a.x-b.x, a.z-b.z
	return dx*dx+dz*dz <= voiceShareM*voiceShareM
}

func recordSighting(aware *components.Awareness, target ecs.Entity, pos components.WorldPos, t float32, flags uint8) {
	for i := range aware.LastSeen {
		if aware.LastSeen[i].Time != 0 && aware.LastSeen[i].Target == target {
			e := &aware.LastSeen[i]
			if e.Time == t {
				flags |= e.Flags
			}
			e.Pos = pos
			e.Time = t
			e.Flags = flags
			return
		}
	}
	oldest := 0
	oldestT := aware.LastSeen[0].Time
	for i := 1; i < components.AwarenessSlots; i++ {
		if aware.LastSeen[i].Time == 0 {
			oldest = i
			break
		}
		if aware.LastSeen[i].Time < oldestT {
			oldestT = aware.LastSeen[i].Time
			oldest = i
		}
	}
	aware.LastSeen[oldest] = components.AwarenessEntry{Target: target, Pos: pos, Time: t, Flags: flags}
}
