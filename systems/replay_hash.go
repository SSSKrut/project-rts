package systems

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strings"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// ReplayHasher folds sim-visible state into one FNV-1a value; two runs of
// the same scene must produce identical hash sequences (WS-B replay gate).
//
// Entities are folded in ENTITY-ID order, not query order: Ark's table layout
// is a function of archetype history (an archetype absent at save time and
// re-created after a load lands in a different iteration slot), so a
// layout-ordered hash reports a mismatch for two worlds holding identical
// state. IDs survive save/load, so ID order is the stable canonical one.
type ReplayHasher struct {
	unitFilter    *ecs.Filter4[components.Unit, components.WorldPos, components.Motion, components.ActionQueue]
	vehFilter     *ecs.Filter4[components.Vehicle, components.WorldPos, components.Motion, components.ActionQueue]
	airFilter     *ecs.Filter3[components.Aircraft, components.WorldPos, components.Motion]
	arrivalFilter *ecs.Filter1[components.AirArrival]
	orderFilter   *ecs.Filter1[components.OrderState]
	planFilter    *ecs.Filter2[components.Squad, components.SquadPlan]
	contactFilter *ecs.Filter1[components.Contact]
	stanceMap     *ecs.Map[components.Stance]
	hpMap         *ecs.Map[components.HP]
	threatMap     *ecs.Map[components.Threat]
	progressMap   *ecs.Map[components.OrderProgress]
	routeMap      *ecs.Map[components.RoadRoute]
	followerMap   *ecs.Map[components.RoadFollower]
	overrideMap   *ecs.Map[components.VehicleOverride]
	queueMap      *ecs.Map[components.ActionQueue]
	world         *ecs.World

	scratch []entHash
}

// entHash is one entity's folded state, keyed for the ID sort.
type entHash struct {
	id uint32
	h  uint64
}

func NewReplayHasher(w *ecs.World) *ReplayHasher {
	return &ReplayHasher{
		world:         w,
		unitFilter:    ecs.NewFilter4[components.Unit, components.WorldPos, components.Motion, components.ActionQueue](w),
		vehFilter:     ecs.NewFilter4[components.Vehicle, components.WorldPos, components.Motion, components.ActionQueue](w),
		orderFilter:   ecs.NewFilter1[components.OrderState](w),
		planFilter:    ecs.NewFilter2[components.Squad, components.SquadPlan](w),
		contactFilter: ecs.NewFilter1[components.Contact](w),
		stanceMap:     ecs.NewMap[components.Stance](w),
		hpMap:         ecs.NewMap[components.HP](w),
		threatMap:     ecs.NewMap[components.Threat](w),
		progressMap:   ecs.NewMap[components.OrderProgress](w),
		routeMap:      ecs.NewMap[components.RoadRoute](w),
		followerMap:   ecs.NewMap[components.RoadFollower](w),
		overrideMap:   ecs.NewMap[components.VehicleOverride](w),
		airFilter:     ecs.NewFilter3[components.Aircraft, components.WorldPos, components.Motion](w),
		arrivalFilter: ecs.NewFilter1[components.AirArrival](w),
		queueMap:      ecs.NewMap[components.ActionQueue](w),
	}
}

const (
	fnvOffset64 uint64 = 14695981039346656037
	fnvPrime64  uint64 = 1099511628211
)

