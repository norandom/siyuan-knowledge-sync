package ontology

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// --- Enum membership / ordering ----------------------------------------------

func TestAllDomains_ExactOrderAndMembership(t *testing.T) {
	got := AllDomains()
	want := []Domain{
		DevOps,
		Forensics,
		Security,
		AIML,
		SoftwareDev,
		QuantFinance,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AllDomains() = %v, want %v", got, want)
	}

	// Spelling lockdown — these strings are part of the spec contract.
	expectedValues := []string{
		"devops",
		"forensics",
		"security",
		"ai-ml",
		"software-dev",
		"quant-finance",
	}
	if len(got) != len(expectedValues) {
		t.Fatalf("AllDomains() length = %d, want %d", len(got), len(expectedValues))
	}
	for i, d := range got {
		if string(d) != expectedValues[i] {
			t.Fatalf("AllDomains()[%d] = %q, want %q", i, string(d), expectedValues[i])
		}
	}
}

func TestAllIntents_ExactOrderAndMembership(t *testing.T) {
	got := AllIntents()
	want := []Intent{
		IntentConfig,
		IntentSOP,
		IntentLog,
		IntentDecision,
		IntentConcept,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AllIntents() = %v, want %v", got, want)
	}

	expectedValues := []string{
		"config",
		"sop",
		"log",
		"decision",
		"concept",
	}
	if len(got) != len(expectedValues) {
		t.Fatalf("AllIntents() length = %d, want %d", len(got), len(expectedValues))
	}
	for i, in := range got {
		if string(in) != expectedValues[i] {
			t.Fatalf("AllIntents()[%d] = %q, want %q", i, string(in), expectedValues[i])
		}
	}
}

func TestAllDomains_ReturnsFreshSliceEachCall(t *testing.T) {
	a := AllDomains()
	if len(a) == 0 {
		t.Fatalf("AllDomains() returned empty slice")
	}
	a[0] = Domain("MUTATED")
	b := AllDomains()
	if b[0] == Domain("MUTATED") {
		t.Fatalf("AllDomains() returned a shared slice; caller mutation leaked: %v", b)
	}
}

func TestAllIntents_ReturnsFreshSliceEachCall(t *testing.T) {
	a := AllIntents()
	if len(a) == 0 {
		t.Fatalf("AllIntents() returned empty slice")
	}
	a[0] = Intent("MUTATED")
	b := AllIntents()
	if b[0] == Intent("MUTATED") {
		t.Fatalf("AllIntents() returned a shared slice; caller mutation leaked: %v", b)
	}
}

// --- ValidateDomain round-trip + rejection -----------------------------------

func TestValidateDomain_RoundTripAll(t *testing.T) {
	for _, d := range AllDomains() {
		d := d
		t.Run(string(d), func(t *testing.T) {
			if v := ValidateDomain(string(d)); v != nil {
				t.Fatalf("ValidateDomain(%q) returned violation %+v, want nil", string(d), v)
			}
		})
	}
}

func TestValidateDomain_RejectsUnknown(t *testing.T) {
	cases := []string{
		"unknown",
		"DevOps",        // case-sensitive: uppercase is invalid
		"DEVOPS",        // all caps invalid
		"ai_ml",         // wrong separator
		"",              // empty string treated as out-of-enum
		" devops",       // whitespace not trimmed
		"devops ",       // trailing whitespace not trimmed
		"random-domain", // unrelated
	}
	for _, v := range cases {
		v := v
		t.Run("input="+v, func(t *testing.T) {
			got := ValidateDomain(v)
			if got == nil {
				t.Fatalf("ValidateDomain(%q) = nil, want *SchemaViolation", v)
			}
			if got.Key != "domain" {
				t.Errorf("violation.Key = %q, want %q", got.Key, "domain")
			}
			if got.OffendingValue != v {
				t.Errorf("violation.OffendingValue = %q, want %q", got.OffendingValue, v)
			}
			// Allowed slice must list every Domain value exactly.
			wantAllowed := make([]string, 0, len(AllDomains()))
			for _, d := range AllDomains() {
				wantAllowed = append(wantAllowed, string(d))
			}
			if !reflect.DeepEqual(got.Allowed, wantAllowed) {
				t.Errorf("violation.Allowed = %v, want %v", got.Allowed, wantAllowed)
			}
		})
	}
}

// --- ValidateIntent round-trip + rejection -----------------------------------

func TestValidateIntent_RoundTripAll(t *testing.T) {
	for _, in := range AllIntents() {
		in := in
		t.Run(string(in), func(t *testing.T) {
			if v := ValidateIntent(string(in)); v != nil {
				t.Fatalf("ValidateIntent(%q) returned violation %+v, want nil", string(in), v)
			}
		})
	}
}

