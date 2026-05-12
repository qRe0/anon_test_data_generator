package config

import (
	"fmt"
	"strings"
)

// GeneratorRegistry checks whether a generator name is known.
type GeneratorRegistry interface {
	IsRegistered(name string) bool
}

// ValidationError describes a single configuration problem.
type ValidationError struct {
	Table  string
	Column string
	Field  string
	Msg    string
}

func (e ValidationError) Error() string {
	switch {
	case e.Column != "":
		return fmt.Sprintf("%s.%s.%s: %s", e.Table, e.Column, e.Field, e.Msg)
	case e.Table != "":
		return fmt.Sprintf("%s.%s: %s", e.Table, e.Field, e.Msg)
	default:
		return fmt.Sprintf("%s: %s", e.Field, e.Msg)
	}
}

// Validate checks the configuration and returns all found issues.
// If reg is nil, generator name checks are skipped.
func Validate(cfg *Config, reg GeneratorRegistry) []error {
	if cfg == nil {
		return []error{ValidationError{Field: "config", Msg: "must not be nil"}}
	}

	var errs []error

	if cfg.Global.DSN == "" {
		errs = append(errs, ValidationError{Field: "global.dsn", Msg: "must not be empty"})
	}
	if cfg.Global.Seed == 0 {
		errs = append(errs, ValidationError{Field: "global.seed", Msg: "must not be zero"})
	}
	if cfg.Global.Locale == "" {
		errs = append(errs, ValidationError{Field: "global.locale", Msg: "must not be empty"})
	}
	if cfg.Global.BatchSize <= 0 {
		errs = append(errs, ValidationError{Field: "global.batch_size", Msg: "must be positive"})
	}

	if len(cfg.Tables) == 0 {
		errs = append(errs, ValidationError{Field: "tables", Msg: "at least one table is required"})
	}

	seenTables := map[string]bool{}
	for i, t := range cfg.Tables {
		tableCtx := t.Name
		if tableCtx == "" {
			tableCtx = fmt.Sprintf("tables[%d]", i)
		}

		if t.Name == "" {
			errs = append(errs, ValidationError{Table: tableCtx, Field: "name", Msg: "must not be empty"})
		} else if seenTables[t.Name] {
			errs = append(errs, ValidationError{Table: t.Name, Field: "name", Msg: "duplicate table entry"})
		} else {
			seenTables[t.Name] = true
		}

		if t.Count <= 0 {
			errs = append(errs, ValidationError{Table: tableCtx, Field: "count", Msg: "must be positive"})
		}
		if t.CleanupStrategy != "" && t.CleanupStrategy != "truncate" && t.CleanupStrategy != "delete" {
			errs = append(errs, ValidationError{Table: tableCtx, Field: "cleanup_strategy", Msg: `must be "truncate" or "delete"`})
		}

		if len(t.Columns) == 0 {
			errs = append(errs, ValidationError{Table: t.Name, Field: "columns", Msg: "at least one column is required"})
		}

		seenCols := map[string]bool{}
		for j, c := range t.Columns {
			colCtx := c.Name
			if colCtx == "" {
				colCtx = fmt.Sprintf("columns[%d]", j)
			}

			if c.Name == "" {
				errs = append(errs, ValidationError{Table: t.Name, Column: colCtx, Field: "name", Msg: "must not be empty"})
			} else if seenCols[c.Name] {
				errs = append(errs, ValidationError{Table: t.Name, Column: c.Name, Field: "name", Msg: "duplicate column entry"})
			} else {
				seenCols[c.Name] = true
			}

			if c.Generator == "" {
				errs = append(errs, ValidationError{Table: t.Name, Column: colCtx, Field: "generator", Msg: "must not be empty"})
			} else if reg != nil && !reg.IsRegistered(c.Generator) {
				errs = append(errs, ValidationError{Table: t.Name, Column: colCtx, Field: "generator", Msg: fmt.Sprintf("%q is not a registered generator", c.Generator)})
			}

			if c.Transformer != "" {
				switch c.Transformer {
				case "masking.partial", "nulling":
				default:
					errs = append(errs, ValidationError{Table: t.Name, Column: colCtx, Field: "transformer", Msg: fmt.Sprintf("%q is not a registered transformer", c.Transformer)})
				}
			}
		}
	}

	return errs
}

// ValidationErrorList formats a slice of errors into a single string.
func ValidationErrorList(errs []error) string {
	if len(errs) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d validation error(s):\n", len(errs))
	for _, e := range errs {
		b.WriteString("  - " + e.Error() + "\n")
	}
	return b.String()
}
