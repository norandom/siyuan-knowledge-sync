package ontology

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// TestCanonicalFolder_AllDomains verifies that every Domain enum value maps
// to the exact canonical wiki folder string from design.md. The
// quant-finance entry is included even though it is reserved-empty
// (Requirement 1.3).
func TestCanonicalFolder_AllDomains(t *testing.T) {
	r := Router{}
	cases := []struct {
		domain Domain
		want   string
	}{
		{DevOps, "wiki/Linux & DevOps"},
		{Forensics, "wiki/Digital Forensics"},
		{Security, "wiki/Security"},
		{AIML, "wiki/AI & ML"},
		{SoftwareDev, "wiki/Software Development"},
		{QuantFinance, "wiki/Quant Finance"},
	}
	for _, tc := range cases {
		t.Run(string(tc.domain), func(t *testing.T) {
			got := r.CanonicalFolder(tc.domain)
			if got != tc.want {
				t.Fatalf("CanonicalFolder(%q) = %q, want %q", tc.domain, got, tc.want)
			}
		})
	}
}

// TestCanonicalFolder_CoversEveryEnum guards against silent drift: if a new
// Domain is added to the enum, this test fails until CanonicalFolder learns
// about it. (Requirement 3.1)
func TestCanonicalFolder_CoversEveryEnum(t *testing.T) {
	r := Router{}
	for _, d := range AllDomains() {
		folder := r.CanonicalFolder(d)
		if folder == "" {
			t.Fatalf("CanonicalFolder(%q) returned empty string", d)
		}
		if !strings.HasPrefix(folder, "wiki/") {
			t.Fatalf("CanonicalFolder(%q) = %q, expected to start with wiki/", d, folder)
		}
	}
}

// TestCanonicalFolder_PanicsOnUnknownDomain verifies the defensive panic
// for an undefined Domain value (validation must precede routing).
func TestCanonicalFolder_PanicsOnUnknownDomain(t *testing.T) {
	r := Router{}
	defer func() {
		rec := recover()
		if rec == nil {
			t.Fatalf("expected panic for unknown domain, got none")
		}
	}()
	_ = r.CanonicalFolder(Domain("bogus-not-a-real-domain"))
}

// TestRoute_AlreadyCanonical asserts that a file already at the top of its
// canonical folder yields RouteNoop with empty TargetPath. (Requirement 3.6)
func TestRoute_AlreadyCanonical(t *testing.T) {
	r := Router{}
	local := "wiki/Linux & DevOps/foo.md"
	got := r.Route(DevOps, local, nil)
	if got.Action != RouteNoop {
		t.Fatalf("Action = %v, want RouteNoop", got.Action)
	}
	if got.SourcePath != local {
		t.Fatalf("SourcePath = %q, want %q", got.SourcePath, local)
	}
	if got.TargetPath != "" {
		t.Fatalf("TargetPath = %q, want empty", got.TargetPath)
	}
	if len(got.AssetWarnings) != 0 {
		t.Fatalf("AssetWarnings = %v, want empty for noop", got.AssetWarnings)
	}
}

// TestRoute_AlreadyDeeperUnderCanonical asserts that a file already nested
// under the canonical folder (with its own sub-structure) is left alone
// (RouteNoop). The router does not flatten existing organization. (Requirement 3.6)
func TestRoute_AlreadyDeeperUnderCanonical(t *testing.T) {
	r := Router{}
	local := "wiki/Linux & DevOps/sub/foo.md"
	got := r.Route(DevOps, local, nil)
	if got.Action != RouteNoop {
		t.Fatalf("Action = %v, want RouteNoop (already under canonical subtree)", got.Action)
	}
	if got.TargetPath != "" {
		t.Fatalf("TargetPath = %q, want empty", got.TargetPath)
	}
}

// TestRoute_OutsideCanonicalMovesToCanonical asserts that a file not under
// the canonical folder is moved to <canonical>/<basename>, preserving only
// the filename (Requirement 3.2).
func TestRoute_OutsideCanonicalMovesToCanonical(t *testing.T) {
	r := Router{}
	local := "legacy/Hosting/foo.md"
	got := r.Route(DevOps, local, nil)
	if got.Action != RouteMove {
		t.Fatalf("Action = %v, want RouteMove", got.Action)
	}
	want := "wiki/Linux & DevOps/foo.md"
	if got.TargetPath != want {
		t.Fatalf("TargetPath = %q, want %q (basename only, not subdirectory)", got.TargetPath, want)
	}
	if got.SourcePath != local {
		t.Fatalf("SourcePath = %q, want %q", got.SourcePath, local)
	}
}

