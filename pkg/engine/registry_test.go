package engine

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

// Dummy node implementation for testing
type dummyNode struct {
	*BaseNode
	cfg any
}

func newDummyFactory() NodeFactory {
	return func(config any) (NodeInterface, error) {
		return &dummyNode{BaseNode: &BaseNode{}, cfg: config}, nil
	}
}

// Test schema structs
type structSchema struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

type nestedSchema struct {
	Settings structSchema `json:"settings"`
}

type validatableSchema struct {
	Count int `json:"count"`
}

func (v validatableSchema) Validate() error {
	if v.Count < 0 {
		return errors.New("count cannot be negative")
	}
	return nil
}

type validatablePtrSchema struct {
	Count int `json:"count"`
}

func (v *validatablePtrSchema) Validate() error {
	if v.Count < 0 {
		return errors.New("count cannot be negative")
	}
	return nil
}

func TestRegistry_Register(t *testing.T) {
	tests := []struct {
		name         string
		typeName     string
		factory      NodeFactory
		configSchema any
		wantErr      error
	}{
		{
			name:         "ok nil schema",
			typeName:     "test_nil",
			factory:      newDummyFactory(),
			configSchema: nil,
			wantErr:      nil,
		},
		{
			name:         "ok struct schema",
			typeName:     "test_struct",
			factory:      newDummyFactory(),
			configSchema: structSchema{},
			wantErr:      nil,
		},
		{
			name:         "ok ptr struct schema",
			typeName:     "test_ptr",
			factory:      newDummyFactory(),
			configSchema: &structSchema{},
			wantErr:      nil,
		},
		{
			name:         "empty name",
			typeName:     "",
			factory:      newDummyFactory(),
			configSchema: nil,
			wantErr:      ErrInvalidRegistration,
		},
		{
			name:         "space padded name",
			typeName:     "  test  ",
			factory:      newDummyFactory(),
			configSchema: nil,
			wantErr:      ErrInvalidRegistration,
		},
		{
			name:         "nil factory",
			typeName:     "test_nil_factory",
			factory:      nil,
			configSchema: nil,
			wantErr:      ErrInvalidRegistration,
		},
		{
			name:         "invalid schema int",
			typeName:     "test_int",
			factory:      newDummyFactory(),
			configSchema: 123,
			wantErr:      ErrInvalidRegistration,
		},
		{
			name:         "invalid schema map",
			typeName:     "test_map",
			factory:      newDummyFactory(),
			configSchema: map[string]string{},
			wantErr:      ErrInvalidRegistration,
		},
		{
			name:         "invalid schema slice",
			typeName:     "test_slice",
			factory:      newDummyFactory(),
			configSchema: []string{},
			wantErr:      ErrInvalidRegistration,
		},
		{
			name:         "invalid schema func",
			typeName:     "test_func",
			factory:      newDummyFactory(),
			configSchema: func() {},
			wantErr:      ErrInvalidRegistration,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRegistry()
			err := r.Register(tt.typeName, tt.factory, tt.configSchema)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Register() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRegistry_DuplicateRegistration(t *testing.T) {
	r := NewRegistry()
	fact1 := newDummyFactory()
	fact2 := newDummyFactory()

	err := r.Register("test", fact1, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = r.Register("test", fact2, structSchema{})
	if !errors.Is(err, ErrDuplicateType) {
		t.Errorf("expected ErrDuplicateType, got %v", err)
	}

	// Verify original is untouched
	node, err := r.Instantiate("test", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	dummy := node.(*dummyNode)
	// For nil schema, decoded config is map[string]any
	if _, ok := dummy.cfg.(map[string]any); !ok {
		t.Errorf("expected map[string]any config, got %T", dummy.cfg)
	}
}

func TestRegistry_Instantiate(t *testing.T) {
	tests := []struct {
		name         string
		registerType string
		schema       any
		configMap    map[string]any
		factory      NodeFactory
		wantErr      error
		checkInst    func(t *testing.T, inst NodeInterface)
	}{
		{
			name:         "nil config mapped to empty map for nil schema",
			registerType: "nil_schema_nil_config",
			schema:       nil,
			configMap:    nil,
			factory:      newDummyFactory(),
			wantErr:      nil,
			checkInst: func(t *testing.T, inst NodeInterface) {
				dummy := inst.(*dummyNode)
				m, ok := dummy.cfg.(map[string]any)
				if !ok || len(m) != 0 {
					t.Errorf("expected empty map[string]any, got %#v", dummy.cfg)
				}
			},
		},
		{
			name:         "decode success struct schema",
			registerType: "struct_schema",
			schema:       structSchema{},
			configMap: map[string]any{
				"name": "Alice",
				"age":  float64(30), // JSON numbers arrive as float64
			},
			factory: newDummyFactory(),
			wantErr: nil,
			checkInst: func(t *testing.T, inst NodeInterface) {
				dummy := inst.(*dummyNode)
				cfg, ok := dummy.cfg.(structSchema)
				if !ok {
					t.Errorf("expected structSchema, got %T", dummy.cfg)
				}
				if cfg.Name != "Alice" || cfg.Age != 30 {
					t.Errorf("decoded config incorrect: %#v", cfg)
				}
			},
		},
		{
			name:         "decode success ptr struct schema",
			registerType: "ptr_schema",
			schema:       &structSchema{},
			configMap: map[string]any{
				"name": "Bob",
				"age":  float64(25),
			},
			factory: newDummyFactory(),
			wantErr: nil,
			checkInst: func(t *testing.T, inst NodeInterface) {
				dummy := inst.(*dummyNode)
				cfg, ok := dummy.cfg.(*structSchema)
				if !ok {
					t.Errorf("expected *structSchema, got %T", dummy.cfg)
				}
				if cfg.Name != "Bob" || cfg.Age != 25 {
					t.Errorf("decoded config incorrect: %#v", cfg)
				}
			},
		},
		{
			name:         "decode success nested struct",
			registerType: "nested_schema",
			schema:       nestedSchema{},
			configMap: map[string]any{
				"settings": map[string]any{
					"name": "Charlie",
					"age":  float64(40),
				},
			},
			factory: newDummyFactory(),
			wantErr: nil,
			checkInst: func(t *testing.T, inst NodeInterface) {
				dummy := inst.(*dummyNode)
				cfg, ok := dummy.cfg.(nestedSchema)
				if !ok {
					t.Errorf("expected nestedSchema, got %T", dummy.cfg)
				}
				if cfg.Settings.Name != "Charlie" || cfg.Settings.Age != 40 {
					t.Errorf("decoded config incorrect: %#v", cfg)
				}
			},
		},
		{
			name:         "unknown field rejected",
			registerType: "unknown_field",
			schema:       structSchema{},
			configMap: map[string]any{
				"name":  "Dave",
				"extra": "field",
			},
			factory:   newDummyFactory(),
			wantErr:   ErrInvalidConfig,
			checkInst: nil,
		},
		{
			name:         "validate hook success (value) with struct schema",
			registerType: "val_hook_val_ok_struct_schema",
			schema:       validatableSchema{},
			configMap: map[string]any{
				"count": float64(10),
			},
			factory: newDummyFactory(),
			wantErr: nil,
			checkInst: func(t *testing.T, inst NodeInterface) {
				dummy := inst.(*dummyNode)
				cfg, ok := dummy.cfg.(validatableSchema)
				if !ok || cfg.Count != 10 {
					t.Errorf("decoded config incorrect: %#v", dummy.cfg)
				}
			},
		},
		{
			name:         "wrong type rejected",
			registerType: "wrong_type",
			schema:       structSchema{},
			configMap: map[string]any{
				"name": "Dave",
				"age":  "not an int",
			},
			factory:   newDummyFactory(),
			wantErr:   ErrInvalidConfig,
			checkInst: nil,
		},
		{
			name:         "validate hook success (value)",
			registerType: "val_hook_val_ok",
			schema:       validatableSchema{},
			configMap: map[string]any{
				"count": float64(10),
			},
			factory: newDummyFactory(),
			wantErr: nil,
			checkInst: func(t *testing.T, inst NodeInterface) {
				dummy := inst.(*dummyNode)
				cfg, ok := dummy.cfg.(validatableSchema)
				if !ok || cfg.Count != 10 {
					t.Errorf("decoded config incorrect: %#v", dummy.cfg)
				}
			},
		},
		{
			name:         "validate hook failure (value)",
			registerType: "val_hook_val_fail",
			schema:       validatableSchema{},
			configMap: map[string]any{
				"count": float64(-1),
			},
			factory:   newDummyFactory(),
			wantErr:   ErrInvalidConfig,
			checkInst: nil,
		},
		{
			name:         "validate hook success (ptr)",
			registerType: "val_hook_ptr_ok",
			schema:       &validatablePtrSchema{},
			configMap: map[string]any{
				"count": float64(5),
			},
			factory: newDummyFactory(),
			wantErr: nil,
			checkInst: func(t *testing.T, inst NodeInterface) {
				dummy := inst.(*dummyNode)
				cfg, ok := dummy.cfg.(*validatablePtrSchema)
				if !ok || cfg.Count != 5 {
					t.Errorf("decoded config incorrect: %#v", dummy.cfg)
				}
			},
		},
		{
			name:         "validate hook failure (ptr)",
			registerType: "val_hook_ptr_fail",
			schema:       &validatablePtrSchema{},
			configMap: map[string]any{
				"count": float64(-5),
			},
			factory:   newDummyFactory(),
			wantErr:   ErrInvalidConfig,
			checkInst: nil,
		},
		{
			name:         "factory error preserved",
			registerType: "factory_err",
			schema:       nil,
			configMap:    nil,
			factory: func(_ any) (NodeInterface, error) {
				return nil, NewNonRetryableError(errors.New("some error"))
			},
			wantErr:   &NodeError{},
			checkInst: nil,
		},
		{
			name:         "factory returns (nil, nil)",
			registerType: "factory_nil_nil",
			schema:       nil,
			configMap:    nil,
			factory: func(_ any) (NodeInterface, error) {
				return nil, nil
			},
			wantErr:   errors.New("returned nil node"),
			checkInst: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRegistry()
			_ = r.Register(tt.registerType, tt.factory, tt.schema)

			inst, err := r.Instantiate(tt.registerType, tt.configMap)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error containing %#v, got nil", tt.wantErr)
				}
				// Special check for NodeError preservation
				if _, ok := tt.wantErr.(*NodeError); ok {
					var nodeErr *NodeError
					if !errors.As(err, &nodeErr) {
						t.Errorf("expected *NodeError, got %v", err)
					}
				} else if !errors.Is(err, tt.wantErr) && err.Error() != tt.wantErr.Error() && !containsErr(err, tt.wantErr) {
					t.Errorf("expected error containing %#v, got %v", tt.wantErr, err)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if tt.checkInst != nil {
					tt.checkInst(t, inst)
				}
			}
		})
	}
}

func containsErr(err, target error) bool {
	if errors.Is(err, target) {
		return true
	}
	if err == nil || target == nil {
		return false
	}
	// simple fallback string match logic
	return strings.Contains(err.Error(), target.Error())
}

func TestRegistry_InstantiateUnknown(t *testing.T) {
	r := NewRegistry()
	_, err := r.Instantiate("unknown", nil)
	if !errors.Is(err, ErrUnknownType) {
		t.Errorf("expected ErrUnknownType, got %v", err)
	}
}

func TestRegistry_NilSchemaIsolation(t *testing.T) {
	r := NewRegistry()
	var factoryMap map[string]any

	err := r.Register("test", func(config any) (NodeInterface, error) {
		m := config.(map[string]any)
		factoryMap = m
		return &dummyNode{BaseNode: &BaseNode{}}, nil
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	callerMap := map[string]any{"key": "value", "nested": map[string]any{"inner": "v"}}

	_, err = r.Instantiate("test", callerMap)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Mutate caller map
	callerMap["key"] = "mutated"
	callerMap["new"] = "added"
	nested := callerMap["nested"].(map[string]any)
	nested["inner"] = "mutated"

	// Verify factory map is isolated
	if factoryMap["key"] != "value" {
		t.Errorf("factory map leaked mutation: key = %v", factoryMap["key"])
	}
	if _, ok := factoryMap["new"]; ok {
		t.Errorf("factory map leaked addition")
	}
	fNested := factoryMap["nested"].(map[string]any)
	if fNested["inner"] != "v" {
		t.Errorf("factory map leaked nested mutation: inner = %v", fNested["inner"])
	}

	// Mutate factory map
	factoryMap["key2"] = "mutated2"
	if _, ok := callerMap["key2"]; ok {
		t.Errorf("caller map leaked mutation from factory map")
	}
}

func TestRegistry_TypesFreshSlice(t *testing.T) {
	r := NewRegistry()
	_ = r.Register("c", newDummyFactory(), nil)
	_ = r.Register("a", newDummyFactory(), nil)
	_ = r.Register("b", newDummyFactory(), nil)

	types := r.Types()
	if len(types) != 3 {
		t.Fatalf("expected 3 types, got %d", len(types))
	}
	if types[0] != "a" || types[1] != "b" || types[2] != "c" {
		t.Errorf("expected sorted [a b c], got %v", types)
	}

	// Mutate slice
	types[0] = "mutated"

	types2 := r.Types()
	if types2[0] != "a" {
		t.Errorf("Types() did not return a fresh slice, got %v", types2)
	}
}

func TestRegistry_IndependentRegistries(t *testing.T) {
	r1 := NewRegistry()
	r2 := NewRegistry()

	_ = r1.Register("test", newDummyFactory(), nil)

	if !r1.Has("test") {
		t.Error("r1 should have 'test'")
	}
	if r2.Has("test") {
		t.Error("r2 should not have 'test'")
	}
}

func TestRegistry_Concurrency(t *testing.T) {
	r := NewRegistry()
	var wg sync.WaitGroup

	numGoroutines := 100

	// Concurrent registration
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			name := "type_abc_" + string(rune(id+65)) // Start at 'A' to avoid whitespace runes
			// We only want normal ascii letters/digits to ensure no whitespace check fails
			if id > 50 {
				name = "type_def_" + string(rune(id+15))
			}
			_ = r.Register(name, newDummyFactory(), nil)
		}(i)
	}

	wg.Wait()

	if len(r.Types()) != numGoroutines {
		t.Errorf("expected %d types, got %d", numGoroutines, len(r.Types()))
	}

	// Concurrent read and instantiate
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			name := "type_abc_" + string(rune(id+65))
			if id > 50 {
				name = "type_def_" + string(rune(id+15))
			}
			if !r.Has(name) {
				t.Errorf("missing type %s", name)
			}
			_, err := r.Instantiate(name, nil)
			if err != nil {
				t.Errorf("unexpected instantiate error: %v", err)
			}
			_ = r.Types()
		}(i)
	}

	wg.Wait()
}

func TestRegistry_ValidateConfig(t *testing.T) {
	r := NewRegistry()

	// Create a factory that panics to prove ValidateConfig doesn't call it.
	panickingFactory := func(_ any) (NodeInterface, error) {
		panic("factory called!")
	}

	_ = r.Register("valid_struct", panickingFactory, structSchema{})
	_ = r.Register("valid_ptr", panickingFactory, &structSchema{})

	tests := []struct {
		name         string
		registerType string
		configMap    map[string]any
		wantErr      error
		errSubstring string
	}{
		{
			name:         "valid struct config",
			registerType: "valid_struct",
			configMap: map[string]any{
				"name": "Alice",
				"age":  30,
			},
			wantErr: nil,
		},
		{
			name:         "valid ptr config",
			registerType: "valid_ptr",
			configMap: map[string]any{
				"name": "Bob",
				"age":  25,
			},
			wantErr: nil,
		},
		{
			name:         "unknown field",
			registerType: "valid_struct",
			configMap: map[string]any{
				"name":  "Charlie",
				"extra": "bad",
			},
			wantErr: ErrInvalidConfig,
		},
		{
			name:         "wrong type array instead of string",
			registerType: "valid_struct",
			configMap: map[string]any{
				"name": []any{"bad"},
			},
			wantErr:      ErrInvalidConfig,
			errSubstring: `field "name": unexpected array value`,
		},
		{
			name:         "unknown type",
			registerType: "does_not_exist",
			configMap:    nil,
			wantErr:      ErrUnknownType,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := r.ValidateConfig(tt.registerType, tt.configMap)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error containing %v, got nil", tt.wantErr)
				}
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("expected error %v, got %v", tt.wantErr, err)
				}
				if tt.errSubstring != "" && !strings.Contains(err.Error(), tt.errSubstring) {
					t.Errorf("expected error to contain %q, got %q", tt.errSubstring, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestRegistry_InstantiateTypeErrorMessageUntouched(t *testing.T) {
	r := NewRegistry()
	_ = r.Register("test_type", newDummyFactory(), structSchema{})

	_, err := r.Instantiate("test_type", map[string]any{
		"name": []any{"bad"},
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("expected ErrInvalidConfig, got %v", err)
	}

	expectedErrString := `invalid node config for type "test_type": failed to decode config: json: cannot unmarshal array into Go struct field structSchema.name of type string`
	if err.Error() != expectedErrString {
		t.Errorf("expected error string %q, got %q", expectedErrString, err.Error())
	}
}

func TestRegistry_ValidateConfigVsInstantiate(t *testing.T) {
	r := NewRegistry()

	factory := func(_ any) (NodeInterface, error) {
		return &dummyNode{BaseNode: &BaseNode{}}, nil
	}

	_ = r.Register("valid_type", factory, structSchema{})
	_ = r.Register("nil_schema_type", factory, nil)
	_ = r.Register("val_hook_fail", factory, validatableSchema{})

	tests := []struct {
		name         string
		registerType string
		configMap    map[string]any
		wantErr      bool
	}{
		{
			name:         "valid struct config",
			registerType: "valid_type",
			configMap: map[string]any{
				"name": "Alice",
				"age":  30,
			},
			wantErr: false,
		},
		{
			name:         "invalid struct config: wrong type",
			registerType: "valid_type",
			configMap: map[string]any{
				"name": []any{"Alice"},
			},
			wantErr: true,
		},
		{
			name:         "invalid struct config: extra fields",
			registerType: "valid_type",
			configMap: map[string]any{
				"extra": "bad",
			},
			wantErr: true,
		},
		{
			name:         "nil schema passthrough",
			registerType: "nil_schema_type",
			configMap: map[string]any{
				"anything": "goes",
			},
			wantErr: false,
		},
		{
			name:         "validate hook failure",
			registerType: "val_hook_fail",
			configMap: map[string]any{
				"count": -1,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err1 := r.ValidateConfig(tt.registerType, tt.configMap)
			_, err2 := r.Instantiate(tt.registerType, tt.configMap)

			if tt.wantErr {
				if !errors.Is(err1, ErrInvalidConfig) {
					t.Errorf("ValidateConfig expected ErrInvalidConfig, got %v", err1)
				}
				if !errors.Is(err2, ErrInvalidConfig) {
					t.Errorf("Instantiate expected ErrInvalidConfig, got %v", err2)
				}
			} else {
				if err1 != nil {
					t.Errorf("ValidateConfig expected no error, got %v", err1)
				}
				if err2 != nil {
					t.Errorf("Instantiate expected no error, got %v", err2)
				}
			}
		})
	}
}

func TestRegistry_ValidateConfig_ZeroValue(t *testing.T) {
	var r Registry
	err := r.ValidateConfig("anything", nil)
	if !errors.Is(err, ErrUnknownType) {
		t.Errorf("expected ErrUnknownType, got %v", err)
	}

	r2 := NewRegistry()
	err = r2.ValidateConfig("anything", nil)
	if !errors.Is(err, ErrUnknownType) {
		t.Errorf("expected ErrUnknownType, got %v", err)
	}
}
