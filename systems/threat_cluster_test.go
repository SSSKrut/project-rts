package systems

import (
	"math"
	"testing"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
)

func liveClusters(th *components.Threat) int {
	n := 0
	for i := range th.Clusters {
		if th.Clusters[i].Weight > 0 {
			n++
		}
	}
	return n
}

// bearing turns a compass angle into the unit vector addCluster expects.
func bearing(deg float64) (float32, float32) {
	r := deg * math.Pi / 180
	return float32(math.Sin(r)), float32(-math.Cos(r))
}

func TestAddClusterSeedsAndMergesByCone(t *testing.T) {
	th := &components.Threat{}
	dx, dz := bearing(0)
	addCluster(th, dx, dz, 20, 0.3, 1)
	if liveClusters(th) != 1 {
		t.Fatalf("first vote made %d clusters", liveClusters(th))
	}
	// A vote a few degrees away lands in the same cone.
	dx, dz = bearing(8)
	addCluster(th, dx, dz, 24, 0.2, 2)
	if liveClusters(th) != 1 {
		t.Fatalf("a near bearing opened a second cluster: %+v", th.Clusters)
	}
	if got := th.Clusters[0].Weight; math.Abs(float64(got-0.5)) > 1e-4 {
		t.Errorf("merged weight %.3f, want 0.5", got)
	}
	if got := th.Clusters[0].LastAt; got != 2 {
		t.Errorf("LastAt %.1f, want the fresher stamp 2", got)
	}
}

func TestAddClusterKeepsOpposedBearingsApart(t *testing.T) {
	th := &components.Threat{}
	for _, deg := range []float64{0, 90, 180} {
		dx, dz := bearing(deg)
		addCluster(th, dx, dz, 20, 0.3, 1)
	}
	if liveClusters(th) != 3 {
		t.Fatalf("three separate bearings collapsed to %d clusters", liveClusters(th))
	}
}

func TestAddClusterIgnoresNonPositiveStrength(t *testing.T) {
	th := &components.Threat{}
	dx, dz := bearing(0)
	addCluster(th, dx, dz, 20, 0, 1)
	addCluster(th, dx, dz, 20, -1, 1)
	if liveClusters(th) != 0 {
		t.Fatalf("a zero-strength vote seeded %d clusters", liveClusters(th))
	}
}

// A full set must not silently swallow a bigger danger: the weakest bearing
// gives way to a stronger vote.
func TestAddClusterEvictsTheWeakestForAStrongerVote(t *testing.T) {
	th := &components.Threat{}
	strengths := []float32{0.6, 0.5, 0.1}
	degs := []float64{0, 90, 180}
	for i := range degs {
		dx, dz := bearing(degs[i])
		addCluster(th, dx, dz, 20, strengths[i], 1)
	}
	dx, dz := bearing(270)
	addCluster(th, dx, dz, 15, 0.4, 2)

	found := false
	for i := range th.Clusters {
		c := th.Clusters[i]
		if c.Weight > 0 && c.Dir.X < -0.9 {
			found = true
		}
		if c.Weight > 0 && math.Abs(float64(c.Weight-0.1)) < 1e-4 {
			t.Error("the weakest bearing survived a stronger vote")
		}
	}
	if !found {
		t.Fatalf("the stronger vote was dropped: %+v", th.Clusters)
	}
}

// ...but a weaker vote on a full set is folded into its closest bearing
// rather than thrown away.
func TestAddClusterFoldsAWeakVoteIntoTheClosestBearing(t *testing.T) {
	th := &components.Threat{}
	degs := []float64{0, 90, 180}
	for _, d := range degs {
		dx, dz := bearing(d)
		addCluster(th, dx, dz, 20, 0.5, 1)
	}
	before := float32(0)
	for i := range th.Clusters {
		before += th.Clusters[i].Weight
	}
	dx, dz := bearing(120) // nearest to the 90 deg bearing
	addCluster(th, dx, dz, 30, 0.05, 2)

	after := float32(0)
	for i := range th.Clusters {
		after += th.Clusters[i].Weight
	}
	if liveClusters(th) != 3 {
		t.Fatalf("cluster count changed to %d", liveClusters(th))
	}
	if after <= before {
		t.Fatalf("the weak vote vanished: total weight %.3f -> %.3f", before, after)
	}
}

func TestClusterDirStaysUnitLength(t *testing.T) {
	th := &components.Threat{}
	for i, deg := range []float64{0, 5, 12, 350, 91, 95, 200} {
		dx, dz := bearing(deg)
		addCluster(th, dx, dz, 20, 0.15, float32(i))
	}
	for i := range th.Clusters {
		c := th.Clusters[i]
		if c.Weight <= 0 {
			continue
		}
		l := math.Hypot(float64(c.Dir.X), float64(c.Dir.Z))
		if math.Abs(l-1) > 1e-3 {
			t.Errorf("cluster %d Dir length %.4f — the merge cone test needs unit vectors", i, l)
		}
	}
}

func TestMergeClusterClampsWeightAtOne(t *testing.T) {
	c := &components.ThreatCluster{Dir: rl.Vector3{Z: -1}, Weight: 0.9, Dist: 10}
	mergeCluster(c, 0, -1, 10, 0.9, 5)
	if c.Weight > 1 {
		t.Fatalf("weight %.3f exceeds 1", c.Weight)
	}
}

