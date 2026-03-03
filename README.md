# Agent Issue Tracker

`agent-issue-tracker` is a small, local-first issue tracker built primarily for coding agents.

It is intended to help an agent turn a plan into structured work, track dependencies, preserve notes between sessions, and quickly answer a practical question: what should I do next?

Repository: `https://github.com/ohnotnow/agent-issue-tracker`

## Status

This project is an early work in progress.

It is being actively built and dogfooded, but it should not be treated as stable, production-ready, or safe for real project tracking yet.

Current limitations include:

- schema evolution is still early and not finalized
- test coverage is improving but not complete
- command behavior may change as the tool is dogfooded
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

The current binary is `agent-issue-tracker`.

Implemented commands:

- `init`
- `create`
- `show`
- `list`
- `status`
- `search`
- `update`
- `close`
- `reopen`
- `cancel`
- `ready`
- `dep add`
- `dep remove`
- `dep list`
- `dep tree`
- `note add`
- `note list`

## Initialization And IDs

Run `agent-issue-tracker init --prefix <value>` to explicitly set the project prefix used for public issue IDs.

Examples:

- `agent-issue-tracker init --prefix ait`
- `agent-issue-tracker init --prefix deliveries`

If no prefix has been set yet, the tool will infer one automatically the first time you use it by normalizing the current project directory basename.

Examples:

- a repository directory named `agent-issue-tracker` defaults to `agent-issue-tracker`
- a repository directory named `dta` defaults to `dta`

The prefix is stored in local project configuration inside the SQLite database. Running `init --prefix ...` later will update the stored prefix and re-key existing public issue IDs to match.

Public issue IDs are hierarchical:

- root issue: `<prefix>-<sqid>`
- first child: `<prefix>-<sqid>.1`
- first grandchild: `<prefix>-<sqid>.1.1`

This makes parent-child structure visible directly in the identifier while keeping the root segment compact and readable.

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
GOCACHE=$(pwd)/.gocache go build -o agent-issue-tracker .
```

## Warning

Do not rely on this for important or long-lived work yet.

The current focus is to validate the workflow by using the tool on itself, tighten the schema and command contract, and improve safety before calling it a usable v1.
