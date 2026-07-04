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

// dimensionMaskFor classifies an entity into its sensor bitmask. Phase 18.5
// has only infantry (units) so this is uniform; vehicles in Phase 19 plug in
// via a Vehicle component check before this fallback.
func dimensionMaskFor(_ ecs.Entity) components.DimensionMask {
	return components.DimInfantry
}

// runDetectPass collects unit/seer snapshots, buckets walls + heightmaps by
// chunk, groups seers by squad, then runs per-group detection in parallel.
// A group's worker owns all its members' Awareness writes; contactRecs go to
// per-worker slices drained in order for the serial upsert.
func (sys *ContactSystem) runDetectPass() {
	sys.unitsBuf = sys.unitsBuf[:0]
	sys.seersBuf = sys.seersBuf[:0]
	sys.contactsBuf = sys.contactsBuf[:0]

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
		sys.unitsBuf = append(sys.unitsBuf, contactUnit{
			ent: ent, pos: *pos, chunk: pos.Chunk,
			x: wx, z: wz, targetY: pos.Local.Y + spec.TargetCenterY,
			faction: faction.ID, dimMask: dimensionMaskFor(ent),
			concealment: stanceConcealmentMul(stance), audioRadius: audio,
		})
		maxR := float32(0)
		for i := uint8(0); i < sensors.Count; i++ {
			if r := sensors.Channels[i].BaseRangeM; r > maxR {
				maxR = r
			}
		}
		sys.seersBuf = append(sys.seersBuf, contactSeer{
			ent: ent, pos: *pos,
			x: wx, z: wz, eyeY: pos.Local.Y + spec.EyeHeight,
			fwdX: float32(math.Sin(float64(mot.Yaw))),
			fwdZ: float32(math.Cos(float64(mot.Yaw))),
			yaw:  mot.Yaw, faction: faction.ID,
			sensors: sensors, aware: aware, maxRange: maxR,
		})
	}
	q.Close()

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
	}
	sys.pool.ParallelForIndexed(len(groups), func(chunkIdx, start, end int) {
		if chunkIdx >= len(sys.workerContacts) {
			chunkIdx = 0
		}
		local := sys.workerContacts[chunkIdx]
		for i := start; i < end; i++ {
			local = processDetectGroup(&groups[i], seers, units, wallsByChunk, hm, elapsed, local)
		}
		sys.workerContacts[chunkIdx] = local
	})
	for i := range sys.workerContacts {
		sys.contactsBuf = append(sys.contactsBuf, sys.workerContacts[i]...)
	}
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
		reach := visionMaxRange // audio bubbles are clamped to this
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
		}
		g.cullR = float32(math.Sqrt(float64(spreadSq))) + reach
	}
}

// processDetectGroup is the per-group hot loop: one cull gate per candidate,
// audio per member, then up to two best-positioned members attempt the full
// visual pipeline; a success is shared to every member's Awareness. The
// group's worker owns all member Awareness writes.
func processDetectGroup(
	g *detectGroup,
	seers []contactSeer,
	units []contactUnit,
	wallsByChunk map[components.ChunkCoord][]losWall,
	hm map[components.ChunkCoord][]float32,
	elapsed float32,
	out []contactRec,
) []contactRec {
	var localWalls []losWall
	for cz := g.minCZ - 1; cz <= g.maxCZ+1; cz++ {
		for cx := g.minCX - 1; cx <= g.maxCX+1; cx++ {
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
				recordSighting(s.aware, cand.ent, cand.pos, elapsed, components.AwareDirect)
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
			if dcx < -1 || dcx > 1 || dcz < -1 || dcz > 1 {
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
		for _, mi := range [2]int32{b0, b1} {
			if mi < 0 {
				continue
			}
			if visualDetect(&seers[mi], cand, localWalls, hm) {
				spotter = mi
				break
			}
		}
		if spotter < 0 {
			continue
		}
		sp := &seers[spotter]
		recordSighting(sp.aware, cand.ent, cand.pos, elapsed, components.AwareDirect)
		for _, mi := range g.members {
			if mi == spotter {
				continue
			}
			s := &seers[mi]
			if s.ent == cand.ent {
				continue
			}
			recordSighting(s.aware, cand.ent, cand.pos, elapsed, 0)
		}
		dx := cand.x - sp.x
		dz := cand.z - sp.z
		closeLOS := dx*dx+dz*dz <= contactCloseRangeM*contactCloseRangeM
		out = appendContactIfHostile(out, *sp, *cand, closeLOS)
	}
	return out
}

// visualDetect runs the full per-pair sensor pipeline: channel gates →
// walls → terrain.
func visualDetect(s *contactSeer, cand *contactUnit, walls []losWall, hm map[components.ChunkCoord][]float32) bool {
	dx := cand.x - s.x
	dz := cand.z - s.z
	d := float32(math.Sqrt(float64(dx*dx + dz*dz)))
	if d < 1e-2 {
		return false
	}
	dotF := (dx*s.fwdX + dz*s.fwdZ) / d
	angle := float32(math.Acos(float64(clamp32(dotF, -1, 1))))
	detected := false
	for i := uint8(0); i < s.sensors.Count; i++ {
		c := &s.sensors.Channels[i]
		if c.DetectMask&cand.dimMask == 0 {
			continue
		}
		if d > c.BaseRangeM {
			continue
		}
		strength := components.Falloff(c.FalloffKind, d, c.BaseRangeM)
		strength *= components.FacingMul(angle, c.Facing)
		strength *= cand.concealment
		if strength < detectionThreshold {
			continue
		}
		detected = true
		break
	}
	if !detected {
		return false
	}
	if anyLosWallBlocks(walls, s.x, s.z, cand.x, cand.z) {
		return false
	}
	return !terrainBlocksLOS(hm, s.x, s.z, s.eyeY, cand.x, cand.z, cand.targetY)
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
	}
	sys.contactsBuf = sys.contactsBuf[:0]
	sys.enforceContactCap(reg)
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
