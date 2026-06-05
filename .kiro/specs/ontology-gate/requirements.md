# Requirements Document

## Project Description (Input)

**Feature:** ontology-gate — Bi-modal ontology + CLI validation gate + AI Skill for `siyuan-knowledge-sync`, plus a folder-by-folder content migration from two legacy sources.

### Problem
The current SiYuan wiki content is being retired. Two source trees of legacy notes need to be triaged, ontology-tagged, deterministically placed in a canonical folder structure, and pushed to SiYuan as the new authoritative content. Agents writing notes need a hardcoded schema they cannot deviate from, and a CLI that refuses non-conforming writes.

### Two orthogonal taxonomy axes (closed enums, hardcoded in the CLI)

**Axis 1 — Domain (the noun, drives physical placement):**
- `devops` → `./wiki/Automation (DevOps)/`
- `quant-finance` → `./wiki/Retail Trading/`
- `forensics` → `./wiki/Digital Forensics/`

The domain set may need to grow to accommodate existing source content under `Hosting/`, `Learn Machine Learn!/`, `Security Testing/`, `Financial Models/`, `Start/`. **Resolve the final domain enum in requirements discovery.**

**Axis 2 — Intent (the verb, drives agent semantic search; hardcoded enum, no others):**
- `config` — defines state; agents use this to generate setups
- `sop` — standard operating procedure; agents follow these steps
- `log` — historical record; agents use this for root-cause analysis
- `decision` — architecture decision record; agents read this for constraints
- `concept` — theory/definitions; agents use this for terminology

### CLI gatekeeper (extends ComplianceEngine in `siyuan-knowledge-sync`)
On every sync/audit:
1. Parse YAML frontmatter; reject files missing required `domain:` and `intent:` keys.
2. Validate values against the hardcoded enums; reject anything outside (`intent: braindump` → rejected; `domain: unknown` → rejected). Return a structured schema-violation error so the agent can self-correct.
3. **Routing semantics: frontmatter wins.** A static domain→folder map is the source of truth for physical placement. When the file's current local path does not match its declared domain's canonical folder, the CLI moves it (with `git mv`), commits the rename, then syncs. Humans get deterministic placement; agents cannot misroute.
4. Validated `domain:` and `intent:` are pushed as `custom-domain` / `custom-intent` SiYuan block attributes via the already-wired `SetBlockAttrs` path (Req 13.4 of `siyuan-knowledge-sync`), so the MCP server can query them via SiYuan SQL for agent semantic search.

### AI Skill
A `SKILL.md` targeted at agents using the sync tool: encodes the two enums, the domain→folder map, the required frontmatter shape, and the rejection-correction loop. The skill is invoked whenever an agent authors a note destined for the wiki; it primes the agent to produce ontology-conformant frontmatter on the first try. (Decide spec-side whether the skill also has a SiYuan-internal artifact.)

### Migration (folder-by-folder, interactive)
Two source trees:
- `/Users/mc/Source/wiki` — legacy local, outdated, needs curation
- `/Users/mc/Source/siyuan-wiki` — snapshot of current SiYuan content; mostly to retire, but worth a triage pass so nothing important is dropped silently

Per-folder workflow:
1. Pick one source folder.
2. Survey its files; propose a `(keep/drop, domain, intent, target-path, content-fixes)` plan per file.
3. User approves / edits the plan.
4. **Content correction & rewrite via the `cobesy` skill** (`/Users/mc/.claude/skills/cobesy/`, the COBESY Cognitive Behavioral Systemic composition path): each kept file is rewritten through cobesy's composition blueprint to fix structure, framing, and sentence-level issues while the ontology is applied. Migration is not just retag-and-move; it is a content patch + organize pass.
5. CLI rewrites frontmatter (adds `domain:` / `intent:`), `git mv` to canonical path, commits, syncs to SiYuan.
6. Repeat next folder.

Pictures/assets live separately and are out of this spec's body work, but must not be silently broken by file moves; flag asset references during the per-folder triage.

### Preservation invariants
- **Original timestamps must be preserved verbatim.** Existing frontmatter `date:` / `lastmod:` (or any temporal field carried from the legacy sources or the SiYuan export) refer to *when the original task was done* and MUST NOT be overwritten with clock-current values during migration. The CLI/skill ADDS `domain:` / `intent:` (and any other new ontology fields) without mutating preserved temporal metadata.
- File mtimes touched by `git mv` / rewrites are operational artifacts and are separate from these frontmatter dates.

