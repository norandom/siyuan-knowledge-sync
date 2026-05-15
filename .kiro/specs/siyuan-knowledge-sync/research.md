# Research & Design Decisions

## Summary
- **Feature**: siyuan-knowledge-sync
- **Discovery Scope**: New Feature (greenfield)
- **Key Findings**:
  - Official MCP Go SDK (v1.6.0) is stable, typed, and maintained by Google — preferred over community `mark3labs/mcp-go` (v0.x)
  - go-git v5 provides pure-Go git repository access without requiring git binary — supports tracked file listing and change detection
  - No existing SiYuan Go SDK exists; must build thin HTTP client (~10 endpoints) using stdlib `net/http`
  - SiYuan API uses auto-generated block IDs — sync client cannot control document IDs, must track ID mapping in state file

## Research Log

### MCP Server Libraries for Go
- **Sources Consulted**: pkg.go.dev, github.com/modelcontextprotocol/go-sdk, github.com/mark3labs/mcp-go
- **Findings**:
  - Official SDK (`modelcontextprotocol/go-sdk` v1.6.0): typed tool handlers with `jsonschema` tags, Apache-2.0, 4.5k stars
  - Community SDK (`mark3labs/mcp-go` v0.54.0): more features (sessions, middleware, OAuth) but v0.x API instability risk
  - Official SDK uses `server.Run(ctx, &mcp.StdioTransport{})` for local tools; SSE/HTTP transports available
  - Go SDK init example: `gopls v0.21.1` was installed using this SDK — confirms real-world usage in tooling
- **Implications**: Use official SDK for stability and typed tool contracts

### Git File Tracking in Go
- **Sources Consulted**: github.com/go-git/go-git, pkg.go.dev
- **Findings**:
  - go-git v5.19.0 provides `tree.Files().ForEach()` for listing tracked files and `worktree.Status()` for change detection
  - Pure Go implementation — cross-platform, no git binary dependency
  - `os/exec` alternative is simpler for one-shot checks but requires git on PATH
  - go-git preferred: portable, eliminates runtime dependency on git binary
- **Implications**: Use go-git for tracked file discovery and change detection

### SiYuan API for Sync Operations
- **Sources Consulted**: docs/API.md, github.com/siyuan-note/siyuan
- **Findings**:
  - `createDocWithMd` is idempotent by path — safe for repeated calls
  - Block IDs are auto-generated server-side (timestamp + random suffix) — cannot pre-assign
  - Documents have three path concepts: hpath (human), storage path (filesystem), block ID (identifier)
  - `getIDsByHPath` + `getHPathByID` translate between hpaths and IDs
  - `exportMdContent` returns full markdown content by document ID
  - SQL endpoint (`/api/query/sql`) allows direct database queries for search
  - Attributes API (`setBlockAttrs`) requires `custom-` prefix for user attributes
  - No known rate limits; export endpoints can be slow for large documents
  - No existing Go SDK — must build HTTP client from scratch
- **Implications**: State file must track SiYuan block IDs. Search via SQL queries. Tag attributes use `custom-` prefix.

## Architecture Pattern Evaluation

| Option | Description | Strengths | Risks / Limitations | Notes |
|--------|-------------|-----------|---------------------|-------|
| Layered CLI | Config → Client → Engine → CLI/MCP | Simple, easy to test, clear dependency direction | Less flexible for future extensions | Chosen: fits scope, Go conventions |
| Hexagonal | Ports & adapters around core domain | Testable core, swappable adapters | Overkill for ~10 API endpoint client | Rejected: adds unnecessary indirection |
| Plugin-based | Sync as engine with pluggable backends | Supports future platform targets | Premature generalization | Rejected: no current need for multi-platform |

## Design Decisions

### Decision: Go over Python
- **Context**: User selected Go for static binaries, compact deployment
- **Alternatives Considered**: Python (had venv set up, removed), Nim (considered, syntax similar to Python but less ecosystem)
- **Selected Approach**: Go with stdlib HTTP, go-git, official MCP SDK
- **Rationale**: Single static binary, no runtime dependency, strong typing, excellent MCP and git library support
- **Trade-offs**: More verbose than Python for HTTP client code; SiYuan API contract is simple enough to offset

### Decision: JSON state file over SQLite
- **Context**: Need to persist local-path-to-SiYuan-ID mapping
- **Alternatives Considered**: SQLite (embedded DB), BoltDB (embedded KV store)
- **Selected Approach**: JSON file (`.siyuan-sync-state.json`)
- **Rationale**: Single-file state, human-readable, easy to debug and recover, zero dependencies
- **Trade-offs**: Not suitable for concurrent access (acceptable for single-process CLI); grows linearly with document count (typical knowledge bases have <10k documents)

### Decision: stdio MCP transport
- **Context**: MCP server needs to communicate with AI agents
- **Alternatives Considered**: SSE/HTTP transport (remote agents)
- **Selected Approach**: stdio transport (local agents only)
- **Rationale**: Simpler security model (no network exposure), sufficient for local agent use case, official SDK supports both transports if needed later
- **Trade-offs**: Cannot serve remote agents; can add HTTP transport later via SDK support

### Decision: SiYuan Skill as separate artifact
- **Context**: User wants a SiYuan skill to create notes in-app
- **Alternatives Considered**: Embed SiYuan plugin code in Go binary, generate plugin dynamically
- **Selected Approach**: Document the SiYuan skill as a separate artifact (SiYuan template + slash command config), not built in Go
- **Rationale**: SiYuan skills are SiYuan-internal configurations (JSON templates, slash commands), not Go code. Sync engine handles detection of skill-created documents via download mode
- **Trade-offs**: Two artifacts to distribute (CLI binary + SiYuan skill config); simpler than embedding SiYuan-specific code in the Go binary

