package util

import "hash/fnv"

// MixHash combines two hash values.
// Based on Clojure's mix-collection-hash algorithm logic (simplified).
func MixHash(h1, h2 uint32) uint32 {
	return h1 ^ (h2 + 0x9e3779b9 + (h1 << 6) + (h1 >> 2))
}

// MixCollectionHash mixes the hash with the count of items.
func MixCollectionHash(hash uint32, count int) uint32 {
	return MixHash(hash, uint32(count))
}

// HashString calculates the hash for a string.
func HashString(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}

