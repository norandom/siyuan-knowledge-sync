package toc

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

type TOCGenerator struct {
	md goldmark.Markdown
}

type heading struct {
	Level int
	Text  string
}

func NewTOCGenerator() *TOCGenerator {
	return &TOCGenerator{
		md: goldmark.New(),
	}
}

func (g *TOCGenerator) Generate(content []byte) (string, error) {
	if len(content) == 0 {
		return "", nil
	}

	reader := text.NewReader(content)
	doc := g.md.Parser().Parse(reader)

	var headings []heading
	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if n.Kind() == ast.KindHeading {
			h := n.(*ast.Heading)
			txt := extractText(n, content)
			if txt != "" {
				headings = append(headings, heading{
					Level: h.Level,
					Text:  txt,
				})
			}
		}
		return ast.WalkContinue, nil
	})

	if len(headings) == 0 {
		return "", nil
	}

	return buildTOC(headings), nil
}

func extractText(n ast.Node, source []byte) string {
	var parts []string
	ast.Walk(n, func(child ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if child.Kind() == ast.KindText {
			parts = append(parts, string(child.Text(source)))
		}
		return ast.WalkContinue, nil
	})
	return strings.TrimSpace(strings.Join(parts, ""))
}

func buildTOC(headings []heading) string {
	var b strings.Builder
	for i, h := range headings {
		indent := strings.Repeat("  ", h.Level-1)
		slug := slugify(h.Text)
		b.WriteString(fmt.Sprintf("%s- [%s](#%s)", indent, h.Text, slug))
		if i < len(headings)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func slugify(text string) string {
	var b strings.Builder
	wasHyphen := false

	for _, r := range strings.TrimSpace(strings.ToLower(text)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			wasHyphen = false
		} else if r == ' ' || r == '-' {
			if !wasHyphen && b.Len() > 0 {
				b.WriteByte('-')
				wasHyphen = true
			}
		}
	}

	result := b.String()
	for strings.HasSuffix(result, "-") {
		result = result[:len(result)-1]
	}
	return result
}
