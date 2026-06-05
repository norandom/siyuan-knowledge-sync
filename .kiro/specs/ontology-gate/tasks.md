# Implementation Plan

- [ ] 1. Foundation: ontology types and parser surface

- [x] 1.1 Closed-enum schema package
  - Introduce the closed `Domain` and `Intent` enums with the values locked in the spec (six domains including the reserved-empty `quant-finance`; five intents).
  - Provide `AllDomains` / `AllIntents` accessors as the only sources for emission and validation.
  - Provide `ValidateDomain` and `ValidateIntent` returning a structured `SchemaViolation` (key, offending value, allowed values) or nil.
  - Provide `CheckOntologyFrontmatter` returning the full list of violations for a parsed frontmatter view (missing-key, out-of-enum, multi-value).
  - Observable: a unit test passes asserting every member of `AllDomains` round-trips through `ValidateDomain`, an unknown value yields a `*SchemaViolation` with the expected `Allowed` slice, and a multi-value YAML node yields a "multi-value" violation.
  - _Requirements: 1.1, 1.2, 1.4, 1.6, 2.5_
  - _Boundary: ontology/schema_

- [ ] 1.2 (P) Frontmatter parser extension and custom-attr injection
  - Extend the existing tag extractor's parsed frontmatter struct with `Domain` and `Intent` scalar fields.
  - Surface them on `Meta` and inject `custom-domain` and `custom-intent` into the existing `Meta.Attrs` map so the already-wired `SetBlockAttrs` call in the sync engine picks them up unchanged.
  - Preserve byte-identical behaviour of the existing `Extract` path used by the compliance audit (rely on the existing drift-guard test).
  - Observable: a unit test parses a file with `domain: devops` and `intent: sop` and asserts `meta.Domain == "devops"`, `meta.Intent == "sop"`, `meta.Attrs["custom-domain"] == "devops"`, `meta.Attrs["custom-intent"] == "sop"`; an existing-file fixture without those keys still parses identically to today.
  - _Requirements: 1.1, 1.4, 4.1, 4.2_
  - _Boundary: tags_

- [ ] 2. Core building blocks

- [ ] 2.1 (P) Canonical-folder router and asset reference scan
  - Implement `Router.CanonicalFolder` mapping each `Domain` constant to its canonical wiki folder (final names from design Section "Components and Interfaces"; `quant-finance` resolves to its folder even though empty).
  - Implement `Router.Route(domain, localPath, body)` returning `RouteNoop` when the local path already matches and `RouteMove` with the canonical target otherwise.
  - Scan the body for relative asset references (markdown image/link patterns) and return per-reference warnings (original path, new resolved path, target-exists flag) regardless of action.
  - Observable: a unit test exercises every domain → canonical-folder mapping; a file already under its canonical folder produces `RouteNoop`; a file with a `![](assets/foo.png)` reference being moved to a deeper folder produces a single `AssetWarning` with the old and new resolved paths.
  - _Requirements: 1.3, 3.1, 3.6, 9.1, 9.2, 9.3, 9.4_
  - _Boundary: ontology/router_
  - _Depends: 1.1_

- [ ] 2.2 (P) yaml.Node-based frontmatter rewriter with preservation guard
  - Implement `AddOntology(content, domain, intent)` that parses YAML via `yaml.Node`, inserts or replaces only the two ontology keys, and re-emits while preserving every other key, its value, and its declaration order.
  - Enforce a post-encode diff guard: re-parse both inputs into a key-indexed view and assert that every non-ontology key's raw value bytes are unchanged. On any other-key mutation, return `ErrUnsafeRewrite` without writing.
  - Treat `date`, `lastmod`, `created`, `updated`, and `original_date` as non-negotiable byte-identical fields covered by the guard.
  - When the content has no frontmatter, prepend a fresh block with only the two ontology keys (no synthesized temporal fields).
  - Observable: a unit test fixture with `date: 2024-03-11`, `lastmod: 2024-03-12`, custom keys, and mixed quoting round-trips through `AddOntology(devops, sop)` and the temporal/custom keys come out byte-identical; a synthetic regression that flips a date value triggers `ErrUnsafeRewrite`.
  - _Requirements: 1.5, 8.1, 8.2, 8.3, 8.4, 8.5_
  - _Boundary: ontology/frontmatter_