func TestValidateIntent_RejectsUnknown(t *testing.T) {
	cases := []string{
		"braindump",
		"Config", // case-sensitive
		"SOP",
		"",
		"howto",
		" config",
	}
	for _, v := range cases {
		v := v
		t.Run("input="+v, func(t *testing.T) {
			got := ValidateIntent(v)
			if got == nil {
				t.Fatalf("ValidateIntent(%q) = nil, want *SchemaViolation", v)
			}
			if got.Key != "intent" {
				t.Errorf("violation.Key = %q, want %q", got.Key, "intent")
			}
			if got.OffendingValue != v {
				t.Errorf("violation.OffendingValue = %q, want %q", got.OffendingValue, v)
			}
			wantAllowed := make([]string, 0, len(AllIntents()))
			for _, in := range AllIntents() {
				wantAllowed = append(wantAllowed, string(in))
			}
			if !reflect.DeepEqual(got.Allowed, wantAllowed) {
				t.Errorf("violation.Allowed = %v, want %v", got.Allowed, wantAllowed)
			}
		})
	}
}

// --- SchemaViolation shape ---------------------------------------------------

func TestSchemaViolation_JSONShape(t *testing.T) {
	v := SchemaViolation{
		File:           "wiki/devops/foo.md",
		Key:            "intent",
		OffendingValue: "braindump",
		Allowed:        []string{"config", "sop", "log", "decision", "concept"},
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	got := string(b)
	for _, want := range []string{
		`"file":"wiki/devops/foo.md"`,
		`"key":"intent"`,
		`"offending_value":"braindump"`,
		`"allowed":["config","sop","log","decision","concept"]`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("json output %q missing %q", got, want)
		}
	}
}

func TestSchemaViolation_ErrorMethod(t *testing.T) {
	v := SchemaViolation{
		File:           "wiki/devops/foo.md",
		Key:            "intent",
		OffendingValue: "braindump",
		Allowed:        []string{"config", "sop", "log", "decision", "concept"},
	}
	msg := v.Error()
	if msg == "" {
		t.Fatalf("Error() returned empty string")
	}
	for _, want := range []string{"intent", "braindump", "wiki/devops/foo.md"} {
		if !strings.Contains(msg, want) {
			t.Errorf("Error() = %q, missing %q", msg, want)
		}
	}
}

// --- CheckOntologyFrontmatter ------------------------------------------------

func scalarNode(v string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
}

func seqNode(values ...string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, v := range values {
		n.Content = append(n.Content, scalarNode(v))
	}
	return n
}

func TestCheckOntologyFrontmatter_BothValid(t *testing.T) {
	got := CheckOntologyFrontmatter("wiki/devops/foo.md", FrontmatterView{
		DomainNode: scalarNode("devops"),
		IntentNode: scalarNode("config"),
	})
	if len(got) != 0 {
		t.Fatalf("expected no violations, got %+v", got)
	}
}

func TestCheckOntologyFrontmatter_MissingDomain(t *testing.T) {
	got := CheckOntologyFrontmatter("wiki/devops/foo.md", FrontmatterView{
		DomainNode: nil,
		IntentNode: scalarNode("config"),
	})
	if len(got) != 1 {
		t.Fatalf("want 1 violation, got %d: %+v", len(got), got)
	}
	v := got[0]
	if v.Key != "domain" || v.OffendingValue != "" || v.File != "wiki/devops/foo.md" {
		t.Errorf("missing-domain violation = %+v", v)
	}
	// Allowed must equal AllDomains() as strings.
	wantAllowed := make([]string, 0, len(AllDomains()))
	for _, d := range AllDomains() {
		wantAllowed = append(wantAllowed, string(d))
	}
	if !reflect.DeepEqual(v.Allowed, wantAllowed) {
		t.Errorf("violation.Allowed = %v, want %v", v.Allowed, wantAllowed)
	}
}

func TestCheckOntologyFrontmatter_MissingIntent(t *testing.T) {
	got := CheckOntologyFrontmatter("wiki/devops/foo.md", FrontmatterView{
		DomainNode: scalarNode("devops"),
		IntentNode: nil,
	})
	if len(got) != 1 {
		t.Fatalf("want 1 violation, got %d: %+v", len(got), got)
	}
	v := got[0]
	if v.Key != "intent" || v.OffendingValue != "" || v.File != "wiki/devops/foo.md" {
		t.Errorf("missing-intent violation = %+v", v)
	}
	wantAllowed := make([]string, 0, len(AllIntents()))
	for _, in := range AllIntents() {
		wantAllowed = append(wantAllowed, string(in))
	}
	if !reflect.DeepEqual(v.Allowed, wantAllowed) {
		t.Errorf("violation.Allowed = %v, want %v", v.Allowed, wantAllowed)
	}
}