func (r *ReplayHasher) Hash() uint64 {
	h := fnvOffset64
	// Per-entity accumulator; sections flush it through fold() in ID order.
	eh := fnvOffset64
	r.scratch = r.scratch[:0]
	u32 := func(v uint32) { eh = (eh ^ uint64(v)) * fnvPrime64 }
	u8 := func(v uint8) { eh = (eh ^ uint64(v)) * fnvPrime64 }
	take := func(ent ecs.Entity) {
		r.scratch = append(r.scratch, entHash{id: ent.ID(), h: eh})
		eh = fnvOffset64
	}
	fold := func() {
		sort.Slice(r.scratch, func(i, j int) bool { return r.scratch[i].id < r.scratch[j].id })
		for _, e := range r.scratch {
			h = (h ^ e.h) * fnvPrime64
		}
		r.scratch = r.scratch[:0]
	}
	f32 := func(v float32) { u32(math.Float32bits(v)) }
	pos := func(p components.WorldPos) {
		u32(uint32(p.Chunk.X))
		u32(uint32(p.Chunk.Z))
		f32(p.Local.X)
		f32(p.Local.Y)
		f32(p.Local.Z)
	}

	q := r.unitFilter.Query()
	for q.Next() {
		_, p, mot, aq := q.Get()
		ent := q.Entity()
		pos(*p)
		f32(mot.Yaw)
		f32(mot.VelocityYaw)
		f32(mot.Speed)
		if st := r.stanceMap.Get(ent); st != nil {
			u8(uint8(st.Code))
			f32(st.LockUntil)
		}
		if hp := r.hpMap.Get(ent); hp != nil {
			f32(hp.Current)
		}
		if th := r.threatMap.Get(ent); th != nil {
			f32(th.Total)
			f32(th.Suppression)
			u8(uint8(th.State))
			for i := range th.Clusters {
				c := &th.Clusters[i]
				f32(c.Dir.X)
				f32(c.Dir.Z)
				f32(c.Dist)
				f32(c.Weight)
			}
		}
		u8(aq.Head)
		u8(aq.Count)
		f32(aq.StopUntil)
		if aq.Count > 0 {
			a := aq.Actions[aq.Head%components.ActionQueueSize]
			u8(uint8(a.Kind))
			pos(a.Target)
		}
		take(ent)
	}
	fold()

	qv := r.vehFilter.Query()
	for qv.Next() {
		_, p, mot, aq := qv.Get()
		ent := qv.Entity()
		pos(*p)
		f32(mot.Yaw)
		f32(mot.Speed)
		if hp := r.hpMap.Get(ent); hp != nil {
			f32(hp.Current)
		}
		u8(aq.Head)
		u8(aq.Count)
		if aq.Count > 0 {
			a := aq.Actions[aq.Head%components.ActionQueueSize]
			u8(uint8(a.Kind))
			pos(a.Target)
		}
		// Route state must survive save/load byte-exact: a zeroed RoadRoute
		// would replan to near-identical steering and hide from pos alone.
		if rt := r.routeMap.Get(ent); rt != nil {
			u8(rt.Count)
			u8(rt.Head)
			u8(rt.Planned)
			u8(rt.Phase)
			u32(uint32(rt.EntryEdge))
			f32(rt.EntryT)
			u32(uint32(rt.ExitEdge))
			f32(rt.ExitT)
			if rt.Head < rt.Count {
				u32(uint32(rt.Nodes[rt.Head]))
			}
		}
		if f := r.followerMap.Get(ent); f != nil {
			u32(uint32(f.Edge))
			f32(f.T)
		}
		// Reflex state: a cleared override still holds LastAt (cooldown), and
		// an active one steers the hull — both must survive save/load exact.
		if ov := r.overrideMap.Get(ent); ov != nil {
			u8(uint8(ov.Kind))
			f32(ov.Until)
			f32(ov.ThreatYaw)
			f32(ov.LastAt)
			pos(ov.Retreat)
		}
		take(ent)
	}
	fold()

	// Airframes. Y is hashed with the rest of the position (pos() covers it),
	// which matters more here than anywhere else: an aircraft's altitude IS
	// simulation state, not a render clamp, and a drift there is invisible in
	// XZ until the airframe walks into a hillside. Arrivals are folded too —
	// an unreleased schedule entry that came back from a save one tick early
	// spawns a whole airframe out of step.
	qair := r.airFilter.Query()
	for qair.Next() {
		ac, p, mot := qair.Get()
		ent := qair.Entity()
		pos(*p)
		f32(mot.Yaw)
		f32(mot.Speed)
		u8(uint8(ac.Kind))
		u8(uint8(ac.AltRef))
		f32(ac.AltSet)
		f32(ac.SpeedSet)
		f32(ac.Fuel)
		if ac.Egressing {
			u8(1)
		} else {
			u8(0)
		}
		if hp := r.hpMap.Get(ent); hp != nil {
			f32(hp.Current)
		}
		if aq := r.queueMap.Get(ent); aq != nil {
			u8(aq.Head)
			u8(aq.Count)
			if aq.Count > 0 {
				a := aq.Actions[aq.Head%components.ActionQueueSize]
				u8(uint8(a.Kind))
				pos(a.Target)
			}
		}
		take(ent)
	}
	fold()

	qarr := r.arrivalFilter.Query()
	for qarr.Next() {
		a := qarr.Get()
		f32(a.At)
		u8(uint8(a.Kind))
		u8(a.FactionID)
		pos(a.Entry)
		take(qarr.Entity())
	}
	fold()

	qo := r.orderFilter.Query()
	for qo.Next() {
		st := qo.Get()
		u8(uint8(st.Code))
		if pr := r.progressMap.Get(qo.Entity()); pr != nil {
			f32(pr.Value)
		}
		take(qo.Entity())
	}
	fold()

	// The brain's plan steers execution: two runs that diverge only in Mode /
	// wave / bound would drift a second later, so hash it at the source.
	qp := r.planFilter.Query()
	for qp.Next() {
		_, plan := qp.Get()
		u8(uint8(plan.Mode))
		u8(plan.Phase)
		u8(plan.Floor)
		u8(plan.WaveMask)
		f32(plan.Timer)
		f32(plan.CalmSince)
		pos(plan.Anchor)
		take(qp.Entity())
	}
	fold()

	qc := r.contactFilter.Query()
	for qc.Next() {
		c := qc.Get()
		pos(c.EstimatedPos)
		f32(c.LastSeenTime)
		u8(uint8(c.PerceivedAffil))
		u8(uint8(c.PerceivedDim))
		u8(uint8(c.Source))
		take(qc.Entity())
	}
	fold()

	return h
}

