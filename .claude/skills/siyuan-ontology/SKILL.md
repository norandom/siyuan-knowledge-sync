---
name: siyuan-ontology
description: >
  Use this skill when authoring or migrating markdown notes destined for the
  SiYuan wiki tree. It enforces the closed-enum ontology (`domain:` + `intent:`)
  defined by the `siyuan-knowledge-sync` CLI gate, reads enums and the canonical
  folder map from `siyuan-knowledge-sync schema --json` (single source of truth,
  never hardcoded in this skill), invokes the local `cobesy` skill to rewrite
  content during migration, and emits versioned `MigrationPlan` JSON consumed
  by `siyuan-knowledge-sync migrate apply`. Triggers: any task that creates or
  rewrites a note for the SiYuan wiki; any folder-by-folder migration of legacy
  notes from `/Users/mc/Source/wiki` or `/Users/mc/Source/siyuan-wiki`; any
  structured-error recovery after the CLI gate refuses a write with a
  `SchemaViolation` payload.
allowed-tools: Read, Write, Edit, Bash, Glob, Grep, Skill
---

# siyuan-ontology

## What this skill does

This skill is the agent-side counterpart of the `ontology-gate` CLI in
`siyuan-knowledge-sync`. It teaches an agent to produce ontology-conformant
frontmatter on the first try, to deterministically self-correct when the CLI
gate refuses a write, and to drive folder-by-folder migrations of legacy notes
into the canonical wiki tree.

Three invariants govern everything below:

1. The enum values for `domain:` and `intent:` and the canonical folder map
   live in `siyuan-knowledge-sync schema --json`. This skill never hardcodes
   them in its prose; it derives them from the live schema call.
2. When the gate refuses a write, the agent parses the structured
   `SchemaViolation`, applies a single deterministic correction, and retries.
   No free-form guessing, no retry-loops that change strategy each round.
3. Original temporal frontmatter (`date:`, `lastmod:`, `created:`, `updated:`,
   `original_date:`) is sacred. It records when the original work was done and
   must be carried through migration byte-for-byte. The cobesy rewrite is
   structurally and prose-level only; it never invents facts and never touches
   dates.

## Sourcing the schema (single source of truth)

The first action of every session that uses this skill is:

```bash
siyuan-knowledge-sync schema --json
```

Parse the JSON. The shape is:

```json
{
  "version": <integer>,
  "domain": {
    "values": ["<one-of-domain.values>", "..."],
    "folders": {
      "<one-of-domain.values>": "wiki/<canonical folder>",
      "...": "..."
    }
  },
  "intent": {
    "values": ["<one-of-intent.values>", "..."]
  },
  "required_keys": ["domain", "intent"]
}
```

Rules:

- Treat `domain.values` and `intent.values` as the only legal values. The skill
  body does not list them; you must read them from this JSON.
- Treat `domain.folders` as the deterministic map from a declared `domain:`
  to its wiki-rooted canonical folder. The CLI will `git mv` files there
  whenever the local path disagrees, so the plan you produce must already
  target the same folder.
- Re-fetch the schema if `version` changes between sessions. Do not rely on
  cached output from a previous run.
- If the `siyuan-knowledge-sync` binary is not on `PATH`, build it from the
  repo root (`go build ./cmd/siyuan-knowledge-sync`) and invoke the local
  binary; do not fall back to hardcoded enum values under any condition.

## Required frontmatter shape

Every wiki-destined markdown file must declare two scalar frontmatter keys:

```yaml
---
title: <human title, optional>
date: <preserved verbatim from source if present>
lastmod: <preserved verbatim from source if present>
domain: <one-of-domain.values>
intent: <one-of-intent.values>
tags: [<optional user tags>]
---

# Body
```

Constraints:

- `domain:` and `intent:` are scalar strings. YAML lists or mappings under
  these keys are rejected by the gate as a multi-value schema violation.
