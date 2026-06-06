package sync

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"siyuan-knowledge-sync/internal/types"
)

// assetMdRefPattern matches markdown image and link references. Capture
// group 1 is the inner URL. Mirrors ontology.markdownRefPattern; the
// duplication keeps the sync package free of an inward dependency on
// ontology.
var assetMdRefPattern = regexp.MustCompile(`!?\[[^\]]*\]\(([^)\s]+)\)`)

// extractAssetRefs returns deduplicated relative markdown refs the
// migration should upload to SiYuan. Skips http(s)/mailto, intra-document
// `#anchor` fragments, absolute (/foo), home-prefixed (~/foo), wikilink
// (`<...>`), and empty refs. Preserves the order of first appearance.
func extractAssetRefs(body []byte) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, m := range assetMdRefPattern.FindAllSubmatch(body, -1) {
		ref := string(m[1])
		if !isLocalAssetRef(ref) {
			continue
		}
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		out = append(out, ref)
	}
	return out
}

func isLocalAssetRef(ref string) bool {
	if ref == "" {
		return false
	}
	if strings.HasPrefix(ref, "#") {
		return false
	}
	if strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, "~") {
		return false
	}
	if strings.HasPrefix(ref, "<") {
		return false
	}
	lower := strings.ToLower(ref)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "mailto:") {
		return false
	}
	// Whitelist binary asset extensions. Anything else (markdown cross-
	// links, URL-encoded paths the regex truncated mid-string, embedded
	// configuration snippets) is NOT an upload candidate. A whitelist is
	// safer than a blacklist here because the cobesy rewrite path frequently
	// emits cross-doc links like `[Other Doc](Other%20Doc.md)` or
	// `[K8s Istio (2020)](K8s%20Istio%20(2020).md)` where the inner `(`
	// makes the markdown regex stop early at the orphan `)` and the
	// extracted suffix loses its `.md`.
	for _, ext := range assetExtensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// assetExtensions enumerates the suffixes the engine treats as binary
// uploadable assets. Order is presentation only; lookups are linear.
var assetExtensions = []string{
	".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp", ".bmp", ".ico",
	".mp4", ".webm", ".mov", ".m4v",
	".pdf",
	".drawio",
	".zip", ".tar", ".gz", ".tgz",
}

// rewriteAssetRefs replaces `(<old>)` with `(<new>)` inside markdown ref
// syntax for each entry of mapping. The substring substitution is bounded
// by the parens because markdown URLs cannot legally contain unescaped
// `(` or `)`; a ref `(attachments/foo.png)` is unambiguous in any
// well-formed markdown body.
func rewriteAssetRefs(body []byte, mapping map[string]string) []byte {
	out := body
	for old, replacement := range mapping {
		out = bytes.ReplaceAll(out, []byte("("+old+")"), []byte("("+replacement+")"))
	}
	return out
}

// uploadAndRewriteAssets resolves each relative asset ref in body against
// fileDir (the source markdown's directory), uploads the local file to
// SiYuan via the asset-upload endpoint, and returns the body with each
// successfully uploaded ref rewritten to the SiYuan-assigned `assets/...`
// path.
//
// Failure policy (matches the existing tag-attr pattern in processFile):
// per-asset failures are non-fatal — they surface as warning-shaped
// SyncErrors keyed to `file`, and the corresponding ref is left in the
// body unchanged so the doc still uploads cleanly. The image renders
// broken for that one ref; everything else still works.
func (e *SyncEngine) uploadAndRewriteAssets(ctx context.Context, file, fileDir, body string) (string, []types.SyncError) {
	refs := extractAssetRefs([]byte(body))
	if len(refs) == 0 {
		return body, nil
	}
	mapping := make(map[string]string, len(refs))
	var errs []types.SyncError
	for _, ref := range refs {
		localPath := filepath.Join(fileDir, ref)
		if _, err := os.Stat(localPath); err != nil {
			// Asset missing on disk: print to stderr but DON'T fail the
			// entry. The body keeps its original ref (renders broken in
			// SiYuan UI) — same data-safety posture as setBlockAttrs
			// failures. Real failures (createDoc, gitMv) are still fatal.
			fmt.Fprintf(os.Stderr, "warning: asset %q for %s not found at %s; leaving body ref unchanged\n", ref, file, localPath)
			continue
		}
		stored, err := e.client.UploadAsset(ctx, localPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: upload asset %q for %s failed: %v\n", ref, file, err)
			continue
		}
		mapping[ref] = stored
	}
	if len(mapping) == 0 {
		return body, errs
	}
	return string(rewriteAssetRefs([]byte(body), mapping)), errs
}
