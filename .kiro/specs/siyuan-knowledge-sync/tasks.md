# Implementation Plan

- [ ] 1. Foundation and configuration
- [x] 1.1 Initialize Go module, directory structure, and build tooling
  - Set up go.mod with Cobra, go-git v5, goldmark v1, MCP Go SDK v1, and yaml.v3 dependencies
  - Create package directories matching the architecture component layout: cmd/, internal/config, internal/siyuan, internal/git, internal/sync, internal/compliance, internal/tags, internal/toc, internal/state, internal/mcp, internal/types
  - `go build ./...` completes with zero errors and all dependencies resolve
  - _Requirements: 1_

- [x] 1.2 Define shared domain types used across components
  - Document metadata, notebook, sync entry/state, compliance issue, and API envelope types
  - All public types compile and are importable by dependent packages
  - _Requirements: 1, 3_

- [x] 1.3 Load and validate the sync configuration file
  - Parse `.siyuan-sync.yaml` with endpoint, token, repo_path, and autofix fields
  - Reject missing endpoint/token/repo_path with actionable error messages pointing to the config file
  - A valid config file in the working directory loads without errors and produces a validated Config struct
  - _Boundary: Config_
  - _Requirements: 1_

- [ ] 2. Core integration services
- [x] 2.1 (P) SiYuan HTTP client with full API coverage
  - Implement all required API methods: notebooks (list, create, remove), documents (create with markdown, remove by ID, rename, list tree, get IDs by hpath), blocks (update, delete), export markdown content, block attributes (set, get), SQL query
  - Handle the code/msg/data API envelope: map code=0 to success, non-zero codes to descriptive Go errors
  - Include request timeout and auth header injection
  - Each method returns typed results and passes tests against a mock SiYuan HTTP server
  - _Boundary: SiYuanClient_
  - _Requirements: 1, 3, 4, 5, 6, 10_

- [x] 2.2 (P) Git-tracked markdown file scanner
  - Discover all git-tracked `.md` files in the configured repository using go-git
  - Return file paths with modification timestamps; exclude untracked, ignored, and non-markdown files
  - Querying a test git repository returns only committed `.md` files with their correct mod times
  - _Boundary: GitScanner_
  - _Requirements: 2_

- [x] 2.3 (P) Sync state tracker with persistent JSON storage
  - Maintain local-path-to-SiYuan-ID mapping in memory, keyed by local path
  - Persist to `.siyuan-sync-state.json` and reload on startup; handle missing/corrupt state files gracefully
  - Support lookup by local path and by SiYuan ID, plus add/update/remove operations
  - A save/load round-trip preserves all entries with correct timestamps
  - _Boundary: StateTracker_
  - _Requirements: 6_

- [ ] 3. Content processing
- [x] 3.1 (P) Tag extraction from markdown frontmatter and inline syntax
  - Extract tags from YAML frontmatter (tags field) and inline markdown tag patterns
  - Format extracted tags as SiYuan block attributes using the `custom-` prefix requirement
  - Given a markdown file with YAML frontmatter tags, produces the expected `custom-tag` key-value pairs
  - _Boundary: TagExtractor_
  - _Requirements: 8_

- [x] 3.2 (P) Table of contents generation from heading structure
  - Parse document heading hierarchy (H1-H6) and generate a TOC with SiYuan-compatible block reference links
  - Handle empty documents and documents with no headings gracefully (produce empty TOC)
  - A document with three heading levels produces a three-level indented TOC with valid block reference links
  - _Boundary: TOCGenerator_
  - _Requirements: 9_

- [x] 3.3 Compliance audit and auto-fix engine
  - Check markdown files for SiYuan formatting issues: block ID validity, heading nesting rules, attribute syntax, asset reference format
  - Report issues grouped by file with severity (error/warning) and fixability flag
  - Apply auto-fix rules when enabled, modifying only the content with detected issues
  - Integrate TagExtractor and TOCGenerator for tag and TOC compliance checks
  - Running audit on a file with known SiYuan violations produces a detailed issue report; with autofix enabled the violations are corrected in the output
  - _Boundary: ComplianceEngine_
  - _Depends: 3.1, 3.2_
  - _Requirements: 7, 8, 9_

- [ ] 4. Sync operations
- [x] 4.1 Sync engine orchestration and upload workflow
  - Compute the diff between local tracked files, state tracker entries, and SiYuan document state to identify new, modified, and deleted documents
  - For new and modified files: run compliance audit (with autofix when configured), then push markdown content to SiYuan via create-or-update, mapping top-level folders to notebooks and subfolders to document hierarchy
  - Report created, updated, and errored documents after the sync run
  - Running sync on a repo with new `.md` files creates those documents in the correct SiYuan notebook path with preserved folder hierarchy
  - _Boundary: SyncEngine_
  - _Depends: 2.1, 2.2, 2.3, 3.3_
  - _Requirements: 3, 4_