### Out of scope (this spec)
- New SiYuan API endpoints; the existing client + Req 13 wiring is reused.
- Free-text taxonomy / SHACL / OWL. The enums are flat strings, hardcoded in Go.
- Reorganizing the original/existing untouched parts of `siyuan-knowledge-sync` (Req 1–13 stay as-is).

### Language
`en` (matches the existing `siyuan-knowledge-sync` spec).

## Boundary Context
- **In scope**: The closed-enum frontmatter ontology (`domain:` + `intent:`); a CLI validation gate that extends the existing `ComplianceEngine` and refuses non-conforming writes with a structured error; deterministic frontmatter-driven auto-routing (`git mv` + commit, then sync); mapping validated ontology values to SiYuan `custom-domain` / `custom-intent` block attributes; an AI Skill (`SKILL.md`) that primes agents to author conformant notes; folder-by-folder interactive migration from `/Users/mc/Source/wiki` and `/Users/mc/Source/siyuan-wiki`, with per-file content correction/rewrite via the local `cobesy` composition skill; preservation of original frontmatter dates; flagging (not migrating) asset references; retirement of legacy SiYuan content under per-file approval.
- **Out of scope**: New SiYuan API endpoints (the existing client + Req 13 wiring is reused); free-text taxonomies / SHACL / OWL; reorganizing the original `siyuan-knowledge-sync` Requirements 1–13; autonomous deletion of legacy SiYuan documents; physical migration of pictures/attachments; a SiYuan-internal slash-command/template artifact (deferred unless explicitly reopened).
- **Adjacent expectations**: Relies on `siyuan-knowledge-sync` Req 13.4 (`SetBlockAttrs` upload wiring), Req 13.1 (frontmatter stripping on upload), Req 7 (compliance audit pipeline), and the existing CLI structure. Assumes a local git working tree for `git mv` + commit semantics. Reuses the local `cobesy` skill at `/Users/mc/.claude/skills/cobesy/` for the composition rewrite path.

## Requirements

### Requirement 1: Closed-Enum Frontmatter Ontology
**Objective:** As a knowledge curator, I want every wiki-destined markdown file to declare exactly one Domain and one Intent from a closed, hardcoded enum, so that physical placement and semantic search are both deterministic.

#### Acceptance Criteria
1. The SiYuan Knowledge Sync shall recognize `domain:` and `intent:` as required top-level YAML frontmatter keys for every file destined for the wiki.
2. The SiYuan Knowledge Sync shall accept exactly these values for `domain:` and shall reject any other value: `devops`, `forensics`, `security`, `ai-ml`, `software-dev`, `quant-finance`.
3. Where the `quant-finance` domain is selected, the SiYuan Knowledge Sync shall treat it as a reserved but initially empty category (no legacy content is mapped to it; the canonical folder exists and accepts future content).
4. The SiYuan Knowledge Sync shall accept exactly these values for `intent:` and shall reject any other value: `config`, `sop`, `log`, `decision`, `concept`.
5. The SiYuan Knowledge Sync shall preserve all other frontmatter keys (including `title`, `date`, `lastmod`, `tags`, and any user-defined keys) verbatim when applying the ontology.
6. If a file declares more than one value for `domain:` or `intent:` (for example, a YAML list), then the SiYuan Knowledge Sync shall reject the file with a schema violation naming the offending key.

### Requirement 2: CLI Validation Gate
**Objective:** As an agent or human author, I want the CLI to refuse non-conforming writes with a structured, actionable error, so that I (or the agent) can self-correct before content reaches SiYuan.

#### Acceptance Criteria
1. When the audit or sync command runs over a file, the SiYuan Knowledge Sync shall parse the YAML frontmatter and validate it against the closed-enum schema from Requirement 1 before issuing any SiYuan write for that file.
2. If the required `domain:` key is missing, then the SiYuan Knowledge Sync shall report a schema violation naming the missing key and abort the write for that file.
3. If the required `intent:` key is missing, then the SiYuan Knowledge Sync shall report a schema violation naming the missing key and abort the write for that file.
4. If a `domain:` or `intent:` value falls outside the closed enum, then the SiYuan Knowledge Sync shall report a schema violation containing the file path, the offending key, the offending value, and the allowed values, and abort the write for that file.
5. The SiYuan Knowledge Sync shall emit schema violations in a structured form (file path, key, offending value, expected enum) suitable for both human reading and agent self-correction.
6. When schema violations occur during a batch sync, the SiYuan Knowledge Sync shall continue processing remaining files; one file's violation shall not abort the batch.
7. While auto-fix is disabled, the SiYuan Knowledge Sync shall never silently insert or repair `domain:` / `intent:` values on the author's behalf.