func TestCheckOntologyFrontmatter_BothMissing(t *testing.T) {
	got := CheckOntologyFrontmatter("wiki/foo.md", FrontmatterView{})
	if len(got) != 2 {
		t.Fatalf("want 2 violations, got %d: %+v", len(got), got)
	}
	keys := []string{got[0].Key, got[1].Key}
	sort.Strings(keys)
	if !reflect.DeepEqual(keys, []string{"domain", "intent"}) {
		t.Errorf("violation keys = %v, want [domain intent]", keys)
	}
}

func TestCheckOntologyFrontmatter_OutOfEnum_Domain(t *testing.T) {
	got := CheckOntologyFrontmatter("wiki/foo.md", FrontmatterView{
		DomainNode: scalarNode("braindump"),
		IntentNode: scalarNode("config"),
	})
	if len(got) != 1 {
		t.Fatalf("want 1 violation, got %d: %+v", len(got), got)
	}
	v := got[0]
	if v.Key != "domain" {
		t.Errorf("violation.Key = %q, want %q", v.Key, "domain")
	}
	if v.OffendingValue != "braindump" {
		t.Errorf("violation.OffendingValue = %q, want %q", v.OffendingValue, "braindump")
	}
	if v.File != "wiki/foo.md" {
		t.Errorf("violation.File = %q, want %q", v.File, "wiki/foo.md")
	}
}

func TestCheckOntologyFrontmatter_OutOfEnum_Intent(t *testing.T) {
	got := CheckOntologyFrontmatter("wiki/foo.md", FrontmatterView{
		DomainNode: scalarNode("devops"),
		IntentNode: scalarNode("howto"),
	})
	if len(got) != 1 {
		t.Fatalf("want 1 violation, got %d: %+v", len(got), got)
	}
	v := got[0]
	if v.Key != "intent" || v.OffendingValue != "howto" {
		t.Errorf("violation = %+v", v)
	}
}

func TestCheckOntologyFrontmatter_MultiValue_Domain(t *testing.T) {
	got := CheckOntologyFrontmatter("wiki/foo.md", FrontmatterView{
		DomainNode: seqNode("devops", "security"),
		IntentNode: scalarNode("config"),
	})
	if len(got) != 1 {
		t.Fatalf("want 1 violation, got %d: %+v", len(got), got)
	}
	v := got[0]
	if v.Key != "domain" {
		t.Errorf("violation.Key = %q, want %q", v.Key, "domain")
	}
	// Spec: multi-value is a distinct violation class; the exact token
	// "multi-value" is acceptable and the test we are required to satisfy.
	if v.OffendingValue != "<multi-value>" {
		t.Errorf("violation.OffendingValue = %q, want %q", v.OffendingValue, "<multi-value>")
	}
}

func TestCheckOntologyFrontmatter_MultiValue_Intent(t *testing.T) {
	got := CheckOntologyFrontmatter("wiki/foo.md", FrontmatterView{
		DomainNode: scalarNode("devops"),
		IntentNode: seqNode("config", "sop"),
	})
	if len(got) != 1 {
		t.Fatalf("want 1 violation, got %d: %+v", len(got), got)
	}
	v := got[0]
	if v.Key != "intent" {
		t.Errorf("violation.Key = %q, want %q", v.Key, "intent")
	}
	if v.OffendingValue != "<multi-value>" {
		t.Errorf("violation.OffendingValue = %q, want %q", v.OffendingValue, "<multi-value>")
	}
}

func TestCheckOntologyFrontmatter_MultiValue_DistinctFromOutOfEnum(t *testing.T) {
	// Sanity: a multi-value violation must NOT be conflated with an out-of-enum
	// violation that happens to contain a valid value among multiples.
	multi := CheckOntologyFrontmatter("wiki/foo.md", FrontmatterView{
		DomainNode: seqNode("devops", "devops"), // each scalar individually valid
		IntentNode: scalarNode("config"),
	})
	if len(multi) != 1 {
		t.Fatalf("want 1 violation, got %d: %+v", len(multi), multi)
	}
	if multi[0].OffendingValue == "devops" {
		t.Fatalf("multi-value violation collapsed into out-of-enum; got %+v", multi[0])
	}
}
