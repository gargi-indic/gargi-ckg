package engine

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
)

// Sentinel errors
var (
	ErrInvalidRegistration = errors.New("invalid node registration")
	ErrDuplicateType       = errors.New("duplicate node type")
	ErrUnknownType         = errors.New("unknown node type")
	ErrInvalidConfig       = errors.New("invalid node config")
)

// NodeFactory is a function that creates a new NodeInterface instance from a decoded config.
// Note: A factory panic is NOT recovered.
type NodeFactory func(config any) (NodeInterface, error)

type registryEntry struct {
	factory NodeFactory
	schema  reflect.Type
	isPtr   bool
}

// Registry manages the available node types and instantiates them.
// It is safe for concurrent use. The zero value is safe to use.
type Registry struct {
	mu      sync.RWMutex
	entries map[string]registryEntry
}

// NewRegistry creates a new, empty Registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// Register adds a new node type to the registry.
// typeName must be non-empty and have no leading/trailing whitespace.
// factory must be non-nil.
// configSchema must be nil, a struct value, or a pointer to a struct.
// Returns ErrDuplicateType if the typeName is already registered.
func (r *Registry) Register(typeName string, factory NodeFactory, configSchema any) error {
	trimmedType := strings.TrimSpace(typeName)
	if trimmedType == "" || trimmedType != typeName {
		return fmt.Errorf("%w: type name must be non-empty and have no leading/trailing whitespace: %q", ErrInvalidRegistration, typeName)
	}

	if factory == nil {
		return fmt.Errorf("%w: factory must be non-nil for type %q", ErrInvalidRegistration, typeName)
	}

	var schemaType reflect.Type
	var isPtr bool

	if configSchema != nil {
		t := reflect.TypeOf(configSchema)
		if t.Kind() == reflect.Ptr {
			isPtr = true
			t = t.Elem()
		}
		if t.Kind() != reflect.Struct {
			return fmt.Errorf("%w: config schema must be nil, a struct, or a pointer to a struct, got %T for type %q", ErrInvalidRegistration, configSchema, typeName)
		}
		schemaType = t
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.entries == nil {
		r.entries = make(map[string]registryEntry)
	}

	if _, exists := r.entries[typeName]; exists {
		return fmt.Errorf("%w: type %q is already registered", ErrDuplicateType, typeName)
	}

	r.entries[typeName] = registryEntry{
		factory: factory,
		schema:  schemaType,
		isPtr:   isPtr,
	}

	return nil
}

type validator interface {
	Validate() error
}

// decodeAndValidateConfig handles the struct marshaling, unmarshaling, trailing data,
// and validation of a given config based on the schema in registryEntry.
func (r *Registry) decodeAndValidateConfig(entry registryEntry, config map[string]any) (any, error) {
	if config == nil {
		config = make(map[string]any)
	}

	if entry.schema == nil {
		m := deepCopyMap(config)
		if m == nil {
			m = make(map[string]any)
		}
		return m, nil
	}

	// Marshal the config map to JSON
	configBytes, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal config map: %w", err)
	}

	// Create a new instance of the schema type
	val := reflect.New(entry.schema)

	decoder := json.NewDecoder(bytes.NewReader(configBytes))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(val.Interface()); err != nil {
		return nil, fmt.Errorf("failed to decode config: %w", err)
	}

	// Check for trailing data
	if decoder.More() {
		return nil, fmt.Errorf("unexpected trailing data in config")
	}

	decodedVal := val.Interface() // *Struct

	// Call Validate(). decodedVal is a pointer, so this correctly triggers Validate()
	// whether it's implemented with a pointer receiver or value receiver.
	if v, ok := decodedVal.(validator); ok {
		if err := v.Validate(); err != nil {
			return nil, fmt.Errorf("validation failed: %w", err)
		}
	}

	if entry.isPtr {
		return decodedVal, nil
	}
	return val.Elem().Interface(), nil
}

// ValidateConfig validates the provided config for the given typeName without calling the node factory.
// Returns ErrUnknownType if the type is not registered, or an error wrapping ErrInvalidConfig if the config is invalid.
func (r *Registry) ValidateConfig(typeName string, config map[string]any) error {
	r.mu.RLock()
	entry, ok := r.entries[typeName]
	r.mu.RUnlock()

	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownType, typeName)
	}

	_, err := r.decodeAndValidateConfig(entry, config)
	if err != nil {
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) {
			return fmt.Errorf("%w: failed to decode config: field %q: unexpected %s value", ErrInvalidConfig, typeErr.Field, typeErr.Value)
		}
		return fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}

	return nil
}

// Instantiate creates a new NodeInterface instance for the given typeName using the provided config.
func (r *Registry) Instantiate(typeName string, config map[string]any) (NodeInterface, error) {
	r.mu.RLock()
	entry, ok := r.entries[typeName]
	r.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownType, typeName)
	}

	decodedConfig, err := r.decodeAndValidateConfig(entry, config)
	if err != nil {
		return nil, fmt.Errorf("%w for type %q: %w", ErrInvalidConfig, typeName, err)
	}

	// Call the factory OUTSIDE the lock
	node, err := entry.factory(decodedConfig)
	if err != nil {
		return nil, fmt.Errorf("factory for type %q failed: %w", typeName, err)
	}
	if node == nil {
		return nil, fmt.Errorf("factory for type %q returned nil node", typeName)
	}

	return node, nil
}

// Has returns true if the typeName is registered.
func (r *Registry) Has(typeName string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.entries == nil {
		return false
	}
	_, ok := r.entries[typeName]
	return ok
}

// Types returns a sorted, fresh slice of all registered type names.
func (r *Registry) Types() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]string, 0, len(r.entries))
	for t := range r.entries {
		types = append(types, t)
	}

	sort.Strings(types)
	return types
}
