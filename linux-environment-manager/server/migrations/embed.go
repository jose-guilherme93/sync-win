// Package migrations holds the SQL schema files embedded into the server binary.
// The files in this directory are the single source of truth for the database schema.
package migrations

import (
	"embed"
	"sort"
)

//go:embed *.sql
var files embed.FS

// Names returns all migration file names in lexical order.
func Names() ([]string, error) {
	entries, err := files.ReadDir(".")
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// Open returns the contents of one embedded migration file.
func Open(name string) ([]byte, error) {
	return files.ReadFile(name)
}
