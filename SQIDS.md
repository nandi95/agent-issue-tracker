# Sqids Notes

## Purpose

This file records the current design decision around introducing Sqids-backed public issue keys for `agent-issue-tracker`.

The goal is not to replace the internal database identifier with a Sqid. The goal is to add a more readable, more reliable public-facing key for CLI use.

## Decision

Sqids looks like a good fit for this project.

Why:

- it is explicitly designed to generate short IDs from non-negative numbers
- encoding and decoding are both first-class operations
- it is intended for visually friendly IDs rather than security-sensitive tokens
- the Go port is official and linked from the Sqids project site
- it supports a custom alphabet and minimum length, which gives us control over shape and readability

For this tool, the best use is:

- keep an internal numeric database ID
- add a separate public key column
- generate a Sqid from the numeric row ID
- expose the public key in CLI output and accept it in CLI input

That gives us stable internal references and more legible external identifiers.

## Recommended Model

Recommended shape:

- internal primary key: integer
- public key: string, unique
- visible format: `ait-<sqid>`

Example:

- internal row ID: `123`
- public key: `ait-Uk3a9`

This is better than using the current random hex string as the only identifier because:

- it is shorter
- it is easier to visually distinguish
- it is easier to read back in terminal output
- it creates a clean boundary between storage identity and CLI identity

## Hierarchical Keys

Sqids should provide the readable root, not the full hierarchy.

If we adopt Beads-style hierarchy later, the clean model is:

- epic key: `ait-Uk3a9`
- child key: `ait-Uk3a9.1`
- later descendant: `ait-Uk3a9.1.1`

That means:

- Sqids solves the root identifier problem
- hierarchy is an application-level suffix
- we should not try to encode the whole task tree into Sqids itself

This likely means keeping separate internal fields for:

- the stable integer primary key
- the public key
- optional parent linkage
- optional child ordinal if we adopt hierarchical suffixes

## Important Constraint

Sqids works on non-negative numbers.

That means it fits naturally once we move to an integer primary key or other numeric surrogate key. It is not a direct replacement for the current random string IDs.

The implementation shape would be:

1. insert the row
2. read the generated numeric row ID
3. encode it with Sqids
4. store `ait-<sqid>` as the public key

This can be done in the same write transaction when we make the schema change.

## Validation Caveat

Sqids can decode user input back into numbers, but not every decodable string is guaranteed to be the canonical encoding we would want to accept as a stable public key.

If we accept user-supplied public keys, we should:

1. strip the `ait-` prefix
2. decode the Sqid
3. re-encode the decoded numeric value
4. confirm the result matches the submitted Sqid

That avoids accepting alternate non-canonical strings for the same underlying numeric value.

## Why This Helps Agent Workflow

For the primary user of this tool, readable public keys should reduce mistakes.

Compared with the current opaque random hex IDs, `ait-<sqid>` style keys should be:

- easier to copy correctly
- easier to distinguish at a glance
- easier to discuss in notes and terminal output

This is especially useful when several issues are visible in one command result.

## Interaction With Output Design

This work pairs naturally with slimming down default `list` and `ready` output.

The likely target shape is:

- default `list` and `ready`: compact, task-selection focused
- `--long`: fuller metadata when needed
- `show`: the canonical full detail view

Readable public keys become more valuable when the default output is compact and optimized for scanning.

## Migration Strategy For This Project

Because this repository is still early WIP and we are currently the only users, we do not need a fully general end-user `migrate` command yet.

The practical plan is:

- implement schema versioning and migration code inside the application
- add tests around the schema transition and key generation
- once the new schema is working, close out the old transition issues
- remove the current local database
- start fresh with the new key model

That is a simpler and safer path than building a polished migration UX too early.

## Deferred Decisions

These are still open:

- exact Sqids options (custom alphabet, minimum length)
- whether hierarchical suffixes ship in the same change or later
- whether the CLI should accept both old IDs and new public keys during the transition
- whether list-like commands should emit only public keys by default

## Sources

- Sqids overview: https://sqids.org/
- Sqids Go page: https://sqids.org/go
- Sqids FAQ: https://sqids.org/faq
- Sqids GitHub org: https://github.com/sqids
