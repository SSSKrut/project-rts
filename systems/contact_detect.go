package systems

import (
	"math"

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

// runDetectPass collects all unit/seer snapshots once, buckets walls by
// chunk, then runs per-seer detection in parallel. Each worker writes to its
// own seer's Awareness + appends to a per-worker contactRec slice; the
// orchestrator then drains those into ContactSystem.contactsBuf for serial
// upsert in applyContactUpsert.
func (sys *ContactSystem) runDetectPass() {
	sys.unitsBuf = sys.unitsBuf[:0]
	sys.seersBuf = sys.seersBuf[:0]
	sys.contactsBuf = sys.contactsBuf[:0]

	q := sys.unitFilter.Query()
	for q.Next() {
		ent := q.Entity()
		_, pos, mot, sensors, aware, faction := q.Get()
		audio := sys.audioEmissionRadius(ent, mot.Speed)
		conceal := float32(1.0)
		if st := sys.stanceMap.Get(ent); st != nil {
			conceal = stanceConcealmentMul(st.Code)
		}
		sys.unitsBuf = append(sys.unitsBuf, contactUnit{
			ent: ent, pos: *pos, chunk: pos.Chunk,
			faction: faction.ID, dimMask: dimensionMaskFor(ent),
			concealment: conceal, audioRadius: audio,
		})
		maxR := float32(0)
		for i := uint8(0); i < sensors.Count; i++ {
			if r := sensors.Channels[i].BaseRangeM; r > maxR {
				maxR = r
			}
		}
		sys.seersBuf = append(sys.seersBuf, contactSeer{
			ent: ent, pos: *pos, yaw: mot.Yaw, faction: faction.ID,
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

	units := sys.unitsBuf
	seers := sys.seersBuf
	wallsByChunk := sys.wallsByChunk
	elapsed := sys.elapsed

	// Drained in worker order so contact creation order is deterministic.
	for i := range sys.workerContacts {
		sys.workerContacts[i] = sys.workerContacts[i][:0]
	}
	sys.pool.ParallelForIndexed(len(seers), func(chunkIdx, start, end int) {
		if chunkIdx >= len(sys.workerContacts) {
			chunkIdx = 0
		}
		local := sys.workerContacts[chunkIdx]
		for i := start; i < end; i++ {
			local = processContactSeer(seers[i], units, wallsByChunk, elapsed, local)
		}
		sys.workerContacts[chunkIdx] = local
	})
	for i := range sys.workerContacts {
		sys.contactsBuf = append(sys.contactsBuf, sys.workerContacts[i]...)
	}
}

// processContactSeer is the per-seer hot loop. Awareness is written directly
// (each seer owns its own slot, no shared write). Contact records returned to
// caller for serial upsert.
func processContactSeer(
	s contactSeer,
	units []contactUnit,
	wallsByChunk map[components.ChunkCoord][]losWall,
	elapsed float32,
	out []contactRec,
) []contactRec {
	var localWalls []losWall
	for dz := int32(-1); dz <= 1; dz++ {
		for dx := int32(-1); dx <= 1; dx++ {
			nb := components.ChunkCoord{X: s.pos.Chunk.X + dx, Z: s.pos.Chunk.Z + dz}
			localWalls = append(localWalls, wallsByChunk[nb]...)
		}
	}

	seerX := float32(s.pos.Chunk.X)*components.ChunkSize + s.pos.Local.X
	seerZ := float32(s.pos.Chunk.Z)*components.ChunkSize + s.pos.Local.Z
	fwdX := float32(math.Sin(float64(s.yaw)))
	fwdZ := float32(math.Cos(float64(s.yaw)))
	maxRng := s.maxRange

	for _, cand := range units {
		if cand.ent == s.ent {
			continue
		}
		dcx := cand.chunk.X - s.pos.Chunk.X
		if dcx < -1 || dcx > 1 {
			continue
		}
		dcz := cand.chunk.Z - s.pos.Chunk.Z
		if dcz < -1 || dcz > 1 {
			continue
		}
		candX := float32(cand.pos.Chunk.X)*components.ChunkSize + cand.pos.Local.X
		candZ := float32(cand.pos.Chunk.Z)*components.ChunkSize + cand.pos.Local.Z
		dx := candX - seerX
		dz := candZ - seerZ
		dSq := dx*dx + dz*dz
		if dSq < 1e-4 {
			continue
		}

		// Audio bubble - ignores FOV and LOS, satisfied any sensor channel.
		audibleSq := cand.audioRadius * cand.audioRadius
		if cand.audioRadius > 0 && dSq <= audibleSq {
			recordSighting(s.aware, cand.ent, cand.pos, elapsed)
			out = appendContactIfHostile(out, s, cand, false)
			continue
		}

		if dSq > maxRng*maxRng {
			continue
		}
		d := float32(math.Sqrt(float64(dSq)))
		invD := 1 / d
		dotF := (dx*fwdX + dz*fwdZ) * invD
		angle := float32(math.Acos(float64(clamp32(dotF, -1, 1))))

		// Walk channels — first satisfying channel wins.
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
			continue
		}
		if anyLosWallBlocks(localWalls, seerX, seerZ, candX, candZ) {
			continue
		}
		recordSighting(s.aware, cand.ent, cand.pos, elapsed)
		closeLOS := d <= contactCloseRangeM
		out = appendContactIfHostile(out, s, cand, closeLOS)
	}
	return out
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
// slot for new targets.
func recordSighting(aware *components.Awareness, target ecs.Entity, pos components.WorldPos, t float32) {
	for i := range aware.LastSeen {
		if aware.LastSeen[i].Time != 0 && aware.LastSeen[i].Target == target {
			aware.LastSeen[i].Pos = pos
			aware.LastSeen[i].Time = t
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
	aware.LastSeen[oldest] = components.AwarenessEntry{Target: target, Pos: pos, Time: t}
}
