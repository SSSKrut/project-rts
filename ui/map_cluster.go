package ui

import (
	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// clusterRadiusM: contacts of the same perceived class within this distance
// of each other (single-link, so the chain can be longer than the radius)
// read as one formation. 25 m is about a squad's frontage in the open.
const clusterRadiusM float32 = 25

// clusterExpandPx: once a cluster's members sit further than this from its
// centre on screen, the marker stops being a single symbol and shows the
// members as dots wired back to it.
const clusterExpandPx float32 = 24

// contactSymbolHalf keeps the aggregate symbol at the size lone contacts
// used before clustering.
const contactSymbolHalf float32 = 9

type ClusterMember struct {
	Entity ecs.Entity
	Pos    components.WorldPos
	Alpha  float32
}

// ContactCluster is one drawn formation. Members are Set.Members[First:First+Count].
type ContactCluster struct {
	Spec   components.SymbolSpec
	Center components.WorldPos
	Alpha  float32    // freshest member — the aggregate never fades faster than its best track
	Rep    ecs.Entity // freshest member: what a click on the collapsed symbol resolves to
	First  int
	Count  int
}

// ContactClusterSet is rebuilt once per frame and read by both the draw and
// the pick path, so what the player clicks is what they see. Buffers are
// reused; nothing here allocates after warmup.
type ContactClusterSet struct {
	Clusters []ContactCluster
	Members  []ClusterMember

	raw    []ClusterMember
	specs  []components.SymbolSpec
	parent []int
	slot   []int
}

func (s *ContactClusterSet) members(c ContactCluster) []ClusterMember {
	return s.Members[c.First : c.First+c.Count]
}

// EchelonForCount turns a head count into the echelon marker the symbol
// wears. Sized off an assumed ~10-man squad: ten bodies read as one squad,
// twenty as two (a section), and so on.
func EchelonForCount(n int) components.Echelon {
	switch {
	case n <= 0:
		return components.EchelonNone
	case n <= 3:
		return components.EchelonTeam
	case n <= 13:
		return components.EchelonSquad
	case n <= 26:
		return components.EchelonSection
	case n <= 45:
		return components.EchelonPlatoon
	default:
		return components.EchelonCompany
	}
}

// Rebuild groups every live Contact by perceived class and proximity.
func (s *ContactClusterSet) Rebuild(world *ecs.World,
	filter *ecs.Filter1[components.Contact],
	overrideMap *ecs.Map[components.ContactSymbolOverride], clock float32) {
	s.Clusters = s.Clusters[:0]
	s.Members = s.Members[:0]
	s.raw = s.raw[:0]
	s.specs = s.specs[:0]
	s.parent = s.parent[:0]
	s.slot = s.slot[:0]
	if world == nil || filter == nil {
		return
	}

	q := filter.Query()
	for q.Next() {
		c := q.Get()
		ent := q.Entity()
		spec := DefaultSpecForDimension(c.PerceivedAffil, c.PerceivedDim)
		if overrideMap != nil {
			if ov := overrideMap.Get(ent); ov != nil {
				spec = ov.Spec
			}
		}
		s.raw = append(s.raw, ClusterMember{
			Entity: ent,
			Pos:    c.EstimatedPos,
			Alpha:  components.ContactAgeAlpha(c.LastSeenTime, clock),
		})
		s.specs = append(s.specs, spec)
	}
	q.Close()

	n := len(s.raw)
	if n == 0 {
		return
	}
	for i := 0; i < n; i++ {
		s.parent = append(s.parent, i)
		s.slot = append(s.slot, -1)
	}
	// Single-link union: a player-classified contact carries a different
	// spec and therefore never merges into the auto-classified crowd.
	r2 := clusterRadiusM * clusterRadiusM
	for i := 0; i < n; i++ {
		for j := 0; j < i; j++ {
			if s.specs[i] != s.specs[j] {
				continue
			}
			if components.DistanceSquared(s.raw[i].Pos, s.raw[j].Pos) <= r2 {
				s.union(i, j)
			}
		}
	}

	// Bucket by root, keeping query order so the layout is stable frame to
	// frame (and identical between the draw and the pick pass).
	for i := 0; i < n; i++ {
		root := s.find(i)
		if s.slot[root] < 0 {
			s.slot[root] = len(s.Clusters)
			s.Clusters = append(s.Clusters, ContactCluster{Spec: s.specs[i]})
		}
		s.Clusters[s.slot[root]].Count++
	}
	first := 0
	for i := range s.Clusters {
		s.Clusters[i].First = first
		first += s.Clusters[i].Count
		s.Clusters[i].Count = 0
	}
	s.Members = append(s.Members, make([]ClusterMember, n)...)
	for i := 0; i < n; i++ {
		c := &s.Clusters[s.slot[s.find(i)]]
		s.Members[c.First+c.Count] = s.raw[i]
		c.Count++
	}

	for i := range s.Clusters {
		c := &s.Clusters[i]
		var sx, sy, sz float32
		for _, m := range s.members(*c) {
			mx, mz := worldPosCenterXZ(m.Pos)
			sx += mx
			sy += m.Pos.Local.Y
			sz += mz
			if m.Alpha > c.Alpha || c.Rep == (ecs.Entity{}) {
				c.Alpha = m.Alpha
				c.Rep = m.Entity
			}
		}
		inv := 1 / float32(c.Count)
		c.Center = components.WorldPos{}.Add(rl.Vector3{X: sx * inv, Y: sy * inv, Z: sz * inv})
		c.Spec.Echelon = EchelonForCount(c.Count)
	}
}

func (s *ContactClusterSet) find(i int) int {
	for s.parent[i] != i {
		s.parent[i] = s.parent[s.parent[i]]
		i = s.parent[i]
	}
	return i
}

func (s *ContactClusterSet) union(a, b int) {
	ra, rb := s.find(a), s.find(b)
	if ra == rb {
		return
	}
	// Lower root wins so the result never depends on the union order.
	if rb < ra {
		ra, rb = rb, ra
	}
	s.parent[rb] = ra
}

// expanded reports whether the cluster's members are far enough apart on
// screen to be drawn individually, and the centre they hang off.
func (s *ContactClusterSet) expanded(c ContactCluster, cam MapCamera,
	content rl.Rectangle) (rl.Vector2, bool) {
	centre := MapWorldToPanel(c.Center, cam, content)
	if c.Count < 2 {
		return centre, false
	}
	limit := clusterExpandPx * clusterExpandPx
	for _, m := range s.members(c) {
		p := MapWorldToPanel(m.Pos, cam, content)
		dx, dy := p.X-centre.X, p.Y-centre.Y
		if dx*dx+dy*dy > limit {
			return centre, true
		}
	}
	return centre, false
}
