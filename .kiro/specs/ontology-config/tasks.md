# Implementation Plan

- [ ] 1. Foundation — convert ontology package state to mutable, seeded from compile-time defaults
- [x] 1.1 Refactor domain and intent state in `internal/ontology/schema.go` to be seeded-then-mutable
  - Introduce package-private `defaultDomains` and `defaultIntents` slices that carry exactly today's hardcoded values (six domains, five intents) in their current canonical order
  - Convert `allDomainsCanonical` and `allIntentsCanonical` from compile-time `var` constants into package-private mutable storage initialised at package init from those defaults
  - Leave the public `AllDomains()` / `AllIntents()` accessor contracts unchanged (still return fresh copies in declared order)
  - Observable completion: every existing test in `internal/ontology` and every downstream consumer's test (audit, sync, migrate, dashboard, schema) passes unchanged because the seeded state matches the prior compile-time values
  - _Requirements: 6.1, 6.2, 7.1_

- [x] 1.2 Refactor canonical folder routing in `internal/ontology/router.go` to be seeded-then-mutable
  - Introduce a package-private `defaultCanonicalFolders` map carrying exactly today's six-domain folder map (`devops → "Linux & DevOps"`, `forensics → "Digital Forensics"`, `security → "Security"`, `ai-ml → "AI & ML"`, `software-dev → "Software Development"`, `quant-finance → "Quant Finance"`)
  - Convert `canonicalFolders` from a fixed `var` map into package-private mutable storage initialised at package init from the default map
  - Keep the `Router.CanonicalFolder()` accessor contract unchanged
  - Observable completion: `Router{}.CanonicalFolder()` returns the same value for every domain id as before the refactor; existing router tests pass without changes
  - _Requirements: 6.1, 6.2, 7.1_

- [ ] 2. Core — Configure entry point and tag-vocabulary state for the ontology package
- [x] 2.1 Add the Configure entry point with full validation and a test-only reset helper
  - Create `internal/ontology/configure.go` exporting `ConfigureDomain`, `ConfigureIntent`, `ConfigureOptions` and `Configure(opts ConfigureOptions) error`
  - Validate domain ids against `^[a-z][a-z0-9-]*$`, reject empty folders, reject folders that start with `_` or `/`, reject duplicate domain ids, reject duplicate canonical folders, validate intent ids against the same charset, reject duplicate intent ids
  - Aggregate every validation failure with `errors.Join` so an operator sees every problem in one run
  - On nil-error path replace the package-level domain slice, intent slice, and canonical folder map atomically (mutate after every check passes); on error path leave package state untouched
  - Preserve input slice order so `AllDomains()` and `AllIntents()` reflect operator order downstream
  - Expose an unexported `resetToDefaultsForTest()` helper that restores the seeded compile-time defaults so tests can isolate state
  - Cover the above with `internal/ontology/configure_test.go`: defaults preserved on no call, rejects duplicate domain id, rejects duplicate domain folder, rejects reserved folder prefix, rejects invalid id charset, preserves input order, is idempotent and resettable
  - Observable completion: `go test ./internal/ontology/...` passes; calling `Configure(opts)` with a fresh non-default `opts` makes `AllDomains()` / `AllIntents()` / `Router{}.CanonicalFolder()` return the supplied values in supplied order
  - _Requirements: 1.3, 1.4, 2.1, 2.2, 2.3, 2.4, 2.5, 3.1, 3.2, 3.3, 7.1, 7.2, 7.3_
  - _Boundary: internal/ontology_
  - _Depends: 1.1, 1.2_

- [x] 2.2 Add the tag-vocabulary state and accessors with nil-vs-empty discrimination
  - Extend `internal/ontology` package state with a private `*[]string` (or equivalent pointer-discriminated storage) for the configured tag vocabulary so `nil` cleanly encodes "open vocabulary" and a non-nil empty slice encodes "closed empty vocabulary"
  - Wire the `Tags` field of `ConfigureOptions` through `Configure(opts)` to that state: nil ⇒ leave open, non-nil ⇒ snapshot to package state, with a duplicate-tag-rejection validation rule
  - Export `AllowedTags() []string` returning a fresh copy (or nil) and `IsKnownTag(tag string) bool` returning true for every value when the vocabulary is open
  - Extend `configure_test.go` with `TestConfigure_TagVocab_NilVsEmpty`, `TestConfigure_RejectsDuplicateTag`, and `TestIsKnownTag_OpenVsClosed`
  - Observable completion: with no `Configure` call, `AllowedTags()` returns nil and `IsKnownTag("anything")` returns true; after `Configure(opts)` with `Tags = []string{"claude", "mcp"}`, `AllowedTags()` returns `["claude","mcp"]` and `IsKnownTag("rust")` returns false
  - _Requirements: 4.1, 4.3, 4.4_
  - _Boundary: internal/ontology_

