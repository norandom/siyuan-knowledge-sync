# Gap Analysis — ontology-gate

## Summary
- **Discovery Scope**: Brownfield extension on top of `siyuan-knowledge-sync` (Reqs 1–13 already shipped at `v0.2.0`).
- **Codebase context** is fully loaded from the same session (learn-codebase pass, Req 12/13 implementation, cobesy SKILL.md inspected). No additional codebase grep required.
- **Headline finding**: Validation and routing land cleanly as extensions to the existing Go components (`tags`, `compliance`, `sync`); the **cognitive** parts of migration (plan generation, cobesy rewrite, diff approval) cannot live in the Go CLI and must live in a project-local Claude Skill that drives the Go CLI via a structured plan contract. **Hybrid (Option B) is the only honest fit** for the brief's "client-side deterministic schema + folder-by-folder interactive migration" combination.
- **Effort total: M–L** (1–2 weeks). **Risk: Medium**, concentrated in (a) the agent⇄CLI plan contract, (b) state-tracker coordination during routing, (c) yaml.Node-preserving frontmatter rewrite.

## Requirement → Asset Map (with gap tags)

| Req | Existing asset | Gap | Tag |
|---|---|---|---|
| **R1** Closed-enum schema | `internal/tags/extractor.go` already parses YAML frontmatter (`splitFrontmatter`, `frontmatterData{Title, Tags}`, `parseFrontmatter`, `ExtractMeta(content) → Meta{Title, Body, Attrs}`) using `yaml.v3` | Add `Domain string` + `Intent string` to `frontmatterData`; closed-enum validation; multi-value detection on `domain:`/`intent:` | **Missing** |
| **R2** CLI validation gate | `internal/compliance/audit.go` `ComplianceEngine.Audit()` + `AutoFix()`; issue type `types.ComplianceIssue{File,Line,Severity,Message,Fixable}`; `SyncEngine.processFile` records per-file errors in `report.Errors`; existing 13.5 graceful-degradation pattern for parse failures | New "schema violation" issue category (with structured `Key`, `OffendingValue`, `Allowed []string`); pre-write abort semantics when a file has a schema violation (distinct from the existing non-fatal compliance warnings) | **Missing** |
| **R3** Frontmatter-wins routing | `SyncEngine.buildHPath()`, `topLevelFolder()`; `state.StateTracker` keyed by `LocalPath` (`Put`/`Remove`); compliance has `checkAssetRefs` audit | `domain → canonical-folder` map; pre-sync mover that `git mv`s + commits; state-tracker move semantics (old key → new key) | **Missing** + **Constraint** (state coordination) |
| **R4** SiYuan custom attrs | `SyncEngine.processFile` already calls `client.SetBlockAttrs(docID, meta.Attrs)` (Req 13.4); `meta.Attrs` is the `custom-<tag>` map | Inject `custom-domain` and `custom-intent` into `meta.Attrs` from the new schema fields (one place) | **Tiny** — mostly wiring |
| **R5** AI Skill (`SKILL.md`) | `.claude/skills/kiro-*` skills demonstrate the SKILL.md format & frontmatter conventions used by Claude Code | New project-local skill, e.g. `.claude/skills/siyuan-ontology/SKILL.md` | **Missing** |
| **R6** Folder-by-folder migration | `cmd/siyuan-knowledge-sync/main.go` (cobra) has `sync`/`download`/`audit`/`mcp-server`. No migration subcommand. No interactive Y/N harness | New CLI: `migrate apply <plan.json>` (mechanical execution of an externally-produced plan); the **plan generation + Y/N loop** is in the Skill, not the CLI | **Missing** + **Architecture seam** |
| **R7** COBESY rewrite | Cobesy SKILL.md at `/Users/mc/.claude/skills/cobesy/SKILL.md` (composition path: Minto/Dirksen/Knowles compress + humanizer). It is a Claude Skill — invokable only by an agent, not by Go code. | The migration loop MUST run agent-side so it can invoke cobesy via the Skill tool, gather the diff, get user approval, then call the CLI to apply | **Architecture constraint** (drives Hybrid) |
| **R8** Preservation invariants | `tags.parseFrontmatter` currently `yaml.Unmarshal`s into a typed struct, losing unknown keys + key order on round-trip; no component currently rewrites local frontmatter — uploads send `meta.Body` (frontmatter-stripped) | A `yaml.Node`-based frontmatter rewriter that adds `domain:`/`intent:` while preserving all other keys, values, and (where possible) ordering and comments. A unit-test guard asserting any date-like field is byte-identical pre/post rewrite | **Missing** |
| **R9** Asset reference safety | `compliance/audit.go` `checkAssetRefs` flags absolute paths + unescaped spaces — but only as compliance warnings, not move-impact checks | Extend asset-ref scanning to compute "would this relative ref resolve at the new path after move?" — only relative refs matter | **Extension** |
| **R10** Legacy retirement | `client.RemoveDocByID` exists (Req 13 client expansion) but is only called by autonomous `prune`. State holds `SiYuanID` per local path | New CLI subcommand (e.g. `retire --plan <list.json>`) that issues `RemoveDocByID` per explicit allow-list; **must not** reuse the autonomous prune path | **Missing** (clean separation from prune) |

