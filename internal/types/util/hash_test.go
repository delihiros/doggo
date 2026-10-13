package util

import "testing"

func TestMixCollectionHash(t *testing.T) {
	tests := []struct {
		name     string
		hash     uint32
		count    int
		expected bool
	}{
		{"zero hash zero count", 0, 0, true},
		{"zero hash non-zero count", 0, 5, true},
		{"non-zero hash zero count", 12345, 0, true},
		{"non-zero hash non-zero count", 12345, 10, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash1 := MixCollectionHash(tt.hash, tt.count)
			hash2 := MixCollectionHash(tt.hash, tt.count)
			if hash1 != hash2 {
				t.Errorf("MixCollectionHash() = %v, second call = %v, want same value", hash1, hash2)
			}
		})
	}
}
