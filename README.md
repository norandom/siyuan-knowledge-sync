# siyuan-knowledge-sync

Sync a folder of markdown notes to [SiYuan](https://b3log.org/siyuan/). Every note gets a `domain` and `intent` tag that controls where it lands in your SiYuan wiki. The tool reads your Git repo, figures out what changed, and pushes only the differences.

## Quick start

### 1. Install

Download the latest binary for your platform from GitHub releases:

```bash
# Linux (amd64)
curl -L -o siyuan-knowledge-sync https://github.com/norandom/siyuan-knowledge-sync/releases/latest/download/siyuan-knowledge-sync_latest_linux_amd64
chmod +x siyuan-knowledge-sync
sudo mv siyuan-knowledge-sync /usr/local/bin/

# Linux (arm64)
curl -L -o siyuan-knowledge-sync https://github.com/norandom/siyuan-knowledge-sync/releases/latest/download/siyuan-knowledge-sync_latest_linux_arm64
chmod +x siyuan-knowledge-sync
sudo mv siyuan-knowledge-sync /usr/local/bin/

# Linux (armv7, e.g. Raspberry Pi)
curl -L -o siyuan-knowledge-sync https://github.com/norandom/siyuan-knowledge-sync/releases/latest/download/siyuan-knowledge-sync_latest_linux_arm
chmod +x siyuan-knowledge-sync
sudo mv siyuan-knowledge-sync /usr/local/bin/

# macOS (Apple silicon)
curl -L -o siyuan-knowledge-sync https://github.com/norandom/siyuan-knowledge-sync/releases/latest/download/siyuan-knowledge-sync_latest_darwin_arm64
chmod +x siyuan-knowledge-sync
sudo mv siyuan-knowledge-sync /usr/local/bin/

# macOS (Intel)
curl -L -o siyuan-knowledge-sync https://github.com/norandom/siyuan-knowledge-sync/releases/latest/download/siyuan-knowledge-sync_latest_darwin_amd64
chmod +x siyuan-knowledge-sync
sudo mv siyuan-knowledge-sync /usr/local/bin/

# Windows (PowerShell)
Invoke-WebRequest -Uri "https://github.com/norandom/siyuan-knowledge-sync/releases/latest/download/siyuan-knowledge-sync_latest_windows_amd64.exe" -OutFile "siyuan-knowledge-sync.exe"
```

Or with Go:

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

The sync is incremental: only Git-tracked files that changed since the last run get uploaded. New notes land in the SiYuan folder matching their `domain`. Notes already at the right path are skipped; notes in the wrong folder get moved (git mv + commit).

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

Omit `ontology:` to use the built-in defaults (run `schema --json` to see them). Provide it to replace the defaults entirely.

## How it works

The tool scans Git-tracked `.md` files, reads `domain`/`intent` from frontmatter, validates against the ontology, and routes each file to the right SiYuan notebook. Only changed files upload. It moves misplaced files (git mv + commit), rewrites asset links, uploads images, and prunes SiYuan docs for locally deleted files.

## License

MIT