- [ ] 2.3 (P) State tracker move semantics
  - Add `StateTracker.Move(oldPath, newPath)` that renames an entry while preserving `SiYuanID`, `NotebookID`, and `SyncedAt`.
  - Return `ErrCollision` when `newPath` already exists and points at a different `SiYuanID`; tolerate `newPath` pointing at the same `SiYuanID` as a no-op.
  - Observable: a unit test puts an entry at `a.md`, calls `Move("a.md", "wiki/Linux & DevOps/a.md")`, asserts the new key is present, the old key is gone, and `SiYuanID`/`SyncedAt` survived; a second test triggers `ErrCollision` on a conflicting target.
  - _Requirements: 3.2, 3.3_
  - _Boundary: state_

- [ ] 2.4 Compliance audit schema-violation rule
  - Add `checkOntologySchema` to the compliance engine: it consumes a parsed `FrontmatterView` and emits issues with the new `Category: "schema"` for each violation class (missing required key, out-of-enum value, multi-value).
  - Add the `Category` field to the compliance issue type (empty = legacy issue, `"schema"` = gate-eligible).
  - Ensure existing audit rules (heading, attribute, asset, tag, TOC) continue to emit issues with empty `Category` and the existing audit_test suite remains green.
  - Observable: a unit test against a fixture with `intent: braindump` produces exactly one schema-category issue with the offending value and allowed list, the existing audit tests still pass, and a clean file produces zero schema-category issues.
  - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.7_
  - _Boundary: compliance_
  - _Depends: 1.1, 1.2_

- [ ] 2.5 (P) Migration plan JSON contract
  - Define the versioned `MigrationPlan` type (V1) with fields `Version`, `Source`, `GeneratedAt`, `Entries`.
  - Define `PlanEntry` with `Op` discriminator (`keep`, `drop_local`, `retire_siyuan`), `SourcePath`, `Domain`, `Intent`, `RewrittenBody`, `SiYuanDocID`, `Notes`.
  - Implement `plan.Validate()` rejecting unknown ops, unknown versions, and per-op missing required fields (keep needs Domain+Intent; retire_siyuan needs SiYuanDocID).
  - Observable: a unit test round-trips a sample plan through JSON marshal/unmarshal byte-identically (except permitted whitespace), and `Validate` rejects each of: bad version, unknown op, keep without Domain, retire without SiYuanDocID.
  - _Requirements: 6.1, 6.2, 6.7, 10.2_
  - _Boundary: migrate/plan_

- [ ] 3. Integration: sync engine and migrate apply

- [ ] 3.1 Sync engine schema gate (abort-this-file on schema violations)
  - In the sync engine's per-file path, after compliance audit, detect issues with `Category: "schema"` and route them into the per-file report as structured `SyncError` messages carrying the JSON-encoded `SchemaViolation`.
  - When schema violations exist for a file, return early before any upload, routing, title, or attrs call for that file; remaining files in the batch must still run.
  - Observable: an engine integration test seeds a two-file batch where one file has `intent: braindump`; that file appears in `report.Errors` with a parsable `SchemaViolation` payload and is absent from `report.Created`/`report.Updated`, while the other file is created normally.
  - _Requirements: 2.6, 3.5_
  - _Boundary: sync/engine_
  - _Depends: 2.4, 1.2_

- [ ] 3.2 Sync engine pre-sync routing (git mv + commit + state.Move + asset warnings)
  - After the schema gate, invoke the router for files that survived; on `RouteMove`, run `git mv` via `os/exec` from the source to the canonical target, then `git commit -m "ontology-route: <old> -> <new>"`.
  - Call `state.Move(old, new)` immediately after the successful git rename so the in-memory and persisted state remain coherent; on `ErrCollision` record a per-file error and skip the upload.
  - Update the in-flight file reference to the new path for the remainder of `processFile`; surface router `AssetWarning`s into the per-file part of the report as warnings (not errors).
  - Observable: an integration test commits a file at `wiki/misc/foo.md` with `domain: devops`; after `Sync`, the file is at the canonical devops folder, a single `ontology-route:` commit exists in the test repo, the state tracker shows the new path with the original `SiYuanID`, and an `![](assets/foo.png)` reference produces a single asset warning entry in the report.
  - _Requirements: 3.2, 3.3, 3.4, 3.6_
  - _Boundary: sync/engine_
  - _Depends: 2.1, 2.3, 3.1_

