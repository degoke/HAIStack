package postgres

import (
	"fmt"
	"regexp"
	"strings"
)

const defaultSchema = "public"

var schemaNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func normalizeSchema(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return defaultSchema, nil
	}
	if !schemaNamePattern.MatchString(name) {
		return "", fmt.Errorf("invalid postgres schema name %q", name)
	}
	return name, nil
}

type dbSchema struct {
	name string
}

func (s dbSchema) migrationsTable() string {
	if s.name == defaultSchema {
		return "hai_schema_migrations"
	}
	return s.name + ".hai_schema_migrations"
}

func (s dbSchema) searchPath() string {
	if s.name == defaultSchema {
		return defaultSchema
	}
	return s.name + ", " + defaultSchema
}