- [x] 4.2 Download sync from SiYuan to local files
  - Fetch all documents from configured SiYuan notebooks via export endpoint and write them as local `.md` files
  - Preserve SiYuan document hierarchy as local folder structure
  - Handle existing-file conflicts with configurable behavior: overwrite, skip, or merge
  - Detect documents created by the SiYuan skill (not previously in state tracker) and create corresponding local files
  - After a successful download, the local `.md` file content matches the SiYuan document content for every notebook
  - _Boundary: SyncEngine_
  - _Depends: 2.1, 2.3_
  - _Requirements: 5, 11_

- [ ] 4.3 Pruning of locally deleted documents within the sync workflow
  - Consume the deleted-files set from the diff computed in 4.1; identify state-tracked documents whose local files no longer exist in the git-tracked set
  - Remove corresponding SiYuan documents via API; detect orphaned child documents (not created by sync) and skip pruning with a dependency conflict report
  - Report all pruned documents and any skipped dependencies after the operation
  - As part of the sync workflow, deleting a previously synced `.md` file and running sync removes the SiYuan document and updates the state file
  - _Boundary: SyncEngine_
  - _Depends: 2.1, 2.2, 2.3, 4.1_
  - _Requirements: 6_

- [ ] 5. Application interfaces
- [x] 5.1 CLI command structure
  - Implement Cobra commands: sync (with `--dry-run` flag), download (with `--conflict overwrite/skip/merge` flag), audit (with `--autofix` flag)
  - Wire command handlers to Config, SyncEngine, and ComplianceEngine
  - Display structured sync/audit reports with file counts and per-document details to stderr
  - Running `siyuan-knowledge-sync audit --autofix` prints a compliance report with fixable issues automatically corrected and unfixable issues listed for manual resolution
  - _Boundary: CLI_
  - _Depends: 1.3, 3.3, 4.1, 4.2, 4.3_
  - _Requirements: 4, 5, 7_

- [x] 5.2 MCP server exposing search, retrieval, and listing tools
  - Register three MCP tools via the official Go SDK: search (keyword/SQL query to matching document IDs with excerpts), retrieve (document ID to full markdown content), list_notebooks (to notebook names with document counts)
  - Use stdio transport for local agent access
  - Return structured JSON responses and handle SiYuan API errors gracefully (server stays alive on tool errors)
  - Starting the MCP server and calling the search tool returns matching document metadata from the connected SiYuan instance
  - _Boundary: MCPServer_
  - _Depends: 2.1, 1.3_
  - _Requirements: 10_

- [ ] 6. Integration and validation
- [x] 6.1 Application entry point and final wiring
  - Create main.go with the root Cobra command and mcp-server subcommand
  - Wire Config loading into persistent flags/pre-run hooks
  - The binary supports all four commands (sync, download, audit, mcp-server) with config loaded from the working directory
  - _Boundary: CLI, MCPServer_
  - _Depends: 5.1, 5.2_
  - _Requirements: 4, 5, 7, 10_

- [x] 6.2 End-to-end integration and validation tests
  - Full sync E2E: create local `.md` files in a test git repo, run sync, verify SiYuan documents created and mapped correctly; modify files, run sync again, verify updates; delete a file, run sync, verify document pruned
  - Download E2E: start with a populated SiYuan notebook, run download, verify local `.md` files match SiYuan content and hierarchy
  - MCP E2E: start MCP server, call search and retrieve tools, verify results match expected SiYuan data
  - Compliance E2E: run audit on files with known violations, verify issues detected; run with autofix, verify fixes applied correctly
  - SiYuan skill integration: create a document via SiYuan skill, run download sync, verify local file created in git-tracked directory
  - All E2E tests pass against a real or containerized SiYuan instance
  - _Depends: 6.1_
  - _Requirements: 3, 4, 5, 6, 7, 8, 9, 10, 11_

- [ ]* 6.3 Performance baseline test for large repositories
  - Sync engine stress test with 1000+ markdown files: verify total sync time is reasonable and per-file errors do not abort the batch
  - MCP search tool response time measurement against a large SiYuan database
  - Performance metrics are logged and within acceptable bounds for the target environment
  - _Depends: 6.1_
  - _Requirements: 4, 10_

- [ ] 7. Cloudflare Access ZTNA and markdown frontmatter fidelity (design Addendum A)

- [x] 7.1 Cloudflare Access service-token configuration and transport
  - Optional Cloudflare Access service-token credentials accepted from the sync configuration alongside the existing endpoint/token settings
  - Configured credentials presented on every SiYuan API request; behavior unchanged when they are absent; updated credentials take effect on the next operation
  - Completed in commit 20644b9; touched-package tests pass and a request against a Cloudflare-Access-protected endpoint succeeds with credentials configured
  - _Requirements: 12.1, 12.2, 12.4_
  - _Boundary: Config, SiYuanClient, CLI_

