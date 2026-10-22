package sim

// WorldID is the unique identifier for a simulation world.
type WorldID string

// World represents a single variant of the simulation environment.
type World struct {
	// ID is the unique identifier for this world.
	ID WorldID

	// Params holds the global parameters for this world variant.
	Params map[string]any
}
