package ui

import (
	"testing"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

type contactSpec struct {
	x, z  float32
	affil components.Affiliation
	dim   components.Dimension
}

func clusterWorld(t *testing.T, contacts []contactSpec) (*ecs.World, *ecs.Filter1[components.Contact]) {
	t.Helper()
	world := ecs.NewWorld(64)
	cm := ecs.NewMap[components.Contact](world)
	for _, c := range contacts {
		e := world.NewEntity()
		cm.Add(e, &components.Contact{
			EstimatedPos:   components.WorldPos{}.Add(rl.Vector3{X: c.x, Z: c.z}),
			LastSeenTime:   0,
			PerceivedAffil: c.affil,
			PerceivedDim:   c.dim,
		})
	}
	return world, ecs.NewFilter1[components.Contact](world)
}

func hostileLine(n int, x0, step float32) []contactSpec {
	out := make([]contactSpec, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, contactSpec{
			x: x0 + step*float32(i), z: 0,
			affil: components.AffilHostile, dim: components.DimInfantryClass,
		})
	}
	return out
}

func counts(set *ContactClusterSet) []int {
	out := make([]int, 0, len(set.Clusters))
	for _, c := range set.Clusters {
		out = append(out, c.Count)
	}
	return out
}

func TestClusterSplitsByDistance(t *testing.T) {
	// Two files of five, 200 m apart: one formation each.
	near := hostileLine(5, 0, 5)
	far := hostileLine(5, 200, 5)
	world, filter := clusterWorld(t, append(near, far...))

	var set ContactClusterSet
	set.Rebuild(world, filter, nil, 0)
	if got := counts(&set); len(got) != 2 || got[0] != 5 || got[1] != 5 {
		t.Fatalf("expected two groups of five, got %v", got)
	}
}

// Single-link: a chain of steps below the radius is one formation even
// though the ends are far past it.
func TestClusterChainsTransitively(t *testing.T) {
	world, filter := clusterWorld(t, hostileLine(6, 0, 20))

	var set ContactClusterSet
	set.Rebuild(world, filter, nil, 0)
	if got := counts(&set); len(got) != 1 || got[0] != 6 {
		t.Fatalf("chain should merge into one, got %v", got)
	}
}

// Different perceived class never merges, however close the tracks sit.
func TestClusterKeepsClassesApart(t *testing.T) {
	world, filter := clusterWorld(t, []contactSpec{
		{x: 0, z: 0, affil: components.AffilHostile, dim: components.DimInfantryClass},
		{x: 2, z: 0, affil: components.AffilHostile, dim: components.DimVehicleClass},
		{x: 4, z: 0, affil: components.AffilNeutral, dim: components.DimInfantryClass},
	})

	var set ContactClusterSet
	set.Rebuild(world, filter, nil, 0)
	if len(set.Clusters) != 3 {
		t.Fatalf("expected 3 singleton clusters, got %d", len(set.Clusters))
	}
}

func TestEchelonForCount(t *testing.T) {
	cases := []struct {
		n    int
		want components.Echelon
	}{
		{1, components.EchelonTeam},
		{3, components.EchelonTeam},
		{10, components.EchelonSquad},
		{13, components.EchelonSquad},
		{20, components.EchelonSection},
		{40, components.EchelonPlatoon},
		{80, components.EchelonCompany},
	}
	for _, tc := range cases {
		if got := EchelonForCount(tc.n); got != tc.want {
			t.Errorf("EchelonForCount(%d) = %v, want %v", tc.n, got, tc.want)
		}
	}
}

// Members hang off the symbol only once they are far enough apart on screen.
func TestClusterExpandsWithZoom(t *testing.T) {
	world, filter := clusterWorld(t, hostileLine(2, 0, 20))
	var set ContactClusterSet
	set.Rebuild(world, filter, nil, 0)
	if len(set.Clusters) != 1 {
		t.Fatalf("expected one cluster, got %d", len(set.Clusters))
	}
	content := rl.Rectangle{X: 0, Y: 0, Width: 400, Height: 400}
	cam := NewMapCamera()
	cam.Center = set.Clusters[0].Center

	cam.Zoom = 0.5 // 20 m apart = 10 px: one symbol
	if _, expanded := set.expanded(set.Clusters[0], cam, content); expanded {
		t.Error("cluster expanded while zoomed out")
	}
	cam.Zoom = 8 // 20 m apart = 160 px: dots on spokes
	if _, expanded := set.expanded(set.Clusters[0], cam, content); !expanded {
		t.Error("cluster stayed collapsed while zoomed in")
	}
}

// The pick path must resolve a collapsed cluster to its representative and
// an expanded one to the member actually under the cursor.
func TestPickContactMatchesLayout(t *testing.T) {
	world, filter := clusterWorld(t, hostileLine(2, 0, 20))
	var set ContactClusterSet
	set.Rebuild(world, filter, nil, 0)
	c := set.Clusters[0]

	panel := ContentToPanel(rl.Rectangle{X: 0, Y: 0, Width: 400, Height: 400}, "Map", PanelMap)
	content := ContentRect(panel)
	cam := NewMapCamera()
	cam.Center = c.Center
	ctx := MapRenderCtx{World: world, Cam: cam, Clusters: &set}

	ctx.Cam.Zoom = 0.5
	centre := MapWorldToPanel(c.Center, ctx.Cam, content)
	if got := PickContactAt(centre, ctx, panel, 12); got != c.Rep {
		t.Errorf("collapsed pick: got %v want rep %v", got, c.Rep)
	}

	ctx.Cam.Zoom = 8
	second := set.Members[c.First+1]
	at := MapWorldToPanel(second.Pos, ctx.Cam, content)
	if got := PickContactAt(at, ctx, panel, 12); got != second.Entity {
		t.Errorf("expanded pick: got %v want member %v", got, second.Entity)
	}
}
