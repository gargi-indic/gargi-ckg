// Package engine provides the core runtime and interfaces for the reactive graph engine.
//
// Example of registering a custom node type:
//
//	type MyConfig struct {
//		MaxRetries int `json:"max_retries"`
//	}
//
//	func (c MyConfig) Validate() error {
//		if c.MaxRetries < 0 {
//			return errors.New("max_retries cannot be negative")
//		}
//		return nil
//	}
//
//	type MyNode struct {
//		*engine.BaseNode
//		cfg MyConfig
//	}
//
//	func (n *MyNode) Execute(ctx context.Context, nc engine.NodeContext) (*engine.StateMutation, error) {
//		// custom logic here
//		return nil, nil
//	}
//
//	// Example of registry initialization (typically done during app startup):
//	// reg := engine.NewRegistry()
//	// err := reg.Register("my_node", func(cfg any) (engine.NodeInterface, error) {
//	// 	return &MyNode{
//	// 		BaseNode: &engine.BaseNode{},
//	// 		cfg:      cfg.(MyConfig),
//	// 	}, nil
//	// }, MyConfig{})
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

// Instantiate creates a new NodeInterface instance for the given typeName using the provided config.
func (r *Registry) Instantiate(typeName string, config map[string]any) (NodeInterface, error) {
	r.mu.RLock()
	entry, ok := r.entries[typeName]
	r.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownType, typeName)
	}

	if config == nil {
		config = make(map[string]any)
	}

	var decodedConfig any

	if entry.schema == nil {
		m := deepCopyMap(config)
		if m == nil {
			m = make(map[string]any)
		}
		decodedConfig = m
	} else {
		// Marshal the config map to JSON
		configBytes, err := json.Marshal(config)
		if err != nil {
			return nil, fmt.Errorf("%w for type %q: failed to marshal config map: %w", ErrInvalidConfig, typeName, err)
		}

		// Create a new instance of the schema type
		val := reflect.New(entry.schema)

		decoder := json.NewDecoder(bytes.NewReader(configBytes))
		decoder.DisallowUnknownFields()

		if err := decoder.Decode(val.Interface()); err != nil {
			return nil, fmt.Errorf("%w for type %q: failed to decode config: %w", ErrInvalidConfig, typeName, err)
		}

		// Check for trailing data
		if decoder.More() {
			return nil, fmt.Errorf("%w for type %q: unexpected trailing data in config", ErrInvalidConfig, typeName)
		}

		decodedVal := val.Interface() // *Struct

		// Call Validate() if implemented by the pointer
		if v, ok := decodedVal.(validator); ok {
			if err := v.Validate(); err != nil {
				return nil, fmt.Errorf("%w for type %q: validation failed: %w", ErrInvalidConfig, typeName, err)
			}
		} else if !entry.isPtr {
			// Call Validate() if implemented by the value (rare, but handled)
			if v, ok := val.Elem().Interface().(validator); ok {
				if err := v.Validate(); err != nil {
					return nil, fmt.Errorf("%w for type %q: validation failed: %w", ErrInvalidConfig, typeName, err)
				}
			}
		}

		if entry.isPtr {
			decodedConfig = decodedVal
		} else {
			decodedConfig = val.Elem().Interface()
		}
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
