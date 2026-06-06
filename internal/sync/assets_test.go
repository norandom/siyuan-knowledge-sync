package sync

import (
	"reflect"
	"strings"
	"testing"
)

func TestExtractAssetRefs_FiltersAndDedupes(t *testing.T) {
	body := []byte(strings.Join([]string{
		"![a](attachments/a.png)",
		"![b](attachments/b.png)",
		"![a-again](attachments/a.png)",                // dup -> single entry
		"[external](https://example.com/x.png)",        // skip
		"[mail](mailto:foo@example.com)",               // skip
		"[abs](/absolute/foo.png)",                     // skip
		"[home](~/Pictures/foo.png)",                   // skip
		"[anchor](#how-it-works)",                      // skip (bug 3 territory)
		"[wiki](<some-wiki-link>)",                     // skip (<-prefixed)
		"![rel-no-prefix](sibling.png)",                // keep (relative)
		"[link-not-image](deeper/dir/diagram.svg)",     // keep
		"[cross-doc](Other Doc.md)",                    // skip (.md cross-link, not asset)
		"[encoded](Zero%20Trust.md)",                   // skip (.md cross-link)
		"[truncated](Kubernetes,%20Istio%20and%20Knative%20(2020", // skip (regex truncated at `(`, not a whitelisted ext)
		"![video](attachments/talk.mp4)",               // keep (mp4 whitelisted)
		"[archive](attachments/setup.zip)",             // keep (zip whitelisted)
	}, "\n"))
	got := extractAssetRefs(body)
	want := []string{
		"attachments/a.png",
		"attachments/b.png",
		"sibling.png",
		"deeper/dir/diagram.svg",
		"attachments/talk.mp4",
		"attachments/setup.zip",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("extractAssetRefs:\n  got:  %v\n  want: %v", got, want)
	}
}

func TestRewriteAssetRefs_ReplacesOnlyInsideParens(t *testing.T) {
	body := []byte(strings.Join([]string{
		"![one](attachments/a.png)",
		"Some prose mentioning attachments/a.png in plain text — must NOT be rewritten.",
		"![two](attachments/b.png)",
		"[link](attachments/a.png \"title\")", // titled link: not handled, leave alone
	}, "\n"))
	mapping := map[string]string{
		"attachments/a.png": "assets/a-20260606-x1.png",
		"attachments/b.png": "assets/b-20260606-x2.png",
	}
	got := string(rewriteAssetRefs(body, mapping))

	// Image and link refs inside `(...)` are rewritten.
	if !strings.Contains(got, "(assets/a-20260606-x1.png)") {
		t.Errorf("expected attachments/a.png replaced in the ![one] ref; got:\n%s", got)
	}
	if !strings.Contains(got, "(assets/b-20260606-x2.png)") {
		t.Errorf("expected attachments/b.png replaced in the ![two] ref; got:\n%s", got)
	}
	// Plain-text mention untouched.
	if !strings.Contains(got, "Some prose mentioning attachments/a.png in plain text") {
		t.Errorf("plain-text mention of attachments/a.png MUST be preserved; got:\n%s", got)
	}
	// Titled link not matched by the naive `(old)` replacement.
	if !strings.Contains(got, `(attachments/a.png "title")`) {
		t.Errorf("titled link form preserved verbatim (out of scope for v1); got:\n%s", got)
	}
}

func TestExtractAssetRefs_EmptyBody(t *testing.T) {
	if got := extractAssetRefs(nil); got != nil {
		t.Errorf("nil body -> got %v, want nil", got)
	}
	if got := extractAssetRefs([]byte("")); got != nil {
		t.Errorf("empty body -> got %v, want nil", got)
	}
}
