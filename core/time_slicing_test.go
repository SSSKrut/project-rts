package core

import "testing"

func TestShouldProcessBucketSingle(t *testing.T) {
	for id := uint32(0); id < 100; id++ {
		for f := uint32(0); f < 100; f++ {
			if !ShouldProcessBucket(id, 1, f) {
				t.Fatalf("bucketCount=1 returned false for id=%d frame=%d", id, f)
			}
			if !ShouldProcessBucket(id, 0, f) {
				t.Fatalf("bucketCount=0 returned false for id=%d frame=%d", id, f)
			}
		}
	}
}

func TestShouldProcessBucketModulo(t *testing.T) {
	// id=5, bucketCount=10 -> true only when frameIdx%10 == 5.
	for f := uint32(0); f < 30; f++ {
		want := (f % 10) == 5
		got := ShouldProcessBucket(5, 10, f)
		if got != want {
			t.Fatalf("frame=%d want=%v got=%v", f, want, got)
		}
	}
}