## Conventions / constraints observed in the codebase
- **Dependency direction** (design.md, original): `Config → Types → Git/SiYuan Client → Sync Engine → CLI/MCP Server`. New components must respect this — schema validation lives in `tags`/`compliance` (Content/Core layer), routing extension lives in `sync` (Core), plan-apply lives in `cmd` (Interface).
- **Non-fatal policy** (Req 13.4/13.5): attr-API failures are recorded per file but do not flip created/updated. R4 inherits this. But R2 (schema gate) is **fatal-per-file** (abort write for that file) — distinct from the existing non-fatal pattern; this distinction must be explicit in design.
- **Test posture**: per-package `*_test.go` with httptest mock SiYuan + real git in `t.TempDir()`; permission/mtime tests now have container-portable guards (post-`d174bb2`).
- **`.dagger/` ignored by `go ./...`** — CI module won't accidentally drag in business logic.

## Implementation Approach Options

### Option A — Pure Go CLI extension (no agent)
Push everything into Go: extend tags/compliance/sync, add `migrate-folder` subcommand that prompts on stdin.
- ❌ Cobesy is a Claude Skill; cannot run in Go. R7 effectively dropped → migration is mechanical-only.
- ❌ The interactive Y/N + plan generation in stdin/tty is fragile and worse than agent-mediated.
- ✅ Single artifact, simplest test surface.
- **Effort**: M. **Risk**: Low for the parts it covers; **High for R7 dropping** (the user explicitly asked for cobesy).

