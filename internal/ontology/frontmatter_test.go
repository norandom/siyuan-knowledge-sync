package ontology

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestAddOntology_PreservesTemporalAndCustomKeys is the spec linchpin
// (Req 1.5, 8.1-8.4): every non-ontology key must round-trip byte-identical
// at the YAML-value level (re-Marshal of the value node) after AddOntology
// inserts the two ontology keys.
func TestAddOntology_PreservesTemporalAndCustomKeys(t *testing.T) {
	src := []byte(`---
title: Foo
date: 2024-03-11
lastmod: "2024-03-12T14:00:00Z"
created: 2023-12-01
updated: 2024-01-02
original_date: 2022-06-15
tags: [a, b]
custom_key: 42
'quoted_key': single
---
# Body

Body content here.
`)

	out, err := AddOntology(src, DevOps, IntentSOP)
	if err != nil {
		t.Fatalf("AddOntology returned unexpected error: %v", err)
	}

	srcFM, _ := splitFM(src)
	outFM, outBody := splitFM(out)
	if outFM == nil {
		t.Fatalf("output had no frontmatter, got:\n%s", string(out))
	}

	// Each non-ontology key must serialize to the same bytes pre/post.
	srcMap := mustValueMarshalMap(t, srcFM)
	outMap := mustValueMarshalMap(t, outFM)

	nonOntologyKeys := []string{
		"title", "date", "lastmod", "created", "updated",
		"original_date", "tags", "custom_key", "quoted_key",
	}
	for _, k := range nonOntologyKeys {
		got, gotOK := outMap[k]
		want, wantOK := srcMap[k]
		if !wantOK {
			t.Fatalf("source missing expected key %q in fixture", k)
		}
		if !gotOK {
			t.Fatalf("output dropped non-ontology key %q", k)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("non-ontology key %q value mutated:\n want=%q\n got =%q", k, want, got)
		}
	}

	// Ontology keys must be present with the requested values.
	if outMap["domain"] == nil {
		t.Fatalf("domain key missing from output")
	}
	if outMap["intent"] == nil {
		t.Fatalf("intent key missing from output")
	}
	if got := strings.TrimSpace(string(outMap["domain"])); got != "devops" {
		t.Fatalf("domain value = %q, want devops", got)
	}
	if got := strings.TrimSpace(string(outMap["intent"])); got != "sop" {
		t.Fatalf("intent value = %q, want sop", got)
	}

	// Body bytes after the closing fence are preserved verbatim.
	if !bytes.Contains(outBody, []byte("# Body")) {
		t.Fatalf("body lost its heading: %q", string(outBody))
	}
	if !bytes.Contains(outBody, []byte("Body content here.")) {
		t.Fatalf("body lost its content: %q", string(outBody))
	}
}

// TestAddOntology_Idempotent (Req 1.5): calling AddOntology twice with the
// same enum values must equal calling it once.
func TestAddOntology_Idempotent(t *testing.T) {
	src := []byte(`---
title: Foo
date: 2024-03-11
lastmod: "2024-03-12T14:00:00Z"
tags: [a, b]
---
# H

x
`)

	once, err := AddOntology(src, DevOps, IntentSOP)
	if err != nil {
		t.Fatalf("first AddOntology: %v", err)
	}
	twice, err := AddOntology(once, DevOps, IntentSOP)
	if err != nil {
		t.Fatalf("second AddOntology: %v", err)
	}

	if !bytes.Equal(once, twice) {
		t.Fatalf("AddOntology not idempotent:\n once=\n%s\n twice=\n%s", string(once), string(twice))
	}
}

// TestAddOntology_NoFrontmatter_FreshPrepend (Req 8.5): when the source has no
// frontmatter, prepend a fresh block with only the two ontology keys and no
// synthesized temporal fields.
func TestAddOntology_NoFrontmatter_FreshPrepend(t *testing.T) {
	src := []byte("# Heading\n\nBody.\n")
	out, err := AddOntology(src, DevOps, IntentSOP)
	if err != nil {
		t.Fatalf("AddOntology: %v", err)
	}

	want := []byte("---\ndomain: devops\nintent: sop\n---\n# Heading\n\nBody.\n")
	if !bytes.Equal(out, want) {
		t.Fatalf("fresh prepend mismatch:\n want=%q\n got =%q", string(want), string(out))
	}

	// Hard invariant: never synthesize a temporal field.
	forbidden := []string{"date:", "lastmod:", "created:", "updated:", "original_date:"}
	outFM, _ := splitFM(out)
	if outFM == nil {
		t.Fatalf("expected a frontmatter block in output")
	}
	for _, f := range forbidden {
		if bytes.Contains(outFM, []byte(f)) {
			t.Fatalf("synthesized temporal field %q in fresh prepend output: %q", f, string(outFM))
		}
	}
}

