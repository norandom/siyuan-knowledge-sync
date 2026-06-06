package tags

import (
	"bytes"
	"regexp"
	"sort"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
	"gopkg.in/yaml.v3"
)

var inlineTagRe = regexp.MustCompile(`(?:\A|\s)#([\pL\pN][\pL\pN_-]*)`)

type frontmatterData struct {
	Title       yaml.Node `yaml:"title"`
	Tags        yaml.Node `yaml:"tags"`
	Domain      yaml.Node `yaml:"domain"`
	Intent      yaml.Node `yaml:"intent"`
	LastUpdated yaml.Node `yaml:"last_updated"`
	Date        yaml.Node `yaml:"date"`
	OriginalDate yaml.Node `yaml:"original_date"`
}

// Meta is the result of a single-pass frontmatter + tag extraction.
type Meta struct {
	Title       string            // frontmatter "title" scalar; "" when absent/unparseable
	Body        []byte            // content with the YAML frontmatter block removed
	Attrs       map[string]string // existing custom-<tag> map (frontmatter + inline)
	Domain      string            // frontmatter "domain" scalar; "" when absent/non-scalar/null
	Intent      string            // frontmatter "intent" scalar; "" when absent/non-scalar/null
	LastUpdated string            // first non-empty of last_updated > date > original_date; "" when absent
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
	_, _, _, _, _, attrs, err := e.extract(content)
	if err != nil {
		return nil, err
	}
	return attrs, nil
}

// ExtractMeta performs a single-pass extraction of the frontmatter title, the
// frontmatter-stripped body, and the custom-<tag> attribute map (identical to
// what Extract returns for the same input). Malformed frontmatter returns a
// non-nil error and a zero Meta rather than a partial result.
//
// ExtractMeta additionally surfaces the frontmatter "domain" and "intent"
// scalars on Meta.Domain / Meta.Intent and injects them into Meta.Attrs as
// "custom-domain" / "custom-intent" so the existing SetBlockAttrs call in the
// sync engine picks them up unchanged. The legacy Extract entry point used by
// the compliance audit is intentionally unaffected by this injection.
func (e *TagExtractor) ExtractMeta(content []byte) (Meta, error) {
	title, body, domain, intent, lastUpdated, attrs, err := e.extract(content)
	if err != nil {
		return Meta{}, err
	}
	if domain != "" {
		attrs["custom-domain"] = domain
	}
	if intent != "" {
		attrs["custom-intent"] = intent
	}
	// Forward the source-of-truth timestamp from frontmatter as a queryable
	// block attribute. SiYuan's own `updated` field is the in-SiYuan
	// modification time and gets clobbered on every sync; this preserves
	// when the original work was actually done.
	if lastUpdated != "" {
		attrs["custom-last-updated"] = lastUpdated
	}
	// Also emit the visible-chip variant: SiYuan renders the `tags` (plural)
	// attribute as the clickable tag pills at the top of each doc. The
	// `custom-<tag>` markers we set above are queryable via SQL but do NOT
	// appear in the UI tag panel. Gather every `custom-<x>` suffix that's
	// NOT a reserved metadata key, sort for determinism, and join with
	// commas (the format SiYuan parses for the tag list).
	if visibleTags := collectVisibleTags(attrs); visibleTags != "" {
		attrs["tags"] = visibleTags
	}
	return Meta{Title: title, Body: body, Attrs: attrs, Domain: domain, Intent: intent, LastUpdated: lastUpdated}, nil
}

// reservedAttrSuffixes holds the `custom-<suffix>` keys that carry
// ontology metadata, not user-supplied tags. They're excluded from the
// visible `tags` attribute so the UI chips don't show `domain`,
// `intent`, or `last-updated` as if they were content tags.
var reservedAttrSuffixes = map[string]struct{}{
	"domain":       {},
	"intent":       {},
	"last-updated": {},
}

