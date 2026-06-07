package compliance

import (
	"strings"
	"testing"

	"siyuan-knowledge-sync/internal/ontology"
	"siyuan-knowledge-sync/internal/types"
)

// configureWithTags is a small helper that calls ontology.Configure with the
// canonical six-domain / five-intent default ontology and the supplied tag
// vocabulary, then registers a t.Cleanup that resets the ontology package
// state back to the compile-time defaults so subtests stay independent.
func configureWithTags(t *testing.T, tags []string) {
	t.Helper()
	opts := ontology.ConfigureOptions{
		Domains: []ontology.ConfigureDomain{
			{ID: "devops", Folder: "Linux & DevOps"},
			{ID: "forensics", Folder: "Digital Forensics"},
			{ID: "security", Folder: "Security"},
			{ID: "ai-ml", Folder: "AI & ML"},
			{ID: "software-dev", Folder: "Software Development"},
			{ID: "quant-finance", Folder: "Quant Finance"},
		},
		Intents: []ontology.ConfigureIntent{
			{ID: "config"},
			{ID: "sop"},
			{ID: "log"},
			{ID: "decision"},
			{ID: "concept"},
		},
		Tags: tags,
	}
	if err := ontology.Configure(opts); err != nil {
		t.Fatalf("ontology.Configure: %v", err)
	}
	t.Cleanup(func() {
		ontology.ResetForTest()
	})
}

// countTagVocabIssues returns the count of schema-category warnings whose
// message indicates an unrecognized-tag violation (i.e. tag-vocab issues only,
// not the ontology missing-key / out-of-enum violations the audit also emits).
func countTagVocabIssues(issues []types.ComplianceIssue) int {
	n := 0
	for _, iss := range issues {
		if iss.Category != "schema" {
			continue
		}
		if iss.Severity != "warning" {
			continue
		}
		if strings.Contains(iss.Message, "unrecognized tag") {
			n++
		}
	}
	return n
}

func tagVocabIssues(issues []types.ComplianceIssue) []types.ComplianceIssue {
	var out []types.ComplianceIssue
	for _, iss := range issues {
		if iss.Category != "schema" {
			continue
		}
		if iss.Severity != "warning" {
			continue
		}
		if strings.Contains(iss.Message, "unrecognized tag") {
			out = append(out, iss)
		}
	}
	return out
}

func TestAudit_TagVocab_SkippedWhenVocabNil(t *testing.T) {
	// No Configure call → vocabulary is nil (open). Audit must not emit any
	// tag-vocabulary warnings regardless of which tags the file declares.
	t.Cleanup(func() { ontology.ResetForTest() })

	content := []byte(`---
domain: devops
intent: sop
tags: [foo, bar]
---

# Doc

Body with #inline-tag.
`)

	engine := NewComplianceEngine(false)
	issues, err := engine.Audit("test.md", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if c := countTagVocabIssues(issues); c != 0 {
		t.Errorf("expected 0 tag-vocab warnings when vocab is nil, got %d", c)
		for _, iss := range tagVocabIssues(issues) {
			t.Logf("  unexpected tag-vocab issue: %s", iss.Message)
		}
	}
}

func TestAudit_TagVocab_EmitsOneWarningPerUnknownTag(t *testing.T) {
	configureWithTags(t, []string{"a", "b"})

	content := []byte(`---
domain: devops
intent: sop
tags: [a, x, b, y]
---

# Doc
`)

	engine := NewComplianceEngine(false)
	issues, err := engine.Audit("test.md", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := tagVocabIssues(issues)
	if len(got) != 2 {
		t.Fatalf("expected exactly 2 tag-vocab warnings for unknown tags x and y, got %d", len(got))
	}

	// Each issue must be a schema-category warning, non-fixable, and reference
	// the offending tag value.
	for _, iss := range got {
		if iss.File != "test.md" {
			t.Errorf("expected File==\"test.md\", got %q", iss.File)
		}
		if iss.Category != "schema" {
			t.Errorf("expected Category==\"schema\", got %q", iss.Category)
		}
		if iss.Severity != "warning" {
			t.Errorf("expected Severity==\"warning\", got %q", iss.Severity)
		}
		if iss.Fixable {
			t.Errorf("expected Fixable==false, got true")
		}
	}

	// Specifically: messages must include the unknown values x and y but NOT
	// the known values a or b.
	gotMessages := make([]string, 0, len(got))
	for _, iss := range got {
		gotMessages = append(gotMessages, iss.Message)
	}
	joined := strings.Join(gotMessages, " | ")
	if !strings.Contains(joined, `"x"`) {
		t.Errorf("expected warnings to reference unknown tag \"x\", got: %s", joined)
	}
	if !strings.Contains(joined, `"y"`) {
		t.Errorf("expected warnings to reference unknown tag \"y\", got: %s", joined)
	}
	for _, known := range []string{`"a"`, `"b"`} {
		for _, msg := range gotMessages {
			if strings.Contains(msg, known) {
				t.Errorf("expected no warning for known tag %s, got message: %s", known, msg)
			}
		}
	}
}

func TestAudit_TagVocab_NoIssuesWhenAllTagsKnown(t *testing.T) {
	configureWithTags(t, []string{"a", "b"})

	content := []byte(`---
domain: devops
intent: sop
tags: [a, b]
---

# Doc
`)

	engine := NewComplianceEngine(false)
	issues, err := engine.Audit("test.md", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if c := countTagVocabIssues(issues); c != 0 {
		t.Errorf("expected 0 tag-vocab warnings when every tag is known, got %d", c)
		for _, iss := range tagVocabIssues(issues) {
			t.Logf("  unexpected tag-vocab issue: %s", iss.Message)
		}
	}
}

func TestAudit_TagVocab_ClosedEmptyVocabRejectsEveryTag(t *testing.T) {
	configureWithTags(t, []string{})

	content := []byte(`---
domain: devops
intent: sop
tags: [foo]
---

# Doc
`)

	engine := NewComplianceEngine(false)
	issues, err := engine.Audit("test.md", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := tagVocabIssues(issues)
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 tag-vocab warning under closed-empty vocab, got %d", len(got))
	}
	if !strings.Contains(got[0].Message, `"foo"`) {
		t.Errorf("expected warning to reference tag \"foo\", got: %s", got[0].Message)
	}
}
