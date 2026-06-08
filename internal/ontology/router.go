package ontology

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Router resolves the canonical wiki folder for a Domain and decides
// whether a file needs to move there. It also scans for relative asset
// references that the move would relocate (reported as warnings, not errors).
type Router struct {
	repoPath string
}

// NewRouter returns a Router that resolves asset paths relative to repoPath.
// A zero-value Router falls back to cwd-relative resolution.
func NewRouter(repoPath string) Router {
	return Router{repoPath: repoPath}
}

// RouteAction tells the engine what to do with a file.
type RouteAction int

const (
	// RouteNoop: file is already at or beneath its canonical folder.
	RouteNoop RouteAction = iota
	// RouteMove: file must move to <canonical>/<basename>.
	RouteMove
)

// RouteDecision is the output of Router.Route.
type RouteDecision struct {
	Action        RouteAction
	SourcePath    string
	TargetPath    string // filled when Action == RouteMove; empty for RouteNoop
	AssetWarnings []AssetWarning
}

// AssetWarning is a relative asset reference whose resolved path would change
// after a routing move.
type AssetWarning struct {
	Reference    string // raw markdown reference, e.g. "assets/foo.png"
	OldResolved  string // path resolved against the source file's directory
	NewResolved  string // path resolved against the target file's directory
	TargetExists bool   // os.Stat(NewResolved) == nil
}

// defaultCanonicalFolders maps each Domain to its top-level repo-relative folder.
// Each domain becomes its own SiYuan notebook. Changing these requires updating
// test fixtures and e2e assertions.
var defaultCanonicalFolders = map[Domain]string{
	DevOps:       "Sysadmin & DevOps",
	Forensics:    "Digital Forensics",
	Security:     "Security",
	AIML:         "AI & ML",
	SoftwareDev:  "Software Development",
	QuantFinance: "Quant Finance",
}

// canonicalFolders is the runtime Domain-to-folder map. Seeded from
// defaultCanonicalFolders; replaceable via Configure().
var canonicalFolders = copyFolderMap(defaultCanonicalFolders)

// copyFolderMap returns a shallow copy of src.
func copyFolderMap(src map[Domain]string) map[Domain]string {
	out := make(map[Domain]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

// CanonicalFolder returns the canonical wiki folder for d.
// Panics on unknown Domain — ValidateDomain must run before routing.
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

// Route decides whether a file declaring domain d at localPath needs to move
// to the canonical folder. If so, it also scans for asset references that
// the move would relocate.
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

// markdownRefPattern matches image and link references. Capture group 1 is the path.
// Does not handle titled links or balanced parens inside URLs (not needed here).
var markdownRefPattern = regexp.MustCompile(`!?\[[^\]]*\]\(([^)\s]+)\)`)

// scanAssetRefs finds relative markdown references in body whose resolved
// path would change between srcSlash and targetSlash.
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

// isRelativeAssetRef returns true for relative file paths only.
// Skips URLs, absolute paths, fragments, wikilinks, and empty refs.
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
