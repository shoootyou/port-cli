# Managing skill entities in the Port catalog

`port skills catalog` lets you provision, inspect, and manage skill entities
stored in your Port organisation's `skill` blueprint — directly from the CLI.

**This is distinct from `port skills sync`**, which reads skill entities from
Port and writes them as files into your local AI tool directories. The catalog
sub-commands are the write side: they create and update the skill entities that
`port skills sync` later reads.

## Prerequisites

- `port` CLI installed and authenticated (`port auth login`)
- The `skill` blueprint must exist in your Port organisation. If it does not,
  run the one-time bootstrap command first:

```sh
port skills catalog blueprint init
```

---

## Skill file format

Each skill is defined in a Markdown file with YAML frontmatter:

```md
---
identifier: my-skill
title: My Skill
description: A short one-line description shown in the Port UI.
location: global
---

Full Markdown instructions for the skill. Everything after the closing `---`
becomes the skill's `instructions` field in Port. Leading and trailing blank
lines are trimmed.
```

**Frontmatter fields:**

| Field | Required | Default | Notes |
|-------|----------|---------|-------|
| `identifier` | ✅ | — | Unique key. Pattern: `^[A-Za-z0-9._-]+$` |
| `title` | no | identifier | Display name shown in the Port UI |
| `description` | ✅ | — | One-line summary |
| `location` | no | `global` | `global` or `project` — controls where `port skills sync` writes the skill |

---

## Bootstrap the blueprint

Run once per organisation before using any other catalog commands:

```sh
port skills catalog blueprint init
```

The operation is idempotent — running it again when the blueprint already exists
produces no error.

---

## Commands

### `port skills catalog create`

Create or update a skill entity from a Markdown file. The default behaviour is
an **upsert**: if the entity exists it is merged with the new values; if it does
not exist it is created.

```sh
port skills catalog create -f my-skill.md
```

**Flags:**

| Flag | Description |
|------|-------------|
| `-f, --file <path>` | Path to the skill Markdown file (required) |
| `--force` | Full replace — overwrites all existing properties. Cannot be combined with `--patch`. |
| `--patch` | Partial update — only the fields present in the file are sent (PATCH). Cannot be combined with `--force`. |
| `--yes` | Skip the confirmation prompt |
| `-o, --output table\|json\|yaml` | Output format (default: `table`) |

**Examples:**

```sh
# Upsert (default)
port skills catalog create -f my-skill.md

# Full replace
port skills catalog create -f my-skill.md --force

# Partial update (fields in file only)
port skills catalog create -f my-skill.md --patch

# Non-interactive, JSON output
port skills catalog create -f my-skill.md --yes -o json
```

---

### `port skills catalog list`

List all skill entities in the Port catalog.

```sh
port skills catalog list
port skills catalog list -o json
```

**Flags:** `-o, --output table|json|yaml` (default: `table`)

Output columns (table): `IDENTIFIER`, `TITLE`, `LOCATION`

---

### `port skills catalog get`

Retrieve a single skill entity by identifier.

```sh
port skills catalog get my-skill
port skills catalog get my-skill -o yaml
```

**Flags:** `-o, --output table|json|yaml` (default: `table`)

---

### `port skills catalog update`

Partially update a skill entity from a Markdown file (PATCH semantics). Only
the fields present in the file are updated. Equivalent to
`port skills catalog create --patch`.

```sh
port skills catalog update -f my-skill.md
```

**Flags:**

| Flag | Description |
|------|-------------|
| `-f, --file <path>` | Path to the skill Markdown file (required) |
| `-o, --output table\|json\|yaml` | Output format (default: `table`) |

---

### `port skills catalog delete`

Delete a skill entity from the Port catalog. Prompts for confirmation before
deleting. Deleting a non-existent skill returns an error.

```sh
port skills catalog delete my-skill
port skills catalog delete my-skill --yes
```

**Flags:** `--yes` — skip the confirmation prompt

---

### `port skills catalog blueprint init`

Bootstrap the `skill` blueprint in Port. Run this once before using any other
catalog commands. Safe to re-run — the operation is idempotent.

```sh
port skills catalog blueprint init
```

---

## Command reference

| Command | Description |
|---------|-------------|
| `port skills catalog create -f <file>` | Create or update (upsert) a skill entity |
| `port skills catalog create -f <file> --force` | Full replace of an existing skill entity |
| `port skills catalog create -f <file> --patch` | Partial update (PATCH) |
| `port skills catalog list` | List all skill entities |
| `port skills catalog get <id>` | Get a single skill entity |
| `port skills catalog update -f <file>` | Partial update (PATCH) — alias for `create --patch` |
| `port skills catalog delete <id>` | Delete a skill entity |
| `port skills catalog blueprint init` | Bootstrap the `skill` blueprint (one-time setup) |

---

## Troubleshooting

**"blueprint not found" error**

Run `port skills catalog blueprint init` to create the `skill` blueprint in your
Port organisation.

**Authentication errors**

Re-run `port auth login` to refresh your token.

**Identifier validation error**

The `identifier` field must match `^[A-Za-z0-9._-]+$`. Spaces and special
characters other than `.`, `_`, and `-` are not allowed.