// collectVisibleTags returns the comma-separated, sorted list of tag
// suffixes from the `custom-<tag>` keys in attrs, excluding any reserved
// ontology-metadata suffixes. Returns "" if no real tags are present so
// the caller knows not to set the attribute at all.
func collectVisibleTags(attrs map[string]string) string {
	var list []string
	for k := range attrs {
		if !strings.HasPrefix(k, "custom-") {
			continue
		}
		suffix := strings.TrimPrefix(k, "custom-")
		if _, reserved := reservedAttrSuffixes[suffix]; reserved {
			continue
		}
		list = append(list, suffix)
	}
	if len(list) == 0 {
		return ""
	}
	sort.Strings(list)
	return strings.Join(list, ",")
}

// extract is the shared single-pass core used by both Extract and ExtractMeta
// so the custom-<tag> attribute map cannot drift between the two entry points.
// It returns the raw domain/intent scalars to ExtractMeta but does NOT inject
// them into the attribute map itself — Extract's audit-path output must remain
// tag-only.
func (e *TagExtractor) extract(content []byte) (title string, body []byte, domain string, intent string, lastUpdated string, attrs map[string]string, err error) {
	result := make(map[string]string)

	fmBytes, body := splitFrontmatter(content)

	if fmBytes != nil {
		parsed, perr := parseFrontmatter(fmBytes)
		if perr != nil {
			return "", nil, "", "", "", nil, perr
		}
		title = parsed.title
		domain = parsed.domain
		intent = parsed.intent
		lastUpdated = parsed.lastUpdated
		for _, tag := range parsed.tags {
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

	return title, body, domain, intent, lastUpdated, result, nil
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

// parsedFrontmatter is the structured result of the single YAML unmarshal so
// callers can pull scalars (title/domain/intent) and the tag list without
// re-parsing.
type parsedFrontmatter struct {
	title       string
	tags        []string
	domain      string
	intent      string
	lastUpdated string // first non-empty of last_updated > date > original_date
}

// parseFrontmatter unmarshals the YAML frontmatter once and returns the title,
// domain, and intent scalars (empty when absent or non-scalar) together with
// the tag list.
func parseFrontmatter(fmBytes []byte) (parsedFrontmatter, error) {
	var fm frontmatterData
	if err := yaml.Unmarshal(fmBytes, &fm); err != nil {
		return parsedFrontmatter{}, err
	}

	// last_updated takes precedence; date and original_date are fall-backs
	// for older fixtures. The engine forwards this value as the
	// `custom-last-updated` block attribute so SiYuan can show + sort by
	// the original timestamp regardless of when the doc was synced.
	lastUpdated := scalarValue(fm.LastUpdated)
	if lastUpdated == "" {
		lastUpdated = scalarValue(fm.Date)
	}
	if lastUpdated == "" {
		lastUpdated = scalarValue(fm.OriginalDate)
	}

	return parsedFrontmatter{
		title:       scalarValue(fm.Title),
		tags:        tagsFromNode(fm.Tags),
		domain:      scalarValue(fm.Domain),
		intent:      scalarValue(fm.Intent),
		lastUpdated: lastUpdated,
	}, nil
}

// scalarValue returns the string value of a yaml.Node only when the node is a
// non-zero, non-null scalar; otherwise it returns "". This preserves the
// existing title-extraction semantics and applies the same rule to the new
// domain/intent fields so non-scalar (sequence/mapping) or null values are
// surfaced as "" — leaving validation to the schema layer (task 2.4).
func scalarValue(node yaml.Node) string {
	if node.IsZero() || node.Kind != yaml.ScalarNode {
		return ""
	}
	v := node.Value
	if v == "" || v == "null" || v == "~" {
		return ""
	}
	return v
}

// parseFrontmatterTags is retained for backward compatibility; it delegates to
// the shared single-pass parser and exposes only the tag list.
func parseFrontmatterTags(fmBytes []byte) ([]string, error) {
	parsed, err := parseFrontmatter(fmBytes)
	if err != nil {
		return nil, err
	}
	return parsed.tags, nil
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
	tag = strings.TrimLeft(tag, "#")
	var b strings.Builder
	b.Grow(len(tag))
	for _, r := range tag {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
