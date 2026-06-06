package ontology

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Router resolves the canonical wiki folder for a Domain and decides
// whether a file at a given local path must move to that folder. The
// router also scans markdown bodies for relative asset references that
// the move would relocate, and reports per-reference AssetWarnings the
// caller can surface (warnings, not errors — Requirement 9.4: asset
// breakage never blocks the move).
//
// Preconditions for Route:
//   - The Domain passed in has already cleared ValidateDomain.
//   - localPath is interpreted as a forward-slash path (filepath.ToSlash
//     is applied defensively); it may be absolute or repo-relative.
//
// Postconditions:
//   - For RouteNoop, TargetPath is empty and AssetWarnings is empty.
//   - For RouteMove, TargetPath is <canonical-folder>/<basename(localPath)>;
//     subdirectory structure under the source is intentionally NOT mirrored
//     under the canonical folder (per design: "preserving the filename").
type Router struct {
	repoPath string
}

// NewRouter returns a Router whose AssetWarning target-existence probe
// resolves paths against repoPath rather than the process working
// directory. Production call sites that run outside the repo root (the
// migrate executor and the sync engine) must use this constructor.
// A zero-value Router{} still works; its probe falls back to cwd-relative
// os.Stat, which is the behavior the unit tests rely on.
func NewRouter(repoPath string) Router {
	return Router{repoPath: repoPath}
}

// RouteAction is the discriminator for what the gate should do with a
// file declaring a given Domain at a given local path.
type RouteAction int

const (
	// RouteNoop means the file is already at (or beneath) its canonical
	// folder; the engine performs no `git mv`.
	RouteNoop RouteAction = iota
	// RouteMove means the file's local path is outside the canonical
	// folder for its declared Domain; the engine must `git mv` it to
	// <canonical>/<basename>.
	RouteMove
)

// RouteDecision is the structured output of Router.Route.
type RouteDecision struct {
	Action        RouteAction
	SourcePath    string
	TargetPath    string // filled when Action == RouteMove; empty for RouteNoop
	AssetWarnings []AssetWarning
}

// AssetWarning describes a single relative asset reference whose resolved
// path would change after the routing move. Emitted regardless of whether
// the target asset exists; TargetExists records that probe result.
type AssetWarning struct {
	Reference    string // raw markdown reference, e.g. "assets/foo.png"
	OldResolved  string // path resolved against the source file's directory
	NewResolved  string // path resolved against the target file's directory
	TargetExists bool   // os.Stat(NewResolved) == nil
}

// canonicalFolders is the hardcoded Domain → wiki folder map.
// Adding or renaming an entry is a Revalidation Trigger (design.md).
var canonicalFolders = map[Domain]string{
	DevOps:       "wiki/Linux & DevOps",
	Forensics:    "wiki/Digital Forensics",
	Security:     "wiki/Security",
	AIML:         "wiki/AI & ML",
	SoftwareDev:  "wiki/Software Development",
	QuantFinance: "wiki/Quant Finance",
}

// CanonicalFolder returns the canonical wiki folder for d.
//
// Panics on an unknown Domain (validation must precede routing — design.md
// `ontology/router`). This is defensive: ValidateDomain at the gate
// upstream means an unknown value can never reach the Router in production.
func (Router) CanonicalFolder(d Domain) string {
	folder, ok := canonicalFolders[d]
	if !ok {
		panic(fmt.Sprintf(
			"ontology.Router: unknown Domain %q (validation must precede routing)",
			string(d),
		))
	}
	return folder
}