- All other frontmatter keys are preserved verbatim by the rewriter. Do not
  introduce, rename, or reorder them.
- If a source file has no temporal frontmatter, do not synthesize one.
- The body content is unconstrained by the ontology; structure and prose
  improvements are the cobesy skill's job, not this one's.

## Structured error format and the self-correction loop

When the CLI gate refuses a write, the per-file `SyncError` carries a
JSON-encoded `SchemaViolation` payload. Its shape is exactly:

```json
{
  "file": "<wiki-rooted path>",
  "key": "<one of: domain, intent>",
  "offending_value": "<the rejected value, or empty string for missing key>",
  "allowed": ["<one-of-domain.values>", "..."]
}
```

Self-correction is deterministic. Parse the JSON and dispatch on `(key,
offending_value)`:

1. `key == "domain"` and `offending_value == ""`: the file is missing
   `domain:`. Insert a single scalar value chosen from `allowed` based on
   the file's content. Retry.
2. `key == "intent"` and `offending_value == ""`: the file is missing
   `intent:`. Insert a single scalar value chosen from `allowed` based on
   the file's content. Retry.
3. `key == "domain"` and `offending_value != ""`: the value is out-of-enum
   or multi-value. Replace it with a single scalar from `allowed`. Retry.
4. `key == "intent"` and `offending_value != ""`: same as 3 for intent.

Retry exactly once per violation. If the same violation re-appears on the
next attempt, escalate to the user (do not loop). Multiple violations on one
file are corrected in a single edit, then retried once.

The `audit` subcommand (`siyuan-knowledge-sync audit -c <config>`) emits the
same JSON shape and is the right pre-flight check before submitting a
`MigrationPlan`.

## Migration workflow

Migrations are folder-by-folder. For each source folder (for example
`/Users/mc/Source/wiki/<folder>` or `/Users/mc/Source/siyuan-wiki/<folder>`),
execute these steps in order:

1. **Survey.** List every `.md` file under the folder. For each file, read
   the YAML frontmatter and the body.
2. **Per-file proposal.** Decide one of `keep`, `drop_local`, or
   `retire_siyuan` (`drop_local` removes from the local tree; `retire_siyuan`
   removes a still-extant SiYuan document via its doc ID). For `keep`,
   propose a `domain:` (one of `domain.values`), an `intent:` (one of
   `intent.values`), a `target_path` derived from `domain.folders[domain]`,
   and any content-fix notes.
3. **Cobesy rewrite (keep only).** Invoke the local `cobesy` skill on the
   body via the Skill tool's composition path. Pass the PRESERVATION
   INVARIANTS block (next section) verbatim as a hard constraint, followed
   by the original body. Use the cobesy output as the proposed rewrite.
4. **Diff approval.** Show the user a unified diff of the original vs the
   proposed (rewritten body + new frontmatter keys). Require explicit
   approval per file. If the user rejects the rewrite, keep the original
   body and still proceed with the ontology and routing steps for that file.
5. **Synthesize the plan.** Build one `MigrationPlan` JSON with `version: 1`
   and one entry per approved file. Skipped or fully rejected files do not
   appear in the plan.
6. **Pre-flight (optional but recommended).** Run `siyuan-knowledge-sync
   audit -c <config>` against the source tree. Resolve any surfaced schema
   violations using the self-correction loop above before submitting.
7. **Apply.** Run `siyuan-knowledge-sync migrate apply <plan.json> -c
   <config>`. Read the `Migration Report` printed to stderr; surface
   per-entry outcomes (created, updated, dropped, retired, errored) back to
   the user.
8. **Next folder.** Repeat from step 1.

## PRESERVATION INVARIANTS (Req 8) — hard constraint for cobesy

When invoking the `cobesy` composition path on a file body during a `keep`
migration, prepend exactly this constraint block to the prompt:

```
PRESERVATION INVARIANTS (do not violate):
- Every existing frontmatter `date:`, `lastmod:`, `created:`, `updated:`,
  and `original_date:` value MUST be carried through verbatim. They record
  when the original task was done; never overwrite them and never invent
  a clock-current value.
- Any other frontmatter key not in {domain, intent} MUST also be
  preserved byte-equal in value. Only `domain:` and `intent:` are
  allowed to be added or changed by ontology-gate.
- Factual claims in the body MUST be preserved. Improve structure,
  framing, sentence-level clarity, and adoption affordances; do NOT
  invent facts, sources, code snippets, or quantitative results.
- If you cannot rewrite the body without violating any of the above,
  return the original body unchanged.
```

Append the original body after the block. The cobesy skill honors this as a
binding constraint, and the CLI's `AddOntology` rewriter enforces the
temporal-key invariant a second time at write time (`ErrUnsafeRewrite` halts
the per-entry apply if anything other than `domain:`/`intent:` would be
touched).

## CLI commands you will invoke

| Command | Purpose |
|---------|---------|
| `siyuan-knowledge-sync schema --json` | Single source of truth for enums + canonical folder map. Always the first call of a session. |
| `siyuan-knowledge-sync audit -c <config>` | Pre-flight check; surfaces schema violations in the same JSON shape as the sync gate. |
| `siyuan-knowledge-sync migrate apply <plan.json> -c <config>` | Execute a validated plan; prints a `MigrationReport` to stderr. |
| `siyuan-knowledge-sync sync -c <config>` | Normal sync after manual edits (not during migration). The gate runs here too. |

The `<config>` path resolution follows the existing CLI behavior; if the
user has a default `.siyuan-sync.yaml` in the repo root, omit the flag.

## `MigrationPlan` JSON shape

The skill emits a single JSON document per folder. Its exact shape:

```json
{
  "version": 1,
  "source": "/Users/mc/Source/wiki/<folder>",
  "generated_at": "<ISO 8601 UTC>",
  "entries": [
    {
      "op": "<one of: keep, drop_local, retire_siyuan>",
      "source_path": "wiki/<relative path>",
      "domain": "<one-of-domain.values, only for op=keep>",
      "intent": "<one-of-intent.values, only for op=keep>",
      "rewritten_body": "<post-cobesy body, only when op=keep AND a rewrite was approved>",
      "siyuan_doc_id": "<SiYuan document ID, only for op=retire_siyuan>",
      "notes": "<optional human note>"
    }
  ]
}
```

Per-op field rules (enforced by `MigrationPlan.Validate()` in
`internal/migrate/plan.go`; a violation causes the CLI to reject the plan at
pre-flight):

- `op=keep`: requires `domain` and `intent` (both enum-valid); forbids
  `siyuan_doc_id`. `rewritten_body` is optional.
- `op=drop_local`: forbids `domain`, `intent`, `rewritten_body`,
  `siyuan_doc_id`.
- `op=retire_siyuan`: requires `siyuan_doc_id`; forbids `domain`, `intent`,
  `rewritten_body`.

`source_path` is always required. `notes` is always optional.

## What this skill does NOT do

- It does not write SiYuan documents directly. It produces a plan; the CLI
  executes it.
- It does not fix asset references. The router scans for relative asset
  links and emits warnings during a move; this skill surfaces those
  warnings to the user but does not relocate `assets/foo.png` files.
- It does not autonomously retire SiYuan documents. A `retire_siyuan` entry
  only appears in a plan when the user has explicitly approved it for the
  named document ID.
- It does not invent enum values, folder names, or canonical paths. Every
  value of `domain:`, `intent:`, and the target folder comes from the live
  `schema --json` output. If the schema is unreachable, the migration
  pauses and the user is told.
- It does not edit `tasks.md`, the steering files, or any other Kiro spec
  artifact. Its outputs are: the per-file frontmatter edit, the cobesy
  invocation, the `MigrationPlan` JSON, and the apply call.
