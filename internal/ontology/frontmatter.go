package ontology

// frontmatter.go implements AddOntology, the yaml.Node-based rewriter that
// inserts (or replaces) the two ontology keys `domain:` and `intent:` in a
// markdown file's YAML frontmatter while preserving every other key, its
// value, and its declaration order verbatim.
//
// Preservation guard
//
//	A post-encode verifier re-parses both the input frontmatter and the newly
//	emitted frontmatter into top-level mappings, then for every key that is
//	not `domain:`/`intent:` it re-Marshals each side's value node and demands
//	byte-equal output. Any mismatch — value bytes, presence — causes the
//	rewriter to return ErrUnsafeRewrite without writing.
//
// Temporal-field invariant (Req 8.1-8.4)
//
//	`date`, `lastmod`, `created`, `updated`, `original_date` are explicitly
//	enumerated and checked FIRST, before the general preservation guard, so
//	regressions on temporal fields fail loud with a dedicated check. The
//	general guard covers these keys too — the enumeration is defense in
//	depth.
//
// Known limitations (documented per the design "Open Questions / Risks")
//
//	yaml.v3's Node API can lose some standalone-line comments on
//	re-encode. The guard compares VALUE BYTES of each non-ontology key
//	(via yaml.Marshal of the value node) — not surrounding comments — so
//	comment loss for non-ontology keys is tolerated. The temporal-fields
//	byte-identity (Req 8.1) is unaffected: scalar values round-trip cleanly.
//
//	CRLF line endings in the input are normalized to LF in the output
//	frontmatter and body. This is a deliberate concession: re-emitting via
//	yaml.Encoder produces LF, and concatenating with a CRLF body would yield
//	mixed line endings. The body's textual content is preserved.

import (
	"bytes"
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"
)

// ErrUnsafeRewrite is returned by AddOntology when the post-encode
// preservation guard detects that the rewrite would mutate, drop, or add
// any key that is not `domain:` or `intent:`. The caller MUST NOT write the
// proposed output to disk.
var ErrUnsafeRewrite = errors.New("ontology: rewriter would touch a non-ontology key")

// temporalKeys are the non-negotiable byte-identical frontmatter fields
// covered by both the dedicated temporal check and the general preservation
// guard (Req 8.1, 8.2).
var temporalKeys = []string{
	"date",
	"lastmod",
	"created",
	"updated",
	"original_date",
}

// AddOntology inserts (or replaces, when values differ) the two scalar keys
// `domain:` and `intent:` in the YAML frontmatter of content, preserving all
// other keys, their values, and their declaration order. If content has no
// frontmatter, AddOntology prepends a fresh block with ONLY the two ontology
// keys (no synthesized temporal fields).
//
// On any encoding outcome that would mutate, drop, or add a non-ontology key
// (per the post-encode preservation guard), AddOntology returns
// (nil, ErrUnsafeRewrite) without writing.
func AddOntology(content []byte, d Domain, i Intent) ([]byte, error) {
	// CRLF normalization (documented limitation): work on LF internally.
	content = bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))

	fmBytes, body, hasFM := splitFrontmatter(content)

	// No frontmatter → fresh prepend. Hard rule: do NOT synthesize any
	// temporal field. Only the two ontology keys.
	if !hasFM {
		fresh := []byte(fmt.Sprintf("---\ndomain: %s\nintent: %s\n---\n", string(d), string(i)))
		out := make([]byte, 0, len(fresh)+len(content))
		out = append(out, fresh...)
		out = append(out, content...)
		return out, nil
	}

	// Parse the frontmatter into a yaml.Node tree.
	var doc yaml.Node
	if err := yaml.Unmarshal(fmBytes, &doc); err != nil {
		return nil, fmt.Errorf("ontology: parse frontmatter: %w", err)
	}

	root := mappingRoot(&doc)
	if root == nil {
		// An empty frontmatter or a non-mapping document: synthesize a
		// mapping containing only the two ontology keys (the caller's
		// guarantee that "other keys" are preserved is vacuous when there
		// are none). Still emit through yaml so style is consistent.
		root = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}}
	}

	// Replace-or-append the two ontology keys, in this order: domain then
	// intent. Replacement preserves any existing key node (and therefore its
	// style); only the VALUE node is overwritten.
	setOntologyKey(root, "domain", string(d))
	setOntologyKey(root, "intent", string(i))

	// Encode the mapping back to YAML.
	var enc bytes.Buffer
	encoder := yaml.NewEncoder(&enc)
	encoder.SetIndent(2)
	if err := encoder.Encode(&doc); err != nil {
		_ = encoder.Close()
		return nil, fmt.Errorf("ontology: encode frontmatter: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("ontology: close encoder: %w", err)
	}
	newFM := enc.Bytes()

	// Temporal-fields invariant check FIRST (Req 8.1-8.4): dedicated, loud.
	if err := verifyTemporalPreservation(fmBytes, newFM); err != nil {
		return nil, err
	}

	// General preservation guard SECOND (Req 1.5): every non-ontology key
	// must have byte-identical yaml.Marshal-d value bytes pre/post.
	if err := verifyPreservation(fmBytes, newFM); err != nil {
		return nil, err
	}

	// Reassemble: `---\n` + encoded YAML (which already ends in \n) + `---\n`
	// + original body.
	out := make([]byte, 0, len(newFM)+len(body)+10)
	out = append(out, []byte("---\n")...)
	out = append(out, newFM...)
	out = append(out, []byte("---\n")...)
	out = append(out, body...)
	return out, nil
}

