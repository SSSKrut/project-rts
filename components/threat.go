package components

// ThreatSource — short-lived ECS entity spawned at the muzzle of each shot.
// Phase 15 SurvivalInstinctSystem reads the field cluster to pick a cover
// slot: the weighted average of nearby ThreatSource origins gives a "where
// is the danger coming from" vector that's stable across one threat pulse
// even as bullets fly out.
//
// Phase 14 M14.5 ships the writer (WeaponSystem.serialApply spawns one
// entity per shot) + a cleanup System (ThreatDecaySystem) that despawns
// expired entries. No reader yet — that's the Phase 15 contract.
//
// Entity layout: ThreatSource + WorldPos. Severity grows with weapon
// damage / RoF so a single MG burst pushes more weight than one rifle
// round. Phase 14 simple: Severity = damage / 100.
type ThreatSource struct {
	Severity  float32 // 0..1 — fire intensity / round weight
	SpawnTime float32 // session-time at spawn
	TTL       float32 // seconds before ThreatDecaySystem despawns
}
