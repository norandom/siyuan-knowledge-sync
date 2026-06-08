# siyuan-knowledge-sync

Sync a folder of markdown notes to [SiYuan](https://b3log.org/siyuan/). Every note gets a `domain` and `intent` tag that controls where it lands in your SiYuan wiki. The tool reads your Git repo, figures out what changed, and pushes only the differences.

## Quick start

### 1. Install

```bash
go install github.com/norandom/siyuan-knowledge-sync/cmd/siyuan-knowledge-sync@latest
```

Or build from source:

```bash
git clone https://github.com/norandom/siyuan-knowledge-sync.git
cd siyuan-knowledge-sync
go build -o siyuan-knowledge-sync ./cmd/siyuan-knowledge-sync
```

### 2. Configure

Create `.siyuan-sync.yaml` in your repo root (or anywhere — pass `-c path/to/config.yaml`):

```yaml
endpoint: http://localhost:6806
token: your-siyuan-api-token
repo_path: /path/to/your/markdown/repo
autofix: true
```

### 3. Tag your notes

Every markdown file needs a YAML frontmatter block with `domain` and `intent`:

```yaml
---
domain: devops
intent: sop
---
# How to deploy the staging server

1. Build the image...
```

Run `siyuan-knowledge-sync schema --json` to see the full list of allowed values:

| domain | SiYuan folder |
|---|---|
| `devops` | Sysadmin & DevOps |
| `forensics` | Digital Forensics |
| `security` | Security |
| `ai-ml` | AI & ML |
| `software-dev` | Software Development |
| `quant-finance` | Quant Finance |

| intent | Purpose |
|---|---|
| `config` | Configuration reference |
| `sop` | Standard operating procedure |
| `log` | Activity or incident log |
| `decision` | Decision record |
| `concept` | Conceptual explanation |

### 4. Sync

```bash
siyuan-knowledge-sync sync
```

Only files tracked by Git that changed since the last sync get uploaded. New files are created in SiYuan under the folder matching their `domain`. Files already at their canonical path are left alone; files in the wrong folder get moved automatically.

## Commands

### `sync` — Upload changes to SiYuan

```bash
siyuan-knowledge-sync sync              # incremental sync
siyuan-knowledge-sync sync --dry-run    # audit only, no changes
```

### `download` — Pull SiYuan content to local files

```bash
siyuan-knowledge-sync download                     # skip conflicts
siyuan-knowledge-sync download --conflict overwrite # replace local files
siyuan-knowledge-sync download --conflict merge     # merge content
```

### `audit` — Check files for compliance issues

```bash
siyuan-knowledge-sync audit             # report issues
siyuan-knowledge-sync audit --autofix   # fix what can be fixed
```

Checks for: missing domain/intent, invalid values, bad heading levels, missing block IDs, unknown tags, TOC problems.

### `schema` — Show ontology configuration

```bash
siyuan-knowledge-sync schema            # human-readable
siyuan-knowledge-sync schema --json     # JSON output
```

### `migrate` — Bulk moves when ontology changes

```bash
siyuan-knowledge-sync migrate plan    # generate a plan
siyuan-knowledge-sync migrate apply plan.json   # execute it
```

## Configuration reference

Full `.siyuan-sync.yaml`:

```yaml
endpoint: http://localhost:6806
token: your-api-token
repo_path: /home/you/notes
autofix: true

# Optional: Cloudflare Access (Zero Trust)
cf_access_client_id: your-cf-client-id
cf_access_client_secret: your-cf-secret

# Optional: override the default ontology
ontology:
  domains:
    - id: devops
      folder: Sysadmin & DevOps
    - id: security
      folder: Security
  intents:
    - id: sop
    - id: log
    - id: decision
  tags:
    - reference
    - draft
    - archived
```

When `ontology:` is omitted, the built-in defaults (see `schema --json`) apply. When provided, it fully replaces the defaults.

## How it works

1. Scans your Git repo for tracked `.md` files
2. Reads YAML frontmatter to extract `domain` and `intent`
3. Validates against the ontology schema (rejects invalid values)
4. Routes each file to the correct SiYuan notebook and folder
5. Uploads only changed files (tracks state between runs)
6. Moves files that are in the wrong folder (git mv + commit)
7. Rewrites image/asset links and uploads assets
8. Prunes SiYuan docs for locally deleted files

## License

MIT
