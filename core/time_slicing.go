package core

// ShouldProcessBucket returns true when an entity is assigned to the current
// tick's bucket. The contract:
//
//   - bucketCount == 0 || bucketCount == 1 -> always true (no slicing).
//   - bucketCount  > 1                     -> entityID-hash modulo bucketCount
//                                            must equal frameIdx % bucketCount.
//
// Phase 11.5 ships with bucketCount=1 everywhere (no-op). Phase 14 raises it
// per-system to spread vision raycasts / tactical AI across N frames.
//
// entityID is taken raw - Ark's ecs.Entity is a {ID, Generation} pair and the
// ID is stable across the entity's lifetime, which is good enough for the
// scheduler hash. We deliberately don't fold generation in: that would shuffle
// buckets every respawn and produce visible cadence jitter.
func ShouldProcessBucket(entityID uint32, bucketCount uint32, frameIdx uint32) bool {
	if bucketCount <= 1 {
		return true
	}
	return entityID%bucketCount == frameIdx%bucketCount
}