// TestAddOntology_ReplacesExisting (Req 1.5): when domain:/intent: are already
// present, they are replaced and every other key (incl. temporal) survives.
func TestAddOntology_ReplacesExisting(t *testing.T) {
	src := []byte(`---
title: Foo
domain: forensics
intent: log
date: 2024-03-11
tags: [a, b]
---
# Body
`)

	out, err := AddOntology(src, DevOps, IntentSOP)
	if err != nil {
		t.Fatalf("AddOntology: %v", err)
	}

	outFM, _ := splitFM(out)
	outMap := mustValueMarshalMap(t, outFM)

	if got := strings.TrimSpace(string(outMap["domain"])); got != "devops" {
		t.Fatalf("domain = %q, want devops", got)
	}
	if got := strings.TrimSpace(string(outMap["intent"])); got != "sop" {
		t.Fatalf("intent = %q, want sop", got)
	}

	srcFM, _ := splitFM(src)
	srcMap := mustValueMarshalMap(t, srcFM)
	for _, k := range []string{"title", "date", "tags"} {
		if !bytes.Equal(srcMap[k], outMap[k]) {
			t.Fatalf("non-ontology key %q changed:\n want=%q\n got =%q", k, srcMap[k], outMap[k])
		}
	}
}

// TestVerifyPreservation_FlippedValue (Req 8.3): the guard rejects an output
// frontmatter in which a non-ontology key's value differs from the input.
func TestVerifyPreservation_FlippedValue(t *testing.T) {
	old := []byte("title: Foo\ndate: 2024-03-11\n")
	// Identical except `date` flipped — simulates a buggy encoder mutating a
	// preserved temporal field.
	now := []byte("title: Foo\ndate: 2024-03-12\ndomain: devops\nintent: sop\n")

	err := verifyPreservation(old, now)
	if !errors.Is(err, ErrUnsafeRewrite) {
		t.Fatalf("expected ErrUnsafeRewrite on flipped date, got %v", err)
	}
}

// TestVerifyPreservation_DroppedKey (Req 1.5): the guard rejects an output
// frontmatter that has dropped a non-ontology key present in the input.
func TestVerifyPreservation_DroppedKey(t *testing.T) {
	old := []byte("title: Foo\ndate: 2024-03-11\ncustom_key: 42\n")
	now := []byte("title: Foo\ndate: 2024-03-11\ndomain: devops\nintent: sop\n") // custom_key dropped

	err := verifyPreservation(old, now)
	if !errors.Is(err, ErrUnsafeRewrite) {
		t.Fatalf("expected ErrUnsafeRewrite on dropped key, got %v", err)
	}
}

// TestVerifyPreservation_HappyPath: when every non-ontology key has the same
// marshaled value pre/post, the guard accepts the rewrite.
func TestVerifyPreservation_HappyPath(t *testing.T) {
	old := []byte("title: Foo\ndate: 2024-03-11\n")
	now := []byte("title: Foo\ndate: 2024-03-11\ndomain: devops\nintent: sop\n")

	if err := verifyPreservation(old, now); err != nil {
		t.Fatalf("guard rejected a preserved rewrite: %v", err)
	}
}

// TestAddOntology_BodyHorizontalRulesPreserved (Req 1.5 body-safety): a markdown
// body that contains lines that look like the frontmatter closer (`---`) must
// survive verbatim; the closer detection must stop at the FIRST `---` line
// after the opener.
func TestAddOntology_BodyHorizontalRulesPreserved(t *testing.T) {
	src := []byte(`---
title: Foo
date: 2024-03-11
---
# Heading

Some text.

---

After a horizontal rule.

---

End.
`)

	out, err := AddOntology(src, DevOps, IntentSOP)
	if err != nil {
		t.Fatalf("AddOntology: %v", err)
	}

	// The body (everything after the FIRST closing `---` of the original
	// frontmatter) must appear verbatim in the output.
	_, srcBody := splitFM(src)
	if !bytes.Contains(out, srcBody) {
		t.Fatalf("output does not contain original body verbatim.\n body=\n%s\n out=\n%s", string(srcBody), string(out))
	}
}