func TestMergeClusterAveragesDistanceByWeight(t *testing.T) {
	c := &components.ThreatCluster{Dir: rl.Vector3{Z: -1}, Weight: 0.5, Dist: 10}
	mergeCluster(c, 0, -1, 30, 0.5, 5)
	if math.Abs(float64(c.Dist-20)) > 1e-3 {
		t.Fatalf("merged distance %.3f, want the 20 m midpoint", c.Dist)
	}
}

func TestMergeClusterPullsTheBearing(t *testing.T) {
	c := &components.ThreatCluster{Dir: rl.Vector3{Z: -1}, Weight: 0.5, Dist: 10} // due N
	dx, dz := bearing(45)
	mergeCluster(c, dx, dz, 10, 0.5, 5)
	// Equal weights: the result must sit between N and NE.
	if c.Dir.X <= 0 {
		t.Fatalf("bearing did not move toward the new vote: %+v", c.Dir)
	}
	if c.Dir.X >= float32(math.Sqrt2/2) {
		t.Fatalf("bearing overshot past the new vote: %+v", c.Dir)
	}
}

func TestSortClustersOrdersByWeightDescending(t *testing.T) {
	th := &components.Threat{}
	th.Clusters[0] = components.ThreatCluster{Dir: rl.Vector3{X: 1}, Weight: 0.2}
	th.Clusters[1] = components.ThreatCluster{Dir: rl.Vector3{Z: 1}, Weight: 0.9}
	th.Clusters[2] = components.ThreatCluster{Dir: rl.Vector3{X: -1}, Weight: 0.5}
	sortClusters(th)
	for i := 1; i < len(th.Clusters); i++ {
		if th.Clusters[i-1].Weight < th.Clusters[i].Weight {
			t.Fatalf("not sorted: %+v", th.Clusters)
		}
	}
	if th.Clusters[0].Dir.Z != 1 {
		t.Errorf("the heaviest bearing is not first: %+v", th.Clusters[0])
	}
}

// The order feeds the replay hash, so ties must resolve on the existing index
// and never on anything else.
func TestSortClustersIsStableOnTies(t *testing.T) {
	th := &components.Threat{}
	th.Clusters[0] = components.ThreatCluster{Dir: rl.Vector3{X: 1}, Weight: 0.4}
	th.Clusters[1] = components.ThreatCluster{Dir: rl.Vector3{Z: 1}, Weight: 0.4}
	th.Clusters[2] = components.ThreatCluster{Dir: rl.Vector3{X: -1}, Weight: 0.4}
	before := th.Clusters
	sortClusters(th)
	if th.Clusters != before {
		t.Fatalf("equal weights were reordered: %+v -> %+v", before, th.Clusters)
	}
}

func TestSortClustersPushesEmptySlotsLast(t *testing.T) {
	th := &components.Threat{}
	th.Clusters[2] = components.ThreatCluster{Dir: rl.Vector3{X: 1}, Weight: 0.3}
	sortClusters(th)
	if th.Clusters[0].Weight != 0.3 {
		t.Fatalf("the only live cluster is not first: %+v", th.Clusters)
	}
	for i := 1; i < len(th.Clusters); i++ {
		if th.Clusters[i].Weight != 0 {
			t.Fatalf("slot %d should be empty: %+v", i, th.Clusters[i])
		}
	}
}

func TestDecayClustersBleedsAndFrees(t *testing.T) {
	th := &components.Threat{}
	th.Clusters[0] = components.ThreatCluster{Dir: rl.Vector3{X: 1}, Weight: 1, Dist: 12, LastAt: 3}
	decayClusters(th, 0.1)
	if th.Clusters[0].Weight >= 1 || th.Clusters[0].Weight <= 0 {
		t.Fatalf("weight after a short decay: %.4f", th.Clusters[0].Weight)
	}
	decayClusters(th, 1000)
	if th.Clusters[0] != (components.ThreatCluster{}) {
		t.Fatalf("an exhausted cluster left residue: %+v", th.Clusters[0])
	}
}

func TestDecayClustersLeavesEmptySlotsAlone(t *testing.T) {
	th := &components.Threat{}
	decayClusters(th, 5)
	for i := range th.Clusters {
		if th.Clusters[i] != (components.ThreatCluster{}) {
			t.Fatalf("slot %d was written: %+v", i, th.Clusters[i])
		}
	}
}

func TestDecayValue(t *testing.T) {
	if got := decayValue(1, 0.5, 0.4); math.Abs(float64(got-0.8)) > 1e-4 {
		t.Errorf("decayValue = %.4f, want 0.8", got)
	}
	if got := decayValue(0.1, 10, 1); got != 0 {
		t.Errorf("over-decay = %.4f, want a floor of 0", got)
	}
	if got := decayValue(0, 1, 1); got != 0 {
		t.Errorf("already empty = %.4f", got)
	}
	if got := decayValue(0.5, 1, 0); got != 0.5 {
		t.Errorf("zero rate changed the value to %.4f", got)
	}
}

func TestClamp01(t *testing.T) {
	if clamp01(-2) != 0 || clamp01(2) != 1 || clamp01(0.3) != 0.3 {
		t.Fatal("clamp01 does not clamp to [0, 1]")
	}
}
