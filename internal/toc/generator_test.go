package toc

import "testing"

func TestGenerate_ThreeLevelHeadingHierarchy(t *testing.T) {
	content := []byte(`# Top
## Middle
### Bottom
`)
	g := NewTOCGenerator()
	result, err := g.Generate(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "- [Top](#top)\n  - [Middle](#middle)\n    - [Bottom](#bottom)"
	if result != expected {
		t.Errorf("expected:\n%q\ngot:\n%q", expected, result)
	}
}

func TestGenerate_SingleHeading(t *testing.T) {
	content := []byte("# Just One")
	g := NewTOCGenerator()
	result, err := g.Generate(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "- [Just One](#just-one)"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestGenerate_FlatStructureAllH1(t *testing.T) {
	content := []byte("# Alpha\n# Beta\n# Gamma\n")
	g := NewTOCGenerator()
	result, err := g.Generate(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "- [Alpha](#alpha)\n- [Beta](#beta)\n- [Gamma](#gamma)"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestGenerate_EmptyDocument(t *testing.T) {
	g := NewTOCGenerator()
	result, err := g.Generate([]byte(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestGenerate_NoHeadings(t *testing.T) {
	content := []byte("Just some paragraph text.\n\nNo headings here.\n")
	g := NewTOCGenerator()
	result, err := g.Generate(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestGenerate_MixedHeadingLevels(t *testing.T) {
	content := []byte("# H1 First\n## H2A\n# H1 Second\n## H2B\n")
	g := NewTOCGenerator()
	result, err := g.Generate(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "- [H1 First](#h1-first)\n  - [H2A](#h2a)\n- [H1 Second](#h1-second)\n  - [H2B](#h2b)"
	if result != expected {
		t.Errorf("expected:\n%q\ngot:\n%q", expected, result)
	}
}

func TestGenerate_MultipleHeadingsAtSameLevel(t *testing.T) {
	content := []byte("## A\n## B\n## C\n")
	g := NewTOCGenerator()
	result, err := g.Generate(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "  - [A](#a)\n  - [B](#b)\n  - [C](#c)"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestGenerate_HeadingWithSpecialCharacters(t *testing.T) {
	content := []byte("# Hello, World!\n## What's up?\n")
	g := NewTOCGenerator()
	result, err := g.Generate(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !containsLink(result, "Hello, World!", "hello-world") {
		t.Errorf("expected link for 'Hello, World!' → 'hello-world', got %q", result)
	}
	if !containsLink(result, "What's up?", "whats-up") {
		t.Errorf("expected link for \"What's up?\" → 'whats-up', got %q", result)
	}
}

func containsLink(toc, text, slug string) bool {
	expected := "- [" + text + "](#" + slug + ")"
	expected2 := "  - [" + text + "](#" + slug + ")"
	return contains(toc, expected) || contains(toc, expected2)
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestGenerate_OnlyBodyTextNoHeadings(t *testing.T) {
	content := []byte("This is a document.\n\nWith only paragraphs and no headings.\n")
	g := NewTOCGenerator()
	result, err := g.Generate(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestGenerate_H6DeepHeading(t *testing.T) {
	content := []byte("###### Deepest")
	g := NewTOCGenerator()
	result, err := g.Generate(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "          - [Deepest](#deepest)"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestGenerate_SkipsHeadingsInsideCodeBlocks(t *testing.T) {
	content := []byte("# Real\n\n```\n# Not a heading\n```\n\n## Also real\n")
	g := NewTOCGenerator()
	result, err := g.Generate(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "- [Real](#real)\n  - [Also real](#also-real)"
	if result != expected {
		t.Errorf("expected:\n%q\ngot:\n%q", expected, result)
	}
}

func TestGenerate_HeadingWithEmphasis(t *testing.T) {
	content := []byte("# **Bold** and *italic* heading\n")
	g := NewTOCGenerator()
	result, err := g.Generate(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "- [Bold and italic heading](#bold-and-italic-heading)"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestGenerate_HeadingWithLeadingTrailingWhitespace(t *testing.T) {
	content := []byte("#   Padded   \n")
	g := NewTOCGenerator()
	result, err := g.Generate(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "- [Padded](#padded)"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestGenerate_UnicodeHeading(t *testing.T) {
	content := []byte("# Cafe\n")
	g := NewTOCGenerator()
	result, err := g.Generate(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "- [Cafe](#cafe)"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestGenerate_PreservesOriginalTextCase(t *testing.T) {
	content := []byte("# My Heading\n")
	g := NewTOCGenerator()
	result, err := g.Generate(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !contains(result, "My Heading") {
		t.Errorf("link text should preserve original case, got %q", result)
	}
}

func TestGenerate_SingleNewlineAtEnd(t *testing.T) {
	content := []byte("# A\n# B\n")
	g := NewTOCGenerator()
	result, err := g.Generate(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "- [A](#a)\n- [B](#b)"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}
