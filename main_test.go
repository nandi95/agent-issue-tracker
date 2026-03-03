package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agent-issue-tracker/internal/ait"
	_ "modernc.org/sqlite"
)

func TestStatusInitializesEmptyDatabase(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	ctx := context.Background()
	app, err := ait.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer app.Close()

	var payload map[string]map[string]int
	runJSONCommand(t, app, []string{"status"}, &payload)

	counts := payload["counts"]
	if counts["total"] != 0 {
		t.Fatalf("expected total=0, got %d", counts["total"])
	}
	if counts["ready"] != 0 {
		t.Fatalf("expected ready=0, got %d", counts["ready"])
	}

	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("expected database to exist at %s: %v", dbPath, err)
	}
}

func TestCreateAndShowIssue(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		runJSONCommand[map[string]any](t, a, []string{"init", "--prefix", "demo"}, nil)

		var created ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Bootstrap CLI", "--description", "Implement first version"}, &created)

		if !strings.HasPrefix(created.ID, "demo-") {
			t.Fatalf("expected public issue id, got %s", created.ID)
		}
		if created.Title != "Bootstrap CLI" {
			t.Fatalf("unexpected title: %s", created.Title)
		}
		if created.Status != ait.StatusOpen {
			t.Fatalf("expected status %s, got %s", ait.StatusOpen, created.Status)
		}

		var shown ait.ShowResponse
		runJSONCommand(t, a, []string{"show", created.ID}, &shown)

		if shown.Issue.ID != created.ID {
			t.Fatalf("expected show to return issue %s, got %s", created.ID, shown.Issue.ID)
		}
		if len(shown.Children) != 0 {
			t.Fatalf("expected no children, got %d", len(shown.Children))
		}
		if len(shown.Notes) != 0 {
			t.Fatalf("expected no notes, got %d", len(shown.Notes))
		}
	})
}

func TestInitSetsPrefixAndHierarchicalIDs(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var initPayload map[string]string
		runJSONCommand(t, a, []string{"init", "--prefix", "deliveries"}, &initPayload)

		if initPayload["prefix"] != "deliveries" {
			t.Fatalf("expected prefix deliveries, got %q", initPayload["prefix"])
		}

		var epic ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Epic", "--type", "epic"}, &epic)
		if !strings.HasPrefix(epic.ID, "deliveries-") {
			t.Fatalf("expected deliveries root id, got %s", epic.ID)
		}

		var child ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Child", "--parent", epic.ID}, &child)
		if child.ID != epic.ID+".1" {
			t.Fatalf("expected first child id %s.1, got %s", epic.ID, child.ID)
		}

		var grandchild ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Grandchild", "--parent", child.ID}, &grandchild)
		if grandchild.ID != child.ID+".1" {
			t.Fatalf("expected first grandchild id %s.1, got %s", child.ID, grandchild.ID)
		}
	})
}

