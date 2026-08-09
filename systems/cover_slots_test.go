package systems

import (
	"math"
	"testing"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

func coverEntities(n int) []ecs.Entity {
	w := ecs.NewWorld()
	out := make([]ecs.Entity, n)
	for i := range out {
		out[i] = w.NewEntity()
	}
	return out
}

func tallProp() components.PropMeta {
	return components.PropMeta{
		Primitive:  components.PrimitiveCylinder,
		Size:       rl.Vector3{X: 1, Y: 4, Z: 1},
		Cover:      0.8,
		BBoxRadius: 1.5,
	}
}

func TestPropCoverSlotsRingsTheProp(t *testing.T) {
	host := coverEntities(1)[0]
	meta := tallProp()
	centre := rl.Vector3{X: 10, Y: 2, Z: 20}
	slots := propCoverSlots(host, centre, meta, 1)
	if len(slots) != 8 {
		t.Fatalf("got %d slots, want 8", len(slots))
	}
	for i, s := range slots {
		dx, dz := s.Local.X-centre.X, s.Local.Z-centre.Z
		r := float32(math.Hypot(float64(dx), float64(dz)))
		if math.Abs(float64(r-meta.BBoxRadius)) > 1e-3 {
			t.Errorf("slot %d sits %.3f m out, want %.3f", i, r, meta.BBoxRadius)
		}
		if s.Local.Y != centre.Y {
			t.Errorf("slot %d changed height: %.2f", i, s.Local.Y)
		}
		// OriginDir points outward, away from the trunk.
		if dot := s.OriginDir.X*dx + s.OriginDir.Z*dz; dot <= 0 {
			t.Errorf("slot %d faces into the prop (dot %.3f)", i, dot)
		}
		l := float32(math.Hypot(float64(s.OriginDir.X), float64(s.OriginDir.Z)))
		if math.Abs(float64(l-1)) > 1e-3 {
			t.Errorf("slot %d OriginDir length %.4f", i, l)
		}
		if s.Host != host || s.HostKind != components.CoverHostProp {
			t.Errorf("slot %d has the wrong host tag", i)
		}
	}
}

func TestPropCoverSlotsScaleWithTheProp(t *testing.T) {
	host := coverEntities(1)[0]
	meta := tallProp()
	centre := rl.Vector3{}
	slots := propCoverSlots(host, centre, meta, 2)
	r := float32(math.Hypot(float64(slots[0].Local.X), float64(slots[0].Local.Z)))
	if math.Abs(float64(r-meta.BBoxRadius*2)) > 1e-3 {
		t.Fatalf("radius %.3f, want %.3f", r, meta.BBoxRadius*2)
	}
}

func TestPropCoverSlotsQualityTracksCover(t *testing.T) {
	host := coverEntities(1)[0]
	meta := tallProp()
	meta.Cover = 0.5
	half := propCoverSlots(host, rl.Vector3{}, meta, 1)
	meta.Cover = 1
	full := propCoverSlots(host, rl.Vector3{}, meta, 1)
	if half[0].Quality >= full[0].Quality {
		t.Fatalf("half cover %d is not below full cover %d", half[0].Quality, full[0].Quality)
	}
	meta.Cover = 4 // out of range: must saturate, not wrap
	if got := propCoverSlots(host, rl.Vector3{}, meta, 1)[0].Quality; got != 255 {
		t.Fatalf("quality %d, want 255", got)
	}
}

func TestPropCoverSlotsRejectNonCover(t *testing.T) {
	host := coverEntities(1)[0]
	meta := tallProp()
	meta.Cover = 0
	if got := propCoverSlots(host, rl.Vector3{}, meta, 1); got != nil {
		t.Errorf("zero cover produced %d slots", len(got))
	}
	meta = tallProp()
	meta.BBoxRadius = 0
	if got := propCoverSlots(host, rl.Vector3{}, meta, 1); got != nil {
		t.Errorf("zero radius produced %d slots", len(got))
	}
	meta = tallProp()
	meta.Cover = 0.001 // rounds to quality 0
	if got := propCoverSlots(host, rl.Vector3{}, meta, 1); got != nil {
		t.Errorf("negligible cover produced %d slots", len(got))
	}
}

func TestPropCoverSlotsStanceFollowsHeight(t *testing.T) {
	host := coverEntities(1)[0]
	tall := tallProp()
	if got := propCoverSlots(host, rl.Vector3{}, tall, 1)[0].Stance; got&components.StanceMaskStand == 0 {
		t.Error("a 4 m prop should allow standing")
	}
	short := tallProp()
	short.Size.Y = 1
	if got := propCoverSlots(host, rl.Vector3{}, short, 1)[0].Stance; got&components.StanceMaskStand != 0 {
		t.Error("a 1 m prop should be crouch-only")
	}
	// Scale is what decides, not the raw size.
	if got := propCoverSlots(host, rl.Vector3{}, short, 3)[0].Stance; got&components.StanceMaskStand == 0 {
		t.Error("a 1 m prop scaled 3x should allow standing")
	}
}

func TestPropCoverSlotsMeasureSpheresByDiameter(t *testing.T) {
	host := coverEntities(1)[0]
	// A sphere of radius 1 is 2 m tall even though Size.Y reads 1.
	meta := components.PropMeta{
		Primitive:  components.PrimitiveSphere,
		Size:       rl.Vector3{X: 1, Y: 1, Z: 1},
		Cover:      0.6,
		BBoxRadius: 1,
	}
	if got := propCoverSlots(host, rl.Vector3{}, meta, 1)[0].Stance; got&components.StanceMaskStand == 0 {
		t.Fatal("a 2 m sphere should allow standing")
	}
}

func windowWall() components.WallSegment {
	return components.WallSegment{
		Length:         6,
		Yaw:            0, // +Z
		Height:         3,
		Thickness:      0.3,
		OpeningKind:    components.OpeningWindow,
		OpeningCenterT: 0.5,
		OpeningWidth:   1.2,
		OpeningBottom:  1,
		OpeningHeight:  1.2,
	}
}

func TestWindowCoverSlotSitsInTheOpening(t *testing.T) {
	host := coverEntities(1)[0]
	w := windowWall()
	base := rl.Vector3{X: 4, Y: 0, Z: 10}
	outward := rl.Vector3{X: 1}
	slots := windowCoverSlots(host, base, w, outward)
	if len(slots) != 1 {
		t.Fatalf("got %d slots, want 1", len(slots))
	}
	s := slots[0]
	// Yaw 0 runs the wall along +Z, so the centre is 3 m along Z.
	if math.Abs(float64(s.Local.Z-(base.Z+3))) > 1e-3 || math.Abs(float64(s.Local.X-base.X)) > 1e-3 {
		t.Errorf("slot at (%.2f, %.2f), want (%.2f, %.2f)", s.Local.X, s.Local.Z, base.X, base.Z+3)
	}
	if want := base.Y + w.OpeningBottom + w.OpeningHeight*0.5; math.Abs(float64(s.Local.Y-want)) > 1e-3 {
		t.Errorf("slot height %.3f, want %.3f", s.Local.Y, want)
	}
	if s.OriginDir != outward {
		t.Errorf("OriginDir %+v, want the wall normal %+v", s.OriginDir, outward)
	}
	if s.HostKind != components.CoverHostWindow {
		t.Errorf("host kind %v", s.HostKind)
	}
}

func TestWindowCoverSlotFollowsTheOpeningOffset(t *testing.T) {
	host := coverEntities(1)[0]
	w := windowWall()
	w.OpeningCenterT = 0.25
	s := windowCoverSlots(host, rl.Vector3{}, w, rl.Vector3{X: 1})[0]
	if math.Abs(float64(s.Local.Z-1.5)) > 1e-3 {
		t.Fatalf("slot Z %.3f, want 1.5", s.Local.Z)
	}
}

func TestWindowCoverSlotRotatesWithYaw(t *testing.T) {
	host := coverEntities(1)[0]
	w := windowWall()
	w.Yaw = math.Pi / 2 // wall now runs along +X
	s := windowCoverSlots(host, rl.Vector3{}, w, rl.Vector3{Z: 1})[0]
	if math.Abs(float64(s.Local.X-3)) > 1e-2 || math.Abs(float64(s.Local.Z)) > 1e-2 {
		t.Fatalf("slot at (%.3f, %.3f), want (3, 0)", s.Local.X, s.Local.Z)
	}
}

func TestWindowCoverSlotsIgnoreNonWindows(t *testing.T) {
	host := coverEntities(1)[0]
	w := windowWall()
	w.OpeningKind = components.OpeningDoor
	if got := windowCoverSlots(host, rl.Vector3{}, w, rl.Vector3{X: 1}); got != nil {
		t.Errorf("a door produced %d cover slots", len(got))
	}
	w = windowWall()
	w.OpeningWidth = 0
	if got := windowCoverSlots(host, rl.Vector3{}, w, rl.Vector3{X: 1}); got != nil {
		t.Errorf("a zero-width opening produced %d cover slots", len(got))
	}
}

// Two walls meeting at (10, 10): one running along X facing -Z, one along Z
// facing -X. The corner slot goes on the bisector.
func perpendicularCorner(e []ecs.Entity) []wallCornerSrc {
	return []wallCornerSrc{
		{entity: e[0], startX: 0, startZ: 10, endX: 10, endZ: 10, normX: 0, normZ: -1},
		{entity: e[1], startX: 10, startZ: 10, endX: 10, endZ: 20, normX: -1, normZ: 0},
	}
}

func TestWallCornerCoverSlotsSitOnTheBisector(t *testing.T) {
	e := coverEntities(2)
	slots := wallCornerCoverSlots(perpendicularCorner(e))
	if len(slots) != 1 {
		t.Fatalf("got %d slots, want 1", len(slots))
	}
	s := slots[0]
	if math.Abs(float64(s.Local.X-10)) > 1e-3 || math.Abs(float64(s.Local.Z-10)) > 1e-3 {
		t.Errorf("slot at (%.3f, %.3f), want the shared vertex (10, 10)", s.Local.X, s.Local.Z)
	}
	want := float32(-math.Sqrt2 / 2)
	if math.Abs(float64(s.OriginDir.X-want)) > 1e-3 || math.Abs(float64(s.OriginDir.Z-want)) > 1e-3 {
		t.Errorf("OriginDir %+v, want the normalised bisector", s.OriginDir)
	}
}

func TestWallCornerCoverSlotsAreOwnedByTheLowerID(t *testing.T) {
	e := coverEntities(2)
	forward := wallCornerCoverSlots(perpendicularCorner(e))
	src := perpendicularCorner(e)
	reversed := wallCornerCoverSlots([]wallCornerSrc{src[1], src[0]})
	if len(forward) != 1 || len(reversed) != 1 {
		t.Fatalf("slot counts %d and %d", len(forward), len(reversed))
	}
	if forward[0].Host != reversed[0].Host {
		t.Fatalf("ownership depends on input order: %v vs %v", forward[0].Host, reversed[0].Host)
	}
}

func TestWallCornerCoverSlotsDedupeAVertex(t *testing.T) {
	e := coverEntities(3)
	src := perpendicularCorner(e)
	// A third wall on the same vertex, facing +Z.
	src = append(src, wallCornerSrc{entity: e[2], startX: 10, startZ: 10, endX: 20, endZ: 10, normX: 0, normZ: 1})
	if got := wallCornerCoverSlots(src); len(got) != 1 {
		t.Fatalf("three walls on one vertex produced %d slots, want 1", len(got))
	}
}

// The regression: the first pair examined at a vertex may be collinear and
// have no bisector. Claiming the dedupe key before that check threw the
// vertex away, so the perfectly good third wall got nothing.
func TestWallCornerCoverSlotsSurviveACollinearPair(t *testing.T) {
	e := coverEntities(3)
	src := []wallCornerSrc{
		{entity: e[0], startX: 0, startZ: 10, endX: 10, endZ: 10, normX: 0, normZ: -1},
		{entity: e[1], startX: 10, startZ: 10, endX: 20, endZ: 10, normX: 0, normZ: 1},
		{entity: e[2], startX: 10, startZ: 10, endX: 10, endZ: 20, normX: -1, normZ: 0},
	}
	if got := wallCornerCoverSlots(src); len(got) != 1 {
		t.Fatalf("got %d slots, want 1 — the collinear pair burned the vertex", len(got))
	}
}

func TestWallCornerCoverSlotsIgnoreDistantEndpoints(t *testing.T) {
	e := coverEntities(2)
	src := perpendicularCorner(e)
	src[1].startX = 30 // no longer shares the vertex
	src[1].endX = 30
	if got := wallCornerCoverSlots(src); len(got) != 0 {
		t.Fatalf("walls 20 m apart produced %d corner slots", len(got))
	}
}

func TestWallCornerCoverSlotsNeedTwoWalls(t *testing.T) {
	e := coverEntities(1)
	if got := wallCornerCoverSlots([]wallCornerSrc{{entity: e[0]}}); got != nil {
		t.Errorf("a lone wall produced %d slots", len(got))
	}
	if got := wallCornerCoverSlots(nil); got != nil {
		t.Errorf("no walls produced %d slots", len(got))
	}
}

func TestWallCornerCoverSlotsAreChunkLocal(t *testing.T) {
	e := coverEntities(2)
	src := perpendicularCorner(e)
	cc := components.ChunkCoord{X: 2, Z: -1}
	src[0].chunk, src[1].chunk = cc, cc
	s := wallCornerCoverSlots(src)[0]
	wantX := 10 - float32(cc.X)*components.ChunkSize
	wantZ := 10 - float32(cc.Z)*components.ChunkSize
	if math.Abs(float64(s.Local.X-wantX)) > 1e-3 || math.Abs(float64(s.Local.Z-wantZ)) > 1e-3 {
		t.Fatalf("slot at (%.2f, %.2f), want chunk-local (%.2f, %.2f)", s.Local.X, s.Local.Z, wantX, wantZ)
	}
}
