package compliance

import (
	"strings"
	"testing"

	"siyuan-knowledge-sync/internal/types"
)

func hasIssueWithMsg(issues []types.ComplianceIssue, substr string) bool {
	for _, iss := range issues {
		if strings.Contains(iss.Message, substr) {
			return true
		}
	}
	return false
}

func hasIssueWithMsgPrefix(issues []types.ComplianceIssue, prefix string) bool {
	for _, iss := range issues {
		if strings.HasPrefix(iss.Message, prefix) {
			return true
		}
	}
	return false
}

func countBySeverity(issues []types.ComplianceIssue, severity string) int {
	n := 0
	for _, iss := range issues {
		if iss.Severity == severity {
			n++
		}
	}
	return n
}

func countFixable(issues []types.ComplianceIssue, fixable bool) int {
	n := 0
	for _, iss := range issues {
		if iss.Fixable == fixable {
			n++
		}
	}
	return n
}

func TestAudit_ValidContent_NoIssues(t *testing.T) {
	content := []byte(`---
title: My Note
---

# Introduction

This is a valid SiYuan-compatible document.

## Section One

Some text here.

## Section Two

More text with a proper [TOC] block.

- [Introduction](#introduction)
  - [Section One](#section-one)
  - [Section Two](#section-two)
`)

	engine := NewComplianceEngine(false)
	issues, err := engine.Audit("test.md", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tocIssues := 0
	for _, iss := range issues {
		if strings.Contains(iss.Message, "TOC") {
			tocIssues++
		}
	}
	nonTocIssues := len(issues) - tocIssues
	if nonTocIssues > 0 {
		t.Errorf("expected no issues for valid content, got %d non-TOC issues", nonTocIssues)
		for _, iss := range issues {
			if !strings.Contains(iss.Message, "TOC") {
				t.Logf("  unexpected issue: %s", iss.Message)
			}
		}
	}
}

func TestAudit_HeadingNesting_SkippedLevel(t *testing.T) {
	content := []byte(`# Top Level

### Skipped H2 (should be H2)

Some content.

#### Another level skip
`)

	engine := NewComplianceEngine(false)
	issues, err := engine.Audit("test.md", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !hasIssueWithMsg(issues, "heading level skipped") {
		t.Errorf("expected heading nesting issues, got %d issues", len(issues))
		for _, iss := range issues {
			t.Logf("  issue: %s (line %d, %s, fixable=%v)", iss.Message, iss.Line, iss.Severity, iss.Fixable)
		}
	}

	errors := countBySeverity(issues, "error")
	if errors == 0 {
		t.Errorf("expected at least one error-level issue for level skipping")
	}

	fixable := countFixable(issues, true)
	if fixable == 0 {
		t.Errorf("expected heading nesting issues to be fixable")
	}
}

func TestAudit_HeadingNesting_FirstHeadingNotH1(t *testing.T) {
	content := []byte(`## Starting with H2

Some text.

### Going deeper
`)

	engine := NewComplianceEngine(false)
	issues, err := engine.Audit("test.md", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !hasIssueWithMsgPrefix(issues, "document does not start with H1") {
		t.Errorf("expected first-heading-not-H1 warning, got %d issues", len(issues))
		for _, iss := range issues {
			t.Logf("  issue: %s (line %d, %s)", iss.Message, iss.Line, iss.Severity)
		}
	}

	warnings := countBySeverity(issues, "warning")
	if warnings == 0 {
		t.Errorf("expected warning-level issue for first heading not H1")
	}
}

func TestAudit_HeadingNesting_LineNumbers(t *testing.T) {
	content := []byte(`# Top (line 1)

Text on line 3.

### Skipped (line 5)
`)

	engine := NewComplianceEngine(false)
	issues, err := engine.Audit("test.md", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false
	for _, iss := range issues {
		if strings.Contains(iss.Message, "heading level skipped") {
			found = true
			if iss.Line != 5 {
				t.Errorf("expected skipped heading on line 5, got line %d", iss.Line)
			}
		}
	}
	if !found {
		t.Errorf("expected to find heading nesting issue with correct line number")
	}
}

func TestAudit_MalformedAttributes(t *testing.T) {
	content := []byte(`# Document

Some paragraph. {: myattr="value"}

Another paragraph. {: custom-tags="ok" id="20240101120000-abc123def"}

{: bad="no-prefix"}
`)

	engine := NewComplianceEngine(false)
	issues, err := engine.Audit("test.md", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !hasIssueWithMsgPrefix(issues, "attribute \"myattr\"") {
		t.Errorf("expected attribute issue for myattr, got %d issues", len(issues))
		for _, iss := range issues {
			t.Logf("  issue: %s (line %d)", iss.Message, iss.Line)
		}
	}

	if !hasIssueWithMsgPrefix(issues, "attribute \"bad\"") {
		t.Errorf("expected attribute issue for bad, got %d issues", len(issues))
	}

	if hasIssueWithMsgPrefix(issues, "attribute \"custom-tags\"") {
		t.Errorf("custom-tags should not be flagged, it has custom- prefix")
	}

	fixable := 0
	for _, iss := range issues {
		if iss.Fixable && strings.Contains(iss.Message, "attribute") {
			fixable++
		}
	}
	if fixable == 0 {
		t.Errorf("expected attribute issues to be fixable")
	}
}

func TestAudit_BlockID_Placeholder(t *testing.T) {
	content := []byte(`# Doc

{: id="TODO"}

{: id="20240101120000-abc123def"}
`)

	engine := NewComplianceEngine(false)
	issues, err := engine.Audit("test.md", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !hasIssueWithMsg(issues, "placeholder block ID") {
		t.Errorf("expected placeholder block ID issue, got %d issues", len(issues))
		for _, iss := range issues {
			t.Logf("  issue: %s (line %d)", iss.Message, iss.Line)
		}
	}

	if hasIssueWithMsg(issues, "malformed block ID") {
		t.Errorf("valid SiYuan block ID should not be flagged")
	}
}

func TestAudit_AssetReferences(t *testing.T) {
	content := []byte(`# Doc

![image](assets/photo.png)

[link](C:\Users\file.txt)

[another](./local/file.md)
`)

	engine := NewComplianceEngine(false)
	issues, err := engine.Audit("test.md", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !hasIssueWithMsg(issues, "absolute or platform-specific") {
		t.Errorf("expected issue for absolute path reference, got %d issues", len(issues))
		for _, iss := range issues {
			t.Logf("  issue: %s (line %d, fixable=%v)", iss.Message, iss.Line, iss.Fixable)
		}
	}
}

func TestAudit_TOC_Missing(t *testing.T) {
	content := []byte(`# My Document

## Section One

Some text.

## Section Two

More text.
`)

	engine := NewComplianceEngine(false)
	issues, err := engine.Audit("test.md", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !hasIssueWithMsg(issues, "TOC marker missing") {
		t.Errorf("expected TOC missing warning, got %d issues", len(issues))
		for _, iss := range issues {
			t.Logf("  issue: %s (line %d)", iss.Message, iss.Line)
		}
	}
}

func TestAudit_TOC_Mismatch(t *testing.T) {
	content := []byte(`# Alpha

## Beta

[TOC]

- [Alpha](#alpha)
  - [Wrong](#wrong)
`)

	engine := NewComplianceEngine(false)
	issues, err := engine.Audit("test.md", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !hasIssueWithMsg(issues, "TOC content does not match") {
		t.Errorf("expected TOC mismatch error, got %d issues", len(issues))
		for _, iss := range issues {
			t.Logf("  issue: %s (line %d)", iss.Message, iss.Line)
		}
	}
}

func TestAudit_NoHeadings_NoTOCIssue(t *testing.T) {
	content := []byte("This document has no headings at all.\n\nJust plain text.\n")

	engine := NewComplianceEngine(false)
	issues, err := engine.Audit("test.md", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if hasIssueWithMsg(issues, "TOC") {
		t.Errorf("expected no TOC issue for document without headings, got %d issues", len(issues))
		for _, iss := range issues {
			t.Logf("  issue: %s", iss.Message)
		}
	}
}

func TestAudit_TagCompliance_ExtractorIntegration(t *testing.T) {
	content := []byte(`---
tags: [tag1, tag2]
---

# Document

Some text with #inline-tag and #another-tag.

{: custom-tag1="" custom-tag2=""}
`)

	engine := NewComplianceEngine(false)
	issues, err := engine.Audit("test.md", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tagIssues := 0
	for _, iss := range issues {
		if strings.Contains(iss.Message, "tag") || strings.Contains(iss.Message, "Tag") {
			tagIssues++
		}
	}
	if tagIssues > 0 {
		for _, iss := range issues {
			if strings.Contains(iss.Message, "tag") || strings.Contains(iss.Message, "Tag") {
				t.Logf("  unexpected tag issue: %s (line %d)", iss.Message, iss.Line)
			}
		}
	}
}

func TestAutofix_HeadingLevelFix(t *testing.T) {
	content := []byte(`# Top

### Skipped H2

#### Now H3

Text.

## Normal H2
`)

	engine := NewComplianceEngine(true)
	fixed, issues, err := engine.AutoFix("test.md", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(issues) == 0 {
		t.Fatal("expected issues to be found")
	}

	if !strings.Contains(string(fixed), "## Skipped H2") {
		t.Errorf("expected ### Skipped H2 to be fixed to ## Skipped H2, got:\n%s", string(fixed))
	}

	if !strings.Contains(string(fixed), "### Now H3") {
		t.Errorf("expected #### Now H3 to be fixed to ### Now H3, got:\n%s", string(fixed))
	}
}

func TestAutofix_AttributeFix(t *testing.T) {
	content := []byte(`# Doc

{: myattr="value" custom-ok="yes"}

{: badattr="bad"}
`)

	engine := NewComplianceEngine(true)
	fixed, issues, err := engine.AutoFix("test.md", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(issues) == 0 {
		t.Fatal("expected issues to be found")
	}

	if !strings.Contains(string(fixed), "custom-myattr") {
		t.Errorf("expected myattr to be prefixed with custom-, got:\n%s", string(fixed))
	}

	if !strings.Contains(string(fixed), "custom-badattr") {
		t.Errorf("expected badattr to be prefixed with custom-, got:\n%s", string(fixed))
	}

	if !strings.Contains(string(fixed), "custom-ok") {
		t.Errorf("expected custom-ok to remain unchanged, got:\n%s", string(fixed))
	}
}

func TestAutofix_TOCInsert(t *testing.T) {
	content := []byte(`# Main

## Section A

## Section B

Some content.
`)

	engine := NewComplianceEngine(true)
	fixed, issues, err := engine.AutoFix("test.md", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(issues) == 0 {
		t.Fatal("expected issues to be found")
	}

	if !strings.Contains(string(fixed), "[TOC]") {
		t.Errorf("expected [TOC] marker to be inserted, got:\n%s", string(fixed))
	}

	if !strings.Contains(string(fixed), "[Main]") {
		t.Errorf("expected TOC to include Main, got:\n%s", string(fixed))
	}

	if !strings.Contains(string(fixed), "[Section A]") {
		t.Errorf("expected TOC to include Section A, got:\n%s", string(fixed))
	}
}

func TestAutofix_TOCUpdate(t *testing.T) {
	content := []byte(`# Intro

## Details

[TOC]

- [Old](#old)
- [Stale](#stale)
`)

	engine := NewComplianceEngine(true)
	fixed, issues, err := engine.AutoFix("test.md", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(issues) == 0 {
		t.Fatal("expected issues to be found")
	}

	if strings.Contains(string(fixed), "[Old]") {
		t.Errorf("expected stale TOC content to be replaced, got:\n%s", string(fixed))
	}

	if !strings.Contains(string(fixed), "[Intro]") {
		t.Errorf("expected TOC to include Intro, got:\n%s", string(fixed))
	}

	if !strings.Contains(string(fixed), "[Details]") {
		t.Errorf("expected TOC to include Details, got:\n%s", string(fixed))
	}
}

func TestAutofix_NoModifyWithoutIssues(t *testing.T) {
	content := []byte("Just plain text with no headings.\n\nNo issues here.\n")

	engine := NewComplianceEngine(true)
	fixed, issues, err := engine.AutoFix("test.md", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(issues) != 0 {
		t.Errorf("expected no issues for plain-text doc, got %d", len(issues))
		for _, iss := range issues {
			t.Logf("  issue: %s", iss.Message)
		}
	}

	if string(fixed) != string(content) {
		t.Errorf("expected content unchanged when no issues, got difference:\n%q vs\n%q", string(fixed), string(content))
	}
}

func TestAutofix_BlockIDFix(t *testing.T) {
	content := []byte(`# Doc

{: id="TODO"}

{: id="20240101120000-abc123def"}
`)

	engine := NewComplianceEngine(true)
	fixed, issues, err := engine.AutoFix("test.md", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(issues) == 0 {
		t.Fatal("expected issues to be found")
	}

	fixedStr := string(fixed)
	if strings.Contains(fixedStr, `id="TODO"`) {
		t.Errorf("expected placeholder block ID to be removed, got:\n%s", fixedStr)
	}

	if !strings.Contains(fixedStr, "20240101120000-abc123def") {
		t.Errorf("expected valid block ID to be preserved, got:\n%s", fixedStr)
	}
}

func TestSeverity_ErrorVsWarning(t *testing.T) {
	headingIssueContent := []byte("# Top\n### Skip\n")
	attrWarningContent := []byte("{: myattr=\"val\"}\n")

	engine := NewComplianceEngine(false)

	hIssues, err := engine.Audit("test1.md", headingIssueContent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	errCount := countBySeverity(hIssues, "error")
	if errCount == 0 {
		t.Errorf("expected heading skip to be error severity")
	}

	aIssues, err := engine.Audit("test2.md", attrWarningContent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	warnFound := false
	for _, iss := range aIssues {
		if iss.Severity == "warning" && strings.Contains(iss.Message, "attribute") {
			warnFound = true
		}
	}
	if !warnFound {
		t.Errorf("expected attribute issue to be warning severity")
	}
}

func TestFixableFlag_TrueForAutoFixable(t *testing.T) {
	content := []byte("# Top\n### Skip\n")

	engine := NewComplianceEngine(false)
	issues, err := engine.Audit("test.md", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, iss := range issues {
		if strings.Contains(iss.Message, "heading level skipped") {
			if !iss.Fixable {
				t.Errorf("expected heading nesting issue to be fixable")
			}
		}
	}
}

func TestFixableFlag_FalseForManual(t *testing.T) {
	content := []byte(`[link](C:\Users\bad.txt)
`)

	engine := NewComplianceEngine(false)
	issues, err := engine.Audit("test.md", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, iss := range issues {
		if strings.Contains(iss.Message, "absolute or platform-specific") {
			if iss.Fixable {
				t.Errorf("expected absolute path issue to be non-fixable")
			}
		}
	}
}

func TestFileFieldInIssues(t *testing.T) {
	content := []byte("# Top\n### Skip\n")

	engine := NewComplianceEngine(false)
	issues, err := engine.Audit("my-file.md", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, iss := range issues {
		if iss.File != "my-file.md" {
			t.Errorf("expected File field to be 'my-file.md', got %q", iss.File)
		}
	}
}

func TestAutofix_PreservesContentOutsideFixes(t *testing.T) {
	content := []byte(`# Top

Paragraph text that should not change.

{: myattr="value"}

More content here.

### Should be H2
`)

	engine := NewComplianceEngine(true)
	fixed, _, err := engine.AutoFix("test.md", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	fixedStr := string(fixed)
	if !strings.Contains(fixedStr, "Paragraph text that should not change") {
		t.Errorf("expected paragraph text to be preserved")
	}

	if !strings.Contains(fixedStr, "More content here") {
		t.Errorf("expected more content to be preserved")
	}
}

func TestAudit_BlockID_LineNumber(t *testing.T) {
	content := []byte(`# Title

Some text.

{: id="FIXME"}

More text.
`)

	engine := NewComplianceEngine(false)
	issues, err := engine.Audit("test.md", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, iss := range issues {
		if strings.Contains(iss.Message, "placeholder") {
			if iss.Line != 5 {
				t.Errorf("expected placeholder ID issue on line 5, got line %d", iss.Line)
			}
		}
	}
}

func TestAudit_IALAttributeInsideFrontmatter(t *testing.T) {
	content := []byte(`---
title: My Doc
tags: [tag1]
---

# Doc

{: myattr="val"}
`)

	engine := NewComplianceEngine(false)
	issues, err := engine.Audit("test.md", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false
	for _, iss := range issues {
		if strings.Contains(iss.Message, "myattr") {
			found = true
			if iss.Line < 5 {
				t.Errorf("expected IAL attribute issue after frontmatter, got line %d", iss.Line)
			}
		}
	}
	if !found {
		t.Errorf("expected attribute issue for myattr outside frontmatter")
	}
}

func TestAutofix_IALCleanup(t *testing.T) {
	content := []byte("{: custom-todo=\"\"}\n")

	engine := NewComplianceEngine(true)
	fixed, issues, err := engine.AutoFix("test.md", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(issues) == 0 {
		t.Errorf("expected at least one issue for placeholder tag, got 0")
	}

	fixedStr := string(fixed)
	if strings.Contains(fixedStr, "custom-todo") {
		t.Errorf("expected placeholder tag to be cleaned up, got:\n%s", fixedStr)
	}
}

func TestAutofix_NoModifyWhenAutofixDisabled(t *testing.T) {
	content := []byte(`# Doc
### Skipped H2
{: myattr="val"}
{: id="TODO"}
`)

	engine := NewComplianceEngine(false)
	fixed, issues, err := engine.AutoFix("test.md", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(issues) == 0 {
		t.Error("expected issues to be found even with autofix=false")
	}

	if string(fixed) != string(content) {
		t.Errorf("expected content unchanged when autofix=false, got:\n%q vs\n%q", string(fixed), string(content))
	}
}

func TestAudit_BlockID_BodyPlaceholderReferences(t *testing.T) {
	content := []byte(`# Doc

Some text with id="FIXME" in body.

Another reference with block="PLACEHOLDER" here.

{: id="20240101120000-abc123def"}
`)

	engine := NewComplianceEngine(false)
	issues, err := engine.Audit("test.md", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	bodyRefCount := 0
	for _, iss := range issues {
		if strings.Contains(iss.Message, "placeholder or dummy block ID reference") {
			bodyRefCount++
		}
	}
	if bodyRefCount < 2 {
		t.Errorf("expected at least 2 body-level placeholder references, got %d", bodyRefCount)
	}
}