func TestOpenMigratesLegacyIDsToPublicKeys(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "legacy.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open legacy db failed: %v", err)
	}

	legacyStatements := []string{
		`CREATE TABLE issues (
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
		`CREATE TABLE issue_dependencies (
			blocked_id TEXT NOT NULL,
			blocker_id TEXT NOT NULL,
			created_at TEXT NOT NULL,
			PRIMARY KEY (blocked_id, blocker_id),
			FOREIGN KEY (blocked_id) REFERENCES issues(id) ON DELETE CASCADE,
			FOREIGN KEY (blocker_id) REFERENCES issues(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE issue_notes (
			id TEXT PRIMARY KEY,
			issue_id TEXT NOT NULL,
			body TEXT NOT NULL,
			created_at TEXT NOT NULL,
			FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE
		);`,
		`INSERT INTO issues (id, type, title, description, status, parent_id, priority, created_at, updated_at, closed_at)
		 VALUES ('legacy-epic', 'epic', 'Legacy Epic', 'Old schema parent', 'open', NULL, 'P1', '2026-03-01T10:00:00Z', '2026-03-01T10:00:00Z', NULL);`,
		`INSERT INTO issues (id, type, title, description, status, parent_id, priority, created_at, updated_at, closed_at)
		 VALUES ('legacy-task', 'task', 'Legacy Task', 'Old schema child', 'open', 'legacy-epic', 'P2', '2026-03-01T10:05:00Z', '2026-03-01T10:05:00Z', NULL);`,
		`INSERT INTO issue_notes (id, issue_id, body, created_at)
		 VALUES ('note-1', 'legacy-task', 'Migrated note', '2026-03-01T10:06:00Z');`,
	}

	for _, stmt := range legacyStatements {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("seed legacy schema failed: %v", err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db failed: %v", err)
	}

	app, err := ait.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer app.Close()

	var shown ait.ShowResponse
	runJSONCommand(t, app, []string{"show", "legacy-task"}, &shown)

	if shown.Issue.ParentID == nil {
		t.Fatalf("expected migrated parent public id, got %+v", shown.Issue.ParentID)
	}
	if !strings.HasPrefix(shown.Issue.ID, *shown.Issue.ParentID+".") {
		t.Fatalf("expected migrated child id to be hierarchical under %s, got %s", *shown.Issue.ParentID, shown.Issue.ID)
	}
	if len(shown.Notes) != 1 || shown.Notes[0].Body != "Migrated note" {
		t.Fatalf("expected migrated note, got %+v", shown.Notes)
	}

	var listed struct {
		Issues []ait.IssueRef `json:"issues"`
	}
	runJSONCommand(t, app, []string{"list"}, &listed)
	if len(listed.Issues) != 2 {
		t.Fatalf("expected 2 migrated issues, got %d", len(listed.Issues))
	}
}

func TestReadyExcludesBlockedIssues(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var blocker ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Blocker"}, &blocker)

		var blocked ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Blocked"}, &blocked)

		runJSONCommand[map[string]any](t, a, []string{"dep", "add", blocked.ID, blocker.ID}, nil)

		var ready struct {
			Issues []ait.IssueRef `json:"issues"`
		}
		runJSONCommand(t, a, []string{"ready"}, &ready)

		if len(ready.Issues) != 1 {
			t.Fatalf("expected exactly one ready issue, got %d", len(ready.Issues))
		}
		if ready.Issues[0].ID != blocker.ID {
			t.Fatalf("expected blocker to be ready, got %s", ready.Issues[0].ID)
		}
	})
}

func TestNotesAreReturnedByShow(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var created ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Task with notes"}, &created)

		var createdNote ait.Note
		runJSONCommand(t, a, []string{"note", "add", created.ID, "Investigated root cause"}, &createdNote)

		if createdNote.IssueID != created.ID {
			t.Fatalf("expected note issue id %s, got %s", created.ID, createdNote.IssueID)
		}

		var shown ait.ShowResponse
		runJSONCommand(t, a, []string{"show", created.ID}, &shown)

		if len(shown.Notes) != 1 {
			t.Fatalf("expected 1 note, got %d", len(shown.Notes))
		}
		if shown.Notes[0].Body != "Investigated root cause" {
			t.Fatalf("unexpected note body: %s", shown.Notes[0].Body)
		}
	})
}

func TestStatusTransitions(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var created ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Transition me"}, &created)

		var updated ait.Issue
		runJSONCommand(t, a, []string{"update", created.ID, "--status", ait.StatusInProgress}, &updated)
		if updated.Status != ait.StatusInProgress {
			t.Fatalf("expected in_progress, got %s", updated.Status)
		}

		var closed ait.Issue
		runJSONCommand(t, a, []string{"close", created.ID}, &closed)
		if closed.Status != ait.StatusClosed {
			t.Fatalf("expected closed, got %s", closed.Status)
		}
		if closed.ClosedAt == nil {
			t.Fatalf("expected closed_at to be set")
		}

		var reopened ait.Issue
		runJSONCommand(t, a, []string{"reopen", created.ID}, &reopened)
		if reopened.Status != ait.StatusOpen {
			t.Fatalf("expected reopened status open, got %s", reopened.Status)
		}
		if reopened.ClosedAt != nil {
			t.Fatalf("expected closed_at to be cleared")
		}
	})
}

func testApp(t *testing.T, fn func(ctx context.Context, a *ait.App)) {
	t.Helper()

	ctx := context.Background()
	app, err := ait.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer app.Close()

	fn(ctx, app)
}


func runJSONCommand[T any](t *testing.T, a *ait.App, args []string, target *T) {
	t.Helper()

	output := captureStdout(t, func() {
		if err := a.Run(context.Background(), args); err != nil {
			t.Fatalf("run(%v) failed: %v", args, err)
		}
	})

	if target == nil {
		return
	}
	if err := json.Unmarshal([]byte(output), target); err != nil {
		t.Fatalf("failed to decode JSON output %q: %v", output, err)
	}
}

func runExpectError(t *testing.T, a *ait.App, args []string) error {
	t.Helper()
	var runErr error
	captureStdout(t, func() {
		runErr = a.Run(context.Background(), args)
	})
	return runErr
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe failed: %v", err)
	}

	originalStdout := os.Stdout
	os.Stdout = writer
	defer func() {
		os.Stdout = originalStdout
	}()

	fn()

	if err := writer.Close(); err != nil {
		t.Fatalf("close writer failed: %v", err)
	}
	bytes, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read stdout failed: %v", err)
	}
	return string(bytes)
}

// --- Step 1: Output contract tests ---

func TestListReturnsIssueRefsByDefault(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "Task A"}, nil)

		var result struct {
			Issues []ait.IssueRef `json:"issues"`
		}
		runJSONCommand(t, a, []string{"list"}, &result)

		if len(result.Issues) != 1 {
			t.Fatalf("expected 1 issue, got %d", len(result.Issues))
		}
		if result.Issues[0].Title != "Task A" {
			t.Fatalf("unexpected title: %s", result.Issues[0].Title)
		}

		// Verify IssueRef shape: decode raw JSON and check no extra fields
		raw := captureStdout(t, func() {
			if err := a.Run(ctx, []string{"list"}); err != nil {
				t.Fatal(err)
			}
		})
		var rawResult map[string][]map[string]any
		if err := json.Unmarshal([]byte(raw), &rawResult); err != nil {
			t.Fatal(err)
		}
		issue := rawResult["issues"][0]
		if _, ok := issue["description"]; ok {
			t.Fatal("default list should not include description field")
		}
		if _, ok := issue["created_at"]; ok {
			t.Fatal("default list should not include created_at field")
		}
	})
}

func TestListLongReturnsFullIssues(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "Task A", "--description", "Details"}, nil)

		var result struct {
			Issues []ait.Issue `json:"issues"`
		}
		runJSONCommand(t, a, []string{"list", "--long"}, &result)

		if len(result.Issues) != 1 {
			t.Fatalf("expected 1 issue, got %d", len(result.Issues))
		}
		if result.Issues[0].Description != "Details" {
			t.Fatalf("expected description in --long output, got %q", result.Issues[0].Description)
		}
	})
}

func TestReadyReturnsIssueRefsByDefault(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "Ready task"}, nil)

		raw := captureStdout(t, func() {
			if err := a.Run(ctx, []string{"ready"}); err != nil {
				t.Fatal(err)
			}
		})
		var rawResult map[string][]map[string]any
		if err := json.Unmarshal([]byte(raw), &rawResult); err != nil {
			t.Fatal(err)
		}
		issue := rawResult["issues"][0]
		if _, ok := issue["description"]; ok {
			t.Fatal("default ready should not include description field")
		}
	})
}

func TestReadyLongReturnsFullIssues(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "Task", "--description", "Full"}, nil)

		var result struct {
			Issues []ait.Issue `json:"issues"`
		}
		runJSONCommand(t, a, []string{"ready", "--long"}, &result)

		if len(result.Issues) != 1 {
			t.Fatalf("expected 1 issue, got %d", len(result.Issues))
		}
		if result.Issues[0].Description != "Full" {
			t.Fatalf("expected description in --long output, got %q", result.Issues[0].Description)
		}
	})
}

// --- Step 2: Type filter ---

func TestReadyFilterByType(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "My Epic", "--type", "epic"}, nil)
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "My Task", "--type", "task"}, nil)

		var tasksOnly struct {
			Issues []ait.IssueRef `json:"issues"`
		}
		runJSONCommand(t, a, []string{"ready", "--type", "task"}, &tasksOnly)

		if len(tasksOnly.Issues) != 1 {
			t.Fatalf("expected 1 task, got %d", len(tasksOnly.Issues))
		}
		if tasksOnly.Issues[0].Type != "task" {
			t.Fatalf("expected type task, got %s", tasksOnly.Issues[0].Type)
		}

		var epicsOnly struct {
			Issues []ait.IssueRef `json:"issues"`
		}
		runJSONCommand(t, a, []string{"ready", "--type", "epic"}, &epicsOnly)

		if len(epicsOnly.Issues) != 1 {
			t.Fatalf("expected 1 epic, got %d", len(epicsOnly.Issues))
		}
	})
}

// --- Step 3: Dependency tests ---

func TestDepAddAndList(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var a1, a2 ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Issue A"}, &a1)
		runJSONCommand(t, a, []string{"create", "--title", "Issue B"}, &a2)

		var depList struct {
			IssueID  string        `json:"issue_id"`
			Blockers []ait.IssueRef `json:"blockers"`
			Blocks   []ait.IssueRef `json:"blocks"`
		}
		runJSONCommand(t, a, []string{"dep", "add", a1.ID, a2.ID}, &depList)

		if len(depList.Blockers) != 1 {
			t.Fatalf("expected 1 blocker, got %d", len(depList.Blockers))
		}
		if depList.Blockers[0].ID != a2.ID {
			t.Fatalf("expected blocker %s, got %s", a2.ID, depList.Blockers[0].ID)
		}
	})
}

func TestDepRemove(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var a1, a2 ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Issue A"}, &a1)
		runJSONCommand(t, a, []string{"create", "--title", "Issue B"}, &a2)

		runJSONCommand[map[string]any](t, a, []string{"dep", "add", a1.ID, a2.ID}, nil)

		var depList struct {
			Blockers []ait.IssueRef `json:"blockers"`
		}
		runJSONCommand(t, a, []string{"dep", "remove", a1.ID, a2.ID}, &depList)

		if len(depList.Blockers) != 0 {
			t.Fatalf("expected 0 blockers after remove, got %d", len(depList.Blockers))
		}
	})
}

func TestDepTree(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var a1, a2, a3 ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Root"}, &a1)
		runJSONCommand(t, a, []string{"create", "--title", "Mid"}, &a2)
		runJSONCommand(t, a, []string{"create", "--title", "Leaf"}, &a3)

		runJSONCommand[map[string]any](t, a, []string{"dep", "add", a1.ID, a2.ID}, nil)
		runJSONCommand[map[string]any](t, a, []string{"dep", "add", a2.ID, a3.ID}, nil)

		var tree ait.DependencyTree
		runJSONCommand(t, a, []string{"dep", "tree", a1.ID}, &tree)

		if tree.Issue.ID != a1.ID {
			t.Fatalf("expected root %s, got %s", a1.ID, tree.Issue.ID)
		}
		if len(tree.Blockers) != 1 || tree.Blockers[0].Issue.ID != a2.ID {
			t.Fatalf("expected mid-level blocker %s", a2.ID)
		}
		if len(tree.Blockers[0].Blockers) != 1 || tree.Blockers[0].Blockers[0].Issue.ID != a3.ID {
			t.Fatalf("expected leaf blocker %s", a3.ID)
		}
	})
}

func TestDepAddTransitiveCycleDetection(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var a1, a2, a3 ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "A"}, &a1)
		runJSONCommand(t, a, []string{"create", "--title", "B"}, &a2)
		runJSONCommand(t, a, []string{"create", "--title", "C"}, &a3)

		// A blocked by B, B blocked by C
		runJSONCommand[map[string]any](t, a, []string{"dep", "add", a1.ID, a2.ID}, nil)
		runJSONCommand[map[string]any](t, a, []string{"dep", "add", a2.ID, a3.ID}, nil)

		// C blocked by A would create A->B->C->A cycle
		err := runExpectError(t, a, []string{"dep", "add", a3.ID, a1.ID})
		if err == nil {
			t.Fatal("expected cycle detection error")
		}
		if !strings.Contains(err.Error(), "cycle") {
			t.Fatalf("expected cycle error message, got: %s", err.Error())
		}
	})
}

func TestDepAddSelfDependency(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var a1 ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Self"}, &a1)

		err := runExpectError(t, a, []string{"dep", "add", a1.ID, a1.ID})
		if err == nil {
			t.Fatal("expected self-dependency error")
		}
		if !strings.Contains(err.Error(), "itself") {
			t.Fatalf("expected self-dependency message, got: %s", err.Error())
		}
	})
}

// --- Step 4c: List filtering tests ---

func TestListFilterByType(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "Epic", "--type", "epic"}, nil)
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "Task", "--type", "task"}, nil)

		var result struct {
			Issues []ait.IssueRef `json:"issues"`
		}
		runJSONCommand(t, a, []string{"list", "--type", "task"}, &result)

		if len(result.Issues) != 1 {
			t.Fatalf("expected 1 task, got %d", len(result.Issues))
		}
		if result.Issues[0].Title != "Task" {
			t.Fatalf("unexpected title: %s", result.Issues[0].Title)
		}
	})
}

func TestListFilterByPriority(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "Urgent", "--priority", "P0"}, nil)
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "Normal", "--priority", "P2"}, nil)

		var result struct {
			Issues []ait.IssueRef `json:"issues"`
		}
		runJSONCommand(t, a, []string{"list", "--priority", "P0"}, &result)

		if len(result.Issues) != 1 {
			t.Fatalf("expected 1 issue, got %d", len(result.Issues))
		}
		if result.Issues[0].Title != "Urgent" {
			t.Fatalf("unexpected title: %s", result.Issues[0].Title)
		}
	})
}

func TestListFilterByStatus(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var created ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "To close"}, &created)
		runJSONCommand[ait.Issue](t, a, []string{"close", created.ID}, nil)
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "Still open"}, nil)

		var result struct {
			Issues []ait.IssueRef `json:"issues"`
		}
		runJSONCommand(t, a, []string{"list", "--status", "closed"}, &result)

		if len(result.Issues) != 1 {
			t.Fatalf("expected 1 closed issue, got %d", len(result.Issues))
		}
		if result.Issues[0].Title != "To close" {
			t.Fatalf("unexpected title: %s", result.Issues[0].Title)
		}
	})
}

func TestListFilterByParent(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var epic ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Parent Epic", "--type", "epic"}, &epic)
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "Child 1", "--parent", epic.ID}, nil)
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "Child 2", "--parent", epic.ID}, nil)
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "Standalone"}, nil)

		var result struct {
			Issues []ait.IssueRef `json:"issues"`
		}
		runJSONCommand(t, a, []string{"list", "--parent", epic.ID}, &result)

		if len(result.Issues) != 2 {
			t.Fatalf("expected 2 children, got %d", len(result.Issues))
		}
	})
}

// --- Step 4d: Negative/error path tests ---

func TestSearchReturnsMatches(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "Authentication bug", "--description", "Login fails"}, nil)
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "Dashboard feature"}, nil)

		var result struct {
			Issues []ait.Issue `json:"issues"`
		}
		runJSONCommand(t, a, []string{"search", "Authentication"}, &result)

		if len(result.Issues) != 1 {
			t.Fatalf("expected 1 match, got %d", len(result.Issues))
		}
		if result.Issues[0].Title != "Authentication bug" {
			t.Fatalf("unexpected title: %s", result.Issues[0].Title)
		}
	})
}

func TestShowNotFound(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		err := runExpectError(t, a, []string{"show", "nonexistent"})
		if err == nil {
			t.Fatal("expected not_found error")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Fatalf("expected not found message, got: %s", err.Error())
		}
	})
}

func TestCancelAndReopen(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var created ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "To cancel"}, &created)

		var cancelled ait.Issue
		runJSONCommand(t, a, []string{"cancel", created.ID}, &cancelled)
		if cancelled.Status != ait.StatusCancelled {
			t.Fatalf("expected cancelled, got %s", cancelled.Status)
		}

		var reopened ait.Issue
		runJSONCommand(t, a, []string{"reopen", created.ID}, &reopened)
		if reopened.Status != ait.StatusOpen {
			t.Fatalf("expected open after reopen, got %s", reopened.Status)
		}
	})
}

func TestCreateEpicWithParent(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		var epic ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Parent", "--type", "epic"}, &epic)

		err := runExpectError(t, a, []string{"create", "--title", "Nested epic", "--type", "epic", "--parent", epic.ID})
		if err == nil {
			t.Fatal("expected validation error for epic with parent")
		}
		if !strings.Contains(err.Error(), "epics cannot have a parent") {
			t.Fatalf("unexpected error: %s", err.Error())
		}
	})
}

func TestHelpShowsUsage(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		output := captureStdout(t, func() {
			if err := a.Run(ctx, []string{"help"}); err != nil {
				t.Fatalf("help failed: %v", err)
			}
		})

		for _, want := range []string{"Commands:", "create", "list", "ready", "dep", "note", "--db"} {
			if !strings.Contains(output, want) {
				t.Fatalf("expected help to contain %q, got:\n%s", want, output)
			}
		}
	})
}

// --- Rekey tests (ait-2KY5X.6) ---

func TestRekeyChangesAllRootIDs(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		runJSONCommand[map[string]string](t, a, []string{"init", "--prefix", "alpha"}, nil)

		var i1, i2 ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "First"}, &i1)
		runJSONCommand(t, a, []string{"create", "--title", "Second"}, &i2)

		if !strings.HasPrefix(i1.ID, "alpha-") || !strings.HasPrefix(i2.ID, "alpha-") {
			t.Fatalf("expected alpha- prefix, got %s and %s", i1.ID, i2.ID)
		}

		runJSONCommand[map[string]string](t, a, []string{"init", "--prefix", "beta"}, nil)

		var listed struct {
			Issues []ait.IssueRef `json:"issues"`
		}
		runJSONCommand(t, a, []string{"list"}, &listed)

		if len(listed.Issues) != 2 {
			t.Fatalf("expected 2 issues, got %d", len(listed.Issues))
		}
		for _, issue := range listed.Issues {
			if !strings.HasPrefix(issue.ID, "beta-") {
				t.Fatalf("expected beta- prefix after rekey, got %s", issue.ID)
			}
		}
	})
}

func TestRekeyPreservesHierarchy(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		runJSONCommand[map[string]string](t, a, []string{"init", "--prefix", "alpha"}, nil)

		var epic ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Epic", "--type", "epic"}, &epic)
		var child ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Child", "--parent", epic.ID}, &child)
		var grandchild ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Grandchild", "--parent", child.ID}, &grandchild)

		// Verify original hierarchy
		if child.ID != epic.ID+".1" {
			t.Fatalf("expected child %s.1, got %s", epic.ID, child.ID)
		}
		if grandchild.ID != child.ID+".1" {
			t.Fatalf("expected grandchild %s.1, got %s", child.ID, grandchild.ID)
		}

		runJSONCommand[map[string]string](t, a, []string{"init", "--prefix", "beta"}, nil)

		var listed struct {
			Issues []ait.IssueRef `json:"issues"`
		}
		runJSONCommand(t, a, []string{"list"}, &listed)

		// Find the epic (no dot in ID)
		var newEpicID string
		for _, issue := range listed.Issues {
			if !strings.Contains(issue.ID, ".") {
				newEpicID = issue.ID
				break
			}
		}
		if newEpicID == "" || !strings.HasPrefix(newEpicID, "beta-") {
			t.Fatalf("expected beta- root epic, got %q", newEpicID)
		}

		// Verify dotted suffixes are maintained
		expectedChild := newEpicID + ".1"
		expectedGrandchild := newEpicID + ".1.1"
		found := map[string]bool{}
		for _, issue := range listed.Issues {
			found[issue.ID] = true
		}
		if !found[expectedChild] {
			t.Fatalf("expected child %s in listed issues: %v", expectedChild, listed.Issues)
		}
		if !found[expectedGrandchild] {
			t.Fatalf("expected grandchild %s in listed issues: %v", expectedGrandchild, listed.Issues)
		}
	})
}

func TestRekeyDependenciesSurvive(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		runJSONCommand[map[string]string](t, a, []string{"init", "--prefix", "alpha"}, nil)

		var i1, i2 ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Blocked"}, &i1)
		runJSONCommand(t, a, []string{"create", "--title", "Blocker"}, &i2)
		runJSONCommand[map[string]any](t, a, []string{"dep", "add", i1.ID, i2.ID}, nil)

		runJSONCommand[map[string]string](t, a, []string{"init", "--prefix", "beta"}, nil)

		// Find the rekeyed IDs
		var listed struct {
			Issues []ait.IssueRef `json:"issues"`
		}
		runJSONCommand(t, a, []string{"list"}, &listed)

		var blockedID, blockerID string
		for _, issue := range listed.Issues {
			if issue.Title == "Blocked" {
				blockedID = issue.ID
			}
			if issue.Title == "Blocker" {
				blockerID = issue.ID
			}
		}

		var depList struct {
			IssueID  string         `json:"issue_id"`
			Blockers []ait.IssueRef `json:"blockers"`
		}
		runJSONCommand(t, a, []string{"dep", "list", blockedID}, &depList)

		if len(depList.Blockers) != 1 {
			t.Fatalf("expected 1 blocker after rekey, got %d", len(depList.Blockers))
		}
		if depList.Blockers[0].ID != blockerID {
			t.Fatalf("expected blocker %s, got %s", blockerID, depList.Blockers[0].ID)
		}
	})
}

func TestRekeyNotesSurvive(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		runJSONCommand[map[string]string](t, a, []string{"init", "--prefix", "alpha"}, nil)

		var created ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Has notes"}, &created)
		runJSONCommand[ait.Note](t, a, []string{"note", "add", created.ID, "Important note"}, nil)

		runJSONCommand[map[string]string](t, a, []string{"init", "--prefix", "beta"}, nil)

		// Find the rekeyed ID
		var listed struct {
			Issues []ait.IssueRef `json:"issues"`
		}
		runJSONCommand(t, a, []string{"list"}, &listed)
		if len(listed.Issues) != 1 {
			t.Fatalf("expected 1 issue, got %d", len(listed.Issues))
		}
		newID := listed.Issues[0].ID

		var noteList struct {
			IssueID string     `json:"issue_id"`
			Notes   []ait.Note `json:"notes"`
		}
		runJSONCommand(t, a, []string{"note", "list", newID}, &noteList)

		if len(noteList.Notes) != 1 {
			t.Fatalf("expected 1 note after rekey, got %d", len(noteList.Notes))
		}
		if noteList.Notes[0].Body != "Important note" {
			t.Fatalf("expected note body 'Important note', got %q", noteList.Notes[0].Body)
		}
	})
}

func TestRekeyDouble(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		runJSONCommand[map[string]string](t, a, []string{"init", "--prefix", "alpha"}, nil)

		var i1 ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Survives double rekey"}, &i1)
		if !strings.HasPrefix(i1.ID, "alpha-") {
			t.Fatalf("expected alpha- prefix, got %s", i1.ID)
		}

		runJSONCommand[map[string]string](t, a, []string{"init", "--prefix", "beta"}, nil)

		var midList struct {
			Issues []ait.IssueRef `json:"issues"`
		}
		runJSONCommand(t, a, []string{"list"}, &midList)
		if !strings.HasPrefix(midList.Issues[0].ID, "beta-") {
			t.Fatalf("expected beta- prefix after first rekey, got %s", midList.Issues[0].ID)
		}

		runJSONCommand[map[string]string](t, a, []string{"init", "--prefix", "gamma"}, nil)

		var finalList struct {
			Issues []ait.IssueRef `json:"issues"`
		}
		runJSONCommand(t, a, []string{"list"}, &finalList)
		if len(finalList.Issues) != 1 {
			t.Fatalf("expected 1 issue, got %d", len(finalList.Issues))
		}
		if !strings.HasPrefix(finalList.Issues[0].ID, "gamma-") {
			t.Fatalf("expected gamma- prefix after second rekey, got %s", finalList.Issues[0].ID)
		}
	})
}

func TestRekeyIdempotent(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		runJSONCommand[map[string]string](t, a, []string{"init", "--prefix", "foo"}, nil)

		var i1, i2 ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "First"}, &i1)
		runJSONCommand(t, a, []string{"create", "--title", "Second"}, &i2)

		// Capture IDs before second init
		var before struct {
			Issues []ait.IssueRef `json:"issues"`
		}
		runJSONCommand(t, a, []string{"list"}, &before)

		beforeIDs := map[string]bool{}
		for _, issue := range before.Issues {
			beforeIDs[issue.ID] = true
		}

		// Run init with same prefix again
		runJSONCommand[map[string]string](t, a, []string{"init", "--prefix", "foo"}, nil)

		var after struct {
			Issues []ait.IssueRef `json:"issues"`
		}
		runJSONCommand(t, a, []string{"list"}, &after)

		if len(after.Issues) != len(before.Issues) {
			t.Fatalf("expected same number of issues, got %d vs %d", len(before.Issues), len(after.Issues))
		}
		for _, issue := range after.Issues {
			if !beforeIDs[issue.ID] {
				t.Fatalf("ID changed after idempotent rekey: %s not in original set", issue.ID)
			}
		}
	})
}

func TestListHuman(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		runJSONCommand[map[string]string](t, a, []string{"init", "--prefix", "test"}, nil)

		var epic ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Stabilize v1", "--type", "epic", "--priority", "P1"}, &epic)
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "Add schema versioning", "--parent", epic.ID, "--priority", "P1"}, nil)
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "Improve prioritization", "--parent", epic.ID, "--priority", "P2"}, nil)
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "Standalone task"}, nil)

		output := captureStdout(t, func() {
			if err := a.Run(ctx, []string{"list", "--human"}); err != nil {
				t.Fatalf("list --human failed: %v", err)
			}
		})

		// Should contain the epic ID
		if !strings.Contains(output, epic.ID) {
			t.Fatalf("expected epic ID %s in output:\n%s", epic.ID, output)
		}
		// Should contain child suffixes
		if !strings.Contains(output, ".1") {
			t.Fatalf("expected child suffix .1 in output:\n%s", output)
		}
		if !strings.Contains(output, ".2") {
			t.Fatalf("expected child suffix .2 in output:\n%s", output)
		}
		// Should contain titles
		if !strings.Contains(output, "Stabilize v1") {
			t.Fatalf("expected epic title in output:\n%s", output)
		}
		if !strings.Contains(output, "Add schema versioning") {
			t.Fatalf("expected child title in output:\n%s", output)
		}
		// Should contain the type label for epics
		if !strings.Contains(output, "epic") {
			t.Fatalf("expected 'epic' type label in output:\n%s", output)
		}
		// Should not be JSON
		if strings.HasPrefix(strings.TrimSpace(output), "{") {
			t.Fatalf("expected non-JSON output, got:\n%s", output)
		}
	})
}

func TestListTree(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		runJSONCommand[map[string]string](t, a, []string{"init", "--prefix", "tree"}, nil)

		var epic ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Epic One", "--type", "epic", "--priority", "P1"}, &epic)
		var child1 ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Child One", "--parent", epic.ID}, &child1)
		var child2 ait.Issue
		runJSONCommand(t, a, []string{"create", "--title", "Child Two", "--parent", epic.ID}, &child2)
		runJSONCommand[ait.Issue](t, a, []string{"create", "--title", "Solo task"}, nil)

		output := captureStdout(t, func() {
			if err := a.Run(ctx, []string{"list", "--tree"}); err != nil {
				t.Fatalf("list --tree failed: %v", err)
			}
		})

		// Should contain tree connectors
		if !strings.Contains(output, "├── ") {
			t.Fatalf("expected ├── connector in output:\n%s", output)
		}
		if !strings.Contains(output, "└── ") {
			t.Fatalf("expected └── connector in output:\n%s", output)
		}
		// Should contain full child IDs
		if !strings.Contains(output, child1.ID) {
			t.Fatalf("expected child ID %s in output:\n%s", child1.ID, output)
		}
		if !strings.Contains(output, child2.ID) {
			t.Fatalf("expected child ID %s in output:\n%s", child2.ID, output)
		}
		// Should contain metadata in parentheses
		if !strings.Contains(output, "(epic, P1, open)") {
			t.Fatalf("expected '(epic, P1, open)' in output:\n%s", output)
		}
		// Children should have (priority, status) format
		if !strings.Contains(output, "(P2, open)") {
			t.Fatalf("expected '(P2, open)' in output:\n%s", output)
		}
		// Should not be JSON
		if strings.HasPrefix(strings.TrimSpace(output), "{") {
			t.Fatalf("expected non-JSON output, got:\n%s", output)
		}
	})
}

func TestListHumanAndTreeMutuallyExclusive(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		err := runExpectError(t, a, []string{"list", "--human", "--tree"})
		if err == nil {
			t.Fatal("expected error for --human --tree")
		}
		if !strings.Contains(err.Error(), "mutually exclusive") {
			t.Fatalf("unexpected error: %s", err.Error())
		}
	})
}

func TestCreateInvalidType(t *testing.T) {
	testApp(t, func(ctx context.Context, a *ait.App) {
		err := runExpectError(t, a, []string{"create", "--title", "Bad type", "--type", "story"})
		if err == nil {
			t.Fatal("expected validation error for invalid type")
		}
		if !strings.Contains(err.Error(), "type must be one of") {
			t.Fatalf("unexpected error: %s", err.Error())
		}
	})
}