- [ ] 3.3 SyncEngine.RouteAndSync exported entry point
  - Expose `SyncEngine.RouteAndSync(ctx, path)` that runs the schema gate, the router, the upload, and the attr-apply for a single file — the same code path 3.1+3.2 use inside `processFile`, exposed for the migrate apply executor.
  - Document the non-fatal semantics inherited from siyuan-knowledge-sync Req 13 (title/attr failures recorded but non-fatal; create/update failure is fatal-for-that-file).
  - Observable: a focused unit test calls `RouteAndSync` directly on a single-file fixture (no batch) and asserts the file ends at the canonical path, the SiYuan mock receives the create call with the frontmatter-stripped body, and `custom-domain`/`custom-intent` arrive in the attrs payload.
  - _Requirements: 4.1, 6.4_
  - _Boundary: sync/engine_
  - _Depends: 3.2_

- [ ] 3.4 Migrate apply executor (keep, drop_local, retire_siyuan)
  - Implement `migrate.Apply(ctx, plan, engine, client)` returning a `MigrationReport`.
  - For `keep`: call `ontology.AddOntology` to add the two keys (preserving everything else); if the entry carries `RewrittenBody`, overwrite the post-frontmatter body; then call `SyncEngine.RouteAndSync` on the file. Surface per-entry errors; never abort the loop on a single failure.
  - For `drop_local`: `git rm` + commit; remove any matching state entry; never call the SiYuan API.
  - For `retire_siyuan`: call `client.RemoveDocByID(SiYuanDocID)`; remove the matching state entry (if any); never autonomously prune unlisted documents.
  - Probe target hpath before any keep write; on collision with a still-extant legacy SiYuan doc, emit a structured collision error unless the plan entry opts into overwrite.
  - Observable: an integration test against a mock SiYuan applies a four-entry plan (two keep, one drop_local, one retire_siyuan) where one keep entry intentionally hits an injected `git mv` failure; the report shows three successes and one structured error, the dropped local file is gone, the retired SiYuan doc received the remove API call, and the remaining keep entry synced successfully.
  - _Requirements: 6.3, 6.4, 6.5, 6.6, 7.5, 10.2, 10.3, 10.4_
  - _Boundary: migrate/apply_
  - _Depends: 2.5, 2.2, 2.1, 3.3_

- [ ] 4. CLI subcommands and AI Skill

- [ ] 4.1 (P) schema subcommand (single source of truth for the Skill)
  - Add a `schema` Cobra subcommand under the existing CLI; `--json` prints the canonical JSON document defined in the design (closed enums, canonical folder map, required keys, contract version).
  - Without `--json`, print a human-readable summary suitable for `--help`-style inspection.
  - Register the subcommand in main alongside the existing sync/download/audit/mcp-server set.
  - Observable: a CLI integration test runs the built binary with `schema --json`, unmarshals the output into a Go struct, and asserts the parsed values match `ontology.AllDomains()` / `AllIntents()` and the router's canonical folder for every domain.
  - _Requirements: 1.2, 1.4, 3.1, 5.1, 5.3_
  - _Boundary: cmd/siyuan-knowledge-sync, ontology/schema, ontology/router_
  - _Depends: 1.1, 2.1_

- [ ] 4.2 migrate apply subcommand
  - Add a `migrate` Cobra subcommand with an `apply` sub-action accepting a positional plan-JSON path.
  - Load + validate the plan, load config (existing path resolution), and call `migrate.Apply`.
  - Print a `MigrationReport` to stderr in the same style as the existing `printSyncReport`.
  - Observable: a CLI integration test writes a small valid plan JSON to a temp file, runs the binary with `migrate apply <path>`, exits zero, and the stderr report lists the per-entry outcomes (created / dropped / retired / errored).
  - _Requirements: 6.1, 10.1_
  - _Boundary: cmd/siyuan-knowledge-sync, migrate/apply_
  - _Depends: 3.4_