// Route decides what action the gate must take for a file declaring
// `domain: d` at localPath. See RouteDecision for the output shape and
// the package doc for the action semantics.
//
// Algorithm:
//  1. Compute canonical = CanonicalFolder(d).
//  2. If localPath is already under canonical (top-level OR nested), the
//     action is RouteNoop. The router does NOT flatten existing sub-
//     directory structure beneath the canonical root.
//  3. Otherwise the action is RouteMove with
//     TargetPath = canonical + "/" + filepath.Base(localPath).
//  4. Scan body for relative markdown asset references and emit an
//     AssetWarning for each reference whose resolved location would
//     differ after the move. For RouteNoop, no AssetWarnings are emitted
//     (the resolved location is unchanged by definition).
func (r Router) Route(d Domain, localPath string, body []byte) RouteDecision {
	canonical := r.CanonicalFolder(d)
	srcSlash := filepath.ToSlash(filepath.Clean(localPath))

	decision := RouteDecision{
		SourcePath: localPath,
	}

	if underCanonical(srcSlash, canonical) {
		decision.Action = RouteNoop
		return decision
	}

	decision.Action = RouteMove
	// Preserve only the basename — design.md "preserving the filename".
	decision.TargetPath = filepath.ToSlash(
		filepath.Join(canonical, filepath.Base(srcSlash)),
	)
	decision.AssetWarnings = scanAssetRefs(body, srcSlash, decision.TargetPath, r.repoPath)
	return decision
}

// underCanonical reports whether srcSlash sits at the top of canonical
// OR anywhere beneath it. Comparison is done on slash-normalized paths.
func underCanonical(srcSlash, canonical string) bool {
	canonical = filepath.ToSlash(filepath.Clean(canonical))
	dir := filepath.ToSlash(filepath.Dir(srcSlash))
	if dir == canonical {
		return true
	}
	// Nested: dir starts with canonical + "/".
	prefix := canonical + "/"
	return strings.HasPrefix(dir, prefix) || strings.HasPrefix(srcSlash, prefix)
}

// markdownRefPattern matches both image (![alt](path)) and link
// ([text](path)) markdown references. Capture group 1 is the inner path.
// We deliberately keep this regex simple — it does not attempt to handle
// titled links ([text](path "title")) or balanced parens inside the URL.
// Those edge cases would not change the OldResolved/NewResolved decision
// in the migration domain we care about.
var markdownRefPattern = regexp.MustCompile(`!?\[[^\]]*\]\(([^)\s]+)\)`)

// scanAssetRefs walks body line by line, tracking ``` code-block state,
// and collects an AssetWarning for each relative markdown reference whose
// resolved path would change between srcSlash and targetSlash.
func scanAssetRefs(body []byte, srcSlash, targetSlash, repoPath string) []AssetWarning {
	if len(body) == 0 {
		return nil
	}
	srcDir := filepath.Dir(srcSlash)
	dstDir := filepath.Dir(targetSlash)

	var out []AssetWarning
	inFence := false

	for _, line := range strings.Split(string(body), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		for _, match := range markdownRefPattern.FindAllStringSubmatch(line, -1) {
			ref := match[1]
			if !isRelativeAssetRef(ref) {
				continue
			}
			oldRes := filepath.ToSlash(filepath.Join(srcDir, ref))
			newRes := filepath.ToSlash(filepath.Join(dstDir, ref))
			if oldRes == newRes {
				// The move did not change where this ref resolves.
				continue
			}
			probePath := newRes
			if repoPath != "" {
				probePath = filepath.Join(repoPath, newRes)
			}
			out = append(out, AssetWarning{
				Reference:    ref,
				OldResolved:  oldRes,
				NewResolved:  newRes,
				TargetExists: pathExists(probePath),
			})
		}
	}
	return out
}

// isRelativeAssetRef filters references the migration cares about:
// only relative paths that point at a file in the working tree. Everything
// network-bound (http, https, mailto), absolute (/foo), home-prefixed
// (~/foo), or empty is skipped.
func isRelativeAssetRef(ref string) bool {
	if ref == "" {
		return false
	}
	// Intra-document fragment anchors (`[Section](#anchor)`) resolve within
	// the same document regardless of file location, so a routing move
	// never invalidates them. The Docker-note migration surfaced 10 false
	// positives per file from TOC-style anchors.
	if strings.HasPrefix(ref, "#") {
		return false
	}
	lower := strings.ToLower(ref)
	if strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "mailto:") {
		return false
	}
	if strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, "~") {
		return false
	}
	if strings.HasPrefix(ref, "<") { // wikilink / HTML — not in scope
		return false
	}
	return true
}

// pathExists returns true iff os.Stat(path) returns no error. Errors
// other than non-existence (permission denied etc.) are treated as "not
// observed" — the warning still fires, the engine can decide what to do.
func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
