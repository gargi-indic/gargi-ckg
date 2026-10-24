package mcp

import (
	"embed"
	"encoding/json"
	"io/fs"
	"path/filepath"
	"sort"
)

//go:embed templates/*
var templatesFS embed.FS

// TemplateListing is the schema for list_templates tool.
type TemplateListing struct {
	Templates []TemplateInfo `json:"templates"`
}

// TemplateInfo represents a single template.
type TemplateInfo struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Files       []string `json:"files"`
}

// listTemplates reads the templates directory and returns a JSON list of available templates.
func listTemplates() (string, error) {
	entries, err := fs.ReadDir(templatesFS, "templates")
	if err != nil {
		return "", err
	}

	var listing TemplateListing
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		name := entry.Name()
		var files []string
		_ = fs.WalkDir(templatesFS, filepath.Join("templates", name), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() {
				rel, _ := filepath.Rel(filepath.Join("templates", name), path)
				files = append(files, rel)
			}
			return nil
		})

		sort.Strings(files)

		listing.Templates = append(listing.Templates, TemplateInfo{
			Name:  name,
			Files: files,
		})
	}

	sort.Slice(listing.Templates, func(i, j int) bool {
		return listing.Templates[i].Name < listing.Templates[j].Name
	})

	b, err := json.Marshal(listing)
	if err != nil {
		return "", err
	}

	return string(b), nil
}