### Requirement 3: Deterministic Auto-Routing (Frontmatter Wins)
**Objective:** As a human navigator, I want each file to live in exactly the folder its `domain:` claims, so that I can find any note by walking the tree without consulting metadata.

#### Acceptance Criteria
1. The SiYuan Knowledge Sync shall maintain a documented, hardcoded `domain → canonical-folder` map covering every value of the `domain:` enum from Requirement 1.2.
2. When a file's local path does not match its declared domain's canonical folder, the SiYuan Knowledge Sync shall move the file into the canonical folder using `git mv`, preserving the filename, before syncing.
3. When the SiYuan Knowledge Sync performs the move in Requirement 3.2, it shall create a git commit recording the rename so the move is auditable.
4. When asset (image/attachment) references inside a moved file would be invalidated by the move, the SiYuan Knowledge Sync shall report a warning listing each affected reference; the move shall still proceed.
5. The SiYuan Knowledge Sync shall not move any file whose frontmatter has failed schema validation (a schema-violating file is never silently routed).
6. While the file's local path already matches the canonical folder for its declared domain, the SiYuan Knowledge Sync shall make no `git mv` and shall create no rename commit.

### Requirement 4: Ontology as SiYuan Block Attributes
**Objective:** As an agent doing semantic search, I want validated Domain and Intent stored on the SiYuan document as queryable block attributes, so that I can filter by intent or domain via SiYuan SQL.

#### Acceptance Criteria
1. When a file with valid `domain:` and `intent:` is synced to SiYuan, the SiYuan Knowledge Sync shall apply `custom-domain` and `custom-intent` block attributes to the synced document.
2. When a file's `domain:` or `intent:` changes between syncs, the SiYuan Knowledge Sync shall update the corresponding `custom-domain` / `custom-intent` block attribute on the existing SiYuan document.
3. If applying the block attributes fails, then the SiYuan Knowledge Sync shall record a per-file error and shall treat the failure as non-fatal, consistent with `siyuan-knowledge-sync` Requirement 13.4's non-fatal title/attribute policy; the file still counts as synced.
4. The MCP server shall expose `custom-domain` and `custom-intent` such that an agent's SQL query can filter and rank SiYuan documents by those attributes.

### Requirement 5: AI Agent Skill for Ontology-Conformant Authoring
**Objective:** As an agent authoring a wiki-destined note, I want a skill that teaches the schema, the canonical-folder map, and the rejection-correction loop before I write, so that I produce conformant frontmatter on the first try.

#### Acceptance Criteria
1. The SiYuan Knowledge Sync shall provide a discoverable AI Skill artifact (`SKILL.md`) that documents the closed `domain:` and `intent:` enums, the canonical-folder map, the required frontmatter shape, and the structured error format from Requirement 2.5.
2. The AI Skill shall instruct the agent to consult the schema before authoring and to self-correct upon receiving a structured schema violation from the CLI gate.
3. When an agent invokes the AI Skill, the SiYuan Knowledge Sync shall surface enough context (current enum values, current map) for the agent to author a passing file without further repository inspection.
4. Where a future SiYuan-internal artifact (slash command, in-app template) is desired, the SiYuan Knowledge Sync shall treat the AI Skill as the source of truth so any in-app artifact derives from it; the in-app artifact itself is out of scope for this spec.

### Requirement 6: Folder-by-Folder Interactive Migration
**Objective:** As the knowledge owner, I want to migrate legacy notes from the two source trees one folder at a time with explicit per-file approval, so that nothing important is dropped silently and outdated content gets corrected in flight.

