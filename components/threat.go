package components

// ThreatSource is a short-lived ECS entity spawned at the muzzle of each
// shot. SurvivalInstinctSystem reads the field cluster to pick a cover slot:
// the weighted average of nearby ThreatSource origins gives a "where is the
// danger coming from" vector that stays stable across one threat pulse even
// as bullets fly out.
//
// Writer: WeaponSystem.serialApply spawns one entity per shot. Cleanup:
// ThreatDecaySystem despawns expired entries.
//
// Entity layout: ThreatSource + WorldPos. Severity grows with weapon damage
// so an MG burst pushes more weight than a single rifle round.
type ThreatSource struct {
	Severity  float32 // 0..1 fire intensity / round weight
	SpawnTime float32 // session-time at spawn
	TTL       float32 // seconds before ThreatDecaySystem despawns
}
