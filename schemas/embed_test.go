package schemas_test

import (
	"testing"

	"github.com/gargi-indic/gargi-flow/schemas"
	"github.com/stretchr/testify/require"
)

func TestSchema(t *testing.T) {
	kinds := []string{"sim", "graph", "policy", "experiment"}

	for _, kind := range kinds {
		t.Run(kind, func(t *testing.T) {
			data, err := schemas.Schema(kind)
			require.NoError(t, err)
			require.NotEmpty(t, data)
		})
	}

	t.Run("unknown_kind", func(t *testing.T) {
		data, err := schemas.Schema("unknown")
		require.Error(t, err)
		require.Nil(t, data)
		require.Contains(t, err.Error(), "unknown schema kind")
	})
}