- [ ] 3. Core — Adjacent boundaries (config decode, compliance check, schema JSON) build out from the new ontology API
- [x] 3.1 (P) Add the `ontology:` YAML decode shape to the config package
  - Extend `internal/config/config.go` with `OntologyConfig`, `OntologyDomain`, `OntologyIntent` decode-target types and an `Ontology *OntologyConfig` field on `Config`
  - Leave `LoadConfig` validation responsibilities unchanged: the loader only decodes the section; validation is `ontology.Configure(opts)`'s job
  - Cover with `internal/config/config_test.go`: a fixture without `ontology:` decodes to `cfg.Ontology == nil`; a fixture with a fully populated `ontology:` section (domains, intents, optional `tags`) decodes to a `*OntologyConfig` whose fields match the YAML literally
  - Observable completion: `go test ./internal/config/...` passes with the new test fixtures; YAML files with no `ontology:` key still decode without error
  - _Requirements: 1.1, 1.2_
  - _Boundary: internal/config_

- [x] 3.2 (P) Add the tag-vocabulary compliance check
  - Extend `internal/compliance/audit.go` with a check that runs only when `ontology.AllowedTags() != nil` and emits one `Category: "schema"`, `Severity: "warning"` issue per file × unrecognized tag
  - The check must never abort the audit; the file proceeds through every other compliance rule, and the warning surfaces alongside any other issues
  - Cover with `internal/compliance/audit_tag_vocab_test.go`: emits one warning per unknown tag on a configured-vocabulary file, emits zero issues when the vocabulary is nil, leaves other compliance categories untouched
  - Observable completion: `go test ./internal/compliance/...` passes with the new tests; a file carrying tags `[a, b]` against vocabulary `[a]` produces exactly one `schema` warning naming `b` while still completing the audit
  - _Requirements: 4.2, 4.3_
  - _Boundary: internal/compliance_
  - _Depends: 2.2_

- [x] 3.3 (P) Extend `schema --json` with an optional `tags.values` field
  - Extend `cmd/siyuan-knowledge-sync/schema.go`'s `schemaDoc` with `Tags *schemaTagsDoc \`json:"tags,omitempty"\`` and add a `schemaTagsDoc` type with a `Values []string \`json:"values"\`` field
  - In `buildSchemaDoc()`, populate `Tags` from `ontology.AllowedTags()` only when the result is non-nil; leave the existing top-level fields unchanged
  - Cover with `cmd/siyuan-knowledge-sync/schema_test.go`: the `tags` field is absent from the JSON when vocabulary is unconfigured, the `tags.values` array matches the configured vocabulary when set, and `domain.values` / `domain.folders` / `intent.values` reflect the result of a preceding `Configure(opts)` call
  - Observable completion: `siyuan-knowledge-sync schema --json` with no `ontology:` section emits byte-identical JSON to the prior release; with a configured vocabulary, the JSON gains a `tags.values` array
  - _Requirements: 5.1, 5.2, 5.3_
  - _Boundary: cmd/siyuan-knowledge-sync_
  - _Depends: 2.2_

- [ ] 4. Integration — wire ontology load into CLI startup
- [x] 4.1 Call `ontology.Configure()` once in `cmd/siyuan-knowledge-sync/main.go` after `LoadConfig`
  - After `LoadConfig` returns and before any subcommand `RunE` runs: if `cfg.Ontology != nil`, build a `ConfigureOptions` by translating the YAML types into the ontology types and call `ontology.Configure(opts)`
  - On non-nil error, print the structured error to stderr and exit non-zero before any SiYuan call, any git mutation, or any state-tracker write
  - When `cfg.Ontology == nil`, do not call `Configure`; the compiled-in defaults stay in effect
  - Cover with `cmd/siyuan-knowledge-sync/main_test.go`: configure is called with the right options when the section is present, an invalid section yields a non-zero exit with no side effects, no configure call happens when the section is absent
  - Observable completion: a config with a valid `ontology:` section makes `schema --json` reflect the operator's values; a config with an invalid section (e.g. duplicate domain id) exits non-zero before any subcommand body runs
  - _Requirements: 1.3, 1.4, 7.1, 7.2, 7.3_
  - _Boundary: cmd/siyuan-knowledge-sync_
  - _Depends: 2.1, 3.1_

- [ ] 5. Validation — end-to-end non-default ontology coverage
- [ ] 5.1 Add a non-default-ontology e2e fixture
  - Extend the e2e harness (in `e2e/`) with a fixture that ships a `.siyuan-sync.yaml` containing an `ontology:` section adding a new `personal` domain mapped to a `Personal` folder, plus the standard defaults
  - The fixture file declares `domain: personal` in its frontmatter and asserts that after `sync` it lands in the `Personal` SiYuan notebook with `custom-domain: personal` set
  - Leave every existing default-config e2e fixture untouched to prove backwards compatibility (Requirement 6.2)
  - Observable completion: the containerized e2e suite passes both the new non-default-ontology case and every pre-existing default-config case
  - _Requirements: 6.2, 7.1_
  - _Boundary: e2e_
  - _Depends: 4.1_

## Implementation Notes

- The ontology package state is mutated exactly once per process invocation by `Configure(opts)` called from `cmd/main.go`; treat that as an invariant and do not introduce other mutation points (tests use `resetToDefaultsForTest()` only).
- The `nil` vs non-nil-empty distinction on the tag vocabulary is load-bearing for Requirement 4.3 (open vs closed vocabulary) and for the `tags,omitempty` JSON output (Requirement 5.3). Preserve it through every layer.
- Existing test fixtures live against the six-domain / five-intent canonical defaults; do not change those defaults' values when refactoring or every downstream fixture will need rewrites.
