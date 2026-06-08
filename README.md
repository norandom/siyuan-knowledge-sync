# siyuan-knowledge-sync

A sync client for [SiYuan](https://b3log.org/siyuan/) that features a bimodal ontology. Syncs markdown files from local folders (Git-style) to the SiYuan knowledge base.

## Overview

siyuan-knowledge-sync reads markdown files tracked in a Git repository and uploads them to SiYuan as documents. It enforces a bimodal ontology — every note must declare a `domain` and `intent` in its YAML frontmatter — and routes files into the correct SiYuan notebook and hierarchy automatically.

### Bimodal Ontology

Every synced note requires two frontmatter fields:

```yaml
---
domain: engineering
intent: reference
---
```

- **Domain** — the knowledge area (e.g. `engineering`, `finance`, `health`)
- **Intent** — the note's purpose (e.g. `reference`, `decision`, `log`, `journal`)

The ontology gate rejects notes with missing or invalid values. This keeps the SiYuan wiki structured and navigable.

## Features

- **Git-aware sync** — only processes files tracked by Git; respects renames and moves
- **Ontology routing** — maps `domain` + `intent` to SiYuan notebooks and document paths
- **Tag vocabulary** — enforces a controlled tag set; auto-fixes unknown tags
- **Compliance auditing** — checks documents for schema violations, heading levels, block IDs, and tag issues
- **Auto-fix** — repairs common problems (heading levels, missing attributes, TOC content)
- **Bidirectional download** — pulls documents from SiYuan back to local markdown
- **Migration tooling** — plan and apply bulk moves when ontology rules change
- **MCP server** — exposes an MCP interface for agent-based access

## Installation

```bash
go install github.com/mc/Source/siyuan-knowledge-sync/cmd/siyuan-knowledge-sync@latest
```

Or build from source:

```bash
git clone https://github.com/mc/Source/siyuan-knowledge-sync.git
cd siyuan-knowledge-sync
go build -o siyuan-knowledge-sync ./cmd/siyuan-knowledge-sync
```

## Usage

### Sync files to SiYuan

```bash
siyuan-knowledge-sync sync --config config.yaml ./path/to/repo
```

### Download from SiYuan

```bash
siyuan-knowledge-sync download --config config.yaml ./output
```

### Audit for compliance issues

```bash
siyuan-knowledge-sync audit --config config.yaml ./path/to/repo
```

### View ontology schema

```bash
siyuan-knowledge-sync schema --json
```

### Configure frontmatter in existing files

```bash
siyuan-knowledge-sync configure ./path/to/files
```

### Migration

```bash
# Generate a migration plan
siyuan-knowledge-sync migrate plan --config config.yaml

# Apply a migration plan
siyuan-knowledge-sync migrate apply plan.json
```

## Configuration

Create a `config.yaml`:

```yaml
siyuan:
  endpoint: http://localhost:6806
  token: your-api-token
sync:
  notebooks:
    engineering: "20240101120000-abc1234"
    finance: "20240101120000-def5678"
tags:
  vocabulary:
    - reference
    - decision
    - log
    - journal
    - draft
```

## Project Structure

```
cmd/siyuan-knowledge-sync/   CLI entry point (cobra commands)
internal/
  compliance/                 Audit checks and auto-fix
  config/                     Configuration loading
  git/                        Git scanner (tracked file enumeration)
  mcp/                        MCP server implementation
  migrate/                    Migration plan and apply
  ontology/                   Domain/intent schema, routing, frontmatter
  siyuan/                     SiYuan HTTP client
  state/                      Sync state tracker
  sync/                       Sync engine, asset handling, dashboard
  tags/                       Tag extraction and frontmatter parsing
  toc/                        Table of contents generation
  types/                      Shared types
```

## Development

```bash
# Run tests
go test ./...

# Build
go build ./cmd/siyuan-knowledge-sync

# Lint (requires golangci-lint)
golangci-lint run
```

## License

MIT
