package buildings

import (
	"fmt"
	"testing"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
)

func at(x, z float32) components.WorldPos {
	return components.WorldPos{}.Add(rl.Vector3{X: x, Z: z})
}

// sample is one generator invocation plus enough context to name it in a
// failure message.
type sample struct {
	name  string
	plans []*components.BuildingPlan
	kind  components.BuildingKind
}

// allSamples spans every template, 1..5 storeys and a spread of seeds — the
// same matrix cmd/gen_sandbox shows, minus the window.
func allSamples() []sample {
	var out []sample
	seeds := []uint64{0xA1, 0xB2, 0xC3, 0xD4, 0xE5, 0xF6}
	pos := at(32, 32)

	for _, seed := range seeds {
		for stories := uint8(1); stories <= 5; stories++ {
			for _, kind := range []components.BuildingKind{
				components.BuildingHouse, components.BuildingBunker,
			} {
				p := GenerateHouse(seed, HouseParams{
					Stories:   stories,
					SizeX:     10,
					SizeZ:     8,
					DoorSides: []uint8{0},
					Interior:  stories > 2,
				}, pos, kind)
				out = append(out, sample{
					name:  fmt.Sprintf("house/%#x/%dst/kind%d", seed, stories, kind),
					plans: []*components.BuildingPlan{p},
					kind:  kind,
				})
			}
		}
		out = append(out, sample{
			name:  fmt.Sprintf("office/%#x", seed),
			plans: []*components.BuildingPlan{GenerateOffice(seed, pos)},
		})
		out = append(out, sample{
			name:  fmt.Sprintf("compound/%#x", seed),
			plans: GenerateCompound(seed, pos),
		})
		out = append(out, sample{
			name:  fmt.Sprintf("compound_plus/%#x", seed),
			plans: GenerateCompoundPlus(seed, pos),
		})
	}
	return out
}

// TestGeneratedPlansValidate is the GPU-free half of the sandbox: every
// template the game can spawn must come out structurally well formed.
func TestGeneratedPlansValidate(t *testing.T) {
	for _, s := range allSamples() {
		for i, p := range s.plans {
			for _, iss := range Validate(p) {
				if iss.Severity == SeverityError {
					t.Errorf("%s plan[%d]: %s", s.name, i, iss)
				}
			}
		}
	}
}

// TestStairsBridgeTwoLevels locks ISSUES #20. BuildingSystem.spawnBuilding
// derives StairLevels{From,To} from Anchors[0] and Anchors[len-1]; when those
// name the SAME level, spatial_bake_transitions reads it as a bunker entrance
// and wires level<->surface instead of level<->level. A cascade flight used to
// carry a single anchor, so 3+ storey buildings ended up with no stair edge
// between floors at all.
//
// Bunker entrance stairs are the one legitimate From == To: the exterior
// surface is not modelled as a Level.
func TestStairsBridgeTwoLevels(t *testing.T) {
	for _, s := range allSamples() {
		if s.kind == components.BuildingBunker {
			continue
		}
		for pi, p := range s.plans {
			for si := range p.Stairs {
				st := &p.Stairs[si]
				if len(st.Anchors) == 0 {
					t.Errorf("%s plan[%d] stair[%d]: no anchors", s.name, pi, si)
					continue
				}
				from := st.Anchors[0].LevelRef
				to := st.Anchors[len(st.Anchors)-1].LevelRef
				if from == to {
					t.Errorf("%s plan[%d] stair[%d]: From == To == %d — "+
						"would bake as a bunker entrance, not a floor link",
						s.name, pi, si, from)
				}
				if int(from) >= len(p.Levels) || int(to) >= len(p.Levels) {
					t.Errorf("%s plan[%d] stair[%d]: LevelRef out of range (%d/%d of %d)",
						s.name, pi, si, from, to, len(p.Levels))
				}
			}
		}
	}
}