- [ ] 4.3 (P) Project-local AI Skill SKILL.md
  - Author `.claude/skills/siyuan-ontology/SKILL.md` with a YAML frontmatter block declaring `name`, `description`, and the triggers the user wants the skill to apply on (notes destined for the wiki; migration loops).
  - Body documents: the call to `siyuan-knowledge-sync schema --json` for enums and folder map (no hardcoded enums in the skill); the required frontmatter shape; the structured `SchemaViolation` format and a deterministic self-correction loop; the migration workflow (survey → per-file cobesy rewrite → diff approval → emit MigrationPlan → `migrate apply`).
  - Encode the preservation invariant (Req 8) as a hard constraint passed to cobesy in the per-file invocation template.
  - Observable: a manual test by a Claude Code session runs `siyuan-knowledge-sync schema --json`, parses it, and produces a sample frontmatter that passes `audit` without any other prompt; a second test writes a deliberately bad value, hits the gate, parses the violation JSON, and self-corrects on the next attempt.
  - _Requirements: 5.1, 5.2, 5.3, 5.4, 6.7, 7.1, 7.2, 7.3, 7.4, 8.3_
  - _Boundary: .claude/skills/siyuan-ontology_
  - _Depends: 4.1_

- [ ] 5. Validation

- [ ] 5.1 Integration tests for sync engine gate + routing
  - Add focused engine tests covering: schema-violating file aborts but the batch completes; valid file at wrong path is routed (single `ontology-route:` commit, state tracker reflects the new path, asset warnings surface); valid file at canonical path produces no extra commit and no `git mv`; `state.Move` collision surfaces as a per-file error rather than a panic.
  - These are container-portable tests (no chmod/mtime fragility); use the existing mock SiYuan harness.
  - Observable: a focused `go test ./internal/sync/ -run TestOntologyGate` run is green with at least four named cases mapping to the requirements listed below.
  - _Requirements: 2.6, 3.2, 3.3, 3.5, 3.6, 4.1, 9.2_
  - _Boundary: sync/engine, ontology, state_
  - _Depends: 3.2, 3.3_

- [ ] 5.2 Integration tests for migrate apply
  - Add tests against the mock SiYuan harness covering: mixed-op plans (keep + drop_local + retire_siyuan) producing the expected per-entry outcomes; per-entry failure isolation (an injected `git mv` failure on one keep does not block the others); `ontology.AddOntology` returning `ErrUnsafeRewrite` from a synthetic regression flips the entry to a structured error; hpath collision on a keep entry surfaces as a structured collision error.
  - Observable: a focused `go test ./internal/migrate/` run is green with the named cases above; each case asserts on `MigrationReport` content, not just exit codes.
  - _Requirements: 6.3, 6.4, 6.5, 6.6, 8.3, 10.2, 10.3, 10.4_
  - _Boundary: migrate/apply, sync/engine_
  - _Depends: 3.4_

- [ ] 5.3 E2E tests against the containerized SiYuan
  - Extend the existing `e2e/` package with cases that exercise the new pipeline against the live SiYuan container: a valid `domain:` + `intent:` file is uploaded and its document carries `custom-domain` and `custom-intent` (verified via `getBlockAttrs`); a routed file is reachable at the new hpath after sync; a single `retire_siyuan` plan entry removes only the targeted SiYuan document and nothing else.
  - Reuse the existing Docker harness; honour the existing skip-if-no-Docker guard.
  - Observable: `go test ./e2e/ -run TestOntology` is green against the running container with the three named cases.
  - _Requirements: 4.1, 4.2, 4.4, 10.2, 10.4_
  - _Boundary: e2e_
  - _Depends: 4.2, 3.4_

## Implementation Notes
<!-- kiro-impl appends cross-cutting learnings here during execution. -->
