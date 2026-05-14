package compliance

import (
	"bytes"
	"regexp"
	"strings"

	"siyuan-knowledge-sync/internal/types"
)

var (
	ialAttrKeyWithValRe = regexp.MustCompile(`(\s|^)([a-zA-Z][a-zA-Z0-9_-]*)(\s*=\s*["'][^"']*["'])`)

	blockIDFixRe    = regexp.MustCompile(`(?i)\bid=["']?(todo|fixme|placeholder|temp|dummy|test|xxx|replace)["']?`)
	blockIDRemoveRe = regexp.MustCompile(`\s+id=["']?[^"'\s]+["']?`)
)

func (e *ComplianceEngine) AutoFix(filePath string, content []byte) ([]byte, []types.ComplianceIssue, error) {
	issues, err := e.Audit(filePath, content)
	if err != nil {
		return nil, nil, err
	}

	if !e.autofix {
		return content, issues, nil
	}

	if len(issues) == 0 {
		return content, nil, nil
	}

	fixed := make([]byte, len(content))
	copy(fixed, content)

	fixed = e.fixHeadingLevels(fixed)
	fixed = e.fixAttributes(fixed)
	fixed = e.fixBlockIDIssues(fixed)
	fixed = e.fixTagIssues(fixed)
	fixed = e.fixTOCContent(fixed)

	return fixed, issues, nil
}

func (e *ComplianceEngine) fixHeadingLevels(content []byte) []byte {
	lines := bytes.Split(content, []byte("\n"))
	inFrontmatter := false
	lastLevel := 0
	depthStack := []int{0}

	for i, line := range lines {
		lineStr := strings.TrimSpace(string(line))

		if lineStr == "---" {
			if i == 0 {
				inFrontmatter = true
				continue
			}
			if inFrontmatter {
				inFrontmatter = false
				continue
			}
		}

		if inFrontmatter {
			continue
		}

		match := headingLineRe.FindStringSubmatch(string(line))
		if match == nil {
			continue
		}

		origLevel := len(match[1])
		fixedLevel := origLevel

		maxAllowed := lastLevel + 1
		if origLevel > maxAllowed {
			fixedLevel = maxAllowed
		}

		if origLevel == 1 {
			depthStack = []int{1}
		} else {
			for len(depthStack) > 0 && depthStack[len(depthStack)-1] >= fixedLevel {
				depthStack = depthStack[:len(depthStack)-1]
			}
			depthStack = append(depthStack, fixedLevel)
		}

		if fixedLevel != origLevel {
			newPrefix := strings.Repeat("#", fixedLevel)
			lines[i] = bytes.Replace(line, []byte(match[1]), []byte(newPrefix), 1)
		}

		lastLevel = fixedLevel
	}

	return bytes.Join(lines, []byte("\n"))
}

func (e *ComplianceEngine) fixAttributes(content []byte) []byte {
	lines := bytes.Split(content, []byte("\n"))
	fixed := false

	for i, line := range lines {
		lineStr := string(line)
		match := ialBlockRe.FindStringSubmatch(lineStr)
		if match == nil {
			continue
		}

		inner := match[1]
		newInner := ialAttrKeyWithValRe.ReplaceAllStringFunc(inner, func(attrMatch string) string {
			parts := ialAttrKeyWithValRe.FindStringSubmatch(attrMatch)
			if parts == nil {
				return attrMatch
			}
			before := parts[1]
			key := parts[2]
			rest := parts[3]
			lower := strings.ToLower(key)

			if lower == "id" || lower == "type" || lower == "updated" || lower == "title" {
				return attrMatch
			}

			if strings.HasPrefix(lower, "custom-") {
				return attrMatch
			}

			if strings.HasPrefix(lower, "ial-") {
				newKey := "custom-" + key[4:]
				return before + newKey + rest
			}

			newKey := "custom-" + key
			return before + newKey + rest
		})

		newInner = placeholderTagRe.ReplaceAllString(newInner, "")
		newInner = strings.TrimSpace(newInner)

		if newInner != inner {
			if newInner == "" {
				lines[i] = []byte{}
			} else {
				newLine := " {: " + newInner + "}"
				lines[i] = []byte(newLine)
			}
			fixed = true
		}
	}

	if !fixed {
		return content
	}

	return bytes.Join(lines, []byte("\n"))
}

