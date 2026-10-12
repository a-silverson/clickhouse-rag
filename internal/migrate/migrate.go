package migrate

import (
	"context"
	"embed"
	"fmt"
	"strings"
)

//go:embed migrations/*.sql
var files embed.FS

func Statements() ([]string, error) {
	raw, err := files.ReadFile("migrations/001_init.sql")
	if err != nil {
		return nil, err
	}
	var statements []string
	var current strings.Builder
	inLine := false
	inBlock := false
	inString := false
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		if inLine {
			if ch == '\n' {
				inLine = false
				current.WriteByte('\n')
			}
			continue
		}
		if inBlock {
			if ch == '*' && i+1 < len(raw) && raw[i+1] == '/' {
				inBlock = false
				i++
			}
			continue
		}
		if inString {
			current.WriteByte(ch)
			if ch == '\'' {
				if i+1 < len(raw) && raw[i+1] == '\'' {
					current.WriteByte(raw[i+1])
					i++
					continue
				}
				inString = false
			}
			continue
		}
		if ch == '-' && i+1 < len(raw) && raw[i+1] == '-' {
			inLine = true
			i++
			continue
		}
		if ch == '/' && i+1 < len(raw) && raw[i+1] == '*' {
			inBlock = true
			i++
			continue
		}
		if ch == '\'' {
			inString = true
			current.WriteByte(ch)
			continue
		}
		if ch == ';' {
			stmt := strings.TrimSpace(current.String())
			if stmt != "" {
				statements = append(statements, stmt)
			}
			current.Reset()
			continue
		}
		current.WriteByte(ch)
	}
	if stmt := strings.TrimSpace(current.String()); stmt != "" {
		statements = append(statements, stmt)
	}
	if len(statements) == 0 {
		return nil, fmt.Errorf("migration 001_init.sql has no statements")
	}
	return statements, nil
}

type Execer interface {
	Exec(ctx context.Context, query string, args ...any) error
}

func Apply(ctx context.Context, exec Execer) error {
	statements, err := Statements()
	if err != nil {
		return err
	}
	for _, stmt := range statements {
		if err := exec.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("apply migration: %w", err)
		}
	}
	return nil
}
