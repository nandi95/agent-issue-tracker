package ait

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

type App struct {
	db *sql.DB
}

func Open(ctx context.Context) (*App, error) {
	dbPath, err := DatabasePath()
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys = ON;`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `PRAGMA busy_timeout = 5000;`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `PRAGMA journal_mode = WAL;`); err != nil {
		db.Close()
		return nil, err
	}
	if err := ensureSchema(ctx, db); err != nil {
		db.Close()
		return nil, err
	}

	return &App{db: db}, nil
}

func (a *App) Close() error {
	if a == nil || a.db == nil {
		return nil
	}
	return a.db.Close()
}

func (a *App) fetchIssue(ctx context.Context, id string) (Issue, error) {
	row := a.db.QueryRowContext(
		ctx,
		`SELECT id, type, title, description, status, parent_id, priority, created_at, updated_at, closed_at
		 FROM issues WHERE id = ?`,
		id,
	)
	iss, err := scanIssue(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Issue{}, &CLIError{Code: "not_found", Message: fmt.Sprintf("issue %s not found", id), ExitCode: 66}
		}
		return Issue{}, err
	}
	return iss, nil
}

func (a *App) fetchIssueRef(ctx context.Context, id string) (IssueRef, error) {
	row := a.db.QueryRowContext(ctx, `SELECT id, title, status, type, priority FROM issues WHERE id = ?`, id)
	var ref IssueRef
	if err := row.Scan(&ref.ID, &ref.Title, &ref.Status, &ref.Type, &ref.Priority); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return IssueRef{}, &CLIError{Code: "not_found", Message: fmt.Sprintf("issue %s not found", id), ExitCode: 66}
		}
		return IssueRef{}, err
	}
	return ref, nil
}

func (a *App) fetchChildren(ctx context.Context, parentID string) ([]Issue, error) {
	return a.queryIssues(
		ctx,
		`SELECT id, type, title, description, status, parent_id, priority, created_at, updated_at, closed_at
		 FROM issues WHERE parent_id = ? ORDER BY created_at ASC`,
		parentID,
	)
}

func (a *App) fetchBlockers(ctx context.Context, id string) ([]IssueRef, error) {
	rows, err := a.db.QueryContext(
		ctx,
		`SELECT i.id, i.title, i.status, i.type, i.priority
		 FROM issue_dependencies d
		 JOIN issues i ON i.id = d.blocker_id
		 WHERE d.blocked_id = ?
		 ORDER BY i.created_at ASC`,
		id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanIssueRefs(rows)
}

func (a *App) fetchBlocks(ctx context.Context, id string) ([]IssueRef, error) {
	rows, err := a.db.QueryContext(
		ctx,
		`SELECT i.id, i.title, i.status, i.type, i.priority
		 FROM issue_dependencies d
		 JOIN issues i ON i.id = d.blocked_id
		 WHERE d.blocker_id = ?
		 ORDER BY i.created_at ASC`,
		id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanIssueRefs(rows)
}

func (a *App) fetchNotes(ctx context.Context, issueID string) ([]Note, error) {
	rows, err := a.db.QueryContext(
		ctx,
		`SELECT id, issue_id, body, created_at
		 FROM issue_notes
		 WHERE issue_id = ?
		 ORDER BY created_at ASC`,
		issueID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Note, 0)
	for rows.Next() {
		var n Note
		if err := rows.Scan(&n.ID, &n.IssueID, &n.Body, &n.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, n)
	}
	return items, rows.Err()
}

func (a *App) queryIssues(ctx context.Context, query string, params ...any) ([]Issue, error) {
	rows, err := a.db.QueryContext(ctx, query, params...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Issue, 0)
	for rows.Next() {
		iss, err := scanIssue(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, iss)
	}
	return items, rows.Err()
}

func (a *App) readyIssues(ctx context.Context) ([]Issue, error) {
	return a.queryIssues(
		ctx,
		`SELECT i.id, i.type, i.title, i.description, i.status, i.parent_id, i.priority, i.created_at, i.updated_at, i.closed_at
		 FROM issues i
		 WHERE i.status IN (?, ?)
		   AND NOT EXISTS (
		     SELECT 1
		     FROM issue_dependencies d
		     JOIN issues blockers ON blockers.id = d.blocker_id
		     WHERE d.blocked_id = i.id
		       AND blockers.status != ?
		   )
		 ORDER BY i.created_at ASC`,
		StatusOpen,
		StatusInProgress,
		StatusClosed,
	)
}

func (a *App) validateParent(ctx context.Context, parentID string) error {
	parent, err := a.fetchIssue(ctx, parentID)
	if err != nil {
		return err
	}
	if parent.Type != "epic" {
		return &CLIError{Code: "validation", Message: "parent must be an epic", ExitCode: 65}
	}
	return nil
}

func (a *App) hasDirectDependency(ctx context.Context, blockedID, blockerID string) bool {
	row := a.db.QueryRowContext(
		ctx,
		`SELECT 1 FROM issue_dependencies WHERE blocked_id = ? AND blocker_id = ?`,
		blockedID,
		blockerID,
	)
	var found int
	return row.Scan(&found) == nil
}

func (a *App) buildDependencyTree(ctx context.Context, ref IssueRef, seen map[string]bool) (DependencyTree, error) {
	if seen[ref.ID] {
		return DependencyTree{
			Issue:  ref,
			Cycles: []string{ref.ID},
		}, nil
	}

	nextSeen := make(map[string]bool, len(seen)+1)
	for k, v := range seen {
		nextSeen[k] = v
	}
	nextSeen[ref.ID] = true

	blockers, err := a.fetchBlockers(ctx, ref.ID)
	if err != nil {
		return DependencyTree{}, err
	}

	tree := DependencyTree{Issue: ref, Blockers: make([]DependencyTree, 0)}
	for _, blocker := range blockers {
		child, err := a.buildDependencyTree(ctx, blocker, nextSeen)
		if err != nil {
			return DependencyTree{}, err
		}
		tree.Blockers = append(tree.Blockers, child)
	}
	return tree, nil
}

func DatabasePath() (string, error) {
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

	return filepath.Join(root, ".ait", "ait.db"), nil
}

func ensureSchema(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS issues (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL CHECK (type IN ('task', 'epic')),
			title TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL CHECK (status IN ('open', 'in_progress', 'closed', 'cancelled')),
			parent_id TEXT NULL,
			priority TEXT NOT NULL DEFAULT 'P2' CHECK (priority IN ('P0', 'P1', 'P2', 'P3', 'P4')),
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			closed_at TEXT NULL,
			FOREIGN KEY (parent_id) REFERENCES issues(id)
		);`,
		`CREATE TABLE IF NOT EXISTS issue_dependencies (
			blocked_id TEXT NOT NULL,
			blocker_id TEXT NOT NULL,
			created_at TEXT NOT NULL,
			PRIMARY KEY (blocked_id, blocker_id),
			FOREIGN KEY (blocked_id) REFERENCES issues(id) ON DELETE CASCADE,
			FOREIGN KEY (blocker_id) REFERENCES issues(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS issue_notes (
			id TEXT PRIMARY KEY,
			issue_id TEXT NOT NULL,
			body TEXT NOT NULL,
			created_at TEXT NOT NULL,
			FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_issues_status ON issues(status);`,
		`CREATE INDEX IF NOT EXISTS idx_issues_parent_id ON issues(parent_id);`,
		`CREATE INDEX IF NOT EXISTS idx_issue_dependencies_blocker_id ON issue_dependencies(blocker_id);`,
		`CREATE INDEX IF NOT EXISTS idx_issue_notes_issue_id ON issue_notes(issue_id);`,
	}

	for _, stmt := range statements {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func dependencyAlreadyExists(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "unique")
}
