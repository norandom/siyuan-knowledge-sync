# Requirements Document

## Introduction
SiYuan Knowledge Sync is a CLI tool and MCP server that synchronizes git-tracked markdown notes with a SiYuan notebook server. It supports bidirectional sync, compliance auditing with auto-fix, pruning of deleted files, tag and TOC support, folder-to-notebook hierarchy mapping, agent-accessible search and retrieval via MCP, Cloudflare Access (ZTNA) service-token authentication for protected endpoints, and markdown frontmatter fidelity (title and tag mapping) on upload.

## Boundary Context
- **In scope**: Git-tracked `.md` file sync with SiYuan, SiYuan compliance audit and auto-fix, bidirectional sync (upload/download), pruning, tag extraction to SiYuan block attributes, TOC generation, folder-to-notebook mapping, hierarchy preservation, MCP server for agent retrieval/search, SiYuan skill for in-app note creation, Cloudflare Access service-token authentication for endpoints behind Cloudflare Access (Zero Trust), YAML frontmatter title and tag mapping on upload.
- **Out of scope**: Non-markdown file sync, real-time collaborative editing, sync with knowledge base platforms other than SiYuan, web UI, multi-user access control.
- **Adjacent expectations**: A git repository with tracked `.md` files must exist locally. A SiYuan server instance with API access must be running and reachable. The SiYuan skill must be installed within the SiYuan workspace.

## Requirements

### Requirement 1: Configuration & Authentication
**Objective:** As a user, I want to configure the SiYuan endpoint and authentication token, so that the system connects to the correct SiYuan instance.

#### Acceptance Criteria
1. The SiYuan Knowledge Sync shall accept a configurable SiYuan endpoint URL.
2. The SiYuan Knowledge Sync shall accept a configurable authentication token for API requests.
3. If the configured endpoint is unreachable or the token is invalid, then the SiYuan Knowledge Sync shall report a clear error and abort the operation.
4. When the endpoint or token changes, the SiYuan Knowledge Sync shall use the updated configuration on the next operation.

### Requirement 2: Git Repository Integration
**Objective:** As a user, I want the system to discover git-tracked markdown files, so that only version-controlled notes are synced.

#### Acceptance Criteria
1. The SiYuan Knowledge Sync shall identify all git-tracked markdown files (`.md`) in the configured repository.
2. If a markdown file is not tracked by git, then the SiYuan Knowledge Sync shall exclude it from sync operations.
3. When a new git-tracked markdown file is added, the SiYuan Knowledge Sync shall include it in the next sync.

### Requirement 3: Notebook & Hierarchy Mapping
**Objective:** As a user, I want folders to represent SiYuan notebooks and subfolders to represent document hierarchy, so that my local file structure maps directly to the SiYuan workspace.

#### Acceptance Criteria
1. The SiYuan Knowledge Sync shall treat each top-level folder as a SiYuan notebook.
2. When a folder is encountered, the SiYuan Knowledge Sync shall create or update the corresponding notebook in SiYuan.
3. The SiYuan Knowledge Sync shall preserve the local folder hierarchy as document hierarchy (parent-child nesting) in SiYuan.
4. If a folder is renamed locally, then the SiYuan Knowledge Sync shall rename the corresponding notebook or document in SiYuan.

### Requirement 4: Content Sync — Upload
**Objective:** As a user, I want local git-tracked markdown notes pushed to SiYuan, so that my notes appear in the SiYuan workspace.

#### Acceptance Criteria
1. When a local markdown file is created or modified, the SiYuan Knowledge Sync shall push the content to the corresponding SiYuan document.
2. The SiYuan Knowledge Sync shall preserve markdown formatting when pushing content to SiYuan.
3. If a document does not yet exist in SiYuan, then the SiYuan Knowledge Sync shall create it at the correct path in the mapped notebook.
4. When sync completes, the SiYuan Knowledge Sync shall report which documents were created or updated.

### Requirement 5: Content Sync — Download
**Objective:** As a user, I want to download existing SiYuan content to local files, so that I can seed or restore my local repository from SiYuan.

#### Acceptance Criteria
1. When download is triggered, the SiYuan Knowledge Sync shall fetch all documents from the configured SiYuan notebooks and write them to the local file system as markdown files.
2. The SiYuan Knowledge Sync shall preserve the document hierarchy from SiYuan as local folder structure.
3. If a local file already exists, then the SiYuan Knowledge Sync shall report the conflict and allow the user to choose overwrite, skip, or merge behavior.

### Requirement 6: Content Pruning
**Objective:** As a user, I want documents removed from SiYuan when their local files are deleted, so that the workspace stays in sync with the local repository.

#### Acceptance Criteria
1. When a previously synced local markdown file is deleted from the git repository, the SiYuan Knowledge Sync shall remove the corresponding document from SiYuan.
2. If the pruned document has child documents in SiYuan that were not created by sync, then the SiYuan Knowledge Sync shall report the dependency conflict and skip pruning that document.
3. The SiYuan Knowledge Sync shall report all pruned documents after a sync operation.

### Requirement 7: SiYuan Compliance Audit & Auto-fix
**Objective:** As a user, I want the system to audit my markdown notes for SiYuan compliance and automatically fix issues, so that synced content renders correctly.