// TestRoute_AssetWarning_BasicMove asserts that a single relative asset
// reference inside a moved file produces exactly one AssetWarning with the
// old and new resolved paths (Requirements 9.1, 9.2).
func TestRoute_AssetWarning_BasicMove(t *testing.T) {
	r := Router{}
	local := "legacy/Hosting/foo.md"
	body := []byte("Some text\n\n![](assets/foo.png)\n")
	got := r.Route(DevOps, local, body)

	if got.Action != RouteMove {
		t.Fatalf("Action = %v, want RouteMove", got.Action)
	}
	if len(got.AssetWarnings) != 1 {
		t.Fatalf("len(AssetWarnings) = %d, want 1; warnings=%+v", len(got.AssetWarnings), got.AssetWarnings)
	}
	w := got.AssetWarnings[0]
	if w.Reference != "assets/foo.png" {
		t.Fatalf("Reference = %q, want %q", w.Reference, "assets/foo.png")
	}
	wantOld := filepath.ToSlash(filepath.Join("legacy/Hosting", "assets/foo.png"))
	wantNew := filepath.ToSlash(filepath.Join("wiki/Linux & DevOps", "assets/foo.png"))
	if filepath.ToSlash(w.OldResolved) != wantOld {
		t.Fatalf("OldResolved = %q, want %q", w.OldResolved, wantOld)
	}
	if filepath.ToSlash(w.NewResolved) != wantNew {
		t.Fatalf("NewResolved = %q, want %q", w.NewResolved, wantNew)
	}
	if w.TargetExists {
		t.Fatalf("TargetExists = true, want false (no fs setup)")
	}
}

// TestRoute_AssetWarning_MarkdownLink covers the plain [text](path) link
// pattern in addition to the ![] image pattern.
func TestRoute_AssetWarning_MarkdownLink(t *testing.T) {
	r := Router{}
	local := "legacy/Hosting/foo.md"
	body := []byte("See [the diagram](diagrams/topology.svg) for details.\n")
	got := r.Route(DevOps, local, body)

	if len(got.AssetWarnings) != 1 {
		t.Fatalf("len(AssetWarnings) = %d, want 1", len(got.AssetWarnings))
	}
	if got.AssetWarnings[0].Reference != "diagrams/topology.svg" {
		t.Fatalf("Reference = %q, want %q", got.AssetWarnings[0].Reference, "diagrams/topology.svg")
	}
}

// TestRoute_AssetScan_IgnoresAbsoluteAndExternal asserts that http(s),
// mailto, absolute, and home-relative references are skipped (only RELATIVE
// references matter).
func TestRoute_AssetScan_IgnoresAbsoluteAndExternal(t *testing.T) {
	r := Router{}
	local := "legacy/Hosting/foo.md"
	body := []byte(strings.Join([]string{
		"![ext](http://example.com/foo.png)",
		"![https](https://example.com/foo.png)",
		"[mail](mailto:foo@example.com)",
		"![absolute](/abs/foo.png)",
		"![home](~/Pictures/foo.png)",
	}, "\n"))
	got := r.Route(DevOps, local, body)
	if len(got.AssetWarnings) != 0 {
		t.Fatalf("len(AssetWarnings) = %d, want 0; warnings=%+v",
			len(got.AssetWarnings), got.AssetWarnings)
	}
}

// TestRoute_AssetScan_SkipsFencedCodeBlocks asserts that references inside
// ``` code fences are not treated as live markdown links.
func TestRoute_AssetScan_SkipsFencedCodeBlocks(t *testing.T) {
	r := Router{}
	local := "legacy/Hosting/foo.md"
	body := []byte(strings.Join([]string{
		"Some prose.",
		"```",
		"![](assets/in-code.png)",
		"[link](assets/also-in-code.png)",
		"```",
		"![](assets/outside.png)",
	}, "\n"))
	got := r.Route(DevOps, local, body)

	got.sortWarningsByRef() // helper defined in router.go for stable test order
	refs := make([]string, len(got.AssetWarnings))
	for i, w := range got.AssetWarnings {
		refs[i] = w.Reference
	}
	want := []string{"assets/outside.png"}
	if !reflect.DeepEqual(refs, want) {
		t.Fatalf("references = %v, want %v", refs, want)
	}
}

