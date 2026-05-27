package core

// ShouldProcessBucket returns true when an entity is assigned to the current
// tick's bucket:
//
//   - bucketCount <= 1: always true (no slicing).
//   - bucketCount  > 1: entityID % bucketCount == frameIdx % bucketCount.
//
// entityID is taken raw - we deliberately don't fold generation in: that
// would shuffle buckets every respawn and produce visible cadence jitter.
func ShouldProcessBucket(entityID uint32, bucketCount uint32, frameIdx uint32) bool {
	if bucketCount <= 1 {
		return true
	}
	return entityID%bucketCount == frameIdx%bucketCount
}
