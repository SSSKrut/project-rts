package components

import rl "github.com/gen2brain/raylib-go/raylib"

// MapPingKind names what triggered the ping. Inspector / future legend can
// remap the colour, but the default palette is fixed per kind.
type MapPingKind uint8

const (
	MapPingKIA MapPingKind = iota
	MapPingContact
	MapPingBuildingCleared
)

// MapPing is the per-event animated marker entity. Phase 15 spawns on KIA
// + EnemyContact (BuildingCleared lands with Phase 16 Buildings 2.0). The
// entity carries WorldPos; the renderer draws a pulsing ring centred there.
//
// SpawnAt + TTL drive both the despawn pass and the pulsing animation (the
// ring radius oscillates at sin(time * 4)).
type MapPing struct {
	Kind     MapPingKind
	SpawnAt  float32
	TTL      float32
	Color    rl.Color
	BaseRadM float32 // base ring radius in metres before pulse
}
