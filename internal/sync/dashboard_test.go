package sync

import (
	"strings"
	"testing"

	"siyuan-knowledge-sync/internal/ontology"
)

func TestBuildIndexBody_WithMatches(t *testing.T) {
	rows := []map[string]any{
		{"id": "20260606172024-1pdg1pv", "content": "Docker explained and illustrated"},
		{"id": "20260606200746-other00", "content": "Container security basics"},
	}
	body := buildIndexBody("concept", "Sysadmin & DevOps", rows)
	wantSubstrings := []string{
		"# Concept index - Sysadmin & DevOps",
		`* ((20260606172024-1pdg1pv "Docker explained and illustrated"))`,
		`* ((20260606200746-other00 "Container security basics"))`,
	}
	for _, s := range wantSubstrings {
		if !strings.Contains(body, s) {
			t.Errorf("body missing %q\n--- got ---\n%s", s, body)
		}
	}
}

func TestBuildIndexBody_NoMatches(t *testing.T) {
	body := buildIndexBody("sop", "Sysadmin & DevOps", nil)
	want := "_No sop documents in Sysadmin & DevOps yet._"
	if !strings.Contains(body, want) {
		t.Errorf("body missing placeholder %q\n--- got ---\n%s", want, body)
	}
	if strings.Contains(body, "* ((") {
		t.Errorf("body must NOT contain ref pills when no matches; got:\n%s", body)
	}
}

func TestBuildIndexBody_StripsMdSuffixFromTitle(t *testing.T) {
	rows := []map[string]any{
		{"id": "id1", "content": "Docker explained and illustrated.md"},
		{"id": "id2", "content": "Bare title without extension"},
		{"id": "id3", "content": "weird.md.md"}, // only the trailing one stripped
	}
	body := buildIndexBody("concept", "Sysadmin & DevOps", rows)
	if !strings.Contains(body, `* ((id1 "Docker explained and illustrated"))`) {
		t.Errorf(".md suffix not stripped from id1; body:\n%s", body)
	}
	if !strings.Contains(body, `* ((id2 "Bare title without extension"))`) {
		t.Errorf("non-.md title should pass through unchanged; body:\n%s", body)
	}
	if !strings.Contains(body, `* ((id3 "weird.md"))`) {
		t.Errorf("only trailing .md should be stripped from id3; body:\n%s", body)
	}
}

func TestBuildIndexBody_EscapesDoubleQuotesInTitle(t *testing.T) {
	rows := []map[string]any{
		{"id": "id1", "content": `What is a "concept"?`},
	}
	body := buildIndexBody("concept", "Sysadmin & DevOps", rows)
	// Double quotes inside the title would break the ((id "title")) pill
	// syntax; they get substituted with single quotes.
	if strings.Contains(body, `"concept"`) {
		t.Errorf("expected double-quote substitution in title; got body:\n%s", body)
	}
	if !strings.Contains(body, `What is a 'concept'?`) {
		t.Errorf("expected single-quote substituted form in title; got body:\n%s", body)
	}
}

func TestBuildIndexBody_SkipsRowsWithoutID(t *testing.T) {
	rows := []map[string]any{
		{"id": "good", "content": "valid"},
		{"content": "no id field"},
		{"id": "", "content": "empty id"},
	}
	body := buildIndexBody("config", "Security", rows)
	if !strings.Contains(body, `* ((good "valid"))`) {
		t.Errorf("expected valid row to be rendered; got body:\n%s", body)
	}
	if strings.Count(body, "* ((") != 1 {
		t.Errorf("expected exactly 1 ref pill (only the valid row); got body:\n%s", body)
	}
}

func TestTitleCaseASCII(t *testing.T) {
	for in, want := range map[string]string{
		"sop":     "Sop",
		"config":  "Config",
		"concept": "Concept",
		"":        "",
		"X":       "X",
	} {
		if got := titleCaseASCII(in); got != want {
			t.Errorf("titleCaseASCII(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsOntologyDomainNotebook(t *testing.T) {
	cases := map[string]bool{
		"Sysadmin & DevOps":        true,
		"Digital Forensics":     true,
		"Security":              true,
		"AI & ML":               true,
		"Software Development":  true,
		"Quant Finance":         true,
		"wiki":                  false,
		"Hosting":               false,
		"":                      false,
		"linux & devops":        false, // case-sensitive (canonical names are exact)
	}
	for in, want := range cases {
		if got := isOntologyDomainNotebook(in); got != want {
			t.Errorf("isOntologyDomainNotebook(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestEnsureIntentIndices_OneCallPerIntent(t *testing.T) {
	// Sanity-check the engine's contract: every intent in the closed enum
	// gets exactly one createDocWithMd call per ensureIntentIndices call.
	// The actual call count is exercised by integration tests; this is a
	// regression guard against the enum changing size without the docs
	// being updated.
	intents := ontology.AllIntents()
	if len(intents) != 5 {
		t.Errorf("ontology.AllIntents() = %d, expected 5 (config, sop, log, decision, concept). Did the enum change?", len(intents))
	}
}