// TestAddOntology_CRLF (Req 1.5): a source using \r\n line endings round-trips
// without producing \r\r\n artifacts. We normalize to \n in the output (the
// body's textual content is preserved); this is documented in frontmatter.go.
func TestAddOntology_CRLF(t *testing.T) {
	src := []byte("---\r\ntitle: Foo\r\ndate: 2024-03-11\r\n---\r\n# H\r\n\r\nBody.\r\n")
	out, err := AddOntology(src, DevOps, IntentSOP)
	if err != nil {
		t.Fatalf("AddOntology: %v", err)
	}
	if bytes.Contains(out, []byte("\r\r\n")) {
		t.Fatalf("output produced \\r\\r\\n artifact: %q", string(out))
	}
	// Body content (without line-ending characters) must survive.
	if !bytes.Contains(out, []byte("# H")) || !bytes.Contains(out, []byte("Body.")) {
		t.Fatalf("body content lost after CRLF input: %q", string(out))
	}
	// Ontology keys present.
	outFM, _ := splitFM(out)
	if outFM == nil {
		t.Fatalf("output missing frontmatter block")
	}
	outMap := mustValueMarshalMap(t, outFM)
	if got := strings.TrimSpace(string(outMap["domain"])); got != "devops" {
		t.Fatalf("CRLF: domain = %q, want devops", got)
	}
	if got := strings.TrimSpace(string(outMap["intent"])); got != "sop" {
		t.Fatalf("CRLF: intent = %q, want sop", got)
	}
}

// TestAddOntology_MalformedYAML returns an error mentioning "parse frontmatter".
func TestAddOntology_MalformedYAML(t *testing.T) {
	src := []byte(`---
domain: [unclosed
---
body
`)
	_, err := AddOntology(src, DevOps, IntentSOP)
	if err == nil {
		t.Fatalf("expected error on malformed YAML")
	}
	if !strings.Contains(err.Error(), "parse frontmatter") {
		t.Fatalf("error %q does not mention 'parse frontmatter'", err.Error())
	}
}

// --- helpers ---

// splitFM is a test-local frontmatter splitter mirroring the convention of
// internal/tags/extractor.go (see splitFrontmatter there). Returns (fmBytes,
// body) where fmBytes is the bytes between the two `---` fences (nil when no
// frontmatter is present) and body is everything after the closing fence.
func splitFM(content []byte) (fm []byte, body []byte) {
	// Normalize for the test by handling either \n or \r\n.
	c := bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
	trimmed := bytes.TrimLeft(c, " \t\n")
	if !bytes.HasPrefix(trimmed, []byte("---")) {
		return nil, content
	}
	afterOpen := trimmed[3:]
	if len(afterOpen) > 0 && afterOpen[0] == '\n' {
		afterOpen = afterOpen[1:]
	}
	closeIdx := bytes.Index(afterOpen, []byte("\n---"))
	if closeIdx < 0 {
		return nil, content
	}
	fm = afterOpen[:closeIdx]
	bodyStart := closeIdx + 4
	if bodyStart > len(afterOpen) {
		return fm, nil
	}
	body = afterOpen[bodyStart:]
	if len(body) > 0 && body[0] == '\n' {
		body = body[1:]
	}
	return fm, body
}

// mustValueMarshalMap parses fmBytes as a YAML mapping and returns
// map[key] -> yaml.Marshal(valueNode). This is the "compare values reliably"
// strategy from the task brief: re-Marshal each top-level value as YAML and
// compare the resulting bytes.
func mustValueMarshalMap(t *testing.T, fmBytes []byte) map[string][]byte {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal(fmBytes, &doc); err != nil {
		t.Fatalf("parsing fixture frontmatter: %v\nbytes:\n%s", err, string(fmBytes))
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		t.Fatalf("fixture frontmatter is not a YAML document: kind=%d", doc.Kind)
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		t.Fatalf("fixture frontmatter root is not a mapping: kind=%d", root.Kind)
	}
	out := make(map[string][]byte, len(root.Content)/2)
	for i := 0; i+1 < len(root.Content); i += 2 {
		k := root.Content[i]
		v := root.Content[i+1]
		b, err := yaml.Marshal(v)
		if err != nil {
			t.Fatalf("re-marshaling value for key %q: %v", k.Value, err)
		}
		out[k.Value] = b
	}
	return out
}
