package components

import "github.com/mlange-42/ark/ecs"

// Affiliation is the perceived side of a Contact (player's belief, not
// ground-truth). APP-6 frame shapes map directly: Friend=rect, Hostile=diamond,
// Neutral=square, Unknown=cloverleaf.
type Affiliation uint8

const (
	AffilUnknown Affiliation = iota
	AffilFriend
	AffilHostile
	AffilNeutral
)

// Dimension is the perceived physical class of a Contact (icon-row selector
// in the symbology builder).
type Dimension uint8

const (
	DimUnknownClass Dimension = iota
	DimInfantryClass
	DimVehicleClass
	DimAirClass
	DimNavalClass
)

// DimensionFromMask converts a single-bit mask to its Dimension equivalent.
// Used by ContactSystem when a sensor confirms a class via DetectMask hit.
func DimensionFromMask(m DimensionMask) Dimension {
	switch m {
	case DimInfantry:
		return DimInfantryClass
	case DimVehicle:
		return DimVehicleClass
	case DimAir:
		return DimAirClass
	case DimNaval:
		return DimNavalClass
	}
	return DimUnknownClass
}

// ClassificationSource ranks who set the contact's perceived affiliation.
// Higher wins: PlayerClassified > CloseRangeID > CombatEvidence > Sensor.
// Auto-rules never downgrade; "Reset to auto" clears PlayerClassified.
type ClassificationSource uint8

const (
	SourceSensor ClassificationSource = iota
	SourceCombatEvidence
	SourceCloseRangeID
	SourcePlayerClassified
)

// Contact is the player's persistent track on a non-player entity. Lives as
// long as either the tracked entity exists or the player keeps it around —
// never auto-deleted, only fades by age (alpha decay) until manually removed
// via the context menu.
type Contact struct {
	Tracked        ecs.Entity
	Observer       uint8 // FactionSide that holds this contact (Phase 18.5: always PlayerFaction)
	EstimatedPos   WorldPos
	LastSeenTime   float32 // global elapsed seconds
	PerceivedAffil Affiliation
	PerceivedDim   Dimension
	Source         ClassificationSource

	// Bearing-only track (Phase 20 M1). An ESM receiver hears an emitter but
	// cannot range it, so EstimatedPos holds the RECEIVER's position and
	// Bearing the direction from it. Nothing may draw a symbol at that point —
	// it is where the listener stood, not where the emitter is. A second
	// source clears BearingOnly and the contact becomes an ordinary track.
	Bearing     float32
	BearingOnly bool
}

// ContactPlayerSet marks a contact whose affiliation was set by the player
// (PlayerClassified source). Auto-promote rules skip these.
type ContactPlayerSet struct{}

// ContactRegistry maps tracked entity → contact entity. Singleton resource so
// ContactSystem's upsert path is O(1) and Map / Inspector readers can resolve
// "is there a contact for this enemy?" in one lookup. Eviction (entity-died,
// contact deleted) keeps the map honest.
type ContactRegistry struct {
	Tracked map[ecs.Entity]ecs.Entity
}

// NewContactRegistry returns a zero-cap registry ready for upsert.
func NewContactRegistry() ContactRegistry {
	return ContactRegistry{Tracked: make(map[ecs.Entity]ecs.Entity, 64)}
}