## Risks & Mitigations
- SiYuan API changes could break sync — mitigate by pinning tested SiYuan version in docs, using standard API endpoints
- Large repositories (>10k .md files) may be slow — mitigate with progress reporting and per-file error isolation
- State file corruption could lose sync mappings — mitigate with human-readable JSON format for manual recovery, rebuild via download
- go-git performance on very large git repos — mitigate by testing with realistic repo sizes; fallback to `os/exec git ls-files` if needed

## References
- [SiYuan API Reference](https://github.com/siyuan-note/siyuan/blob/master/API.md) — official API documentation
- [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk) — official MCP implementation in Go
- [go-git](https://github.com/go-git/go-git) — pure Go git implementation
- [goldmark](https://github.com/yuin/goldmark) — CommonMark-compliant markdown parser for Go
- [Cobra](https://github.com/spf13/cobra) — Go CLI library

---

## Addendum: Requirements 12–13 Discovery & Decisions

### Summary
- **Discovery Scope**: Extension (light, integration-focused)
- **Key Findings**:
  - The SiYuan endpoint sits behind Cloudflare Access (Zero Trust); unauthenticated API POSTs 302-redirect to the team's `<team>.cloudflareaccess.com` login host. Access requires a service-token pair (`CF-Access-Client-Id`/`CF-Access-Client-Secret`). The pre-fix client returned the opaque error `parse response: unexpected end of JSON input` for this case.
  - Reusable in-repo building blocks already exist but were unused: `tags.splitFrontmatter`/`parseFrontmatterTags`/`extractInlineTags`, `siyuan.Client.RenameDoc`, `siyuan.Client.SetBlockAttrs`. SiYuan also exposes `/api/filetree/renameDocByID` (`{id,title}`) — a better fit than path-based `renameDoc` since the upload path holds the doc ID.
  - Real-world run also exposed the download `.md`-extension prune landmine (fixed separately in `20644b9`, covered by existing Req 5/6 design).

### Research Log

#### Cloudflare Access challenge shape
- **Context**: Determine how to detect "endpoint requires CF Access" for Req 12.3.
- **Sources Consulted**: live responses from `wiki.`/`docs.because-security.com`; Cloudflare Access service-token docs.
- **Findings**: protected endpoint replies HTTP 302 with `Location` host `*.cloudflareaccess.com` and sets `CF_AppSession`; with a valid service token, the SiYuan JSON envelope is returned normally (verified: `exportMdContent` 200, 8282 chars). `wiki.` (public publish view) instead returns 403 with an empty body for `/api/export/*`.
- **Implications**: detection must run before JSON decode and key off non-JSON content-type / redirect host / empty 4xx body; the followed-redirect HTML is also a usable signal.

### Architecture Pattern Evaluation

| Option | Description | Strengths | Risks / Limitations | Notes |
|--------|-------------|-----------|---------------------|-------|
| Extend `tags` with `ExtractMeta` | Title+body+attrs from one parse pass in the package that already owns frontmatter | Cohesive, no new package, single parse | Slightly widens `tags` responsibility | Chosen |
| New `internal/frontmatter` package | Dedicated frontmatter concern | Narrow SRP | Duplicates existing split/parse logic; extra indirection for one consumer | Rejected (simplification) |
| Generic `headers:` config map | Arbitrary per-request headers | Maximally flexible | Speculative; CF Access is the only current need | Interface generalized via `SetHeader`, config kept to two explicit CF fields |

### Design Decisions

#### Decision: Generalize transport headers, keep config explicit
- **Context**: Req 12 needs two CF headers on every request.
- **Selected Approach**: `Client.SetHeader(key,value)` + `extraHeaders` map (implemented `20644b9`); config exposes only `cf_access_client_id`/`cf_access_client_secret`.
- **Rationale**: interface generalizes (future headers cost nothing) without speculative config surface; `NewClient` signature unchanged so existing tests/callers are untouched.
- **Trade-offs**: a generic `headers:` map was deferred as speculative.

#### Decision: Adopt SiYuan-native title/attr APIs and in-repo frontmatter parsing
- **Context**: Req 13 title + tag mapping.
- **Selected Approach**: `renameDocByID` (new thin client method) + existing `SetBlockAttrs`; frontmatter via existing split/parse + `yaml.v3`.
- **Rationale**: build-vs-adopt — everything needed already exists in the stack; no new dependency.
- **Trade-offs**: adds one client method; `tags` package scope widens slightly.

#### Decision: Title/attr failures are non-fatal
- **Context**: ordering of create → title → attrs.
- **Selected Approach**: body upload determines created/updated; title/attr API errors are recorded per-file but do not fail the file.
- **Rationale**: content fidelity (the primary value) should not be lost because a secondary metadata call failed; keeps component ownership unambiguous.
- **Follow-up**: assert this in sync-engine tests.

### Risks & Mitigations
- Go's `http.Client` auto-follows redirects → CF challenge may surface as login HTML rather than a 302 — mitigation: detect by non-JSON content-type/body markers, not status code alone.
- `updateBlock` on a document root replaces content (observed in real run) — stripping frontmatter improves fidelity; out-of-scope structural concerns tracked separately.
- Credential leakage via error strings — mitigation: explicit invariant + test that error text excludes the secret (12.5).
