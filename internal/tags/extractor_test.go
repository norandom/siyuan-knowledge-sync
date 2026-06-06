package tags

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

func tagsEqual(a, b map[string]string) bool {
	return reflect.DeepEqual(a, b)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func TestExtract_FrontmatterListSyntax(t *testing.T) {
	content := []byte(`---
tags: [tag1, tag2, tag3]
---

# My Document

Some content here.
`)

	extractor := NewTagExtractor()
	result, err := extractor.Extract(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := map[string]string{
		"custom-tag1": "",
		"custom-tag2": "",
		"custom-tag3": "",
	}
	if !tagsEqual(result, expected) {
		t.Errorf("expected %v, got %v", expected, result)
	}
}

func TestExtract_FrontmatterBlockSequence(t *testing.T) {
	content := []byte(`---
tags:
  - tag1
  - tag2
  - tag3
---

# Document
`)

	extractor := NewTagExtractor()
	result, err := extractor.Extract(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := map[string]string{
		"custom-tag1": "",
		"custom-tag2": "",
		"custom-tag3": "",
	}
	if !tagsEqual(result, expected) {
		t.Errorf("expected %v, got %v", expected, result)
	}
}

func TestExtract_InlineTags(t *testing.T) {
	content := []byte(`# My Document

Some body text with #mytag and another #second-tag here.
`)

	extractor := NewTagExtractor()
	result, err := extractor.Extract(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := map[string]string{
		"custom-mytag":      "",
		"custom-second-tag": "",
	}
	if !tagsEqual(result, expected) {
		t.Errorf("expected %v, got %v", expected, result)
	}
}

func TestExtract_EmptyMarkdown(t *testing.T) {
	extractor := NewTagExtractor()
	result, err := extractor.Extract([]byte(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("expected empty map, got %v", result)
	}
}

func TestExtract_NoFrontmatter(t *testing.T) {
	content := []byte(`# Document without frontmatter

Some text here.
`)

	extractor := NewTagExtractor()
	result, err := extractor.Extract(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("expected empty map, got %v", result)
	}
}

func TestExtract_NoTags(t *testing.T) {
	content := []byte(`---
title: My Note
date: 2026-01-01
---

# My Note

Some content without any tags.
`)

	extractor := NewTagExtractor()
	result, err := extractor.Extract(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("expected empty map, got %v", result)
	}
}

func TestExtract_BothFrontmatterAndInline(t *testing.T) {
	content := []byte(`---
tags: [tag1, tag2]
---

# Document

Here is some inline #tag3 text.
`)

	extractor := NewTagExtractor()
	result, err := extractor.Extract(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := map[string]string{
		"custom-tag1": "",
		"custom-tag2": "",
		"custom-tag3": "",
	}
	if !tagsEqual(result, expected) {
		t.Errorf("expected %v, got %v", expected, result)
	}
}

func TestExtract_DuplicateTags(t *testing.T) {
	content := []byte(`---
tags: [mytag]
---

# Document

This has #mytag repeated in body.
`)

	extractor := NewTagExtractor()
	result, err := extractor.Extract(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 1 {
		t.Errorf("expected 1 tag, got %d: %v", len(result), result)
	}
	if _, ok := result["custom-mytag"]; !ok {
		t.Errorf("expected custom-mytag to be present, got %v", result)
	}
}

func TestExtract_TagsInCodeBlockIgnored(t *testing.T) {
	content := []byte(`# Document

` + "```" + `
#code-tag should be ignored
` + "```" + `

Real #valid-tag here.
`)

	extractor := NewTagExtractor()
	result, err := extractor.Extract(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := result["custom-code-tag"]; ok {
		t.Errorf("code block tag should be ignored")
	}
	if _, ok := result["custom-valid-tag"]; !ok {
		t.Errorf("valid tag outside code block should be present")
	}
}

func TestExtract_TagsInInlineCodeIgnored(t *testing.T) {
	content := []byte("This has `#ignored` inline code and #real tag.\n")

	extractor := NewTagExtractor()
	result, err := extractor.Extract(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := result["custom-ignored"]; ok {
		t.Errorf("inline code tag should be ignored")
	}
	if _, ok := result["custom-real"]; !ok {
		t.Errorf("real tag should be present, got %v", sortedKeys(result))
	}
}

func TestExtract_SpecialCharsInTagNames(t *testing.T) {
	content := []byte(`---
tags: [my_tag, my-tag, tag123, MIXEDCase]
---

`)

	extractor := NewTagExtractor()
	result, err := extractor.Extract(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := map[string]string{
		"custom-my_tag":    "",
		"custom-my-tag":    "",
		"custom-tag123":    "",
		"custom-mixedcase": "",
	}
	if !tagsEqual(result, expected) {
		t.Errorf("expected %v, got %v", expected, result)
	}
}

func TestExtract_SingleTagInFrontmatter(t *testing.T) {
	content := []byte(`---
tags: single-tag
---

# Doc
`)

	extractor := NewTagExtractor()
	result, err := extractor.Extract(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := map[string]string{
		"custom-single-tag": "",
	}
	if !tagsEqual(result, expected) {
		t.Errorf("expected %v, got %v", expected, result)
	}
}

func TestExtract_EmptyTagsList(t *testing.T) {
	content := []byte(`---
tags: []
---

# Doc
`)

	extractor := NewTagExtractor()
	result, err := extractor.Extract(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("expected empty map, got %v", result)
	}
}

func TestExtract_AllKeysHaveCustomPrefix(t *testing.T) {
	content := []byte(`---
tags: [a, b, c]
---

#d #e #f
`)

	extractor := NewTagExtractor()
	result, err := extractor.Extract(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for k := range result {
		if len(k) < 7 || k[:7] != "custom-" {
			t.Errorf("key %q does not have custom- prefix", k)
		}
	}
}

func TestExtract_FrontmatterSingleString(t *testing.T) {
	content := []byte(`---
tags: some-tag
---

# Doc
`)

	extractor := NewTagExtractor()
	result, err := extractor.Extract(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := result["custom-some-tag"]; !ok {
		t.Errorf("expected custom-some-tag to be present, got %v", sortedKeys(result))
	}
}

func TestExtract_MultipleInlineSameLine(t *testing.T) {
	content := []byte("#tag1 #tag2 #tag3 on same line.\n")

	extractor := NewTagExtractor()
	result, err := extractor.Extract(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := map[string]string{
		"custom-tag1": "",
		"custom-tag2": "",
		"custom-tag3": "",
	}
	if !tagsEqual(result, expected) {
		t.Errorf("expected %v, got %v", expected, result)
	}
}

func TestExtract_WindowsLineEndings(t *testing.T) {
	content := []byte("---\r\ntags:\r\n  - win-tag\r\n---\r\n\r\n# Doc\r\n")

	extractor := NewTagExtractor()
	result, err := extractor.Extract(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := result["custom-win-tag"]; !ok {
		t.Errorf("expected custom-win-tag to be present, got %v", sortedKeys(result))
	}
}

func TestExtract_LeadingWhitespaceBeforeFrontmatter(t *testing.T) {
	content := []byte("\n\n---\ntags: [ws-tag]\n---\n\n# Doc\n")

	extractor := NewTagExtractor()
	result, err := extractor.Extract(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := result["custom-ws-tag"]; !ok {
		t.Errorf("expected custom-ws-tag to be present, got %v", sortedKeys(result))
	}
}

// --- ExtractMeta (task 7.3) ---

func TestExtractMeta_FrontmatterTitleAndTags(t *testing.T) {
	content := []byte(`---
title: My Frontmatter Title
tags: [tag1, tag2]
---

# Heading

Body with #tag3 here.
`)

	extractor := NewTagExtractor()
	meta, err := extractor.ExtractMeta(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if meta.Title != "My Frontmatter Title" {
		t.Errorf("expected title %q, got %q", "My Frontmatter Title", meta.Title)
	}
	if bytesContains(meta.Body, "---") {
		t.Errorf("expected body to have frontmatter block removed, got %q", string(meta.Body))
	}
	if !bytesContains(meta.Body, "# Heading") {
		t.Errorf("expected body to retain content, got %q", string(meta.Body))
	}

	expectedAttrs := map[string]string{
		"custom-tag1": "",
		"custom-tag2": "",
		"custom-tag3": "",
		"tags":        "tag1,tag2,tag3",
	}
	if !tagsEqual(meta.Attrs, expectedAttrs) {
		t.Errorf("expected attrs %v, got %v", expectedAttrs, meta.Attrs)
	}

	extractResult, err := extractor.Extract(content)
	if err != nil {
		t.Fatalf("unexpected Extract error: %v", err)
	}
	if !tagsEqual(stripExtractMetaInjections(meta.Attrs), extractResult) {
		t.Errorf("Attrs drifted from Extract: meta=%v extract=%v", meta.Attrs, extractResult)
	}
}

// stripExtractMetaInjections returns m with ExtractMeta-only keys removed
// (custom-domain, custom-intent, custom-last-updated, tags). Used by drift
// guards that compare Extract vs ExtractMeta — the injected keys are
// deliberate extensions, not drift.
func stripExtractMetaInjections(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		switch k {
		case "tags", "custom-domain", "custom-intent", "custom-last-updated":
			continue
		}
		out[k] = v
	}
	return out
}

func TestExtractMeta_NoFrontmatter(t *testing.T) {
	content := []byte(`# Document without frontmatter

Some body text with #inlinetag here.
`)

	extractor := NewTagExtractor()
	meta, err := extractor.ExtractMeta(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if meta.Title != "" {
		t.Errorf("expected empty title, got %q", meta.Title)
	}
	if !reflect.DeepEqual(meta.Body, content) {
		t.Errorf("expected body to equal input when no frontmatter, got %q", string(meta.Body))
	}

	expectedAttrs := map[string]string{
		"custom-inlinetag": "",
		"tags":             "inlinetag",
	}
	if !tagsEqual(meta.Attrs, expectedAttrs) {
		t.Errorf("expected attrs %v, got %v", expectedAttrs, meta.Attrs)
	}

	extractResult, err := extractor.Extract(content)
	if err != nil {
		t.Fatalf("unexpected Extract error: %v", err)
	}
	if !tagsEqual(stripExtractMetaInjections(meta.Attrs), extractResult) {
		t.Errorf("Attrs drifted from Extract: meta=%v extract=%v", meta.Attrs, extractResult)
	}
}

func TestExtractMeta_FrontmatterWithoutTitle(t *testing.T) {
	content := []byte(`---
tags: [only-tag]
date: 2026-01-01
---

# Heading
`)

	extractor := NewTagExtractor()
	meta, err := extractor.ExtractMeta(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if meta.Title != "" {
		t.Errorf("expected empty title when frontmatter has no title, got %q", meta.Title)
	}
	if bytesContains(meta.Body, "---") {
		t.Errorf("expected body to have frontmatter block removed, got %q", string(meta.Body))
	}

	expectedAttrs := map[string]string{
		"custom-only-tag":     "",
		"custom-last-updated": "2026-01-01",
		"tags":                "only-tag",
	}
	if !tagsEqual(meta.Attrs, expectedAttrs) {
		t.Errorf("expected attrs %v, got %v", expectedAttrs, meta.Attrs)
	}
}

func TestExtractMeta_NonScalarTitle(t *testing.T) {
	content := []byte(`---
title:
  - not
  - a
  - scalar
tags: [t1]
---

# Heading
`)

	extractor := NewTagExtractor()
	meta, err := extractor.ExtractMeta(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if meta.Title != "" {
		t.Errorf("expected empty title for non-scalar title, got %q", meta.Title)
	}
}

func TestExtractMeta_MalformedFrontmatter(t *testing.T) {
	content := []byte(`---
title: "unterminated
tags: [t1, t2
---

# Heading
`)

	extractor := NewTagExtractor()
	meta, err := extractor.ExtractMeta(content)
	if err == nil {
		t.Fatalf("expected error for malformed frontmatter, got nil (meta=%+v)", meta)
	}
	if meta.Title != "" || meta.Body != nil || meta.Attrs != nil {
		t.Errorf("expected zero Meta on error, got %+v", meta)
	}
}

func TestExtractMeta_AttrsMatchExtract_DriftGuard(t *testing.T) {
	cases := [][]byte{
		[]byte("---\ntitle: T\ntags: [a, b, c]\n---\n\n# Doc\n\n#d #e in body.\n"),
		[]byte("# No frontmatter\n\nJust #inline and #more tags.\n"),
		[]byte("---\ntags:\n  - x\n  - y\n---\n\nBody #z here.\n"),
		[]byte("---\ntitle: Only Title\n---\n\nNo tags at all.\n"),
		[]byte(""),
		[]byte("Plain text, no tags, no frontmatter.\n"),
		[]byte("---\ntags: single\n---\n\n# Doc\n"),
	}

	extractor := NewTagExtractor()
	for i, content := range cases {
		meta, metaErr := extractor.ExtractMeta(content)
		extractResult, extractErr := extractor.Extract(content)

		if (metaErr == nil) != (extractErr == nil) {
			t.Errorf("case %d: error mismatch metaErr=%v extractErr=%v", i, metaErr, extractErr)
			continue
		}
		if metaErr != nil {
			continue
		}
		if !tagsEqual(stripExtractMetaInjections(meta.Attrs), extractResult) {
			t.Errorf("case %d: Attrs drift: meta=%v extract=%v", i, meta.Attrs, extractResult)
		}
	}
}

func bytesContains(b []byte, sub string) bool {
	return strings.Contains(string(b), sub)
}

// --- ontology-gate task 1.2: Domain/Intent surfacing + custom-attr injection ---

func TestExtractMeta_DomainAndIntent_BothPresent(t *testing.T) {
	content := []byte(`---
title: Doc
domain: devops
intent: sop
tags: [tag1]
---

# Heading
`)

	extractor := NewTagExtractor()
	meta, err := extractor.ExtractMeta(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if meta.Domain != "devops" {
		t.Errorf("expected Meta.Domain == %q, got %q", "devops", meta.Domain)
	}
	if meta.Intent != "sop" {
		t.Errorf("expected Meta.Intent == %q, got %q", "sop", meta.Intent)
	}
	if got, ok := meta.Attrs["custom-domain"]; !ok || got != "devops" {
		t.Errorf("expected Attrs[custom-domain]=%q, got %q (present=%v)", "devops", got, ok)
	}
	if got, ok := meta.Attrs["custom-intent"]; !ok || got != "sop" {
		t.Errorf("expected Attrs[custom-intent]=%q, got %q (present=%v)", "sop", got, ok)
	}
	if _, ok := meta.Attrs["custom-tag1"]; !ok {
		t.Errorf("expected existing custom-tag1 to still be present, got %v", sortedKeys(meta.Attrs))
	}
}

func TestExtractMeta_DomainOnly_NoIntent(t *testing.T) {
	content := []byte(`---
domain: devops
---

# Doc
`)

	extractor := NewTagExtractor()
	meta, err := extractor.ExtractMeta(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if meta.Domain != "devops" {
		t.Errorf("expected Meta.Domain == %q, got %q", "devops", meta.Domain)
	}
	if meta.Intent != "" {
		t.Errorf("expected Meta.Intent == \"\" when intent key absent, got %q", meta.Intent)
	}
	if got, ok := meta.Attrs["custom-domain"]; !ok || got != "devops" {
		t.Errorf("expected Attrs[custom-domain]=%q, got %q (present=%v)", "devops", got, ok)
	}
	if _, ok := meta.Attrs["custom-intent"]; ok {
		t.Errorf("expected Attrs[custom-intent] to be absent when intent key missing, got map=%v", meta.Attrs)
	}
}

func TestExtractMeta_NeitherDomainNorIntent_BehaviorPreserved(t *testing.T) {
	content := []byte(`---
title: T
tags: [tag1]
---

# Doc
`)

	extractor := NewTagExtractor()
	meta, err := extractor.ExtractMeta(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if meta.Domain != "" {
		t.Errorf("expected empty Meta.Domain when absent, got %q", meta.Domain)
	}
	if meta.Intent != "" {
		t.Errorf("expected empty Meta.Intent when absent, got %q", meta.Intent)
	}
	if _, ok := meta.Attrs["custom-domain"]; ok {
		t.Errorf("expected Attrs to omit custom-domain when frontmatter has no domain key, got %v", meta.Attrs)
	}
	if _, ok := meta.Attrs["custom-intent"]; ok {
		t.Errorf("expected Attrs to omit custom-intent when frontmatter has no intent key, got %v", meta.Attrs)
	}
	// Existing tag behavior unchanged (plus the new visible `tags` attr).
	expected := map[string]string{"custom-tag1": "", "tags": "tag1"}
	if !tagsEqual(meta.Attrs, expected) {
		t.Errorf("expected attrs %v, got %v", expected, meta.Attrs)
	}
}

func TestExtractMeta_DomainSequence_NotSurfaced(t *testing.T) {
	content := []byte(`---
domain: [devops, forensics]
intent: sop
---

# Doc
`)

	extractor := NewTagExtractor()
	meta, err := extractor.ExtractMeta(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if meta.Domain != "" {
		t.Errorf("expected Meta.Domain == \"\" for non-scalar (sequence) domain, got %q", meta.Domain)
	}
	if _, ok := meta.Attrs["custom-domain"]; ok {
		t.Errorf("expected Attrs to omit custom-domain when domain is a sequence, got %v", meta.Attrs)
	}
	// intent scalar still surfaces normally.
	if meta.Intent != "sop" {
		t.Errorf("expected Meta.Intent == %q, got %q", "sop", meta.Intent)
	}
	if got, ok := meta.Attrs["custom-intent"]; !ok || got != "sop" {
		t.Errorf("expected Attrs[custom-intent]=%q, got %q (present=%v)", "sop", got, ok)
	}
}

func TestExtractMeta_ArbitraryIntentScalar_NoEnumCheckInParser(t *testing.T) {
	content := []byte(`---
intent: braindump
---

# Doc
`)

	extractor := NewTagExtractor()
	meta, err := extractor.ExtractMeta(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if meta.Intent != "braindump" {
		t.Errorf("expected Meta.Intent == %q (parser must not enum-validate), got %q", "braindump", meta.Intent)
	}
	if got, ok := meta.Attrs["custom-intent"]; !ok || got != "braindump" {
		t.Errorf("expected Attrs[custom-intent]=%q, got %q (present=%v)", "braindump", got, ok)
	}
}

func TestExtract_DoesNotInjectDomainOrIntent_AuditPathUnchanged(t *testing.T) {
	// Regression: legacy Extract entry point (consumed by internal/compliance/audit.go)
	// must NOT include custom-domain or custom-intent. Injection is ExtractMeta-only.
	content := []byte(`---
domain: devops
intent: sop
tags: [foo, bar]
---

# Doc
`)

	extractor := NewTagExtractor()
	result, err := extractor.Extract(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := map[string]string{
		"custom-foo": "",
		"custom-bar": "",
	}
	if !tagsEqual(result, expected) {
		t.Errorf("Extract leaked domain/intent or other keys.\n  expected: %v\n  got:      %v", expected, result)
	}
	if _, ok := result["custom-domain"]; ok {
		t.Errorf("Extract must NOT include custom-domain (audit path), got %v", result)
	}
	if _, ok := result["custom-intent"]; ok {
		t.Errorf("Extract must NOT include custom-intent (audit path), got %v", result)
	}
}

// TestNormalizeTag_StripsHashtagAndInvalidChars_BugFix asserts that
// normalizeTag produces SiYuan-acceptable attribute name suffixes. SiYuan
// rejects setBlockAttrs atomically when ANY key contains a character outside
// [a-zA-Z0-9_-] after the `custom-` prefix; a single rogue key nukes the
// whole multi-attr call, including the file's `custom-domain` and
// `custom-intent`. The original siyuan-knowledge-sync Req 13 e2e fixtures
// used bare tag values (no `#` prefix), so this never tripped until the
// folder-by-folder wiki migration ran against real frontmatter containing
// `tags: ['#docker', '#supply-chain', ...]`.
func TestNormalizeTag_StripsHashtagAndInvalidChars_BugFix(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"hashtag-prefix simple", "#docker", "docker"},
		{"hashtag-prefix with hyphens", "#supply-chain", "supply-chain"},
		{"hashtag-prefix with underscore", "#my_tag", "my_tag"},
		{"multiple leading hashtags", "##weird", "weird"},
		{"hashtag-only collapses to empty", "#", ""},
		{"mixed case lowercased", "#MIXED_Case", "mixed_case"},
		{"spaces become hyphens then hashtag stripped", "#tag with spaces", "tag-with-spaces"},
		{"embedded hash dropped", "tag#middle", "tagmiddle"},
		{"punctuation stripped", "tag.with/punct", "tagwithpunct"},
		{"colon stripped", "ns:tag", "nstag"},
		{"plus stripped", "c++", "c"},
		{"already valid passthrough", "my-tag_123", "my-tag_123"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeTag(tc.in)
			if got != tc.want {
				t.Errorf("normalizeTag(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestExtractMeta_HashtagFrontmatterTags_ProducesValidAttrKeys is the
// integration-level guard: the wiki migration's real input shape
// (YAML tags as quoted hashtag strings) MUST round-trip through ExtractMeta
// into attr keys that SiYuan accepts. Captures the exact frontmatter shape
// from `Linux & DevOps/Docker explained and illustrated.md`.
func TestExtractMeta_HashtagFrontmatterTags_ProducesValidAttrKeys(t *testing.T) {
	content := []byte(`---
last_updated: '2024-01-28T07:14:56.539000+00:00'
tags:
  - '#docker'
  - '#linux'
  - '#containers'
  - '#software-composition-analysis'
  - '#supply-chain'
  - '#secrets-management'
  - '#least-privilege'
  - '#application-security'
domain: devops
intent: concept
---

# Body
`)

	extractor := NewTagExtractor()
	meta, err := extractor.ExtractMeta(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := map[string]string{
		"custom-docker":                        "",
		"custom-linux":                         "",
		"custom-containers":                    "",
		"custom-software-composition-analysis": "",
		"custom-supply-chain":                  "",
		"custom-secrets-management":            "",
		"custom-least-privilege":               "",
		"custom-application-security":          "",
		"custom-domain":                        "devops",
		"custom-intent":                        "concept",
		"custom-last-updated":                  "2024-01-28T07:14:56.539000+00:00",
		"tags":                                 "application-security,containers,docker,least-privilege,linux,secrets-management,software-composition-analysis,supply-chain",
	}
	if !tagsEqual(meta.Attrs, want) {
		t.Errorf("attrs mismatch\n  got:  %v\n  want: %v", meta.Attrs, want)
	}

	// Belt-and-braces: every key must be SiYuan-acceptable: `custom-` prefix
	// + [a-z0-9_-]+ suffix. A single rogue key would atomically nuke the
	// whole setBlockAttrs call on the real SiYuan API. The `tags` key is
	// an exception — SiYuan recognizes it natively (drives the visible
	// chip rendering), so it's allowed without the `custom-` prefix.
	const prefix = "custom-"
	for k := range meta.Attrs {
		if k == "tags" {
			continue
		}
		if !strings.HasPrefix(k, prefix) {
			t.Errorf("key %q lacks custom- prefix", k)
			continue
		}
		suffix := strings.TrimPrefix(k, prefix)
		if suffix == "" {
			t.Errorf("key %q has empty suffix after custom-", k)
		}
		for _, r := range suffix {
			ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-'
			if !ok {
				t.Errorf("key %q has SiYuan-invalid char %q in suffix", k, r)
				break
			}
		}
	}
}
