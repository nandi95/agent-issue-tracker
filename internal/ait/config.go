package ait

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var prefixPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func ProjectRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	root := cwd
	current := cwd
	for {
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
			root = current
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}

	return root, nil
}

func NormalizePrefix(value string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '-' || r == '_' || r == ' ':
			return '-'
		default:
			return '-'
		}
	}, normalized)

	for strings.Contains(normalized, "--") {
		normalized = strings.ReplaceAll(normalized, "--", "-")
	}
	normalized = strings.Trim(normalized, "-")

	if !prefixPattern.MatchString(normalized) {
		return "", &CLIError{Code: "validation", Message: "prefix must contain only lowercase letters, numbers, and hyphens", ExitCode: 65}
	}

	return normalized, nil
}

func DefaultPrefix() (string, error) {
	root, err := ProjectRoot()
	if err != nil {
		return "", err
	}

	return NormalizePrefix(filepath.Base(root))
}

func ensureProjectPrefix(ctx context.Context, db *sql.DB) (string, error) {
	row := db.QueryRowContext(ctx, `SELECT prefix FROM project_config WHERE id = 1`)

	var prefix string
	if err := row.Scan(&prefix); err == nil {
		return prefix, nil
	} else if err != sql.ErrNoRows {
		return "", err
	}

	inferred, err := inferPrefixFromIssues(ctx, db)
	if err != nil {
		return "", err
	}
	if inferred == "" {
		inferred, err = DefaultPrefix()
		if err != nil {
			return "", err
		}
	}

	if _, err := db.ExecContext(
		ctx,
		`INSERT INTO project_config (id, prefix, updated_at) VALUES (1, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET prefix = project_config.prefix, updated_at = project_config.updated_at`,
		inferred,
		NowUTC(),
	); err != nil {
		return "", err
	}

	row = db.QueryRowContext(ctx, `SELECT prefix FROM project_config WHERE id = 1`)
	if err := row.Scan(&prefix); err != nil {
		return "", err
	}

	return prefix, nil
}

func setProjectPrefix(ctx context.Context, db *sql.DB, prefix string) (string, error) {
	normalized, err := NormalizePrefix(prefix)
	if err != nil {
		return "", err
	}

	if _, err := db.ExecContext(
		ctx,
		`INSERT INTO project_config (id, prefix, updated_at) VALUES (1, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET prefix = excluded.prefix, updated_at = excluded.updated_at`,
		normalized,
		NowUTC(),
	); err != nil {
		return "", err
	}

	return normalized, nil
}

func inferPrefixFromIssues(ctx context.Context, db *sql.DB) (string, error) {
	row := db.QueryRowContext(
		ctx,
		`SELECT public_id
		 FROM issues
		 WHERE parent_id IS NULL
		   AND public_id IS NOT NULL
		   AND public_id != ''
		 ORDER BY created_at ASC, id ASC
		 LIMIT 1`,
	)

	var publicID string
	if err := row.Scan(&publicID); err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", err
	}

	rootID := publicID
	if dot := strings.IndexByte(rootID, '.'); dot >= 0 {
		rootID = rootID[:dot]
	}

	cut := strings.LastIndex(rootID, "-")
	if cut <= 0 {
		return "", nil
	}

	prefix := rootID[:cut]
	if !prefixPattern.MatchString(prefix) {
		return "", nil
	}

	return prefix, nil
}

func (a *App) ensureProjectPrefix(ctx context.Context) (string, error) {
	return ensureProjectPrefix(ctx, a.db)
}

func (a *App) setProjectPrefix(ctx context.Context, prefix string) (string, error) {
	return setProjectPrefix(ctx, a.db, prefix)
}
