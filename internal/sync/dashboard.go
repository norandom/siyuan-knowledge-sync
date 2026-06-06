package sync

import (
	"context"
	"fmt"
	"strings"

	"siyuan-knowledge-sync/internal/ontology"
)

// ensureIntentIndices upserts one index document per intent in
// ontology.AllIntents() under the notebook identified by boxID. Each
// index is a pre-rendered markdown bullet list of SiYuan block-ref
// pills (`((id "title"))`) — one line per matching doc.
//
// We pre-render at sync time rather than emit a live {{<SQL>}} embed
// because SiYuan's markdown parser (createDocWithMd + updateBlock) does
// not recognize multi-line `{{...}}` as a query_embed block; only the UI
// can construct one. A single-line {{select ...}} embed renders matches
// as full document content (heavy), not as a compact link list. The
// sy-query-view plugin's `dv.addlist` would solve this but also requires
// multi-line JS in the embed body, hitting the same parser limitation.
//
// Static ref pills `((id "title"))` are SiYuan's native syntax and
// render as clickable hover-preview pills in any version, no plugin.
//
// Refresh policy: indices are rebuilt every sync. createDocWithMd is
// idempotent by hpath, so re-running overwrites the body. Users SHOULD
// NOT hand-edit these docs — they are derived artifacts.
//
// Caching: indicesEnsured (per session) caps the rebuilds at one per
// notebook per process run.
//
// Errors are returned but the call site applies non-fatal policy: a
// failed index upsert never blocks the actual file sync.
func (e *SyncEngine) ensureIntentIndices(ctx context.Context, boxID, domainName string) error {
	for _, intent := range ontology.AllIntents() {
		intentStr := string(intent)
		hpath := fmt.Sprintf("/_%s_index.md", intentStr)

		// SiYuan's createDocWithMd is NOT idempotent by hpath: each call
		// makes a fresh doc, even if the hpath already exists. Without an
		// explicit cleanup step the index docs pile up — every sync run
		// adds 5 more, one per intent. So we lookup any docs already at
		// this hpath via SQL and remove them before re-creating. Errors
		// during cleanup are tolerated (the create itself will surface
		// any persistent state issue).
		existing, err := e.client.SQLQuery(ctx, fmt.Sprintf(
			"SELECT id FROM blocks WHERE type='d' AND box='%s' AND hpath='%s'",
			boxID, hpath,
		))
		if err == nil {
			for _, row := range existing {
				if id, ok := row["id"].(string); ok && id != "" {
					_ = e.client.RemoveDocByID(ctx, id)
				}
			}
		}

		// Query the docs in this notebook that carry this intent. The
		// ial-LIKE pattern matches the custom-intent IAL attribute SiYuan
		// stores per the engine's setBlockAttrs call. Content for type='d'
		// blocks is the doc title.
		stmt := fmt.Sprintf(
			"SELECT id, content FROM blocks WHERE type='d' AND box='%s' AND ial LIKE '%%custom-intent=\"%s\"%%' ORDER BY updated DESC",
			boxID, intentStr,
		)
		rows, err := e.client.SQLQuery(ctx, stmt)
		if err != nil {
			return fmt.Errorf("query %s docs: %w", intentStr, err)
		}
		body := buildIndexBody(intentStr, domainName, rows)
		if _, err := e.client.CreateDocWithMd(ctx, boxID, hpath, body); err != nil {
			return fmt.Errorf("ensure %s index: %w", intentStr, err)
		}
	}
	return nil
}

// buildIndexBody renders the markdown body for one (intent, domain)
// index document from a list of SiYuan SQL result rows. Each row must
// have string fields `id` and `content` (content being the doc title
// for type='d' blocks).
//
// Output shape:
//
//	# <Intent> index - <Domain>
//
//	* ((<id> "<title>"))
//	* ((<id> "<title>"))
//
// If no matches, a placeholder line is emitted so the index doc is
// still discoverable rather than empty.
func buildIndexBody(intentStr, domainName string, rows []map[string]any) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s index - %s\n\n", titleCaseASCII(intentStr), domainName)
	if len(rows) == 0 {
		fmt.Fprintf(&b, "_No %s documents in %s yet._\n", intentStr, domainName)
		return b.String()
	}
	for _, r := range rows {
		id, _ := r["id"].(string)
		title, _ := r["content"].(string)
		if id == "" {
			continue
		}
		// SiYuan stores `blocks.content` for type='d' blocks as the doc
		// title; when no explicit frontmatter title was set the title
		// retains the source file's `.md` extension. Strip it for prettier
		// ref-pill display — the extension is presentation-only metadata,
		// not semantic content.
		title = strings.TrimSuffix(title, ".md")
		// Defensive: title may contain " which would break the ref-pill
		// syntax. Substitute with a single quote since `((id ...))` only
		// needs SOMETHING to label the link.
		title = strings.ReplaceAll(title, `"`, `'`)
		fmt.Fprintf(&b, "* ((%s \"%s\"))\n", id, title)
	}
	return b.String()
}

// isOntologyDomainNotebook reports whether notebookName matches one of
// the 6 closed-enum canonical folders. The intent-index machinery is an
// ontology artifact and MUST NOT fire for transitional / legacy
// notebooks (Hosting, DevOps, etc. from the pre-migration tree) — those
// don't have a meaningful intent axis, and polluting them with empty
// index docs would surprise the user.
func isOntologyDomainNotebook(notebookName string) bool {
	r := ontology.Router{}
	for _, d := range ontology.AllDomains() {
		if r.CanonicalFolder(d) == notebookName {
			return true
		}
	}
	return false
}

// titleCaseASCII uppercases the first ASCII byte. The intent enum is
// validated by the schema layer to be lowercase ASCII (e.g. "sop",
// "config", "concept"), so this is safe.
func titleCaseASCII(s string) string {
	if s == "" {
		return s
	}
	if s[0] >= 'a' && s[0] <= 'z' {
		return string(s[0]-32) + s[1:]
	}
	return s
}
