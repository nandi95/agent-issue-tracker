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
