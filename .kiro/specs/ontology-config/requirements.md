# Requirements Document

## Introduction

The siyuan-knowledge-sync CLI today carries its closed-enum bi-modal ontology — the `Domain` enum, the `Intent` enum, and the canonical Domain-to-folder map — as compile-time Go constants in `internal/ontology/schema.go` and `internal/ontology/router.go`. Adding a domain, renaming an intent, or pointing a domain at a different SiYuan notebook requires editing the Go source, recompiling, and re-releasing the binary. Different deployments cannot diverge.

This feature moves that schema into a YAML configuration section read at CLI startup. When the configuration is absent the CLI falls back to today's exact hardcoded values, so existing users see no behavior change. Operators who want a different ontology — adding domains, renaming canonical folders, pinning a controlled tag vocabulary — edit the configuration file instead of the source. Every downstream consumer (compliance audit, sync engine, migrate plan validator, dashboard generator, `schema --json` subcommand, AI Skill) reads from the loaded ontology so drift between callers stays impossible.

## Boundary Context

- **In scope**: defining the YAML shape of the ontology section, loading and validating it at CLI startup, falling back to the current hardcoded defaults when absent, propagating the loaded values to every in-process consumer, reflecting the loaded values in `schema --json`, optional controlled tag vocabulary with non-aborting warnings on unrecognized tags.
- **Out of scope**: multi-environment configuration profiles (one config file per invocation is enough), tooling that translates one vocabulary into another, runtime config reload (re-invoking the CLI picks up changes), breaking changes to the top-level shape of `schema --json` (downstream consumers must keep working unchanged).
- **Adjacent expectations**: the AI Skill at `.claude/skills/siyuan-ontology/SKILL.md` continues to read enums and the folder map exclusively from `schema --json` and requires no edits when an operator renames a domain or adds an intent through the config file.

## Requirements

### Requirement 1: Ontology configuration loading

**Objective:** As a deployment operator, I want to define my ontology in the existing configuration file, so that I can customize domains, intents, folders, and tag vocabulary without rebuilding the CLI.

#### Acceptance Criteria

1. When the siyuan-knowledge-sync CLI loads its configuration file, the siyuan-knowledge-sync CLI shall accept an optional `ontology:` section that may declare a domain list, an intent list, and an optional controlled tag vocabulary.
2. When the loaded configuration file contains no `ontology:` section, the siyuan-knowledge-sync CLI shall use the compiled-in default schema (the six domains, five intents, and canonical folder map shipped with the release).
3. If the `ontology:` section is present but fails validation, the siyuan-knowledge-sync CLI shall refuse to start with a structured error naming the failing field, and shall not mutate the working tree, the SiYuan server, or the local state tracker.
4. The siyuan-knowledge-sync CLI shall load the effective ontology exactly once per process invocation; subsequent commands in the same invocation see the same values.

### Requirement 2: Domain configuration shape

**Objective:** As a deployment operator, I want each domain entry to declare both its identifier and its canonical folder in one place, so that renaming a notebook or re-routing a domain is a single edit.

#### Acceptance Criteria

1. The siyuan-knowledge-sync CLI shall accept each configured domain entry as a pair of an identifier and a folder name where the identifier is a non-empty lowercase string of letters, digits, and `-` only, and the folder is a non-empty string with no leading `/` and no leading `_`.
2. If two configured domain entries declare the same identifier, the siyuan-knowledge-sync CLI shall refuse to start with a duplicate-identifier error naming both entries.
3. If two configured domain entries declare the same folder, the siyuan-knowledge-sync CLI shall refuse to start with a duplicate-folder error naming both entries.
4. If any configured domain folder starts with `_` or `/`, the siyuan-knowledge-sync CLI shall refuse to start with a reserved-prefix error (the engine reserves the `_` prefix for index docs and routes the canonical folder relative to the repository root).
5. The siyuan-knowledge-sync CLI shall preserve the order in which domains appear in the configuration when listing them in user-facing outputs (CLI text, JSON schema, AI Skill enumerations).