func (e *ComplianceEngine) fixBlockIDIssues(content []byte) []byte {
	lines := bytes.Split(content, []byte("\n"))
	fixed := false

	for i, line := range lines {
		lineStr := string(line)
		match := ialBlockRe.FindStringSubmatch(lineStr)
		if match == nil {
			continue
		}

		inner := match[1]

		if blockIDFixRe.MatchString(inner) {
			inner = blockIDFixRe.ReplaceAllString(inner, "")
			fixed = true
		}

		inner = strings.TrimSpace(inner)

		if inner == "" {
			lines[i] = []byte{}
		} else {
			newLine := " {: " + inner + "}"
			lines[i] = []byte(newLine)
		}

		if !fixed && blockIDRemoveRe.MatchString(inner) && !blockIDRe.MatchString(inner) {
			inner = blockIDRemoveRe.ReplaceAllString(inner, "")
			inner = strings.TrimSpace(inner)
			fixed = true
			if inner == "" {
				lines[i] = []byte{}
			} else {
				newLine := " {: " + inner + "}"
				lines[i] = []byte(newLine)
			}
		}
	}

	if !fixed {
		return content
	}
	return bytes.Join(lines, []byte("\n"))
}

func (e *ComplianceEngine) fixTagIssues(content []byte) []byte {
	lines := bytes.Split(content, []byte("\n"))

	for i, line := range lines {
		lineStr := string(line)
		match := ialBlockRe.FindStringSubmatch(lineStr)
		if match == nil {
			continue
		}

		origInner := match[1]

		inner := placeholderTagRe.ReplaceAllString(origInner, "")
		inner = strings.TrimSpace(inner)

		if inner != origInner {
			if inner == "" {
				lines[i] = []byte{}
			} else {
				lines[i] = []byte(" {: " + inner + "}")
			}
		}
	}

	return bytes.Join(lines, []byte("\n"))
}

func (e *ComplianceEngine) fixTOCContent(content []byte) []byte {
	genTOC, err := e.tocGenerator.Generate(content)
	if err != nil || genTOC == "" {
		return content
	}

	lines := bytes.Split(content, []byte("\n"))
	tocLine := -1
	for i, line := range lines {
		if tocMarkerRe.Match(line) {
			tocLine = i
			break
		}
	}

	if tocLine < 0 {
		return e.insertTOC(content, genTOC)
	}

	tocEnd := len(lines)
	for i := tocLine + 1; i < len(lines); i++ {
		if headingLineRe.Match(lines[i]) {
			tocEnd = i
			break
		}
	}

	var resultLines [][]byte
	resultLines = append(resultLines, lines[:tocLine+1]...)
	resultLines = append(resultLines, []byte(""))
	var tocItems []string
	if genTOC != "" {
		tocItems = strings.Split(genTOC, "\n")
	}
	for _, item := range tocItems {
		resultLines = append(resultLines, []byte(item))
	}

	if tocEnd < len(lines) {
		resultLines = append(resultLines, []byte(""))
		resultLines = append(resultLines, lines[tocEnd:]...)
	}

	return bytes.Join(resultLines, []byte("\n"))
}

func (e *ComplianceEngine) insertTOC(content []byte, genTOC string) []byte {
	lines := bytes.Split(content, []byte("\n"))

	insertPos := -1
	inFrontmatter := false
	for i, line := range lines {
		lineStr := string(line)
		if strings.TrimSpace(lineStr) == "---" {
			if i == 0 && !inFrontmatter {
				inFrontmatter = true
				continue
			}
			if inFrontmatter {
				inFrontmatter = false
				continue
			}
		}
		if inFrontmatter {
			continue
		}
		if headingLineRe.MatchString(lineStr) {
			insertPos = i + 1
			break
		}
	}

	if insertPos < 0 {
		insertPos = 0
	}

	var resultLines [][]byte
	resultLines = append(resultLines, lines[:insertPos]...)
	resultLines = append(resultLines, []byte("[TOC]"))
	resultLines = append(resultLines, []byte(""))
	for _, item := range strings.Split(genTOC, "\n") {
		resultLines = append(resultLines, []byte(item))
	}
	resultLines = append(resultLines, []byte(""))
	resultLines = append(resultLines, lines[insertPos:]...)

	return bytes.Join(resultLines, []byte("\n"))
}
