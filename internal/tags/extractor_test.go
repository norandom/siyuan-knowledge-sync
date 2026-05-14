package tags

import (
	"reflect"
	"sort"
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