#### Acceptance Criteria
1. When audit is triggered, the SiYuan Knowledge Sync shall check each markdown file for SiYuan-specific formatting requirements (block IDs, valid heading structure, asset references, attribute syntax).
2. The SiYuan Knowledge Sync shall report all compliance issues found, grouped by file and severity.
3. Where auto-fix is enabled, the SiYuan Knowledge Sync shall automatically correct fixable compliance issues before syncing.
4. If an issue cannot be auto-fixed, then the SiYuan Knowledge Sync shall report the file and issue details so the user can resolve it manually.
5. The SiYuan Knowledge Sync shall not modify files that have no compliance issues.

### Requirement 8: Tag Support
**Objective:** As a user, I want tag metadata from my notes mapped to SiYuan block attributes, so that I can organize and filter notes by tags.

#### Acceptance Criteria
1. The SiYuan Knowledge Sync shall extract tags from markdown frontmatter or inline tag syntax and set them as SiYuan block attributes.
2. When tags are modified locally, the SiYuan Knowledge Sync shall update the corresponding block attributes in SiYuan.
3. If a tag format is not recognized by SiYuan, then the SiYuan Knowledge Sync shall report a compliance issue.

### Requirement 9: Table of Contents Support
**Objective:** As a user, I want table of contents to be generated and synced with documents, so that long documents are navigable in SiYuan.

#### Acceptance Criteria
1. The SiYuan Knowledge Sync shall recognize TOC markers in markdown documents and generate a table of contents based on the document's heading structure.
2. When the heading structure changes, the SiYuan Knowledge Sync shall regenerate the TOC on the next sync.
3. The generated TOC shall use SiYuan-compatible block references.

### Requirement 10: MCP Server (Agent Interface)
**Objective:** As an AI agent user, I want an MCP server that allows agents to retrieve and search SiYuan documents, so that agents can work with the synced knowledge base.

#### Acceptance Criteria
1. The MCP server shall expose a tool to search SiYuan documents by keyword or query.
2. The MCP server shall expose a tool to retrieve the full content of a specific SiYuan document.
3. The MCP server shall expose a tool to list notebooks and their document structure.
4. When an agent searches for content, the MCP server shall return matching document IDs, titles, and relevant excerpts.
5. If the SiYuan endpoint is unreachable, then the MCP server shall return an error to the calling agent.

### Requirement 11: SiYuan Skill Integration
**Objective:** As a user, I want a SiYuan skill that creates notes within the SiYuan interface, so that notes created in-app are also synced to the local git repository.

#### Acceptance Criteria
1. The SiYuan skill shall create a new markdown document in the correct notebook and path based on user input within SiYuan.
2. When a document is created via the skill, the SiYuan Knowledge Sync shall detect the new document and create the corresponding local file in the git-tracked directory.
3. The skill shall support creating documents with initial markdown content.
4. The skill shall support creating documents with tags applied via block attributes.

### Requirement 12: Cloudflare Access (ZTNA) Endpoint Support
**Objective:** As an operator, I want to provide Cloudflare Access service-token credentials, so that the system can reach a SiYuan endpoint protected by Cloudflare Access (Zero Trust) without interactive browser authentication.

#### Acceptance Criteria
1. Where Cloudflare Access service-token credentials are configured, the SiYuan Knowledge Sync shall present those credentials on every SiYuan API request.
2. Where Cloudflare Access service-token credentials are not configured, the SiYuan Knowledge Sync shall operate unchanged against endpoints that are not protected by Cloudflare Access.
3. If the SiYuan endpoint requires Cloudflare Access and no valid service-token credentials are configured, then the SiYuan Knowledge Sync shall report a clear error stating that Cloudflare Access authentication is required and abort the operation.
4. When the Cloudflare Access service-token credentials change, the SiYuan Knowledge Sync shall use the updated credentials on the next operation.
5. The SiYuan Knowledge Sync shall treat Cloudflare Access service-token credentials as sensitive configuration and shall not emit them in logs, reports, or error messages.

### Requirement 13: Markdown Frontmatter Fidelity on Upload
**Objective:** As a user, I want YAML frontmatter handled correctly when my notes are pushed to SiYuan, so that document titles and tags are accurate and the frontmatter block does not appear as visible body content.

#### Acceptance Criteria
1. When a markdown file containing YAML frontmatter is uploaded to SiYuan, the SiYuan Knowledge Sync shall exclude the frontmatter block from the document body content.
2. Where a markdown file's frontmatter defines a title, the SiYuan Knowledge Sync shall set the SiYuan document title to that frontmatter title.
3. Where a markdown file's frontmatter does not define a title, the SiYuan Knowledge Sync shall derive the SiYuan document title from the file name excluding its file extension.
4. When a markdown file with frontmatter or inline tags is uploaded, the SiYuan Knowledge Sync shall apply the extracted tags as SiYuan block attributes on the corresponding document, consistent with Requirement 8.
5. If the frontmatter cannot be parsed, then the SiYuan Knowledge Sync shall report a compliance issue and upload the document body without applying title or tag mapping.
