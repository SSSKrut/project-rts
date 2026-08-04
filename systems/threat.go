package systems

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
	"rts-go/core"
)

// ThreatSystem is the per-unit Threat aggregator. Each tick: decay every
// channel, drain DangerBuffer (Strength → channel + weighted ThreatDir),
// clamp to [0, 1], recompute Total + State, reset ring for next tick.
type ThreatSystem struct {
	filter    *ecs.Filter4[components.Unit, components.Threat, components.DangerBuffer, components.WorldPos]
	vehFilter *ecs.Filter4[components.Vehicle, components.Threat, components.DangerBuffer, components.WorldPos]
	lastTick  float32

	// Unsafe-area layer (P3): blasts that land close together in time and
	// space raise a zone entity, and everyone standing in a live zone keeps
	// taking DangerUnsafeArea until they walk out of it.
	blastFilter  *ecs.Filter2[components.BlastMark, components.WorldPos]
	unsafeFilter *ecs.Filter2[components.UnsafeArea, components.WorldPos]
	posMap       *ecs.Map[components.WorldPos]
	unsafeMap    *ecs.Map[components.UnsafeArea]
	world        *ecs.World
	blasts       []blastSnap
	zones        []zoneSnap
	pendingZones []zoneSnap
}

type blastSnap struct {
	x, z, severity float32
}

type zoneSnap struct {
	pos      components.WorldPos
	x, z     float32
	radius   float32
	severity float32
}

func NewThreatSystem() *ThreatSystem {
	return &ThreatSystem{}
}

func (sys *ThreatSystem) InitUI(w *ecs.World) {
	sys.filter = ecs.NewFilter4[components.Unit, components.Threat, components.DangerBuffer, components.WorldPos](w)
	sys.vehFilter = ecs.NewFilter4[components.Vehicle, components.Threat, components.DangerBuffer, components.WorldPos](w)
	sys.blastFilter = ecs.NewFilter2[components.BlastMark, components.WorldPos](w)
	sys.unsafeFilter = ecs.NewFilter2[components.UnsafeArea, components.WorldPos](w)
	sys.posMap = ecs.NewMap[components.WorldPos](w)
	sys.unsafeMap = ecs.NewMap[components.UnsafeArea](w)
	sys.world = w
}

func (ThreatSystem) Name() string { return "threat" }

