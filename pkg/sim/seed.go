package sim

import (
	"encoding/binary"
	"hash/fnv"
)

// Seed generates a deterministic pseudo-random seed for a simulation run.
// It uses FNV-1a 64 over the following byte layout:
// - experimentSeed: 8 bytes, little-endian
// - len(world): 8 bytes, little-endian
// - world bytes: len(world) bytes
// - replication: 8 bytes, little-endian
func Seed(experimentSeed uint64, world WorldID, replication int) uint64 {
	h := fnv.New64a()

	buf := make([]byte, 8)

	// experimentSeed
	binary.LittleEndian.PutUint64(buf, experimentSeed)
	h.Write(buf)

	// len(world)
	worldBytes := []byte(world)
	binary.LittleEndian.PutUint64(buf, uint64(len(worldBytes)))
	h.Write(buf)

	// world bytes
	h.Write(worldBytes)

	// replication
	binary.LittleEndian.PutUint64(buf, uint64(replication))
	h.Write(buf)

	return h.Sum64()
}