#### Acceptance Criteria
1. The SiYuan Knowledge Sync shall support migration from the two source trees `/Users/mc/Source/wiki` and `/Users/mc/Source/siyuan-wiki`.
2. When a migration session begins on a chosen source folder, the SiYuan Knowledge Sync shall survey the folder's files and shall produce a per-file plan containing: a keep-or-drop decision, a proposed `domain:`, a proposed `intent:`, a proposed canonical target path, and any content-fix notes.
3. The SiYuan Knowledge Sync shall require explicit user approval (per file or for the whole plan) before mutating files.
4. When the user approves a per-file plan, the SiYuan Knowledge Sync shall apply the plan in this order: rewrite frontmatter (add ontology fields), run the cobesy content rewrite from Requirement 7, `git mv` to the canonical path, commit, sync to SiYuan.
5. When the user marks a file as drop, the SiYuan Knowledge Sync shall remove the file from the wiki tree (with a git commit) but shall not autonomously delete the corresponding document from the live SiYuan instance (see Requirement 10).
6. The SiYuan Knowledge Sync shall complete the current folder's approved plan before advancing to the next folder.
7. Where a legacy folder corresponds to a domain that does not exist in the closed enum (for example, the `Financial Models` legacy folder under the dropped retail-trading concept), the SiYuan Knowledge Sync shall require an explicit per-file domain reassignment or drop decision; it shall not silently invent a domain.

### Requirement 7: Content Correction & Rewrite via COBESY
**Objective:** As the knowledge owner, I want each kept file rewritten for structure, framing, and sentence-level clarity using the cobesy composition skill, so that the migrated wiki is more adoptable than the legacy source.

#### Acceptance Criteria
1. When a file is approved for keep in Requirement 6, the SiYuan Knowledge Sync shall pass that file's content through the `cobesy` composition path before committing the migrated version.
2. The cobesy rewrite shall preserve the file's factual claims and the file's temporal frontmatter as required by Requirement 8.
3. The SiYuan Knowledge Sync shall present the rewritten content to the user as a diff against the original and shall require explicit approval before committing it.
4. If the user rejects the rewrite, then the SiYuan Knowledge Sync shall keep the original content for that file and shall still proceed with the ontology and routing steps for it.
5. Where a source file already meets the cobesy quality bar (the composition path returns no material changes), the SiYuan Knowledge Sync shall record the rewrite step as a no-op so that no spurious content churn is committed.

### Requirement 8: Preservation of Original Temporal Frontmatter
**Objective:** As the knowledge owner, I want every original frontmatter date carried through migration verbatim, because those dates record when the original work was done.

#### Acceptance Criteria
1. The SiYuan Knowledge Sync shall carry every existing frontmatter `date:` and `lastmod:` value from a source file to the migrated file unchanged.
2. The SiYuan Knowledge Sync shall preserve any other date-like field present in the source frontmatter (for example `created`, `updated`, `original_date`) unchanged.
3. If the cobesy rewrite or any other migration step proposes a value for a preserved temporal field that differs from the source, then the SiYuan Knowledge Sync shall reject the proposed change and shall surface the conflict for human review.
4. The SiYuan Knowledge Sync shall not insert a clock-current timestamp into any preserved temporal frontmatter field.
5. Where a source file has no temporal frontmatter at all, the SiYuan Knowledge Sync shall leave the migrated file without one (it shall not synthesize a date).

### Requirement 9: Asset Reference Safety
**Objective:** As the knowledge owner, I want migrations to flag any image/attachment reference that the move would break, so I can decide whether to repair the reference or accept the break.

#### Acceptance Criteria
1. When the SiYuan Knowledge Sync moves a file under Requirement 3.2 or Requirement 6.4, it shall scan the file for asset references (image links and attachment links).
2. If a relative asset reference would resolve to a different or non-existent target after the move, then the SiYuan Knowledge Sync shall report the affected reference with its original path, its new resolved path, and whether the target exists at that new path.
3. The SiYuan Knowledge Sync shall not autonomously rewrite or relocate the asset files themselves.
4. The SiYuan Knowledge Sync shall continue the migration after emitting asset warnings; broken asset references shall not block the move, the commit, or the sync.

### Requirement 10: Retirement of Existing SiYuan Content
**Objective:** As the knowledge owner, I want the new authoritative wiki to replace the legacy SiYuan content under per-file approval, so that nothing important is wiped accidentally.

#### Acceptance Criteria
1. The SiYuan Knowledge Sync shall be able to operate against a SiYuan instance that already contains legacy documents without overwriting them by surprise.
2. When the migration triage marks a legacy SiYuan document as drop, the SiYuan Knowledge Sync shall queue the corresponding SiYuan document for deletion and shall require explicit user approval before issuing the SiYuan removal.
3. The SiYuan Knowledge Sync shall not autonomously prune any legacy SiYuan document.
4. Where a legacy SiYuan document occupies the same hpath as a newly migrated file, the SiYuan Knowledge Sync shall report the hpath collision to the user and shall require an explicit overwrite or rename decision before any write to that hpath.
