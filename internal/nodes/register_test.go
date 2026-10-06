package nodes_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/gargi-indic/gargi-ckg/internal/nodes"
	"github.com/gargi-indic/gargi-ckg/pkg/engine"
)

type fakeSignalSource struct {
	waitFunc func(ctx context.Context, execution engine.ExecutionID, node engine.NodeID, signal string) (map[string]any, error)
}

func (f *fakeSignalSource) Wait(ctx context.Context, execution engine.ExecutionID, node engine.NodeID, signal string) (map[string]any, error) {
	if f.waitFunc != nil {
		return f.waitFunc(ctx, execution, node, signal)
	}
	return nil, nil
}

type fakeHTTPDoer struct {
	doFunc func(req *http.Request) (*http.Response, error)
}

func (f *fakeHTTPDoer) Do(req *http.Request) (*http.Response, error) {
	if f.doFunc != nil {
		return f.doFunc(req)
	}
	return nil, nil
}

func TestRegisterDefaults(t *testing.T) {
	deps := nodes.Dependencies{
		HTTPClient: &fakeHTTPDoer{},
		Signals:    &fakeSignalSource{},
	}

	t.Run("success", func(t *testing.T) {
		reg := engine.NewRegistry()
		err := nodes.RegisterDefaults(reg, deps)
		if err != nil {
			t.Fatalf("RegisterDefaults failed: %v", err)
		}

		types := []string{"passthrough", "transform", "conditional_branch", "http_call", "wait_for_signal"}
		for _, typ := range types {
			if !reg.Has(typ) {
				t.Errorf("expected registry to have %s", typ)
			}
		}
	})

	t.Run("nil registry", func(t *testing.T) {
		err := nodes.RegisterDefaults(nil, deps)
		if err == nil {
			t.Error("expected error for nil registry")
		}
	})

	t.Run("duplicate registration", func(t *testing.T) {
		reg := engine.NewRegistry()
		_ = nodes.RegisterDefaults(reg, deps)
		err := nodes.RegisterDefaults(reg, deps)
		if err == nil {
			t.Error("expected error for duplicate registration")
		}
	})

	t.Run("nil signal source at instantiate time", func(t *testing.T) {
		reg := engine.NewRegistry()
		_ = nodes.RegisterDefaults(reg, nodes.Dependencies{}) // no signals provided

		_, err := reg.Instantiate("wait_for_signal", map[string]any{"signal": "test"})
		if err == nil {
			t.Fatal("expected instantiation to fail due to nil signal source")
		}
		if engine.ClassOf(err) != engine.ClassNonRetryable {
			t.Errorf("expected non-retryable error, got %v", engine.ClassOf(err))
		}
	})
}
