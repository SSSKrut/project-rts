package components

import rl "github.com/gen2brain/raylib-go/raylib"

type PrimitiveKind uint8

const (
	PrimitiveCube PrimitiveKind = iota
	PrimitiveSphere
	PrimitiveCylinder
	PrimitiveCone
	PrimitivePlane
	// PrimitiveTree is a cylinder trunk + cone canopy composite (Oak/Birch).
	// Pine uses a single Cone.
	PrimitiveTree
)

// PropMeta carries every per-type attribute gameplay or rendering may need.
// POD so the registry sits in a fixed array and is indexed by PropType in
// O(1) without a map lookup.
//
// Size semantics by primitive:
//   Cube     - Size.X/Y/Z are full side lengths.
//   Sphere   - Size.X is radius; Y/Z unused.
//   Cylinder - Size.X = radius, Size.Y = height.
//   Cone     - Size.X = base radius, Size.Y = height.
//   Plane    - Size.X/Z are full extents on XZ.
//   Tree     - Size.X = trunk radius, Size.Y = trunk height,
//              Size.Z = canopy radius. Canopy height = 1.5 x trunk height.
type PropMeta struct {
	Primitive   PrimitiveKind
	Size        rl.Vector3
	Color       rl.Color // canopy / body
	TrunkColor  rl.Color // PrimitiveTree only
	Cover       float32  // [0, 1]
	HP          float32
	BBoxRadius  float32 // flat-XZ radius for spatial queries
	BlocksLOS   bool
	BlocksMove  bool
	Traversable bool // bridges, doors
}

// PropTypeRegistry: 256 slots indexed by PropType.
type PropTypeRegistry struct {
	Metas [256]PropMeta
}
