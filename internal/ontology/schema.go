// Package ontology defines the closed-enum bi-modal frontmatter schema
// (`domain:` + `intent:`) that the SiYuan Knowledge Sync gate enforces.
//
// This package is the single source of truth for the two enums and their
// validation. It depends on no other internal package; every downstream
// consumer (compliance audit, sync engine, migrate plan, CLI `schema`
// subcommand, agent Skill via `schema --json`) reads from here.
package ontology

import (
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Domain is the noun axis of the ontology. It drives physical placement
// (canonical-folder map) and is one of a closed set of string values.
type Domain string

// Closed Domain enum. Order is the canonical emission order for AllDomains
// and any user-facing list (CLI, SchemaViolation.Allowed).
const (
	DevOps       Domain = "devops"
	Forensics    Domain = "forensics"
	Security     Domain = "security"
	AIML         Domain = "ai-ml"
	SoftwareDev  Domain = "software-dev"
	QuantFinance Domain = "quant-finance" // reserved, initially empty
)

// Intent is the verb axis of the ontology. It drives agent semantic search
// and is one of a closed set of string values.
type Intent string

// Closed Intent enum. Order is the canonical emission order for AllIntents
// and any user-facing list.
const (
	IntentConfig   Intent = "config"
	IntentSOP      Intent = "sop"
	IntentLog      Intent = "log"
	IntentDecision Intent = "decision"
	IntentConcept  Intent = "concept"
)

// allDomainsCanonical is the immutable in-package canonical ordering.
// Callers receive copies via AllDomains so they cannot mutate it.
var allDomainsCanonical = []Domain{
	DevOps,
	Forensics,
	Security,
	AIML,
	SoftwareDev,
	QuantFinance,
}

var allIntentsCanonical = []Intent{
	IntentConfig,
	IntentSOP,
	IntentLog,
	IntentDecision,
	IntentConcept,
}

// AllDomains returns a fresh copy of the closed Domain enum in canonical
// order. Callers may mutate the returned slice without affecting subsequent
// calls.
func AllDomains() []Domain {
	out := make([]Domain, len(allDomainsCanonical))
	copy(out, allDomainsCanonical)
	return out
}

// AllIntents returns a fresh copy of the closed Intent enum in canonical
// order. Callers may mutate the returned slice without affecting subsequent
// calls.
func AllIntents() []Intent {
	out := make([]Intent, len(allIntentsCanonical))
	copy(out, allIntentsCanonical)
	return out
}

// SchemaViolation is the structured error emitted by the ontology gate.
// It is JSON-marshalable so the CLI can write one violation per line to
// stderr and agents can parse it for self-correction.
type SchemaViolation struct {
	File           string   `json:"file"`
	Key            string   `json:"key"`
	OffendingValue string   `json:"offending_value"`
	Allowed        []string `json:"allowed"`
}

// Error implements the error interface with a human-readable summary.
// The structured form is the JSON marshal of the struct.
func (v SchemaViolation) Error() string {
	return fmt.Sprintf(
		"ontology schema violation in %s: key=%q offending=%q allowed=[%s]",
		v.File, v.Key, v.OffendingValue, strings.Join(v.Allowed, ","),
	)
}

// MarshalJSON is provided explicitly so that *SchemaViolation values
// emitted as errors round-trip through json.Marshal with the same shape
// as the struct.
func (v SchemaViolation) MarshalJSON() ([]byte, error) {
	type alias SchemaViolation
	return json.Marshal(alias(v))
}

// allowedDomainStrings returns the AllDomains values as a fresh []string,
// used to populate SchemaViolation.Allowed.
func allowedDomainStrings() []string {
	ds := AllDomains()
	out := make([]string, len(ds))
	for i, d := range ds {
		out[i] = string(d)
	}
	return out
}

// allowedIntentStrings returns the AllIntents values as a fresh []string.
func allowedIntentStrings() []string {
	is := AllIntents()
	out := make([]string, len(is))
	for i, in := range is {
		out[i] = string(in)
	}
	return out
}

// ValidateDomain reports whether value is a member of the closed Domain
// enum. It returns nil on success, or a *SchemaViolation describing the
// rejection (Key="domain", OffendingValue=value, Allowed=AllDomains()).
//
// Comparison is case-sensitive and whitespace-strict. An empty string is
// treated as out-of-enum (use CheckOntologyFrontmatter for missing-key
// detection from a parsed YAML view).
func ValidateDomain(value string) *SchemaViolation {
	for _, d := range allDomainsCanonical {
		if string(d) == value {
			return nil
		}
	}
	return &SchemaViolation{
		Key:            "domain",
		OffendingValue: value,
		Allowed:        allowedDomainStrings(),
	}
}

// ValidateIntent reports whether value is a member of the closed Intent
// enum. Semantics mirror ValidateDomain.
func ValidateIntent(value string) *SchemaViolation {
	for _, in := range allIntentsCanonical {
		if string(in) == value {
			return nil
		}
	}
	return &SchemaViolation{
		Key:            "intent",
		OffendingValue: value,
		Allowed:        allowedIntentStrings(),
	}
}

// FrontmatterView is the minimal parsed-YAML shape CheckOntologyFrontmatter
// needs. Callers (tags/extractor, compliance/audit) populate it from a
// frontmatter mapping node — a nil node means the key was absent.
type FrontmatterView struct {
	DomainNode *yaml.Node // nil if the `domain:` key is missing
	IntentNode *yaml.Node // nil if the `intent:` key is missing
}

// multiValueToken is the SchemaViolation.OffendingValue placeholder used
// when the YAML node is a sequence (list) for a key that must be scalar.
// This is a distinct violation class from out-of-enum.
const multiValueToken = "<multi-value>"

// CheckOntologyFrontmatter returns every schema violation in m for file.
// Violation classes detected:
//   - missing required key: a nil DomainNode/IntentNode
//   - multi-value: a SequenceNode (YAML list) for a key that must be scalar
//   - out-of-enum: a scalar node whose value is not in the enum
//
// The slice is empty when both keys are present, scalar, and in-enum.
// The returned slice's iteration order is: domain violation first (if any),
// intent violation second (if any).
func CheckOntologyFrontmatter(file string, m FrontmatterView) []SchemaViolation {
	var out []SchemaViolation

	if v := checkOne(file, "domain", m.DomainNode, allowedDomainStrings(), ValidateDomain); v != nil {
		out = append(out, *v)
	}
	if v := checkOne(file, "intent", m.IntentNode, allowedIntentStrings(), ValidateIntent); v != nil {
		out = append(out, *v)
	}
	return out
}

// checkOne runs the missing/multi-value/out-of-enum cascade for a single
// frontmatter key.
func checkOne(
	file, key string,
	node *yaml.Node,
	allowed []string,
	validate func(string) *SchemaViolation,
) *SchemaViolation {
	if node == nil {
		return &SchemaViolation{
			File:           file,
			Key:            key,
			OffendingValue: "",
			Allowed:        allowed,
		}
	}
	if node.Kind == yaml.SequenceNode {
		return &SchemaViolation{
			File:           file,
			Key:            key,
			OffendingValue: multiValueToken,
			Allowed:        allowed,
		}
	}
	if v := validate(node.Value); v != nil {
		v.File = file
		return v
	}
	return nil
}