- [x] 7.2 Cloudflare Access challenge detection and actionable error
  - Detect a Cloudflare Access challenge response (redirect to a Cloudflare Access host, non-JSON content type, or empty forbidden body) before attempting to decode the SiYuan envelope
  - Return a typed, actionable error stating that Cloudflare Access authentication is required, and aborting the operation; non-challenge non-JSON responses return a clearer generic error than the previous opaque parse failure
  - Service-token credential values never appear in returned errors or logs
  - Observable: hitting a Cloudflare-Access-protected endpoint without credentials yields the actionable Cloudflare Access error instead of an "unexpected end of JSON input" message
  - _Requirements: 12.3, 12.5_
  - _Boundary: SiYuanClient_

- [x] 7.3 (P) Frontmatter metadata extraction
  - Extract the document title, the frontmatter-stripped body, and the existing custom- tag attribute map from markdown content in a single pass, reusing the existing frontmatter split and tag parsing
  - The existing tag-extraction entry point used by the compliance audit remains unchanged
  - Observable: given a note with YAML frontmatter, extraction returns the frontmatter title, a body with the frontmatter block removed, and the same custom- tag map the auditor already produces; malformed frontmatter returns an error rather than a partial result
  - _Requirements: 13.1, 13.3, 13.4_
  - _Boundary: TagExtractor_

- [ ] 7.4 Rename-document-by-ID client capability
  - Add a SiYuan client operation that sets a document's title by document ID, envelope-checked like the other client methods
  - Shares the SiYuan client with 7.2; sequence after it to avoid file contention (not parallel-safe with 7.2)
  - Observable: calling the operation issues the rename-by-ID request with the document ID and title and surfaces API errors via the standard error envelope
  - _Requirements: 13.2_
  - _Boundary: SiYuanClient_
  - _Depends: 7.2_

- [ ] 7.5 Frontmatter-aware upload in the sync engine
  - On upload, send the frontmatter-stripped body to SiYuan instead of the raw content
  - After the document exists, set its title from the frontmatter title, falling back to the file name without its extension; apply the extracted tags as block attributes using the existing attribute operation
  - On frontmatter parse failure, record a compliance issue and upload the body without title or tag mapping; title and attribute API failures are recorded per file but do not change the created/updated outcome
  - Observable: syncing a note with frontmatter produces a SiYuan document whose body has no YAML block, whose title matches the frontmatter title, and whose tags appear as custom- block attributes; a parse failure still uploads the body and reports a compliance issue
  - Explicit integration task wiring TagExtractor and SiYuanClient into the upload path
  - _Requirements: 13.1, 13.2, 13.3, 13.4, 13.5_
  - _Boundary: SyncEngine_
  - _Depends: 7.3, 7.4_

- [ ] 7.6 Tests for Cloudflare Access error handling
  - Cover: typed Cloudflare Access error for a redirect to a Cloudflare Access host and for an empty forbidden body without credentials; clearer generic error for other non-JSON responses; error text never contains the configured secret
  - Observable: the test suite for the client passes with these cases asserting the error type and that credentials are absent from error strings
  - _Requirements: 12.3, 12.5_
  - _Boundary: SiYuanClient_
  - _Depends: 7.2_

- [ ] 7.7 Tests for frontmatter fidelity upload
  - Cover: metadata extraction (title present/absent, body stripped, attrs match the auditor, parse failure errors); rename-by-ID request shape; upload path producing stripped body, frontmatter title, fallback filename title, applied tag attributes, parse-failure degradation, and non-fatal title/attribute API errors keeping the file in the created set
  - Observable: the tag-extractor and sync-engine test suites pass with these cases mapped to the acceptance criteria
  - _Requirements: 13.1, 13.2, 13.3, 13.4, 13.5_
  - _Boundary: TagExtractor, SyncEngine_
  - _Depends: 7.3, 7.4, 7.5_

## Implementation Notes
- 7.2: `siyuan.Client.doRequest` now classifies non-JSON / Cloudflare Access responses before envelope decode and returns `*CloudflareAccessError` or a clearer generic error. All client methods (incl. the new `RenameDocByID` in 7.4 and the upload calls in 7.5) inherit this centrally — no per-method CF/non-JSON handling needed. CF challenge detection walks the redirect chain via `r.Request.Response` (Go's client follows redirects by default).
- 7.3: `tags.Extract` and the new `tags.ExtractMeta` now share a single `extract` core (drift-proof). For 7.5, call `ExtractMeta` once and use `Meta.Body` (frontmatter-stripped) for create/update, `Meta.Title` (""=absent → filename-without-ext fallback in the engine), `Meta.Attrs` for `SetBlockAttrs`. Malformed frontmatter → `(Meta{}, err)`; engine must treat that as the 13.5 compliance-issue degradation path.
