package engine

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"regexp"
)

// ExecutionID represents a unique identifier for a graph execution.
type ExecutionID string

// String returns the string representation of the ExecutionID.
func (e ExecutionID) String() string {
	return string(e)
}

var uuidRegex = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// NewExecutionID generates a new random ExecutionID in UUIDv4 format.
func NewExecutionID() (ExecutionID, error) {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}

	// Set version to 4
	b[6] = (b[6] & 0x0f) | 0x40
	// Set variant to 10
	b[8] = (b[8] & 0x3f) | 0x80

	buf := make([]byte, 36)
	hex.Encode(buf[0:8], b[0:4])
	buf[8] = '-'
	hex.Encode(buf[9:13], b[4:6])
	buf[13] = '-'
	hex.Encode(buf[14:18], b[6:8])
	buf[18] = '-'
	hex.Encode(buf[19:23], b[8:10])
	buf[23] = '-'
	hex.Encode(buf[24:36], b[10:16])

	return ExecutionID(buf), nil
}

// ParseExecutionID validates and returns an ExecutionID from a string.
// The string must be in lowercase UUIDv4 format.
func ParseExecutionID(s string) (ExecutionID, error) {
	if s == "" {
		return "", errors.New("execution ID cannot be empty")
	}

	if !uuidRegex.MatchString(s) {
		return "", errors.New("invalid execution ID format")
	}

	return ExecutionID(s), nil
}
