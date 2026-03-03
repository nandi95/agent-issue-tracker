# Agent Issue Tracker

`ait` is a small, local-first issue tracker built primarily for coding agents.

It is intended to help an agent turn a plan into structured work, track dependencies, preserve notes between sessions, and quickly answer a practical question: what should I do next?

Repository: `https://github.com/ohnotnow/ait`

## Status

This project is an early work in progress.

It is being actively built and dogfooded, but it should not be treated as stable, production-ready, or safe for real project tracking yet.

Current limitations include:

- schema evolution is still early and not finalised
- command behaviour may change as the tool is dogfooded
- compatibility and data migration guarantees do not exist yet

If you try it, assume the data model and CLI may change.

## Current Goals

The tool is optimized for agent workflow first:

- create epics and tasks
- model dependencies
- store progress notes
- resume work after session loss or conversation compaction
- surface unblocked work via `ready`

Human-friendly output is intentionally secondary for now. JSON is the default interface.

## Current Command Set

The current binary is `ait`.

Implemented commands:

- `init`
- `create`
- `show`
- `list` (`--type`, `--status`, `--priority`, `--parent`, `--all`, `--long`)
- `status`
- `search`
- `update`
- `close`
- `reopen`
- `cancel`
- `ready` (`--type`, `--long`)
- `dep add`
- `dep remove`
- `dep list`
- `dep tree`
- `note add`
- `note list`

## Output Modes

By default, `list` and `ready` return a slim view with only the fields an agent typically needs: `id`, `title`, `status`, `type`, and `priority`. This keeps token usage low and makes it easier to reason about results quickly.

Pass `--long` to get the full issue record including `description`, `parent_id`, timestamps, and `closed_at`.

```bash
ait list                  # slim (5 fields per issue)
ait list --long           # full record
ait ready --type task     # slim, tasks only (excludes epics)
ait ready --long          # full record, all types
```

## Dependency Cycle Detection

When adding a dependency with `dep add`, the tool performs a transitive reachability check. If the new dependency would create a cycle (e.g. A depends on B, B depends on C, and you try to make C depend on A), the command is rejected with a validation error.

## Initialisation And IDs

Run `ait init --prefix <value>` to set the project prefix used for public issue IDs.

Examples:

- `ait init --prefix ait`
- `ait init --prefix deliveries`

If no prefix has been set yet, the tool will infer one automatically the first time you use it by normalizing the current project directory basename.

Examples:

- a repository directory named `ait` defaults to `ait`
- a repository directory named `dta` defaults to `dta`

The prefix is stored in local project configuration inside the SQLite database. Running `init --prefix ...` later will update the stored prefix and re-key existing public issue IDs to match.

Public issue IDs are hierarchical:

- root issue: `<prefix>-<sqid>`
- first child: `<prefix>-<sqid>.1`
- first grandchild: `<prefix>-<sqid>.1.1`

This makes parent-child structure visible directly in the identifier while keeping the root segment compact and readable.

## Custom Database Path

By default, the database is stored at `.ait/ait.db` in the current git repository root. You can override this with the `--db` flag:

```bash
ait --db /path/to/other.db list
ait --db /path/to/other.db create --title "Task in another DB"
```

This is useful for git worktrees (pointing back to the main repo's database), keeping separate databases for different subsystems, or using `:memory:` for testing.

## Local Storage

The tool uses SQLite and creates a local database at `.ait/ait.db` in the current git repository root (or the current directory if no git root is found).

That database stores:

- issues, dependencies, and notes
- project-level configuration such as the current public ID prefix

This keeps issue state close to the codebase it belongs to and makes it easy to inspect or back up.

## Development

Current development priorities are tracked in the tool itself.

To run the test suite:

```bash
GOCACHE=$(pwd)/.gocache go test ./...
```

To build the binary:

```bash
GOCACHE=$(pwd)/.gocache go build -o ait .
```

## Warning

Do not rely on this for important or long-lived work yet.

The current focus is to validate the workflow by using the tool on itself, tighten the schema and command contract, and improve safety before calling it a usable v1.
