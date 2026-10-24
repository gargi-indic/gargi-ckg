package mcp

import (
	"context"
	"io/fs"
	"path/filepath"

	"github.com/gargi-indic/gargi-flow/internal/simbackend"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/gargi-indic/gargi-flow/schemas"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Options configures the mcp server.
type Options struct {
	Backend simbackend.Backend
	Version string
}

// GetSchemaInput is the input for get_schema tool.
type GetSchemaInput struct {
	Kind string `json:"kind"`
}

// NewServer returns an *mcp.Server (go-sdk) named "gargi" with this part's tools and resources registered.
func NewServer(opts Options) *mcp.Server {
	if opts.Version == "" {
		opts.Version = "dev"
	}

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "gargi",
		Version: opts.Version,
	}, &mcp.ServerOptions{
		Capabilities: &mcp.ServerCapabilities{
			Resources: &mcp.ResourceCapabilities{},
		},
	})

	// Register tools
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_schema",
		Description: "Returns the JSON Schema text for the specified kind.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, input GetSchemaInput) (*mcp.CallToolResult, any, error) {
		schema, err := schemas.Schema(input.Kind)
		if err != nil {
			return toolErrorf(engine.CodeInvalidValue, "unknown schema kind: %q", input.Kind), nil, nil
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: string(schema),
				},
			},
		}, nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_templates",
		Description: "Returns a JSON list of available templates.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		listing, err := listTemplates()
		if err != nil {
			return toolErrorf(engine.CodeInvalidValue, "failed to list templates: %v", err), nil, nil
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: listing,
				},
			},
		}, nil, nil
	})

	// Register concrete schemas
	for _, kind := range []string{"sim", "graph", "policy", "experiment"} {
		schemaKind := kind // capture for closure
		uri := "gargi://schemas/" + schemaKind
		server.AddResource(&mcp.Resource{
			URI:         uri,
			Name:        "JSON Schema: " + schemaKind,
			Description: "JSON schema for " + schemaKind,
			MIMEType:    "application/schema+json",
		}, func(_ context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			schema, err := schemas.Schema(schemaKind)
			if err != nil {
				return nil, err
			}
			return &mcp.ReadResourceResult{
				Contents: []*mcp.ResourceContents{
					{
						URI:      uri,
						MIMEType: "application/schema+json",
						Text:     string(schema),
					},
				},
			}, nil
		})
	}

	// Register concrete templates
	entries, err := fs.ReadDir(templatesFS, "templates")
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			name := entry.Name()
			_ = fs.WalkDir(templatesFS, filepath.Join("templates", name), func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !d.IsDir() {
					rel, _ := filepath.Rel(filepath.Join("templates", name), path)
					uri := "gargi://templates/" + name + "/" + rel
					templateName := name
					templateRel := rel // capture for closure
					server.AddResource(&mcp.Resource{
						URI:         uri,
						Name:        "Template: " + templateName + " " + templateRel,
						Description: "Starter template for " + templateName + " " + templateRel,
						MIMEType:    "application/yaml",
					}, func(_ context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
						content, err := fs.ReadFile(templatesFS, filepath.Join("templates", templateName, templateRel))
						if err != nil {
							return nil, err
						}
						return &mcp.ReadResourceResult{
							Contents: []*mcp.ResourceContents{
								{
									URI:      uri,
									MIMEType: "application/yaml",
									Text:     string(content),
								},
							},
						}, nil
					})
				}
				return nil
			})
		}
	}
	return server
}
