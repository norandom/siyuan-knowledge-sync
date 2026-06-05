package compliance

import (
	"bytes"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"siyuan-knowledge-sync/internal/ontology"
	"siyuan-knowledge-sync/internal/tags"
	"siyuan-knowledge-sync/internal/toc"
	"siyuan-knowledge-sync/internal/types"
)

var (
	blockIDRe       = regexp.MustCompile(`\d{14}-[a-z0-9]{6,}`)
	placeholderIDRe = regexp.MustCompile(`(?i)\b(id|block)[-=]["']?\s*(todo|fixme|placeholder|temp|dummy|test|xxx|replace)`)
	ialBlockRe      = regexp.MustCompile(`\{:\s*(.*?)\s*\}`)
	customAttrRe    = regexp.MustCompile(`custom-[a-zA-Z][a-zA-Z0-9_-]*`)
	attrKeyRe       = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9_-]*)\s*=`)

	assetRefRe = regexp.MustCompile(`\]\(([^)]+)\)`)
	badAssetRe   = regexp.MustCompile(`\]\(\s*([a-zA-Z]:|[\/\\])`)

	tocMarkerRe  = regexp.MustCompile(`(?i)^\[TOC\]|^<!--\s*TOC\s*-->`)
	headingLineRe = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)

	placeholderTagRe = regexp.MustCompile(`(?i)(?:^|\s+)custom-(todo|fixme|placeholder|temp|dummy|xxx)=""`)
	tagWordRe        = regexp.MustCompile(`\btag\b`)
)

type ComplianceEngine struct {
	autofix      bool
	tagExtractor *tags.TagExtractor
	tocGenerator *toc.TOCGenerator
}

func NewComplianceEngine(autofix bool) *ComplianceEngine {
	return &ComplianceEngine{
		autofix:      autofix,
		tagExtractor: tags.NewTagExtractor(),
		tocGenerator: toc.NewTOCGenerator(),
	}
}

func (e *ComplianceEngine) Audit(filePath string, content []byte) ([]types.ComplianceIssue, error) {
	var issues []types.ComplianceIssue

	issues = append(issues, e.checkBlockIDs(filePath, content)...)
	issues = append(issues, e.checkHeadingNesting(filePath, content)...)
	issues = append(issues, e.checkAttributes(filePath, content)...)
	issues = append(issues, e.checkAssetRefs(filePath, content)...)
	issues = append(issues, e.checkTagCompliance(filePath, content)...)
	issues = append(issues, e.checkTOCCompliance(filePath, content)...)
	issues = append(issues, e.checkOntologySchema(filePath, content)...)

	return issues, nil
}

// schemaIssue builds a schema-category compliance issue. Schema violations
// are file-scoped (Line=0), non-auto-fixable, and error-severity per Req 2.7
// (auto-fix must never invent ontology values).
func schemaIssue(file, message string) types.ComplianceIssue {
	return types.ComplianceIssue{
		File:     file,
		Line:     0,
		Severity: "error",
		Message:  message,
		Fixable:  false,
		Category: "schema",
	}
}

// checkOntologySchema produces ComplianceIssues with Category == "schema"
// for missing-required-key, multi-value, and out-of-enum violations of the
// `domain:` / `intent:` frontmatter ontology. These issues are gate-eligible:
// the sync engine aborts a file when any schema-category issue is present.
//
// When the file has no frontmatter, both keys are missing → two violations
// are emitted. When the frontmatter YAML cannot be parsed, a single
// "frontmatter parse error" violation is emitted (a malformed file is not
// silently routed).
func (e *ComplianceEngine) checkOntologySchema(filePath string, content []byte) []types.ComplianceIssue {
	fmBody, ok := extractFrontmatterYAML(content)
	if !ok {
		// No frontmatter at all → both required keys are missing.
		view := ontology.FrontmatterView{DomainNode: nil, IntentNode: nil}
		return toComplianceIssues(filePath, ontology.CheckOntologyFrontmatter(filePath, view))
	}

	var root yaml.Node
	if err := yaml.Unmarshal(fmBody, &root); err != nil {
		return []types.ComplianceIssue{
			schemaIssue(filePath, "frontmatter parse error: "+err.Error()),
		}
	}

	domainNode, intentNode := findOntologyNodes(&root)
	view := ontology.FrontmatterView{DomainNode: domainNode, IntentNode: intentNode}
	return toComplianceIssues(filePath, ontology.CheckOntologyFrontmatter(filePath, view))
}

// extractFrontmatterYAML returns the YAML body between the two `---` fences
// (excluding the fence lines themselves). It returns (nil, false) when the
// file has no frontmatter block.
func extractFrontmatterYAML(content []byte) ([]byte, bool) {
	end := findFrontmatterEnd(content)
	if end < 0 {
		return nil, false
	}
	// Skip the leading "---\n".
	lines := bytes.SplitN(content[:end], []byte("\n"), 2)
	if len(lines) < 2 {
		return nil, false
	}
	body := lines[1]
	// Trim the trailing "---\n" fence (the last line before end).
	// Find the last "---" line within body.
	bodyLines := bytes.Split(body, []byte("\n"))
	for i := len(bodyLines) - 1; i >= 0; i-- {
		if strings.TrimSpace(string(bodyLines[i])) == "---" {
			return bytes.Join(bodyLines[:i], []byte("\n")), true
		}
	}
	return body, true
}

// findOntologyNodes walks a parsed YAML document root and returns the value
// nodes for the top-level `domain:` and `intent:` keys. A nil node means the
// key is absent.
func findOntologyNodes(root *yaml.Node) (domainNode, intentNode *yaml.Node) {
	if root == nil || len(root.Content) == 0 {
		return nil, nil
	}
	mapping := root.Content[0]
	if mapping.Kind != yaml.MappingNode {
		return nil, nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		k := mapping.Content[i]
		v := mapping.Content[i+1]
		if k.Kind != yaml.ScalarNode {
			continue
		}
		switch k.Value {
		case "domain":
			domainNode = v
		case "intent":
			intentNode = v
		}
	}
	return domainNode, intentNode
}

// toComplianceIssues converts ontology.SchemaViolation values into the
// schema-category compliance issues consumed by the engine. The human-
// readable Message is sourced from SchemaViolation.Error() — which already
// names the key, the offending value, and lists the allowed values.
func toComplianceIssues(filePath string, sv []ontology.SchemaViolation) []types.ComplianceIssue {
	if len(sv) == 0 {
		return nil
	}
	out := make([]types.ComplianceIssue, 0, len(sv))
	for _, v := range sv {
		out = append(out, schemaIssue(filePath, v.Error()))
	}
	return out
}

func makeIssue(file string, line int, severity, message string, fixable bool) types.ComplianceIssue {
	return types.ComplianceIssue{
		File:     file,
		Line:     line,
		Severity: severity,
		Message:  message,
		Fixable:  fixable,
	}
}

func (e *ComplianceEngine) checkBlockIDs(file string, content []byte) []types.ComplianceIssue {
	var issues []types.ComplianceIssue
	lines := bytes.Split(content, []byte("\n"))

	for i, line := range lines {
		lineNum := i + 1
		lineStr := string(line)

		match := ialBlockRe.FindStringSubmatch(lineStr)
		if match == nil {
			continue
		}

		ialContent := match[1]

		if placeholderIDRe.MatchString(ialContent) {
			issues = append(issues, makeIssue(file, lineNum, "error",
				"placeholder block ID detected", true))
			continue
		}

		if strings.Contains(ialContent, "id=") || strings.Contains(ialContent, "\"id\"") {
			if !blockIDRe.MatchString(ialContent) {
				issues = append(issues, makeIssue(file, lineNum, "error",
					"malformed block ID in IAL", true))
			}
		}
	}

	bodyContent := content
	fmEnd := findFrontmatterEnd(content)
	if fmEnd >= 0 {
		bodyContent = content[fmEnd:]
	}

	offset := fmEnd
	if offset < 0 {
		offset = 0
	}
	locs := placeholderIDRe.FindAllIndex(bodyContent, -1)
	for _, loc := range locs {
		lineNum := lineForOffset(content, offset+loc[0])
		issues = append(issues, makeIssue(file, lineNum, "warning",
			"placeholder or dummy block ID reference detected", true))
	}

	return issues
}

func (e *ComplianceEngine) checkHeadingNesting(file string, content []byte) []types.ComplianceIssue {
	var issues []types.ComplianceIssue
	lines := bytes.Split(content, []byte("\n"))

	type headingInfo struct {
		line  int
		level int
		text  string
	}

	var headings []headingInfo
	inFrontmatter := false

	for i, line := range lines {
		lineStr := string(line)

		trimmed := strings.TrimSpace(lineStr)
		if trimmed == "---" {
			if !inFrontmatter {
				inFrontmatter = true
				continue
			}
			inFrontmatter = false
			continue
		}

		if inFrontmatter {
			continue
		}

		match := headingLineRe.FindStringSubmatch(lineStr)
		if match == nil {
			continue
		}

		headings = append(headings, headingInfo{
			line:  i + 1,
			level: len(match[1]),
			text:  strings.TrimSpace(match[2]),
		})
	}

	if len(headings) == 0 {
		return nil
	}

	if headings[0].level > 1 {
		issues = append(issues, makeIssue(file, headings[0].line, "warning",
			"document does not start with H1; first heading is H"+strings.Repeat("#", headings[0].level), true))
	}

	lastLevel := headings[0].level
	for i := 1; i < len(headings); i++ {
		curr := headings[i]
		if curr.level > lastLevel+1 {
			issues = append(issues, makeIssue(file, curr.line, "error",
				"heading level skipped: H"+strings.Repeat("#", lastLevel)+
					" to H"+strings.Repeat("#", curr.level)+
					" (\""+truncateText(curr.text, 50)+"\")", true))
		}
		if curr.level <= lastLevel+1 {
			lastLevel = curr.level
		}
	}

	return issues
}

func (e *ComplianceEngine) checkAttributes(file string, content []byte) []types.ComplianceIssue {
	var issues []types.ComplianceIssue
	lines := bytes.Split(content, []byte("\n"))

	for i, line := range lines {
		lineStr := string(line)
		match := ialBlockRe.FindStringSubmatch(lineStr)
		if match == nil {
			continue
		}

		inner := match[1]
		if strings.TrimSpace(inner) == "" {
			continue
		}

		keyMatches := attrKeyRe.FindAllStringSubmatch(inner, -1)
		for _, km := range keyMatches {
			key := km[1]
			lower := strings.ToLower(key)

			if lower == "id" || lower == "type" || lower == "updated" || lower == "title" {
				continue
			}

			if strings.HasPrefix(lower, "custom-") {
				continue
			}

			if strings.HasPrefix(lower, "ial-") {
				issues = append(issues, makeIssue(file, i+1, "warning",
					"attribute \""+key+"\" uses deprecated IAL prefix; should use custom- prefix", true))
				continue
			}

			if !customAttrRe.MatchString(key) {
				issues = append(issues, makeIssue(file, i+1, "warning",
					"attribute \""+key+"\" missing custom- prefix", true))
			}
		}
	}

	return issues
}

func (e *ComplianceEngine) checkAssetRefs(file string, content []byte) []types.ComplianceIssue {
	var issues []types.ComplianceIssue
	lines := bytes.Split(content, []byte("\n"))

	for i, line := range lines {
		lineStr := string(line)

		if badAssetRe.MatchString(lineStr) {
			issues = append(issues, makeIssue(file, i+1, "warning",
				"absolute or platform-specific path in asset reference", false))
		}

		matches := assetRefRe.FindAllStringSubmatch(lineStr, -1)
		for _, m := range matches {
			if len(m) < 2 {
				continue
			}
			path := strings.TrimSpace(m[1])

			if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
				continue
			}

			if strings.Contains(path, " ") {
				issues = append(issues, makeIssue(file, i+1, "warning",
					"asset reference contains unescaped space: "+m[1], true))
			}
		}
	}

	return issues
}

func (e *ComplianceEngine) checkTagCompliance(file string, content []byte) []types.ComplianceIssue {
	var issues []types.ComplianceIssue

	_, err := e.tagExtractor.Extract(content)
	if err != nil {
		issues = append(issues, makeIssue(file, 0, "warning",
			"tag extraction failed: "+err.Error(), false))
		return issues
	}

	lines := bytes.Split(content, []byte("\n"))
	for i, line := range lines {
		lineStr := string(line)
		if !ialBlockRe.MatchString(lineStr) {
			continue
		}

		match := ialBlockRe.FindStringSubmatch(lineStr)
		inner := match[1]

		if tagWordRe.MatchString(inner) && !customAttrRe.MatchString(inner) {
			issues = append(issues, makeIssue(file, i+1, "warning",
				"tag attribute missing custom- prefix", true))
		}

		if placeholderTagRe.MatchString(" " + inner) {
			issues = append(issues, makeIssue(file, i+1, "warning",
				"placeholder tag value detected in IAL", true))
		}
	}

	return issues
}

func (e *ComplianceEngine) checkTOCCompliance(file string, content []byte) []types.ComplianceIssue {
	var issues []types.ComplianceIssue

	genTOC, err := e.tocGenerator.Generate(content)
	if err != nil {
		issues = append(issues, makeIssue(file, 0, "warning",
			"TOC generation failed: "+err.Error(), false))
		return issues
	}

	if genTOC == "" {
		return nil
	}

	tocLine := -1
	lines := bytes.Split(content, []byte("\n"))
	for i, line := range lines {
		if tocMarkerRe.Match(line) {
			tocLine = i
			break
		}
	}

	if tocLine < 0 {
		issues = append(issues, makeIssue(file, 1, "warning",
			"TOC marker missing; document has headings but no [TOC]", true))
		return issues
	}

	tocEnd := len(lines)
	for i := tocLine + 1; i < len(lines); i++ {
		if headingLineRe.Match(lines[i]) {
			tocEnd = i
			break
		}
	}

	var tocContent string
	if tocLine+1 < tocEnd {
		tocLines := lines[tocLine+1 : tocEnd]
		var sb strings.Builder
		for _, l := range tocLines {
			if len(l) > 0 && l[0] == '-' {
				sb.WriteString(string(l))
				sb.WriteByte('\n')
			}
		}
		tocContent = strings.TrimRight(sb.String(), "\n")
	}

	if tocContent != genTOC {
		issues = append(issues, makeIssue(file, tocLine+1, "error",
			"TOC content does not match heading structure", true))
	}

	return issues
}

func findFrontmatterEnd(content []byte) int {
	lines := bytes.Split(content, []byte("\n"))
	if len(lines) == 0 {
		return -1
	}
	if strings.TrimSpace(string(lines[0])) != "---" {
		return -1
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(string(lines[i])) == "---" {
			return bytesIndexN(content, '\n', i) + 1
		}
	}
	return -1
}

func bytesIndexN(data []byte, sep byte, n int) int {
	count := 0
	for i, b := range data {
		if b == sep {
			count++
			if count >= n {
				return i
			}
		}
	}
	return len(data)
}

func lineForOffset(content []byte, offset int) int {
	line := 1
	for i := 0; i < offset && i < len(content); i++ {
		if content[i] == '\n' {
			line++
		}
	}
	return line
}

func truncateText(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