func (ThreatSystem) LODPolicy() core.LODPolicy {
	// Active-only: filters are not tier-scoped (see ContactSystem note).
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

// PostLoad re-seeds lastTick so the first post-load dt is the same float32
// subtraction a continuous run would compute.
func (sys *ThreatSystem) PostLoad(simNow float64) { sys.lastTick = float32(simNow) }

func (sys *ThreatSystem) Update(ctx core.UpdateContext) {
	now := float32(ctx.SimNow)
	dt := now - sys.lastTick
	if dt < 0 || dt > 1.0 {
		dt = float32(ctx.Delta.Seconds())
	}
	sys.lastTick = now

	sys.snapshotZones()

	q := sys.filter.Query()
	for q.Next() {
		_, threat, buf, pos := q.Get()
		sys.pushZoneDanger(buf, pos, now, dt)
		tickThreat(threat, buf, pos, dt)
	}

	// Vehicles run the same aggregator so their reflexes read a live Threat.
	qv := sys.vehFilter.Query()
	for qv.Next() {
		_, threat, buf, pos := qv.Get()
		sys.pushZoneDanger(buf, pos, now, dt)
		tickThreat(threat, buf, pos, dt)
	}

	sys.spawnZones(now)
}

// snapshotZones caches live zones and raises new ones where blast marks have
// clustered. Spawning is deferred to spawnZones — Ark forbids archetype
// changes while the unit query is live.
func (sys *ThreatSystem) snapshotZones() {
	sys.zones = sys.zones[:0]
	qz := sys.unsafeFilter.Query()
	for qz.Next() {
		area, pos := qz.Get()
		x, z := worldXZ(*pos)
		sys.zones = append(sys.zones, zoneSnap{
			pos: *pos, x: x, z: z, radius: area.Radius, severity: area.Severity,
		})
	}

	sys.blasts = sys.blasts[:0]
	qb := sys.blastFilter.Query()
	for qb.Next() {
		mark, pos := qb.Get()
		x, z := worldXZ(*pos)
		sys.blasts = append(sys.blasts, blastSnap{x: x, z: z, severity: mark.Severity})
	}
	if len(sys.blasts) < components.UnsafeMinBlasts {
		return
	}

	// One blast is an accident, several in one place is a barrage. Seed on
	// the first mark that has enough company and is not already covered.
	sys.pendingZones = sys.pendingZones[:0]
	for i := range sys.blasts {
		b := &sys.blasts[i]
		if sys.zoneCovers(b.x, b.z) {
			continue
		}
		var sumX, sumZ, sumSev, n float32
		maxD := float32(0)
		for j := range sys.blasts {
			o := &sys.blasts[j]
			dx, dz := o.x-b.x, o.z-b.z
			d2 := dx*dx + dz*dz
			if d2 > components.UnsafeClusterR*components.UnsafeClusterR {
				continue
			}
			sumX += o.x
			sumZ += o.z
			sumSev += o.severity
			n++
			if d := float32(math.Sqrt(float64(d2))); d > maxD {
				maxD = d
			}
		}
		if int(n) < components.UnsafeMinBlasts {
			continue
		}
		cx, cz := sumX/n, sumZ/n
		if sys.zoneCovers(cx, cz) {
			continue
		}
		radius := maxD + components.UnsafeAreaMinR
		sev := sumSev / n
		if sev > 1 {
			sev = 1
		}
		zp := components.WorldPos{}.Add(rl.Vector3{X: cx, Z: cz})
		sys.pendingZones = append(sys.pendingZones, zoneSnap{
			pos: zp, x: cx, z: cz, radius: radius, severity: sev,
		})
		// Cover the rest of this barrage with the zone we just decided on.
		sys.zones = append(sys.zones, sys.pendingZones[len(sys.pendingZones)-1])
	}
}

func (sys *ThreatSystem) zoneCovers(x, z float32) bool {
	for i := range sys.zones {
		zn := &sys.zones[i]
		dx, dz := x-zn.x, z-zn.z
		if dx*dx+dz*dz <= zn.radius*zn.radius {
			return true
		}
	}
	return false
}

// pushZoneDanger keeps everyone standing in a live zone under pressure: the
// event repeats every tick (scaled by dt) so leaving is the only way out,
// and Pos is the zone centre so the bearing points OUT of it.
func (sys *ThreatSystem) pushZoneDanger(buf *components.DangerBuffer,
	pos *components.WorldPos, now, dt float32) {
	if len(sys.zones) == 0 || dt <= 0 {
		return
	}
	x, z := worldXZ(*pos)
	for i := range sys.zones {
		zn := &sys.zones[i]
		dx, dz := x-zn.x, z-zn.z
		if dx*dx+dz*dz > zn.radius*zn.radius {
			continue
		}
		components.PushDanger(buf, components.DangerEvent{
			Kind:     components.DangerUnsafeArea,
			Pos:      zn.pos,
			Strength: components.UnsafeDangerPerSec * zn.severity * dt,
			Time:     now,
		})
	}
}

func (sys *ThreatSystem) spawnZones(now float32) {
	for i := range sys.pendingZones {
		zn := &sys.pendingZones[i]
		e := sys.world.NewEntity()
		p := zn.pos
		sys.posMap.Add(e, &p)
		sys.unsafeMap.Add(e, &components.UnsafeArea{
			Radius:    zn.radius,
			Severity:  zn.severity,
			ExpiresAt: float64(now) + float64(components.UnsafeAreaTTL),
		})
	}
	sys.pendingZones = sys.pendingZones[:0]
}

// tickThreat applies the per-unit decay + drain + classify pipeline. Pulled
// out as a free function so it can be table-tested without an ECS world.
func tickThreat(threat *components.Threat, buf *components.DangerBuffer, pos *components.WorldPos, dt float32) {
	threat.Suppression = decayValue(threat.Suppression, dt, components.ChannelDecayRates[components.ThreatChannelSuppression])
	threat.ShotsFired = decayValue(threat.ShotsFired, dt, components.ChannelDecayRates[components.ThreatChannelShotsFired])
	threat.Endangered = decayValue(threat.Endangered, dt, components.ChannelDecayRates[components.ThreatChannelEndangered])
	threat.Injury = decayValue(threat.Injury, dt, components.ChannelDecayRates[components.ThreatChannelInjury])

	decayClusters(threat, dt)

	// Drain the buffer: each event contributes Strength to its channel and a
	// directional vote into the cluster set. Per-tick drain means
	// recency*strength collapses to strength.
	if buf.Count > 0 {
		// Walk Count entries ending at Head (oldest first).
		start := int(buf.Head) - int(buf.Count)
		if start < 0 {
			start += components.DangerBufferSize
		}
		for i := uint8(0); i < buf.Count; i++ {
			idx := (start + int(i)) % components.DangerBufferSize
			ev := &buf.Events[idx]
			channel := components.ChannelForDanger(ev.Kind)
			switch channel {
			case components.ThreatChannelSuppression:
				threat.Suppression += ev.Strength
			case components.ThreatChannelShotsFired:
				threat.ShotsFired += ev.Strength
			case components.ThreatChannelEndangered:
				threat.Endangered += ev.Strength
			case components.ThreatChannelInjury:
				threat.Injury += ev.Strength
			default:
				continue
			}
			delta := pos.Sub(ev.Pos)
			dx, dz := delta.X, delta.Z
			d := float32(math.Sqrt(float64(dx*dx + dz*dz)))
			if d > 1e-3 {
				addCluster(threat, dx/d, dz/d, d, ev.Strength, ev.Time)
			}
		}
		buf.Head = 0
		buf.Count = 0
	}
	sortClusters(threat)

	threat.Suppression = clamp01(threat.Suppression)
	threat.ShotsFired = clamp01(threat.ShotsFired)
	threat.Endangered = clamp01(threat.Endangered)
	threat.Injury = clamp01(threat.Injury)

	total := threat.Suppression + threat.ShotsFired + threat.Endangered + threat.Injury
	if total > 1 {
		total = 1
	}
	threat.Total = total
	threat.State = components.ClassifyThreat(total)
	// Every channel decayed to zero → the bearings are stale intel; readers
	// (reflex orientation, cover pick) must not act on them.
	if total == 0 {
		threat.ThreatDir = rl.Vector3{}
		threat.Clusters = [components.ThreatClusterCount]components.ThreatCluster{}
		return
	}
	threat.ThreatDir = rl.Vector3{X: threat.Clusters[0].Dir.X, Z: threat.Clusters[0].Dir.Z}
}

// decayClusters bleeds weight and drops exhausted bearings.
func decayClusters(threat *components.Threat, dt float32) {
	for i := range threat.Clusters {
		c := &threat.Clusters[i]
		if c.Weight <= 0 {
			continue
		}
		c.Weight -= components.ThreatClusterDecay * dt
		if c.Weight <= 0 {
			*c = components.ThreatCluster{}
		}
	}
}

// addCluster merges a directional vote into the nearest bearing within the
// merge cone, or seeds a new one. A full set gives up its weakest bearing
// only to a stronger vote — otherwise the vote merges into its closest
// cluster, so no danger is silently dropped.
func addCluster(threat *components.Threat, dx, dz, dist, strength, now float32) {
	if strength <= 0 {
		return
	}
	best, free := -1, -1
	bestDot := components.ThreatClusterMergeCos
	weakest, weakestW := -1, float32(math.MaxFloat32)
	closest, closestDot := -1, float32(-2)
	for i := range threat.Clusters {
		c := &threat.Clusters[i]
		if c.Weight <= 0 {
			if free < 0 {
				free = i
			}
			continue
		}
		dot := c.Dir.X*dx + c.Dir.Z*dz
		if dot > bestDot {
			bestDot, best = dot, i
		}
		if dot > closestDot {
			closestDot, closest = dot, i
		}
		if c.Weight < weakestW {
			weakestW, weakest = c.Weight, i
		}
	}
	switch {
	case best >= 0:
		mergeCluster(&threat.Clusters[best], dx, dz, dist, strength, now)
	case free >= 0:
		threat.Clusters[free] = components.ThreatCluster{
			Dir:    rl.Vector3{X: dx, Z: dz},
			Dist:   dist,
			Weight: strength,
			LastAt: now,
		}
	case strength > weakestW && weakest >= 0:
		threat.Clusters[weakest] = components.ThreatCluster{
			Dir:    rl.Vector3{X: dx, Z: dz},
			Dist:   dist,
			Weight: strength,
			LastAt: now,
		}
	case closest >= 0:
		mergeCluster(&threat.Clusters[closest], dx, dz, dist, strength, now)
	}
}

func mergeCluster(c *components.ThreatCluster, dx, dz, dist, strength, now float32) {
	w := c.Weight + strength
	if w <= 0 {
		return
	}
	nx := (c.Dir.X*c.Weight + dx*strength) / w
	nz := (c.Dir.Z*c.Weight + dz*strength) / w
	if l := float32(math.Sqrt(float64(nx*nx + nz*nz))); l > 1e-6 {
		c.Dir = rl.Vector3{X: nx / l, Z: nz / l}
	}
	c.Dist = (c.Dist*c.Weight + dist*strength) / w
	c.Weight = w
	c.LastAt = now
	if c.Weight > 1 {
		c.Weight = 1
	}
}

// sortClusters keeps the dominant bearing first (insertion sort over three
// slots — the order is part of the replay hash, so it must be total: equal
// weights break on the existing index).
func sortClusters(threat *components.Threat) {
	for i := 1; i < len(threat.Clusters); i++ {
		c := threat.Clusters[i]
		j := i - 1
		for j >= 0 && threat.Clusters[j].Weight < c.Weight {
			threat.Clusters[j+1] = threat.Clusters[j]
			j--
		}
		threat.Clusters[j+1] = c
	}
}

// decayValue applies a per-second linear drop clamped at 0.
func decayValue(value, dt, rate float32) float32 {
	if value <= 0 || rate <= 0 {
		return value
	}
	value -= rate * dt
	if value < 0 {
		return 0
	}
	return value
}

func clamp01(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
