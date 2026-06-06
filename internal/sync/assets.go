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
	return true
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
			errs = append(errs, types.SyncError{
				File:    file,
				Message: fmt.Sprintf("asset %q not found at %s; leaving body reference unchanged", ref, localPath),
			})
			continue
		}
		stored, err := e.client.UploadAsset(ctx, localPath)
		if err != nil {
			errs = append(errs, types.SyncError{
				File:    file,
				Message: fmt.Sprintf("upload asset %q failed: %v; leaving body reference unchanged", ref, err),
			})
			continue
		}
		mapping[ref] = stored
	}
	if len(mapping) == 0 {
		return body, errs
	}
	return string(rewriteAssetRefs([]byte(body), mapping)), errs
}
