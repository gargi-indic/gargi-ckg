package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gargi-indic/gargi-flow/internal/mcp"
	"github.com/gargi-indic/gargi-flow/schemas"
	sdk_mcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func setupTestServer(t *testing.T) (*sdk_mcp.Server, *sdk_mcp.ClientSession) {
	s := mcp.NewServer(mcp.Options{Version: "test"})

	clientTransport, serverTransport := sdk_mcp.NewInMemoryTransports()

	c := sdk_mcp.NewClient(&sdk_mcp.Implementation{
		Name:    "test-client",
		Version: "1.0",
	}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	go func() {
		if err := s.Run(ctx, serverTransport); err != nil && !errors.Is(err, context.Canceled) {
			panic(err)
		}
	}()

	session, err := c.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)

	return s, session
}

func TestTools(t *testing.T) {
	ctx := context.Background()
	_, session := setupTestServer(t)

	t.Run("ListTools contains get_schema and list_templates", func(t *testing.T) {
		res, err := session.ListTools(ctx, &sdk_mcp.ListToolsParams{})
		require.NoError(t, err)

		foundSchema, foundTemplates := false, false
		for _, tool := range res.Tools {
			if tool.Name == "get_schema" {
				foundSchema = true
			}
			if tool.Name == "list_templates" {
				foundTemplates = true
			}
		}
		require.True(t, foundSchema, "get_schema tool not found")
		require.True(t, foundTemplates, "list_templates tool not found")
	})

	t.Run("get_schema returns valid JSON equaling embedded schema", func(t *testing.T) {
		res, err := session.CallTool(ctx, &sdk_mcp.CallToolParams{
			Name: "get_schema",
			Arguments: map[string]interface{}{
				"kind": "sim",
			},
		})
		require.NoError(t, err)
		require.False(t, res.IsError)
		require.Len(t, res.Content, 1)

		textCtx, ok := res.Content[0].(*sdk_mcp.TextContent)
		require.True(t, ok)

		require.True(t, json.Valid([]byte(textCtx.Text)))

		expected, err := schemas.Schema("sim")
		require.NoError(t, err)
		require.Equal(t, string(expected), textCtx.Text)
	})

	t.Run("get_schema unknown kind returns IsError", func(t *testing.T) {
		res, err := session.CallTool(ctx, &sdk_mcp.CallToolParams{
			Name: "get_schema",
			Arguments: map[string]interface{}{
				"kind": "unknown",
			},
		})
		require.NoError(t, err)
		require.True(t, res.IsError)
		require.Len(t, res.Content, 1)

		textCtx, ok := res.Content[0].(*sdk_mcp.TextContent)
		require.True(t, ok)

		var errRes map[string]interface{}
		err2 := json.Unmarshal([]byte(textCtx.Text), &errRes)
		require.NoError(t, err2)
		require.Contains(t, errRes, "errors")
	})

	t.Run("list_templates names empty and loan-underwriting", func(t *testing.T) {
		res, err := session.CallTool(ctx, &sdk_mcp.CallToolParams{
			Name:      "list_templates",
			Arguments: map[string]interface{}{},
		})
		require.NoError(t, err)
		require.False(t, res.IsError)
		require.Len(t, res.Content, 1)

		textCtx, ok := res.Content[0].(*sdk_mcp.TextContent)
		require.True(t, ok)

		var listing struct {
			Templates []struct {
				Name string `json:"name"`
			} `json:"templates"`
		}
		err = json.Unmarshal([]byte(textCtx.Text), &listing)
		require.NoError(t, err)

		var names []string
		for _, templ := range listing.Templates {
			names = append(names, templ.Name)
		}
		require.Contains(t, names, "empty")
		require.Contains(t, names, "loan-underwriting")
	})
}

func TestResources(t *testing.T) {
	ctx := context.Background()
	_, session := setupTestServer(t)

	t.Run("read gargi://schemas/sim", func(t *testing.T) {
		res, err := session.ReadResource(ctx, &sdk_mcp.ReadResourceParams{
			URI: "gargi://schemas/sim",
		})
		require.NoError(t, err)
		require.Len(t, res.Contents, 1)

		require.Equal(t, "application/schema+json", res.Contents[0].MIMEType)

		expected, err := schemas.Schema("sim")
		require.NoError(t, err)
		require.Equal(t, string(expected), res.Contents[0].Text)
	})

	t.Run("read gargi://templates/loan-underwriting/sim.yaml", func(t *testing.T) {
		res, err := session.ReadResource(ctx, &sdk_mcp.ReadResourceParams{
			URI: "gargi://templates/loan-underwriting/sim.yaml",
		})
		require.NoError(t, err)
		require.Len(t, res.Contents, 1)

		require.Equal(t, "application/yaml", res.Contents[0].MIMEType)

		expected, err := os.ReadFile("../../examples/simulation/loan/sim.yaml")
		require.NoError(t, err)
		require.Equal(t, string(expected), res.Contents[0].Text)
	})
}

func TestLoanUnderwritingTemplateIsByteIdentical(t *testing.T) {
	filesToCompare := []string{
		"sim.yaml",
		"graph.yaml",
		"experiment.yaml",
		"policies/recession-v1.yaml",
	}

	for _, file := range filesToCompare {
		t.Run(file, func(t *testing.T) {
			expectedPath := filepath.Join("../../examples/simulation/loan", file)
			expected, err := os.ReadFile(expectedPath)
			require.NoError(t, err, "failed to read expected file")

			actualPath := filepath.Join("templates/loan-underwriting", file)
			actual, err := os.ReadFile(actualPath)
			require.NoError(t, err, "failed to read actual file")

			require.Equal(t, expected, actual, "template file %s drifted from example", file)
		})
	}
}

func TestStreamableHTTP(t *testing.T) {
	s := mcp.NewServer(mcp.Options{Version: "test"})

	sseServer := sdk_mcp.NewStreamableHTTPHandler(func(*http.Request) *sdk_mcp.Server { return s }, nil)

	httpServer := httptest.NewServer(sseServer)
	defer httpServer.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sseClient := &sdk_mcp.StreamableClientTransport{
		Endpoint: httpServer.URL,
	}

	c := sdk_mcp.NewClient(&sdk_mcp.Implementation{
		Name:    "test-client",
		Version: "1.0",
	}, nil)

	session, err := c.Connect(ctx, sseClient, nil)
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	res, err := session.CallTool(ctx, &sdk_mcp.CallToolParams{
		Name:      "list_templates",
		Arguments: map[string]interface{}{},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Len(t, res.Content, 1)
}