### Option B — Hybrid: deterministic CLI + Claude Skill orchestrator (recommended)
Go CLI owns the deterministic, testable mechanics: schema validation gate, attr injection, file mover, asset scan, retire pathway, plan apply. A new `.claude/skills/siyuan-ontology/SKILL.md` owns the cognitive workflow: per-folder survey, cobesy rewrite, diff presentation, user approval, then it shells out to the CLI to apply.
- ✅ Matches the brief's explicit "validation client-side / agent self-corrects" mandate.
- ✅ Cobesy stays in the Claude Skill layer where it's invokable.
- ✅ Each CLI subcommand stays small, pure, fully testable.
- ❌ Two-artifact coordination — the **plan JSON contract is critical** and must be versioned.
- ❌ More artifacts; more places to keep enums in sync (the SKILL.md must mirror the CLI's hardcoded enums; design should call out a single source-of-truth strategy, e.g. CLI ships the enums and the skill reads them via `siyuan-knowledge-sync schema --json`).
- **Effort**: M–L. **Risk**: Medium (concentrated in the plan contract and the state-tracker move semantics).

### Option C — Thin CLI + agent does everything
Agent calls into a minimal CLI that only does the final `sync`. All YAML parsing, validation, routing, frontmatter rewrite handled in agent prompts.
- ❌ Contradicts the brief: "LLM output lacks determinism. Fix this client-side."
- ❌ Loses Go's type safety + tests for schema enforcement.
- ✅ Easiest to prototype.
- **Effort**: S. **Risk**: High (no deterministic gate; the entire point of this spec).

## Recommended approach: **Option B (Hybrid)**

The brief's mandate ("CLI is the gatekeeper, agent is the self-correcter, cobesy rewrites content") IS the hybrid split. Anything else either drops cobesy or drops determinism.

### Key design decisions to surface in `/kiro-spec-design`
1. **Single source of truth for the closed enums**. Hardcoded in Go (`internal/ontology` package?) and surfaced via either (a) a `schema --json` CLI subcommand the Skill calls, or (b) an embedded `schema.json` the Skill reads directly. Avoid enum drift between Go and the SKILL.md.
2. **Plan JSON contract** (version, per-file fields: `source_path`, `decision: keep|drop`, `domain`, `intent`, `target_path`, `frontmatter_patch`, `rewritten_body`, `asset_warnings`). Atomic apply semantics (transactional per folder).
3. **StateTracker move semantics**. Options: (a) `StateTracker.Move(oldPath, newPath)`; (b) Remove+Put inside the engine. Pick one and document.
4. **Frontmatter rewriter**. `yaml.v3` `yaml.Node` API preserves keys/values; can preserve comments only if we own the parse-edit-emit loop. Lossy fallback: round-trip via a sorted-keys serializer with a `# preserved-on-migrate` marker.
5. **Canonical-folder map** — final folder name strings (R3.1). Initial proposal:
   - `devops` → `./wiki/Linux & DevOps/`
   - `forensics` → `./wiki/Digital Forensics/`
   - `security` → `./wiki/Security/`
   - `ai-ml` → `./wiki/AI & ML/`
   - `software-dev` → `./wiki/Software Development/`
   - `quant-finance` → `./wiki/Quant Finance/` (created with a `.gitkeep`, empty)
6. **AI Skill placement & name**. Project-local at `.claude/skills/siyuan-ontology/SKILL.md`; what does the SKILL.md `description:` frontmatter say so Claude Code auto-invokes it on the right intents?
7. **Cobesy invocation flow**. The skill should invoke cobesy via Skill tool with a per-file prompt that pins facts + temporal-frontmatter preservation as hard constraints (Req 8.3).

## Effort & Risk

| Area | Effort | Risk | One-liner justification |
|---|---|---|---|
| R1 schema in `tags` | S | Low | Extends existing `frontmatterData` + `ExtractMeta` pattern; closed-enum validation is trivial Go. |
| R2 CLI gate in `compliance` | S–M | Low | New structured `SchemaViolation` issue + abort-before-write in `processFile`; existing audit pattern. |
| R3 frontmatter-wins routing | M | Med | New mover + state-tracker move semantics + git mv/commit + asset-impact scan. Edge cases (case-sensitive FS, files already at target, partial state). |
| R4 attrs (`custom-domain`/`-intent`) | S | Low | One-line injection into `meta.Attrs` once R1 lands. |
| R5 SKILL.md | S | Low | Documentation artifact, no runtime risk. |
| R6 `migrate apply` CLI + Skill loop | M | Med | Plan contract design; Skill drives, CLI applies; atomicity guarantees. |
| R7 cobesy integration (Skill side) | S | Low | Cobesy already proven; the Skill invokes it via Skill tool. CLI is unaware. |
| R8 frontmatter rewriter | S–M | Med | `yaml.Node` round-trip with preservation is the tricky bit; comments-preservation is a stretch goal. |
| R9 asset-ref move-impact | S | Low | Reuse `checkAssetRefs` regex; add relative-path resolution. |
| R10 explicit `retire` | S | Low | New subcommand calling existing `RemoveDocByID`; must not collide with autonomous prune. |

**Aggregate: Effort M–L (1–2 weeks); Risk Medium**, concentrated in R3 + R6/R7 plan contract + R8 frontmatter preservation.

## Research items to carry into design
- **Plan JSON schema** — finalize contract (versioned, atomic, idempotent). Probably the single most impactful design decision.
- **StateTracker.Move semantics** — add method vs Remove+Put; what happens if the new path collides with an existing entry.
- **Frontmatter rewriter fidelity** — pick yaml.Node approach; decide on comment-preservation goal (preserve / drop / stretch-goal).
- **Asset-ref resolution rules** — relative-only? what about SiYuan-style `assets/foo-<id>.png`? Sample real legacy content during design discovery.
- **Single source of truth for enums** — CLI-emits-JSON vs embedded-schema-file; how does the Skill stay in sync.
- **Final canonical folder names** — confirm the initial proposals above with the user during design review.
- **`quant-finance` empty domain** — `.gitkeep` vs README placeholder vs nothing.
- **Skill metadata** — name, `description` triggers, whether it should auto-invoke or be explicitly invoked.
- **Cobesy invocation prompt template** — needs to pin preservation invariants (R8) hard.

## Recommendation for `/kiro-spec-design`
Proceed with **Option B (Hybrid)** as the default. Use the research items above as design-discovery topics. Most have a single defensible answer; the plan JSON contract deserves the longest discussion.

---

## Design synthesis outcomes (recorded at design time)

### Generalization
- The `SchemaViolation` type was kept generic (`Key`, `OffendingValue`, `Allowed`) so future axes beyond `domain`/`intent` can be validated against the same structure without new code paths.
- The validator/router/rewriter split (`ontology/schema` + `ontology/router` + `ontology/frontmatter`) keeps each concern under one file with one reason to change. A future axis adds enum constants and a row in the canonical-folder map; nothing else moves.

### Build vs adopt
- **Adopt** `gopkg.in/yaml.v3` `yaml.Node` for the lossless rewriter — already in `go.mod`; no new dependency.
- **Adopt** `os/exec` system `git` for `git mv` / `git commit` during routing — consistent with how the existing test suite already drives git; avoids dragging `go-git` into a worktree-mutation role it has not played in production. `git` was already a required runtime dependency for test fixtures and (per Req 12 of `siyuan-knowledge-sync`) for the e2e harness.
- **Adopt** cobesy as-is through the Claude `Skill` tool. No wrapper, no copy.
- **Adopt** the existing `SetBlockAttrs` / `RemoveDocByID` paths in the SiYuan client — Req 13.4 wiring is reused unchanged; this spec just feeds new keys into the same `meta.Attrs` map.
- **Build** the `ontology/frontmatter` rewriter (no off-the-shelf Go library preserves arbitrary YAML comments + key order across an in-place edit reliably enough). The post-encode diff guard pins the safety invariant.
- **Build** the `MigrationPlan` JSON contract. There is no standard format here; the contract is small and project-specific.

### Simplification
- One new CLI subcommand (`migrate`) covers keep / drop / retire via the plan's `op` discriminator — no separate `retire` or `route` subcommands.
- `migrate apply <plan.json>` is the only mutating endpoint of the migration workflow. The Skill handles all UX; the CLI only takes a fully-decided plan. This keeps the CLI deterministic, headless, and testable.
- The schema validator is invoked from `compliance/audit` (one place); `sync/engine.processFile` gates on it via the issue category. There is no second "ontology-only" audit pipeline.
- The Skill does not embed the enums. It calls `schema --json` so drift is structurally impossible.
- The `quant-finance` empty domain is implemented as a regular enum value with a regular canonical folder containing a `.gitkeep`. No special-case code.
