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
	Title yaml.Node `yaml:"title"`
	Tags  yaml.Node `yaml:"tags"`
}

// Meta is the result of a single-pass frontmatter + tag extraction.
type Meta struct {
	Title string            // frontmatter "title" scalar; "" when absent/unparseable
	Body  []byte            // content with the YAML frontmatter block removed
	Attrs map[string]string // existing custom-<tag> map (frontmatter + inline)
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
	_, _, attrs, err := e.extract(content)
	if err != nil {
		return nil, err
	}
	return attrs, nil
}

// ExtractMeta performs a single-pass extraction of the frontmatter title, the
// frontmatter-stripped body, and the custom-<tag> attribute map (identical to
// what Extract returns for the same input). Malformed frontmatter returns a
// non-nil error and a zero Meta rather than a partial result.
func (e *TagExtractor) ExtractMeta(content []byte) (Meta, error) {
	title, body, attrs, err := e.extract(content)
	if err != nil {
		return Meta{}, err
	}
	return Meta{Title: title, Body: body, Attrs: attrs}, nil
}

// extract is the shared single-pass core used by both Extract and ExtractMeta
// so the custom-<tag> attribute map cannot drift between the two entry points.
func (e *TagExtractor) extract(content []byte) (title string, body []byte, attrs map[string]string, err error) {
	result := make(map[string]string)

	fmBytes, body := splitFrontmatter(content)

	if fmBytes != nil {
		parsedTitle, tags, perr := parseFrontmatter(fmBytes)
		if perr != nil {
			return "", nil, nil, perr
		}
		title = parsedTitle
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

	return title, body, result, nil
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

// parseFrontmatter unmarshals the YAML frontmatter once and returns the title
// scalar (empty when absent or non-scalar) together with the tag list.
func parseFrontmatter(fmBytes []byte) (string, []string, error) {
	var fm frontmatterData
	if err := yaml.Unmarshal(fmBytes, &fm); err != nil {
		return "", nil, err
	}

	title := ""
	if !fm.Title.IsZero() && fm.Title.Kind == yaml.ScalarNode {
		if v := fm.Title.Value; v != "" && v != "null" && v != "~" {
			title = v
		}
	}

	tags := tagsFromNode(fm.Tags)
	return title, tags, nil
}

// parseFrontmatterTags is retained for backward compatibility; it delegates to
// the shared single-pass parser and exposes only the tag list.
func parseFrontmatterTags(fmBytes []byte) ([]string, error) {
	_, tags, err := parseFrontmatter(fmBytes)
	return tags, err
}

func tagsFromNode(node yaml.Node) []string {
	if node.IsZero() {
		return nil
	}

	var tags []string
	switch node.Kind {
	case yaml.ScalarNode:
		if node.Value != "" && node.Value != "null" && node.Value != "~" {
			tags = append(tags, node.Value)
		}
	case yaml.SequenceNode:
		for _, n := range node.Content {
			if n.Kind == yaml.ScalarNode && n.Value != "" {
				tags = append(tags, n.Value)
			}
		}
	}

	return tags
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