// TestRoute_AssetWarning_TargetExistsTrue asserts that when the new
// resolved asset path actually exists on disk, TargetExists is true.
// The router is path-relative — callers (sync engine) chdir to the
// repo root before invoking Route, so this test mirrors that contract
// by chdir-ing into the tmp tree.
func TestRoute_AssetWarning_TargetExistsTrue(t *testing.T) {
	r := Router{}
	tmp := t.TempDir()

	// Lay out tmp/legacy/Hosting/foo.md and tmp/wiki/Linux & DevOps/assets/foo.png.
	srcDir := filepath.Join(tmp, "legacy", "Hosting")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}
	dstAssetDir := filepath.Join(tmp, "wiki", "Linux & DevOps", "assets")
	if err := os.MkdirAll(dstAssetDir, 0o755); err != nil {
		t.Fatalf("mkdir dst asset: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dstAssetDir, "foo.png"), []byte("png"), 0o644); err != nil {
		t.Fatalf("write asset: %v", err)
	}

	// Chdir into tmp so the relative paths in the body resolve there.
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir tmp: %v", err)
	}

	local := "legacy/Hosting/foo.md"
	body := []byte("![](assets/foo.png)\n")
	got := r.Route(DevOps, local, body)

	if got.Action != RouteMove {
		t.Fatalf("Action = %v, want RouteMove", got.Action)
	}
	if len(got.AssetWarnings) != 1 {
		t.Fatalf("len(AssetWarnings) = %d, want 1", len(got.AssetWarnings))
	}
	w := got.AssetWarnings[0]
	if !w.TargetExists {
		t.Fatalf("TargetExists = false, want true (file present at %s)",
			filepath.Join(dstAssetDir, "foo.png"))
	}
}

// TestRoute_Noop_NoAssetWarnings asserts that when Action == RouteNoop the
// router emits no AssetWarnings even if the body has relative refs (the
// paths are unchanged, so nothing could break).
func TestRoute_Noop_NoAssetWarnings(t *testing.T) {
	r := Router{}
	local := "wiki/Linux & DevOps/foo.md"
	body := []byte("![](assets/foo.png)\n[link](other/bar.md)\n")
	got := r.Route(DevOps, local, body)
	if got.Action != RouteNoop {
		t.Fatalf("Action = %v, want RouteNoop", got.Action)
	}
	if len(got.AssetWarnings) != 0 {
		t.Fatalf("AssetWarnings = %v, want empty for noop", got.AssetWarnings)
	}
}

// TestRoute_QuantFinance_CanonicalRouting asserts that even though
// quant-finance is reserved-empty, Route still resolves to its canonical
// folder (Requirement 1.3).
func TestRoute_QuantFinance_CanonicalRouting(t *testing.T) {
	r := Router{}
	local := "legacy/Financial Models/bond.md"
	got := r.Route(QuantFinance, local, nil)
	if got.Action != RouteMove {
		t.Fatalf("Action = %v, want RouteMove", got.Action)
	}
	if got.TargetPath != "wiki/Quant Finance/bond.md" {
		t.Fatalf("TargetPath = %q, want %q", got.TargetPath, "wiki/Quant Finance/bond.md")
	}
}

// TestRoute_RepeatedRefs covers a body with the same reference twice — we
// accept per-occurrence warnings (the spec says "per reference"; per-
// occurrence is the simpler stable behavior).
func TestRoute_RepeatedRefs(t *testing.T) {
	r := Router{}
	local := "legacy/Hosting/foo.md"
	body := []byte("![](assets/foo.png)\n![](assets/foo.png)\n")
	got := r.Route(DevOps, local, body)
	if len(got.AssetWarnings) < 1 {
		t.Fatalf("len(AssetWarnings) = %d, want >= 1", len(got.AssetWarnings))
	}
	for _, w := range got.AssetWarnings {
		if w.Reference != "assets/foo.png" {
			t.Fatalf("Reference = %q, want %q", w.Reference, "assets/foo.png")
		}
	}
}

// sortWarningsByRef is a test helper exported on RouteDecision via a
// receiver below so the test file can keep deterministic ordering across
// asset-scan tests without leaking ordering guarantees into the public
// contract.
func (d *RouteDecision) sortWarningsByRef() {
	sort.SliceStable(d.AssetWarnings, func(i, j int) bool {
		return d.AssetWarnings[i].Reference < d.AssetWarnings[j].Reference
	})
}

