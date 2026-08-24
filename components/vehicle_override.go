package components

// VehicleReflexKind selects the emergency behaviour a vehicle class runs when
// threat crosses the reflex threshold. One per class, stored in VehicleSpec.
type VehicleReflexKind uint8

const (
	VehicleReflexNone VehicleReflexKind = iota
	// VehicleReflexFaceThreat pivots the hull so the frontal (thickest) armor
	// faces the danger; the vehicle holds position.
	VehicleReflexFaceThreat
	// VehicleReflexSmokeAndReverse drops a concealment field and backs away
	// while keeping the nose on the threat.
	VehicleReflexSmokeAndReverse
	// VehicleReflexFlee routes a full-speed retreat through the normal order
	// queue (soft-skinned classes).
	VehicleReflexFlee
	// VehicleReflexGoDark shuts the radar down under fire the crew cannot
	// answer (Phase 20 M2) — the AI half of the P4 emissions dilemma. No
	// locomotion: the mount stays put, only the emitter goes quiet.
	VehicleReflexGoDark
)

// VehicleOverride is the reactive-driver state, always present on a vehicle
// (Kind == None while idle). While Kind != None the reflex owns the driver
// (FaceThreat / SmokeAndReverse) or the order queue (Flee). Plain data so it
// rides save/load by memcpy; LastAt survives a cleared reflex to gate the
// re-entry cooldown.
type VehicleOverride struct {
	Kind      VehicleReflexKind
	Until     float32  // sim time the reflex releases control
	ThreatYaw float32  // hull yaw that points at the dominant threat
	Retreat   WorldPos // origin snapshot (SmokeAndReverse) / retreat goal (Flee)
	LastAt    float32  // sim time the last reflex fired — cooldown anchor
}

// VehicleReflexLabel is the Inspector reason string for an active reflex.
func VehicleReflexLabel(k VehicleReflexKind) string {
	switch k {
	case VehicleReflexFaceThreat:
		return "Facing threat"
	case VehicleReflexSmokeAndReverse:
		return "Smoke and reverse"
	case VehicleReflexFlee:
		return "Fleeing"
	case VehicleReflexGoDark:
		return "Radar dark"
	}
	return ""
}
