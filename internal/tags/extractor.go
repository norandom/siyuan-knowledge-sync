package tags

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
	"gopkg.in/yaml.v3"
)

var inlineTagRe = regexp.MustCompile(`(?:\A|\s)#([\pL\pN][\pL\pN_-]*)`)

type frontmatterData struct {
	Tags yaml.Node `yaml:"tags"`
}

type TagExtractor struct {
	md goldmark.Markdown
}

func NewTagExtractor() *TagExtractor {
	return &TagExtractor{
		md: goldmark.New(),
	}
}

func (e *TagExtractor) Extract(content []byte) (map[string]string, error) {
	result := make(map[string]string)

	fmBytes, body := splitFrontmatter(content)

	if fmBytes != nil {
		tags, err := parseFrontmatterTags(fmBytes)
		if err != nil {
			return nil, err
		}
		for _, tag := range tags {
			tag = normalizeTag(tag)
			if tag != "" {
				result["custom-"+tag] = ""
			}
		}
	}

	inlineTags := extractInlineTags(e.md, body)
	for _, tag := range inlineTags {
		tag = normalizeTag(tag)
		if tag != "" {
			result["custom-"+tag] = ""
		}
	}

	return result, nil
}

func splitFrontmatter(content []byte) ([]byte, []byte) {
	trimmed := bytes.TrimLeft(content, " \t\r\n")
	if !bytes.HasPrefix(trimmed, []byte("---")) {
		return nil, content
	}

	afterOpen := trimmed[3:]
	if len(afterOpen) > 0 && afterOpen[0] == '\n' {
		afterOpen = afterOpen[1:]
	} else if len(afterOpen) > 1 && afterOpen[0] == '\r' && afterOpen[1] == '\n' {
		afterOpen = afterOpen[2:]
	}

	closeIdx := bytes.Index(afterOpen, []byte("\n---"))
	if closeIdx < 0 {
		return nil, content
	}

	fm := afterOpen[:closeIdx]

	bodyStart := closeIdx + 4
	if bodyStart > len(afterOpen) {
		return fm, nil
	}
	body := afterOpen[bodyStart:]
	if len(body) > 0 && body[0] == '\n' {
		body = body[1:]
	}

	return fm, body
}

func parseFrontmatterTags(fmBytes []byte) ([]string, error) {
	var fm frontmatterData
	if err := yaml.Unmarshal(fmBytes, &fm); err != nil {
		return nil, err
	}

	if fm.Tags.IsZero() {
		return nil, nil
	}

	var tags []string

	switch fm.Tags.Kind {
	case yaml.ScalarNode:
		if fm.Tags.Value != "" && fm.Tags.Value != "null" && fm.Tags.Value != "~" {
			tags = append(tags, fm.Tags.Value)
		}
	case yaml.SequenceNode:
		for _, node := range fm.Tags.Content {
			if node.Kind == yaml.ScalarNode && node.Value != "" {
				tags = append(tags, node.Value)
			}
		}
	}

	return tags, nil
}

func extractInlineTags(md goldmark.Markdown, body []byte) []string {
	if len(body) == 0 {
		return nil
	}

	reader := text.NewReader(body)
	doc := md.Parser().Parse(reader)

	var tags []string
	seen := make(map[string]bool)
	var codeDepth int

	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		isCode := n.Kind() == ast.KindCodeSpan ||
			n.Kind() == ast.KindCodeBlock ||
			n.Kind() == ast.KindFencedCodeBlock

		if entering {
			if isCode {
				codeDepth++
				return ast.WalkContinue, nil
			}
			if codeDepth > 0 {
				return ast.WalkContinue, nil
			}
			if n.Kind() == ast.KindText {
				raw := n.Text(body)
				text := string(raw)
				matches := inlineTagRe.FindAllStringSubmatch(text, -1)
				for _, m := range matches {
					tag := m[1]
					if !seen[tag] {
						seen[tag] = true
						tags = append(tags, tag)
					}
				}
			}
			return ast.WalkContinue, nil
		}

		if isCode {
			codeDepth--
		}
		return ast.WalkContinue, nil
	})

	return tags
}

func normalizeTag(tag string) string {
	tag = strings.TrimSpace(tag)
	tag = strings.ToLower(tag)
	tag = strings.ReplaceAll(tag, " ", "-")
	return tag
}
