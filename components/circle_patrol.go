package components

// CirclePatrol drives a unit along a circular path around Center. Used by
// Phase 18.5 wildlife / hostile-dummy test spawns to validate FoW contact
// aging on moving targets. Not a substitute for real tactical AI — once
// vehicles / proper enemy spawners land this can be deleted along with the
// circle_patrol system.
type CirclePatrol struct {
	Center  WorldPos
	RadiusM float32
	Speed   float32
	Phase   float32
}