// splitFrontmatter returns (fmBytes, body, hasFrontmatter). When the content
// has a frontmatter block bounded by `---` fences, fmBytes is the YAML
// payload between them (without either fence) and body is everything after
// the closing fence. hasFrontmatter is false when no opening `---` is found
// or no closing `---` line exists; in that case fmBytes is nil and body is
// the original content.
//
// Tolerates leading whitespace/newlines before the opening fence and
// handles both `\n---\n` and `\n---<EOF>` for the closer. CRLF is normalized
// by the caller (AddOntology) before invocation.
func splitFrontmatter(content []byte) (fm []byte, body []byte, has bool) {
	trimmed := bytes.TrimLeft(content, " \t\n")
	if !bytes.HasPrefix(trimmed, []byte("---")) {
		return nil, content, false
	}
	afterOpen := trimmed[3:]
	if len(afterOpen) > 0 && afterOpen[0] == '\n' {
		afterOpen = afterOpen[1:]
	} else {
		// The opening `---` must be followed by a newline (or EOF — but an
		// EOF after the opener with no closer is not a frontmatter block).
		return nil, content, false
	}

	closeIdx := bytes.Index(afterOpen, []byte("\n---"))
	if closeIdx < 0 {
		return nil, content, false
	}

	fm = afterOpen[:closeIdx]
	bodyStart := closeIdx + 4 // past the "\n---"
	if bodyStart >= len(afterOpen) {
		return fm, nil, true
	}
	body = afterOpen[bodyStart:]
	if len(body) > 0 && body[0] == '\n' {
		body = body[1:]
	}
	return fm, body, true
}

// mappingRoot returns the top-level MappingNode of a parsed YAML document, or
// nil when the document is empty or its root is not a mapping.
func mappingRoot(doc *yaml.Node) *yaml.Node {
	if doc == nil || doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil
	}
	return root
}

// setOntologyKey replaces the value of the existing key in root, or appends
// a new (key,value) pair when the key is absent. The key node's style is
// preserved on replace; on append, key and value are emitted with default
// scalar style (unquoted, plain). The value is always a `!!str` scalar.
func setOntologyKey(root *yaml.Node, key, value string) {
	for i := 0; i+1 < len(root.Content); i += 2 {
		k := root.Content[i]
		if k.Value == key {
			// Preserve the key node as-is; overwrite the value node in place.
			root.Content[i+1] = &yaml.Node{
				Kind:  yaml.ScalarNode,
				Tag:   "!!str",
				Value: value,
			}
			return
		}
	}
	// Append at the end (preserves ordering of every other key).
	root.Content = append(root.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value},
	)
}