// TestRoofCapsTopStorey locks the roof contract: every above-ground building
// gets exactly one roof, anchored to its TOP level (the cutaway renderer drops
// the roof together with that level), sitting at eave height, and its flat
// deck never collapses into a ridge.
func TestRoofCapsTopStorey(t *testing.T) {
	for _, s := range allSamples() {
		for pi, p := range s.plans {
			if s.kind == components.BuildingBunker {
				if len(p.Roofs) != 0 {
					t.Errorf("%s plan[%d]: bunker should have no roof, got %d",
						s.name, pi, len(p.Roofs))
				}
				continue
			}
			if len(p.Roofs) != 1 {
				t.Errorf("%s plan[%d]: want 1 roof, got %d", s.name, pi, len(p.Roofs))
				continue
			}
			r := p.Roofs[0]
			if want := uint8(len(p.Levels) - 1); r.LevelRef != want {
				t.Errorf("%s plan[%d]: roof LevelRef=%d, want top level %d",
					s.name, pi, r.LevelRef, want)
			}
			deckX := r.Roof.SizeX - 2*r.Roof.Inset
			deckZ := r.Roof.SizeZ - 2*r.Roof.Inset
			if deckX < components.RoofMinDeck-1e-3 || deckZ < components.RoofMinDeck-1e-3 {
				t.Errorf("%s plan[%d]: deck %.2f x %.2f below RoofMinDeck %.2f",
					s.name, pi, deckX, deckZ, components.RoofMinDeck)
			}
			if r.Roof.Height <= 0 {
				t.Errorf("%s plan[%d]: roof height %.2f", s.name, pi, r.Roof.Height)
			}
			// Eave sits on top of the topmost wall ring, not inside it.
			wantY := p.Pos.Local.Y + float32(p.Stories)*components.FloorHeight
			if diff := r.Local.Y - wantY; diff > 1e-3 || diff < -1e-3 {
				t.Errorf("%s plan[%d]: roof Y=%.2f, want eave %.2f",
					s.name, pi, r.Local.Y, wantY)
			}
		}
	}
}

// TestCourtyardIsAClosedRing locks the toroidal experiment: the well must stay
// open (no floor strip covers its centre) and be sealed by an inner wall ring
// on EVERY storey — LevelNavGrid seeds the whole level AABB walkable and only
// walls carve it, so a missing inner ring means units cut straight across the
// void instead of walking the loop.
func TestCourtyardIsAClosedRing(t *testing.T) {
	for _, seed := range []uint64{0xA1, 0xB2, 0xC3} {
		for stories := uint8(1); stories <= 4; stories++ {
			p := DefaultCourtyardParams()
			p.Stories = stories
			plan := GenerateCourtyard(seed, p, at(32, 32))

			for _, iss := range Validate(plan) {
				if iss.Severity == SeverityError {
					t.Errorf("courtyard %#x/%dst: %s", seed, stories, iss)
				}
			}

			cx, cz := plan.Pos.Local.X, plan.Pos.Local.Z
			for i := range plan.Floors {
				f := plan.Floors[i]
				if absF(f.Local.X-cx) < f.Floor.SizeX*0.5 && absF(f.Local.Z-cz) < f.Floor.SizeZ*0.5 {
					t.Errorf("courtyard %#x/%dst: floor[%d] covers the well centre",
						seed, stories, i)
				}
			}

			// 8 walls per storey: 4 outer + 4 inner.
			if want := int(stories) * 8; len(plan.Walls) != want {
				t.Errorf("courtyard %#x/%dst: %d walls, want %d (4 outer + 4 inner per storey)",
					seed, stories, len(plan.Walls), want)
			}
			// A gallery narrower than a stair run cannot be walked around.
			gx := (p.SizeX - p.WellX) * 0.5
			gz := (p.SizeZ - p.WellZ) * 0.5
			if gx < MinGalleryWidth-1e-3 || gz < MinGalleryWidth-1e-3 {
				t.Errorf("courtyard %#x: gallery %.2f x %.2f below min %.2f",
					seed, gx, gz, MinGalleryWidth)
			}
		}
	}
}

func absF(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// TestMultiStoreyHasStairs guards the other direction: a building with N
// storeys must actually offer a way up, or OccupyBuilding on an upper level is
// unreachable.
func TestMultiStoreyHasStairs(t *testing.T) {
	for stories := uint8(2); stories <= 5; stories++ {
		p := GenerateHouse(0xA1, HouseParams{
			Stories: stories, SizeX: 10, SizeZ: 8, DoorSides: []uint8{0},
		}, at(32, 32), components.BuildingHouse)

		linked := map[[2]uint8]bool{}
		for i := range p.Stairs {
			st := &p.Stairs[i]
			if len(st.Anchors) < 2 {
				continue
			}
			a := st.Anchors[0].LevelRef
			b := st.Anchors[len(st.Anchors)-1].LevelRef
			if a > b {
				a, b = b, a
			}
			linked[[2]uint8{a, b}] = true
		}
		for s := uint8(0); s+1 < stories; s++ {
			if !linked[[2]uint8{s, s + 1}] {
				t.Errorf("stories=%d: no stair links L%d to L%d", stories, s, s+1)
			}
		}
	}
}
