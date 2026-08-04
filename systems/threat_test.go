package systems

import (
	"math"
	"testing"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
)

const epsilon = 1e-5

func nearlyEqual(a, b float32) bool {
	return math.Abs(float64(a-b)) <= epsilon
}

// TestThreatDecaySuppression: pre-loaded Suppression decays by the per-second
// rate (0.10) without any new events.
func TestThreatDecaySuppression(t *testing.T) {
	threat := &components.Threat{Suppression: 0.5}
	buf := &components.DangerBuffer{}
	pos := &components.WorldPos{}

	tickThreat(threat, buf, pos, 1.0)

	want := float32(0.5 - 0.10)
	if !nearlyEqual(threat.Suppression, want) {
		t.Errorf("Suppression = %f, want %f", threat.Suppression, want)
	}
	if !nearlyEqual(threat.Total, want) {
		t.Errorf("Total = %f, want %f", threat.Total, want)
	}
	if threat.State != components.ThreatAlerted {
		t.Errorf("State = %d, want ThreatAlerted (0.4 in [0.33,0.66))", threat.State)
	}
}

// TestBulletImpactAccumulation: one BulletImpact event raises Suppression by
// Strength and writes ThreatDir from the event toward the unit.
func TestBulletImpactAccumulation(t *testing.T) {
	threat := &components.Threat{}
	buf := &components.DangerBuffer{}
	components.PushDanger(buf, components.DangerEvent{
		Kind:     components.DangerBulletImpact,
		Strength: 0.5,
		Pos:      components.WorldPos{Local: rl.Vector3{X: 10}},
	})
	pos := &components.WorldPos{Local: rl.Vector3{X: 0}}

	tickThreat(threat, buf, pos, 0)

	if !nearlyEqual(threat.Suppression, 0.5) {
		t.Errorf("Suppression = %f, want 0.5", threat.Suppression)
	}
	if !nearlyEqual(threat.Total, 0.5) {
		t.Errorf("Total = %f, want 0.5", threat.Total)
	}
	if threat.ThreatDir.X >= 0 {
		t.Errorf("ThreatDir.X = %f, want negative (unit west of event)", threat.ThreatDir.X)
	}
	if !nearlyEqual(threat.ThreatDir.X, -1.0) {
		t.Errorf("ThreatDir.X = %f, want -1.0 (normalised)", threat.ThreatDir.X)
	}
}

// TestBufferDrain: tickThreat drains the ring even on zero-dt ticks.
func TestBufferDrain(t *testing.T) {
	buf := &components.DangerBuffer{}
	components.PushDanger(buf, components.DangerEvent{
		Kind: components.DangerBulletImpact, Strength: 0.3,
	})
	if buf.Count != 1 {
		t.Fatalf("setup: Count = %d, want 1", buf.Count)
	}

	tickThreat(&components.Threat{}, buf, &components.WorldPos{}, 0)

	if buf.Count != 0 {
		t.Errorf("buffer not drained: Count = %d", buf.Count)
	}
	if buf.Head != 0 {
		t.Errorf("Head not reset: Head = %d", buf.Head)
	}
}

// TestChannelClamp: oversized event still clamps the channel to 1.
func TestChannelClamp(t *testing.T) {
	threat := &components.Threat{}
	buf := &components.DangerBuffer{}
	components.PushDanger(buf, components.DangerEvent{
		Kind: components.DangerBulletImpact, Strength: 1.5,
	})

	tickThreat(threat, buf, &components.WorldPos{}, 0)

	if !nearlyEqual(threat.Suppression, 1.0) {
		t.Errorf("Suppression = %f, want clamp to 1.0", threat.Suppression)
	}
	if !nearlyEqual(threat.Total, 1.0) {
		t.Errorf("Total = %f, want clamp to 1.0", threat.Total)
	}
	if threat.State != components.ThreatThreatened {
		t.Errorf("State = %d, want ThreatThreatened", threat.State)
	}
}