// DumpState writes the hashed fields as per-entity text (float bits in hex) —
// forensics for save/load hash divergence.
func (r *ReplayHasher) DumpState(path string) error {
	var b strings.Builder
	fb := math.Float32bits
	q := r.unitFilter.Query()
	for q.Next() {
		_, p, mot, aq := q.Get()
		ent := q.Entity()
		fmt.Fprintf(&b, "U %d:%d pos=%d,%d,%08x,%08x,%08x mot=%08x,%08x,%08x",
			ent.ID(), ent.Gen(), p.Chunk.X, p.Chunk.Z,
			fb(p.Local.X), fb(p.Local.Y), fb(p.Local.Z),
			fb(mot.Yaw), fb(mot.VelocityYaw), fb(mot.Speed))
		if st := r.stanceMap.Get(ent); st != nil {
			fmt.Fprintf(&b, " st=%d,%08x", st.Code, fb(st.LockUntil))
		}
		if hp := r.hpMap.Get(ent); hp != nil {
			fmt.Fprintf(&b, " hp=%08x", fb(hp.Current))
		}
		if th := r.threatMap.Get(ent); th != nil {
			fmt.Fprintf(&b, " th=%08x,%08x,%d", fb(th.Total), fb(th.Suppression), th.State)
		}
		fmt.Fprintf(&b, " aq=%d,%d,%08x", aq.Head, aq.Count, fb(aq.StopUntil))
		if aq.Count > 0 {
			a := aq.Actions[aq.Head%components.ActionQueueSize]
			fmt.Fprintf(&b, " act=%d@%d,%d,%08x,%08x,%08x", a.Kind,
				a.Target.Chunk.X, a.Target.Chunk.Z,
				fb(a.Target.Local.X), fb(a.Target.Local.Y), fb(a.Target.Local.Z))
		}
		b.WriteByte('\n')
	}
	qo := r.orderFilter.Query()
	for qo.Next() {
		st := qo.Get()
		fmt.Fprintf(&b, "O %d:%d code=%d", qo.Entity().ID(), qo.Entity().Gen(), st.Code)
		if pr := r.progressMap.Get(qo.Entity()); pr != nil {
			fmt.Fprintf(&b, " prog=%08x", fb(pr.Value))
		}
		b.WriteByte('\n')
	}
	qc := r.contactFilter.Query()
	for qc.Next() {
		c := qc.Get()
		fmt.Fprintf(&b, "C %d:%d pos=%d,%d,%08x,%08x,%08x t=%08x affil=%d dim=%d src=%d\n",
			qc.Entity().ID(), qc.Entity().Gen(),
			c.EstimatedPos.Chunk.X, c.EstimatedPos.Chunk.Z,
			fb(c.EstimatedPos.Local.X), fb(c.EstimatedPos.Local.Y), fb(c.EstimatedPos.Local.Z),
			fb(c.LastSeenTime), c.PerceivedAffil, c.PerceivedDim, c.Source)
	}
	qh := ecs.NewFilter2[components.ChunkCoord, components.Heightmap](r.world).Query()
	for qh.Next() {
		cc, hm := qh.Get()
		h := fnvOffset64
		for _, v := range hm.Heights {
			h = (h ^ uint64(math.Float32bits(v))) * fnvPrime64
		}
		fmt.Fprintf(&b, "H %d,%d %016x\n", cc.X, cc.Z, h)
	}
	// Same reason the hash folds in ID order: table layout is not state, and
	// an unsorted dump makes two identical worlds look different.
	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	sort.Strings(lines)
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}
