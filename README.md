# siyuan-knowledge-sync

Sync a folder of markdown notes to [SiYuan](https://b3log.org/siyuan/). Every note gets a `domain` and `intent` tag that controls where it lands in your SiYuan wiki. The tool reads your Git repo, figures out what changed, and pushes only the differences.

## Concepts

**Ontology.** In information science, an ontology is "a formal, explicit specification of a shared conceptualization" ([Gruber, 1993](https://doi.org/10.1006/ijhc.1995.1081)). In this tool, an ontology defines the set of allowed `domain` and `intent` values, the SiYuan folder each domain maps to, and an optional tag vocabulary. The defaults ship as examples; you can replace them entirely.

**Tag.** A tag is a piece of metadata attached to a document that describes its content or role, independent of its location in a folder hierarchy. Here, tags live in YAML frontmatter (`domain:`, `intent:`, and optional free-form tags) and drive routing, search, and compliance checks.

**Bimodal.** Bimodal means the ontology has two independent axes. Every note carries exactly one `domain` (what it is about) and one `intent` (what kind of document it is). For example, a note tagged `domain: security` / `intent: sop` is a security-related standard operating procedure. These two dimensions are orthogonal: you can combine any domain with any intent.

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
domain: security
intent: sop
---
# How to rotate API keys

1. Generate the new key...
```

The values shown below are the built-in defaults. You can replace them entirely in your config (see [Configuration reference](#configuration-reference)). To see the active set, run `siyuan-knowledge-sync schema --json`.

**Default domains** (the topic of the note):

| domain | SiYuan folder |
|---|---|
| `devops` | Sysadmin & DevOps |
| `forensics` | Digital Forensics |
| `security` | Security |
| `ai-ml` | AI & ML |
| `software-dev` | Software Development |
| `quant-finance` | Quant Finance |

**Default intents** (the kind of document):

| intent | Purpose |
|---|---|
| `config` | Configuration reference |
| `sop` | Standard operating procedure |
| `log` | Activity or incident log |
| `decision` | Decision record |
| `concept` | Conceptual explanation |

You can define your own domains and intents in the config file. For example, a team focused on product management might use `domain: product` / `intent: prd`, while a research group might use `domain: neuroscience` / `intent: hypothesis`.

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
  # Replace the built-in domains with your own.
  # Each entry needs an id (used in frontmatter) and a folder (SiYuan destination).
  domains:
    - id: product
      folder: Product Management
    - id: engineering
      folder: Engineering
    - id: research
      folder: Research

  # Replace the built-in intents.
  # Each entry needs an id. Add a folder to route intents to their own index doc.
  intents:
    - id: prd
    - id: rfc
    - id: postmortem
    - id: adr

  # Optional: restrict allowed tags to this list.
  # Omit to allow any tag. Set to [] to forbid free-form tags entirely.
  tags:
    - draft
    - review
    - archived
```

Omit `ontology:` to use the built-in defaults. Provide it to replace the defaults entirely. Run `siyuan-knowledge-sync schema --json` to verify your active configuration.

## Repo layout

The tool walks every `.md` file tracked by Git. Where a file lives on disk does not matter for discovery -- Git tracking is the only prerequisite. What matters is the `domain` in its frontmatter.

**Canonical layout.** Each domain maps to a folder name (see defaults above or your config). A file with `domain: security` belongs in the `Security/` folder. If it is already there (or in a subdirectory of it), nothing happens. If it is somewhere else, the tool moves it with `git mv` and commits the change.

```
notes/
├── Security/                     ← domain: security files live here
│   ├── rotate-keys.md
│   └── incident-response/
│       └── playbook.md           ← subdirectories are fine
├── Sysadmin & DevOps/            ← domain: devops
│   └── deploy-staging.md
├── Random Notes/                 ← will be routed on next sync
│   └── old-security-note.md      ← domain: security → moves to Security/
└── README.md                     ← not .md with frontmatter? ignored
```

**Root-level files.** A `.md` file at the repo root (no directory) syncs to a SiYuan notebook named `root`. This is rarely what you want -- use a domain folder instead.

**Routing is flat.** When a file moves, only its basename is preserved. `Random Notes/old-security-note.md` becomes `Security/old-security-note.md`, not `Security/Random Notes/old-security-note.md`.

**Assets.** Relative image/link references (e.g. `![](images/diagram.png)`) are scanned during routing. If a move would break a reference, the tool reports a warning but still moves the file. Place assets alongside their notes or use absolute paths.

## License

MIT