// TestMultiKindChannels: events of different kinds populate different
// Threat channels.
func TestMultiKindChannels(t *testing.T) {
	threat := &components.Threat{}
	buf := &components.DangerBuffer{}
	components.PushDanger(buf, components.DangerEvent{Kind: components.DangerBulletImpact, Strength: 0.2})
	components.PushDanger(buf, components.DangerEvent{Kind: components.DangerGunshot, Strength: 0.3})
	components.PushDanger(buf, components.DangerEvent{Kind: components.DangerDamageTaken, Strength: 0.4})

	tickThreat(threat, buf, &components.WorldPos{}, 0)

	if !nearlyEqual(threat.Suppression, 0.2) {
		t.Errorf("Suppression = %f, want 0.2", threat.Suppression)
	}
	if !nearlyEqual(threat.ShotsFired, 0.3) {
		t.Errorf("ShotsFired = %f, want 0.3", threat.ShotsFired)
	}
	if !nearlyEqual(threat.Injury, 0.4) {
		t.Errorf("Injury = %f, want 0.4", threat.Injury)
	}
	if !nearlyEqual(threat.Total, 0.9) {
		t.Errorf("Total = %f, want 0.9", threat.Total)
	}
}

// TestThreatDirWeightedAverage: strong east event + weak west event leaves
// ThreatDir pointing west (away from the dominant threat, toward the unit).
func TestThreatDirWeightedAverage(t *testing.T) {
	threat := &components.Threat{}
	buf := &components.DangerBuffer{}
	components.PushDanger(buf, components.DangerEvent{
		Kind: components.DangerBulletImpact, Strength: 0.5,
		Pos: components.WorldPos{Local: rl.Vector3{X: 10}},
	})
	components.PushDanger(buf, components.DangerEvent{
		Kind: components.DangerBulletImpact, Strength: 0.1,
		Pos: components.WorldPos{Local: rl.Vector3{X: -10}},
	})

	tickThreat(threat, buf, &components.WorldPos{}, 0)

	if threat.ThreatDir.X >= 0 {
		t.Errorf("ThreatDir.X = %f, want negative (strong east event dominates)", threat.ThreatDir.X)
	}
}

// TestClassifyThreatBands verifies the four-band classifier on threshold
// edges. The constants are load-bearing for SurvivalInstinct and
// StanceController — freeze them with a test.
func TestClassifyThreatBands(t *testing.T) {
	cases := []struct {
		total float32
		want  components.ThreatState
	}{
		{0.0, components.ThreatSafe},
		{0.049, components.ThreatSafe},
		{0.05, components.ThreatVigilant},
		{0.329, components.ThreatVigilant},
		{0.33, components.ThreatAlerted},
		{0.659, components.ThreatAlerted},
		{0.66, components.ThreatThreatened},
		{1.0, components.ThreatThreatened},
	}
	for _, c := range cases {
		got := components.ClassifyThreat(c.total)
		if got != c.want {
			t.Errorf("ClassifyThreat(%f) = %d, want %d", c.total, got, c.want)
		}
	}
}

// TestRingOverwrite: pushing more than DangerBufferSize events keeps the
// newest ones; Count caps at DangerBufferSize.
func TestRingOverwrite(t *testing.T) {
	buf := &components.DangerBuffer{}
	for i := 0; i < components.DangerBufferSize+3; i++ {
		components.PushDanger(buf, components.DangerEvent{
			Kind: components.DangerBulletImpact, Strength: 0.05,
		})
	}
	if buf.Count != components.DangerBufferSize {
		t.Errorf("Count = %d, want %d (cap)", buf.Count, components.DangerBufferSize)
	}
}

// TestChannelDecayRatesDistinct locks the per-channel decay rates so a
// future tune doesn't silently shift behaviour.
func TestChannelDecayRatesDistinct(t *testing.T) {
	if components.ChannelDecayRates[components.ThreatChannelSuppression] != 0.10 {
		t.Errorf("Suppression decay = %f, want 0.10",
			components.ChannelDecayRates[components.ThreatChannelSuppression])
	}
	if components.ChannelDecayRates[components.ThreatChannelInjury] != 0.05 {
		t.Errorf("Injury decay = %f, want 0.05",
			components.ChannelDecayRates[components.ThreatChannelInjury])
	}
}

