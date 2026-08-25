package components

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
)

// AssetSide indexes the faction axis of the model set. The two sides answer the
// same doctrine with different engineering (see PLANET-SETTING in the model
// repo), so the split is permanent — but it is an ART axis: both sides share a
// VehicleSpec row, so swapping a model can never move a replay hash.
type AssetSide uint8

const (
	SideWest AssetSide = iota
	SideEast
	AssetSideCount
)

// AssetSideForFaction maps who owns the hull to which factory built it.
func AssetSideForFaction(f uint8) AssetSide {
	if f == FactionPlayer {
		return SideWest
	}
	return SideEast
}

// VehicleAssetName binds a class and a side to a baked model. Empty means "no
// model" — the renderer draws its placeholder box and the sim uses hull-box
// offsets.
//
// This lives in components rather than in the renderer because two very
// different consumers need it: the draw path resolves it to a ModelID at boot,
// and cmd/genmounts reads it to bake the sim-facing mount table below. Neither
// the string nor the manifest is ever read during a tick.
var VehicleAssetName = [VehicleKindCount][AssetSideCount]string{
	VehicleTruck:     {SideWest: "m35", SideEast: "ural"},
	VehicleBTR:       {SideWest: "commando", SideEast: "btr60"},
	VehicleBMP:       {SideWest: "m113", SideEast: "bmp1"},
	VehicleTank:      {SideWest: "tank_w", SideEast: "tank_e"},
	VehicleATCarrier: {SideWest: "ferret", SideEast: "brdm"},
	VehicleCar:       {SideWest: "mrap_2", SideEast: "btr40"},
	VehicleAAGun:     {SideWest: "aa_gun_w", SideEast: "aa_gun_e"},
	VehicleCommand:   {}, // no model yet — the renderer draws its box
}

// RotateYawXZ turns a model-space offset (forward +Z, up +Y, left +X) into a
// world delta for a hull at the given yaw. Matches the heading convention the
// driver integrates with: forward is (sin yaw, cos yaw).
func RotateYawXZ(v rl.Vector3, yaw float32) rl.Vector3 {
	s := float32(math.Sin(float64(yaw)))
	c := float32(math.Cos(float64(yaw)))
	return rl.Vector3{X: v.X*c + v.Z*s, Y: v.Y, Z: -v.X*s + v.Z*c}
}