// TestRoute_AnchorRefs_NotClassifiedAsAssetWarning_BugFix asserts that
// markdown intra-document fragment links (`[Section](#anchor-name)`) and
// inline `<a href="#x">` style refs are NOT classified as relative asset
// references. They resolve within the same document regardless of file
// location, so the router must not emit AssetWarnings for them.
//
// Bug surfaced by the real wiki migration of
// `wiki/Linux & DevOps/Docker explained and illustrated.md`: its
// auto-generated TOC contained 10 `[Section](#anchor)` links, all of which
// were incorrectly classified as broken asset refs, polluting stderr with
// 10 spurious warnings per file.
func TestRoute_AnchorRefs_NotClassifiedAsAssetWarning_BugFix(t *testing.T) {
	r := Router{}
	local := "legacy/Hosting/foo.md"
	body := []byte(strings.Join([]string{
		"- [How Docker works](#how-docker-works)",
		"- [Top 3 security concerns](#top-3-security-concerns)",
		"- [Handy snippets](#handy-snippets)",
		"See [this section](#privileged-workstations-or-servers) for details.",
	}, "\n"))
	got := r.Route(DevOps, local, body)
	if len(got.AssetWarnings) != 0 {
		t.Fatalf("len(AssetWarnings) = %d, want 0 (anchors must NOT be classified as asset refs); warnings=%+v",
			len(got.AssetWarnings), got.AssetWarnings)
	}
}

// TestRoute_TargetExistsProbe_UsesRepoPath_WhenSet_BugFix asserts that when
// the router is constructed with a repoPath (via NewRouter), the
// TargetExists probe resolves the new asset location against that root —
// not against the process working directory.
//
// Bug surfaced by the real wiki migration: assets had been pre-migrated to
// `wiki/Linux & DevOps/attachments/*.png` (present on disk), but the
// router's `os.Stat` ran with a cwd of the pocket-know dev tree, not the
// wiki repo root, so every probe returned false. The Docker note's per-
// entry error report listed 5 phantom "target_exists=false" warnings on
// assets that were verifiably present.
func TestRoute_TargetExistsProbe_UsesRepoPath_WhenSet_BugFix(t *testing.T) {
	repoRoot := t.TempDir()
	// Place an asset at the path the route would resolve to AFTER the move.
	// The file lives under repoRoot at `wiki/Linux & DevOps/attachments/p.png`.
	canonicalAssetDir := filepath.Join(repoRoot, "wiki", "Linux & DevOps", "attachments")
	if err := os.MkdirAll(canonicalAssetDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(canonicalAssetDir, "p.png"), []byte("png"), 0o644); err != nil {
		t.Fatalf("write asset: %v", err)
	}

	// Source file (pre-move) lives at the legacy path; body references the
	// asset relatively as `attachments/p.png` (the post-rewrite shape from
	// cobesy).
	local := "wiki/Automation (DevOps)/Containers/note.md"
	body := []byte("![](attachments/p.png)\n")

	r := NewRouter(repoRoot)
	got := r.Route(DevOps, local, body)

	if len(got.AssetWarnings) != 1 {
		t.Fatalf("len(AssetWarnings) = %d, want 1; warnings=%+v",
			len(got.AssetWarnings), got.AssetWarnings)
	}
	if !got.AssetWarnings[0].TargetExists {
		t.Errorf("TargetExists = false; want true (asset at %q is present and probe must resolve against repoPath %q)",
			got.AssetWarnings[0].NewResolved, repoRoot)
	}
}

// TestRoute_DefaultRouter_FallsBackToCwdProbe asserts backward
// compatibility: a `Router{}` instantiated without repoPath still works
// (existing call sites that haven't migrated to NewRouter, and the older
// tests, must keep functioning). The probe falls back to cwd-relative
// os.Stat, which returns false for synthetic test paths.
func TestRoute_DefaultRouter_FallsBackToCwdProbe(t *testing.T) {
	r := Router{}
	local := "legacy/Hosting/foo.md"
	body := []byte("![](assets/foo.png)\n")
	got := r.Route(DevOps, local, body)
	if len(got.AssetWarnings) != 1 {
		t.Fatalf("len(AssetWarnings) = %d, want 1", len(got.AssetWarnings))
	}
	if got.AssetWarnings[0].TargetExists {
		t.Errorf("TargetExists = true for a synthetic path; want false (no real asset on disk)")
	}
}
