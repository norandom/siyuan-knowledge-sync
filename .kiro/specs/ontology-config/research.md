# Research Log — ontology-config

## Discovery scope
Light discovery: this is an extension of an existing closed-enum schema layer (`internal/ontology`) that already has every downstream consumer pointed at three accessors (`AllDomains`, `AllIntents`, `Router.CanonicalFolder`). The work is contained to that package, the `internal/config` loader, and one CLI wiring point. No external dependencies to evaluate; no new protocols.

## Existing patterns surveyed

- **`internal/config/config.go`** already defines `Config` as a YAML-tagged Go struct loaded via `gopkg.in/yaml.v3`. Extending it with an `Ontology *OntologyConfig` field is the established pattern (parallel to how `CFAccessClientID` was added in the Cloudflare Access work).
- **`internal/ontology/schema.go`** keeps its enum canonical order in package-level slices (`allDomainsCanonical`, `allIntentsCanonical`) and exposes copies via `AllDomains()` / `AllIntents()`. Every consumer already pays the cost of indirection through these accessors, so converting the underlying slices from immutable initial state to "loaded-once-at-startup state" requires no consumer changes.
- **`internal/ontology/router.go`** stores `canonicalFolders` as a package-level `map[Domain]string` consumed via `Router.CanonicalFolder()`. Same pattern: read-after-init through one accessor.
- **`cmd/siyuan-knowledge-sync/schema.go::buildSchemaDoc()`** is the single point that walks the ontology and emits the JSON shape consumed by the AI Skill and any external scripts. It already calls the same three accessors, so its output will reflect loaded values automatically once the package-level state is initialized from config.
- **`internal/compliance/audit.go`** already classifies issues with a `Category` field and has a `"schema"` category for ontology-gate violations. Re-using that category for the new "unrecognized tag" warning is consistent with the existing taxonomy.

## Build vs. adopt decisions

| Concern | Choice | Rationale |
|---|---|---|
| YAML parsing | Re-use existing `gopkg.in/yaml.v3` (already a dep) | No reason to introduce a second YAML library; the existing one is fine. |
| Schema initialization model | Package-level state mutated once by `Configure(opts)` at startup | Preserves the package-level API every consumer already uses. Switching to dependency-injected `Schema` struct would touch every caller in the codebase for no observable benefit. |
| Tag-vocabulary warning surface | Existing `Category: "schema"` issue class in `internal/compliance` | Re-uses the existing schema-violation taxonomy rather than inventing a third category. |
| Default fallback strategy | Compile-time constants initialize the package state at load; `Configure(opts)` only fires when CLI's main passes a non-nil `Ontology` section from the loaded config | Zero-config users hit zero code path changes — strongest possible backwards-compat guarantee. |
| Multi-config / runtime reload | Explicitly out of scope per Requirement 1.4 ("loaded exactly once per process invocation") | Adds significant complexity for no current user need. |

## Synthesis outcomes

- The refactor is small in surface area but visible across many test fixtures because every fixture has been written against the literal six-domain / five-intent default set. Default-resolution path keeps all existing fixtures green; new tests target the configure-and-validate path explicitly.
- `Configure()` will be idempotent + replaceable so tests can reset state between cases. A `resetToDefaultsForTest()` helper inside the ontology package keeps test isolation cheap.
- The boundary stays inside `internal/ontology` + `internal/config` + `cmd/siyuan-knowledge-sync/main.go` + one new compliance check. No SiYuan-side API change, no migrate.Apply change, no SKILL.md edit.

## Risks

- **Concurrent-access risk** is low: the CLI is a fork-on-invoke binary that calls `Configure()` once in `main()` before any goroutine touches the ontology package. Documented invariant rather than enforced via mutex.
- **Vocabulary creep**: leaving the tag-vocabulary check as a warning (not an error) is deliberate per Requirement 4.2 — operators stay in control of how strict their wiki becomes.
- **Test-suite drift**: a few existing tests assert the literal six-domain canonical set (e.g., the AllDomains drift guard in `internal/ontology/schema_test.go`). Those tests stay correct because the default path keeps the same six values; they don't need rewriting.