### Requirement 3: Intent configuration shape

**Objective:** As a deployment operator, I want intents to be a simple ordered list, so that adding a new intent verb or reordering them takes one line per change.

#### Acceptance Criteria

1. The siyuan-knowledge-sync CLI shall accept each configured intent entry as an identifier that is a non-empty lowercase string of letters, digits, and `-` only.
2. If two configured intent entries declare the same identifier, the siyuan-knowledge-sync CLI shall refuse to start with a duplicate-identifier error naming both entries.
3. The siyuan-knowledge-sync CLI shall preserve the order in which intents appear in the configuration when listing them in user-facing outputs (CLI text, JSON schema, AI Skill enumerations, generated index documents).

### Requirement 4: Optional controlled tag vocabulary

**Objective:** As a deployment operator, I want the option to declare a controlled tag vocabulary, so that my wiki's tag taxonomy stays stable as contributors are added.

#### Acceptance Criteria

1. Where the loaded `ontology:` section includes a `tags:` list, the siyuan-knowledge-sync CLI shall treat that list as the authoritative controlled tag vocabulary for downstream gates.
2. While a controlled tag vocabulary is configured, when the audit or sync flow encounters a file whose frontmatter or inline tags include a value outside the configured set, the siyuan-knowledge-sync CLI shall surface a per-file structured warning identifying the file and the unrecognized tag and shall not abort the file's sync.
3. Where the loaded `ontology:` section omits the `tags:` list, the siyuan-knowledge-sync CLI shall accept any tag value as today (the existing open-vocabulary behavior).
4. If two configured tag entries declare the same identifier, the siyuan-knowledge-sync CLI shall refuse to start with a duplicate-tag error naming both entries.

### Requirement 5: Schema introspection through `schema --json`

**Objective:** As an AI Skill or external consumer, I want `schema --json` to reflect the loaded ontology in real time, so that one source of truth drives every downstream reader.

#### Acceptance Criteria

1. When invoked, the siyuan-knowledge-sync CLI's `schema --json` subcommand shall emit a JSON document whose `domain.values`, `domain.folders`, and `intent.values` correspond exactly to the effective ontology — the values loaded from the configuration file if present, otherwise the compiled-in defaults.
2. Where a controlled tag vocabulary is configured, the siyuan-knowledge-sync CLI's `schema --json` subcommand shall include the configured vocabulary as a `tags.values` array; where no vocabulary is configured the `tags` field shall be omitted or empty so consumers can distinguish "unconfigured" from "empty vocabulary".
3. The siyuan-knowledge-sync CLI shall preserve every existing top-level field name, key shape, and ordering convention in the `schema --json` output, so the AI Skill and any external consumer that reads the existing fields keeps working without changes.

### Requirement 6: Backwards-compatible default behavior

**Objective:** As an existing user with no `ontology:` section in my configuration, I want zero behavior change after upgrading, so that picking up the new CLI version is risk-free.

#### Acceptance Criteria

1. While the loaded configuration file lacks an `ontology:` section, the siyuan-knowledge-sync CLI shall behave identically to the version that hardcoded the schema: the same six domains, the same five intents, the same canonical folder map, the same validation error messages.
2. The siyuan-knowledge-sync CLI's existing automated test surface (unit, integration, and containerized end-to-end) shall continue to pass against the default-resolution path; no fixture rewrites unrelated to exercising the new configurable behavior are required.

### Requirement 7: Single effective ontology across consumers

**Objective:** As a maintainer, I want every in-process consumer to read the same effective ontology, so that drift between callers is impossible by construction.

#### Acceptance Criteria

1. The siyuan-knowledge-sync CLI shall surface a single effective ontology (domains, intents, folder map, optional tag vocabulary) that the compliance audit, sync engine, migrate plan validator, dashboard generator, and CLI subcommands all read.
2. When two consumers query the effective ontology within the same process invocation, the siyuan-knowledge-sync CLI shall return the same values to both.
3. The siyuan-knowledge-sync CLI shall not consult the configuration file again after the initial load within the same process invocation.
