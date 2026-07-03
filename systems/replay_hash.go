package systems

import (
	"math"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// ReplayHasher folds sim-visible state into one FNV-1a value; two runs of
// the same scene must produce identical hash sequences (WS-B replay gate).
type ReplayHasher struct {
	unitFilter    *ecs.Filter4[components.Unit, components.WorldPos, components.Motion, components.ActionQueue]
	orderFilter   *ecs.Filter1[components.OrderState]
	contactFilter *ecs.Filter1[components.Contact]
	stanceMap     *ecs.Map[components.Stance]
	hpMap         *ecs.Map[components.HP]
	threatMap     *ecs.Map[components.Threat]
	progressMap   *ecs.Map[components.OrderProgress]
}

func NewReplayHasher(w *ecs.World) *ReplayHasher {
	return &ReplayHasher{
		unitFilter:    ecs.NewFilter4[components.Unit, components.WorldPos, components.Motion, components.ActionQueue](w),
		orderFilter:   ecs.NewFilter1[components.OrderState](w),
		contactFilter: ecs.NewFilter1[components.Contact](w),
		stanceMap:     ecs.NewMap[components.Stance](w),
		hpMap:         ecs.NewMap[components.HP](w),
		threatMap:     ecs.NewMap[components.Threat](w),
		progressMap:   ecs.NewMap[components.OrderProgress](w),
	}
}

const (
	fnvOffset64 uint64 = 14695981039346656037
	fnvPrime64  uint64 = 1099511628211
)

func (r *ReplayHasher) Hash() uint64 {
	h := fnvOffset64
	u32 := func(v uint32) { h = (h ^ uint64(v)) * fnvPrime64 }
	u8 := func(v uint8) { h = (h ^ uint64(v)) * fnvPrime64 }
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
		}
		u8(aq.Head)
		u8(aq.Count)
		f32(aq.StopUntil)
		if aq.Count > 0 {
			a := aq.Actions[aq.Head%components.ActionQueueSize]
			u8(uint8(a.Kind))
			pos(a.Target)
		}
	}

	qo := r.orderFilter.Query()
	for qo.Next() {
		st := qo.Get()
		u8(uint8(st.Code))
		if pr := r.progressMap.Get(qo.Entity()); pr != nil {
			f32(pr.Value)
		}
	}

	qc := r.contactFilter.Query()
	for qc.Next() {
		c := qc.Get()
		pos(c.EstimatedPos)
		f32(c.LastSeenTime)
		u8(uint8(c.PerceivedAffil))
		u8(uint8(c.PerceivedDim))
		u8(uint8(c.Source))
	}

	return h
}