// TestCrossfireSplitsIntoTwoClusters: shooters from opposite bearings must
// stay two distinct clusters — an averaged direction points between them,
// i.e. at cover that stops neither.
func TestCrossfireSplitsIntoTwoClusters(t *testing.T) {
	threat := &components.Threat{}
	buf := &components.DangerBuffer{}
	pos := &components.WorldPos{}
	components.PushDanger(buf, components.DangerEvent{
		Kind: components.DangerBulletImpact, Strength: 0.4,
		Pos: components.WorldPos{Local: rl.Vector3{X: 20}},
	})
	components.PushDanger(buf, components.DangerEvent{
		Kind: components.DangerBulletImpact, Strength: 0.3,
		Pos: components.WorldPos{Local: rl.Vector3{Z: 20}},
	})
	tickThreat(threat, buf, pos, 0)

	if threat.Clusters[0].Weight <= 0 || threat.Clusters[1].Weight <= 0 {
		t.Fatalf("want two live clusters, got %+v", threat.Clusters)
	}
	if threat.Clusters[0].Weight < threat.Clusters[1].Weight {
		t.Errorf("clusters not sorted by weight: %+v", threat.Clusters)
	}
	// Dominant = the stronger west-pointing bearing (unit is west of it).
	if threat.Clusters[0].Dir.X >= 0 {
		t.Errorf("dominant Dir.X = %f, want negative", threat.Clusters[0].Dir.X)
	}
	if !nearlyEqual(threat.ThreatDir.X, threat.Clusters[0].Dir.X) {
		t.Errorf("ThreatDir must mirror the dominant cluster: %v vs %v",
			threat.ThreatDir, threat.Clusters[0].Dir)
	}
	if !nearlyEqual(threat.Clusters[0].Dist, 20) {
		t.Errorf("Dist = %f, want 20", threat.Clusters[0].Dist)
	}
}

// TestNearbyBearingsMerge: two events inside the merge cone are one cluster.
func TestNearbyBearingsMerge(t *testing.T) {
	threat := &components.Threat{}
	buf := &components.DangerBuffer{}
	pos := &components.WorldPos{}
	components.PushDanger(buf, components.DangerEvent{
		Kind: components.DangerBulletImpact, Strength: 0.2,
		Pos: components.WorldPos{Local: rl.Vector3{X: 20}},
	})
	components.PushDanger(buf, components.DangerEvent{
		Kind: components.DangerBulletImpact, Strength: 0.2,
		Pos: components.WorldPos{Local: rl.Vector3{X: 20, Z: 4}},
	})
	tickThreat(threat, buf, pos, 0)

	if threat.Clusters[1].Weight != 0 {
		t.Errorf("want one merged cluster, got %+v", threat.Clusters)
	}
	if !nearlyEqual(threat.Clusters[0].Weight, 0.4) {
		t.Errorf("merged Weight = %f, want 0.4", threat.Clusters[0].Weight)
	}
}

// TestClusterDecayDropsStaleBearing: with no fresh events the bearing bleeds
// out and the slot frees.
func TestClusterDecayDropsStaleBearing(t *testing.T) {
	threat := &components.Threat{}
	buf := &components.DangerBuffer{}
	pos := &components.WorldPos{}
	components.PushDanger(buf, components.DangerEvent{
		Kind: components.DangerBulletImpact, Strength: 0.5,
		Pos: components.WorldPos{Local: rl.Vector3{X: 20}},
	})
	tickThreat(threat, buf, pos, 0)
	if threat.Clusters[0].Weight <= 0 {
		t.Fatal("cluster not seeded")
	}
	// Long enough for both the channel and the cluster to bleed out.
	tickThreat(threat, buf, pos, 30)
	if threat.Clusters[0].Weight != 0 {
		t.Errorf("stale cluster survived: %+v", threat.Clusters[0])
	}
	if threat.ThreatDir.X != 0 || threat.ThreatDir.Z != 0 {
		t.Errorf("ThreatDir must clear with the last cluster: %v", threat.ThreatDir)
	}
}