// verifyTemporalPreservation enforces the Req 8.1-8.4 hard invariant: for
// every temporal key present in oldFM, the same key must be present in newFM
// with a yaml.Marshal-identical value. Returns ErrUnsafeRewrite (wrapped
// with the offending key for debuggability) when the invariant is violated.
func verifyTemporalPreservation(oldFM, newFM []byte) error {
	oldVals, err := valueMarshalMap(oldFM)
	if err != nil {
		return fmt.Errorf("ontology: re-parse original frontmatter: %w", err)
	}
	newVals, err := valueMarshalMap(newFM)
	if err != nil {
		return fmt.Errorf("ontology: re-parse encoded frontmatter: %w", err)
	}
	for _, k := range temporalKeys {
		oldV, oldOK := oldVals[k]
		if !oldOK {
			continue
		}
		newV, newOK := newVals[k]
		if !newOK {
			return fmt.Errorf("%w: dropped temporal key %q", ErrUnsafeRewrite, k)
		}
		if !bytes.Equal(oldV, newV) {
			return fmt.Errorf("%w: temporal key %q value mutated", ErrUnsafeRewrite, k)
		}
	}
	return nil
}

// verifyPreservation enforces Req 1.5: for every key in the union of oldFM
// and newFM EXCEPT `domain` and `intent`, the key must appear in BOTH and
// the yaml.Marshal-ed value bytes must be byte-equal. Returns
// ErrUnsafeRewrite on any violation.
//
// Comparison uses yaml.Marshal(valueNode) as the canonical form, which
// normalizes whitespace/quoting differences that the yaml.v3 encoder may
// introduce for scalars and serializes complex values deterministically.
//
// Comments around non-ontology keys are NOT compared (yaml.v3's Node API
// loses some standalone-line comments on re-encode; this is the documented
// concession at the top of this file).
func verifyPreservation(oldFM, newFM []byte) error {
	oldVals, err := valueMarshalMap(oldFM)
	if err != nil {
		return fmt.Errorf("ontology: re-parse original frontmatter: %w", err)
	}
	newVals, err := valueMarshalMap(newFM)
	if err != nil {
		return fmt.Errorf("ontology: re-parse encoded frontmatter: %w", err)
	}

	// Build the union of keys, skipping the two ontology keys.
	seen := make(map[string]struct{}, len(oldVals)+len(newVals))
	for k := range oldVals {
		if k == "domain" || k == "intent" {
			continue
		}
		seen[k] = struct{}{}
	}
	for k := range newVals {
		if k == "domain" || k == "intent" {
			continue
		}
		seen[k] = struct{}{}
	}

	for k := range seen {
		oldV, oldOK := oldVals[k]
		newV, newOK := newVals[k]
		if !oldOK {
			return fmt.Errorf("%w: rewriter added non-ontology key %q", ErrUnsafeRewrite, k)
		}
		if !newOK {
			return fmt.Errorf("%w: rewriter dropped non-ontology key %q", ErrUnsafeRewrite, k)
		}
		if !bytes.Equal(oldV, newV) {
			return fmt.Errorf("%w: non-ontology key %q value changed", ErrUnsafeRewrite, k)
		}
	}
	return nil
}

// valueMarshalMap parses fmBytes as a YAML mapping and returns a map from
// key string to yaml.Marshal(valueNode) bytes. Empty input yields an empty
// map (not an error). A non-mapping root yields an empty map; the rewriter
// only invokes this on inputs known to be top-level mappings.
func valueMarshalMap(fmBytes []byte) (map[string][]byte, error) {
	out := map[string][]byte{}
	if len(bytes.TrimSpace(fmBytes)) == 0 {
		return out, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(fmBytes, &doc); err != nil {
		return nil, err
	}
	root := mappingRoot(&doc)
	if root == nil {
		return out, nil
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		k := root.Content[i]
		v := root.Content[i+1]
		b, err := yaml.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("marshal value for key %q: %w", k.Value, err)
		}
		out[k.Value] = b
	}
	return out, nil
}
