package ontology

import (
	"reflect"
	"strings"
	"testing"
)

// --- Defaults preserved when Configure is never called -----------------------

func TestConfigure_DefaultsPreservedWhenNoCall(t *testing.T) {
	resetToDefaultsForTest()
	t.Cleanup(resetToDefaultsForTest)

	gotDomains := AllDomains()
	wantDomains := []Domain{
		DevOps,
		Forensics,
		Security,
		AIML,
		SoftwareDev,
		QuantFinance,
	}
	if !reflect.DeepEqual(gotDomains, wantDomains) {
		t.Fatalf("AllDomains() = %v, want %v", gotDomains, wantDomains)
	}

	gotIntents := AllIntents()
	wantIntents := []Intent{
		IntentConfig,
		IntentSOP,
		IntentLog,
		IntentDecision,
		IntentConcept,
	}
	if !reflect.DeepEqual(gotIntents, wantIntents) {
		t.Fatalf("AllIntents() = %v, want %v", gotIntents, wantIntents)
	}

	if got := (Router{}).CanonicalFolder(DevOps); got != "Linux & DevOps" {
		t.Fatalf("CanonicalFolder(DevOps) = %q, want %q", got, "Linux & DevOps")
	}
}

// --- Duplicate domain id ------------------------------------------------------

func TestConfigure_RejectsDuplicateDomainID(t *testing.T) {
	resetToDefaultsForTest()
	t.Cleanup(resetToDefaultsForTest)

	before := AllDomains()
	opts := ConfigureOptions{
		Domains: []ConfigureDomain{
			{ID: "devops", Folder: "Linux & DevOps"},
			{ID: "devops", Folder: "Other Folder"},
		},
		Intents: []ConfigureIntent{{ID: "config"}},
	}
	if err := Configure(opts); err == nil {
		t.Fatal("expected non-nil error for duplicate domain id, got nil")
	}
	if got := AllDomains(); !reflect.DeepEqual(got, before) {
		t.Fatalf("AllDomains() mutated on error path: got %v, want %v", got, before)
	}
}

// --- Duplicate domain folder --------------------------------------------------

func TestConfigure_RejectsDuplicateDomainFolder(t *testing.T) {
	resetToDefaultsForTest()
	t.Cleanup(resetToDefaultsForTest)

	before := AllDomains()
	opts := ConfigureOptions{
		Domains: []ConfigureDomain{
			{ID: "devops", Folder: "Shared Folder"},
			{ID: "security", Folder: "Shared Folder"},
		},
		Intents: []ConfigureIntent{{ID: "config"}},
	}
	if err := Configure(opts); err == nil {
		t.Fatal("expected non-nil error for duplicate domain folder, got nil")
	}
	if got := AllDomains(); !reflect.DeepEqual(got, before) {
		t.Fatalf("AllDomains() mutated on error path: got %v, want %v", got, before)
	}
}

// --- Reserved folder prefix ---------------------------------------------------

func TestConfigure_RejectsReservedFolderPrefix(t *testing.T) {
	cases := map[string]string{
		"underscore-prefix": "_index",
		"slash-prefix":      "/absolute",
	}
	for name, folder := range cases {
		t.Run(name, func(t *testing.T) {
			resetToDefaultsForTest()
			t.Cleanup(resetToDefaultsForTest)

			before := AllDomains()
			opts := ConfigureOptions{
				Domains: []ConfigureDomain{
					{ID: "devops", Folder: folder},
				},
				Intents: []ConfigureIntent{{ID: "config"}},
			}
			if err := Configure(opts); err == nil {
				t.Fatalf("expected non-nil error for reserved folder prefix %q, got nil", folder)
			}
			if got := AllDomains(); !reflect.DeepEqual(got, before) {
				t.Fatalf("AllDomains() mutated on error path: got %v, want %v", got, before)
			}
		})
	}
}

// --- Invalid domain id charset ------------------------------------------------

func TestConfigure_RejectsInvalidDomainIDCharset(t *testing.T) {
	cases := map[string]string{
		"uppercase":           "DevOps",
		"starts-with-digit":   "9domain",
		"has-underscore":      "my_domain",
	}
	for name, id := range cases {
		t.Run(name, func(t *testing.T) {
			resetToDefaultsForTest()
			t.Cleanup(resetToDefaultsForTest)

			before := AllDomains()
			opts := ConfigureOptions{
				Domains: []ConfigureDomain{
					{ID: id, Folder: "Some Folder"},
				},
				Intents: []ConfigureIntent{{ID: "config"}},
			}
			if err := Configure(opts); err == nil {
				t.Fatalf("expected non-nil error for invalid domain id %q, got nil", id)
			}
			if got := AllDomains(); !reflect.DeepEqual(got, before) {
				t.Fatalf("AllDomains() mutated on error path: got %v, want %v", got, before)
			}
		})
	}
}

// --- Invalid intent id charset ------------------------------------------------

func TestConfigure_RejectsInvalidIntentIDCharset(t *testing.T) {
	cases := map[string]string{
		"uppercase":         "Config",
		"starts-with-digit": "9config",
		"has-underscore":    "my_intent",
	}
	for name, id := range cases {
		t.Run(name, func(t *testing.T) {
			resetToDefaultsForTest()
			t.Cleanup(resetToDefaultsForTest)

			beforeI := AllIntents()
			opts := ConfigureOptions{
				Domains: []ConfigureDomain{
					{ID: "devops", Folder: "Linux & DevOps"},
				},
				Intents: []ConfigureIntent{{ID: id}},
			}
			if err := Configure(opts); err == nil {
				t.Fatalf("expected non-nil error for invalid intent id %q, got nil", id)
			}
			if got := AllIntents(); !reflect.DeepEqual(got, beforeI) {
				t.Fatalf("AllIntents() mutated on error path: got %v, want %v", got, beforeI)
			}
		})
	}
}

// --- Duplicate intent id ------------------------------------------------------

func TestConfigure_RejectsDuplicateIntentID(t *testing.T) {
	resetToDefaultsForTest()
	t.Cleanup(resetToDefaultsForTest)

	before := AllIntents()
	opts := ConfigureOptions{
		Domains: []ConfigureDomain{
			{ID: "devops", Folder: "Linux & DevOps"},
		},
		Intents: []ConfigureIntent{
			{ID: "config"},
			{ID: "config"},
		},
	}
	if err := Configure(opts); err == nil {
		t.Fatal("expected non-nil error for duplicate intent id, got nil")
	}
	if got := AllIntents(); !reflect.DeepEqual(got, before) {
		t.Fatalf("AllIntents() mutated on error path: got %v, want %v", got, before)
	}
}

// --- Empty domains ------------------------------------------------------------

func TestConfigure_RejectsEmptyDomains(t *testing.T) {
	resetToDefaultsForTest()
	t.Cleanup(resetToDefaultsForTest)

	before := AllDomains()
	opts := ConfigureOptions{
		Domains: nil,
		Intents: []ConfigureIntent{{ID: "config"}},
	}
	if err := Configure(opts); err == nil {
		t.Fatal("expected non-nil error for empty domains, got nil")
	}
	if got := AllDomains(); !reflect.DeepEqual(got, before) {
		t.Fatalf("AllDomains() mutated on error path: got %v, want %v", got, before)
	}
}

// --- Empty intents ------------------------------------------------------------

func TestConfigure_RejectsEmptyIntents(t *testing.T) {
	resetToDefaultsForTest()
	t.Cleanup(resetToDefaultsForTest)

	before := AllIntents()
	opts := ConfigureOptions{
		Domains: []ConfigureDomain{
			{ID: "devops", Folder: "Linux & DevOps"},
		},
		Intents: nil,
	}
	if err := Configure(opts); err == nil {
		t.Fatal("expected non-nil error for empty intents, got nil")
	}
	if got := AllIntents(); !reflect.DeepEqual(got, before) {
		t.Fatalf("AllIntents() mutated on error path: got %v, want %v", got, before)
	}
}

// --- Domain input order preserved ---------------------------------------------

func TestConfigure_PreservesDomainInputOrder(t *testing.T) {
	resetToDefaultsForTest()
	t.Cleanup(resetToDefaultsForTest)

	in := []ConfigureDomain{
		{ID: "quant-finance", Folder: "Quant Finance"},
		{ID: "security", Folder: "Security"},
		{ID: "ai-ml", Folder: "AI & ML"},
		{ID: "devops", Folder: "Linux & DevOps"},
		{ID: "software-dev", Folder: "Software Development"},
	}
	opts := ConfigureOptions{
		Domains: in,
		Intents: []ConfigureIntent{{ID: "config"}},
	}
	if err := Configure(opts); err != nil {
		t.Fatalf("Configure: unexpected error: %v", err)
	}

	got := AllDomains()
	want := []Domain{
		Domain("quant-finance"),
		Domain("security"),
		Domain("ai-ml"),
		Domain("devops"),
		Domain("software-dev"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AllDomains() = %v, want %v", got, want)
	}

	r := Router{}
	for _, d := range in {
		if got := r.CanonicalFolder(Domain(d.ID)); got != d.Folder {
			t.Fatalf("CanonicalFolder(%q) = %q, want %q", d.ID, got, d.Folder)
		}
	}
}

// --- Intent input order preserved ---------------------------------------------

func TestConfigure_PreservesIntentInputOrder(t *testing.T) {
	resetToDefaultsForTest()
	t.Cleanup(resetToDefaultsForTest)

	in := []ConfigureIntent{
		{ID: "concept"},
		{ID: "log"},
		{ID: "config"},
		{ID: "sop"},
		{ID: "decision"},
	}
	opts := ConfigureOptions{
		Domains: []ConfigureDomain{
			{ID: "devops", Folder: "Linux & DevOps"},
		},
		Intents: in,
	}
	if err := Configure(opts); err != nil {
		t.Fatalf("Configure: unexpected error: %v", err)
	}

	got := AllIntents()
	want := []Intent{
		Intent("concept"),
		Intent("log"),
		Intent("config"),
		Intent("sop"),
		Intent("decision"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AllIntents() = %v, want %v", got, want)
	}
}

// --- Idempotent + resettable --------------------------------------------------

func TestConfigure_IsIdempotentAndResettable(t *testing.T) {
	resetToDefaultsForTest()
	t.Cleanup(resetToDefaultsForTest)

	first := ConfigureOptions{
		Domains: []ConfigureDomain{
			{ID: "alpha", Folder: "Alpha Folder"},
			{ID: "beta", Folder: "Beta Folder"},
		},
		Intents: []ConfigureIntent{{ID: "config"}, {ID: "sop"}},
	}
	if err := Configure(first); err != nil {
		t.Fatalf("Configure(first): unexpected error: %v", err)
	}
	got := AllDomains()
	want := []Domain{Domain("alpha"), Domain("beta")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("after first Configure: AllDomains() = %v, want %v", got, want)
	}

	resetToDefaultsForTest()
	got = AllDomains()
	wantDefaults := []Domain{
		DevOps,
		Forensics,
		Security,
		AIML,
		SoftwareDev,
		QuantFinance,
	}
	if !reflect.DeepEqual(got, wantDefaults) {
		t.Fatalf("after reset: AllDomains() = %v, want %v", got, wantDefaults)
	}

	second := ConfigureOptions{
		Domains: []ConfigureDomain{
			{ID: "gamma", Folder: "Gamma Folder"},
		},
		Intents: []ConfigureIntent{{ID: "config"}},
	}
	if err := Configure(second); err != nil {
		t.Fatalf("Configure(second): unexpected error: %v", err)
	}
	got = AllDomains()
	want = []Domain{Domain("gamma")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("after second Configure: AllDomains() = %v, want %v", got, want)
	}
	if got := (Router{}).CanonicalFolder(Domain("gamma")); got != "Gamma Folder" {
		t.Fatalf("CanonicalFolder(gamma) = %q, want %q", got, "Gamma Folder")
	}
}

// --- Aggregated validation errors --------------------------------------------

func TestConfigure_AggregatesValidationErrors(t *testing.T) {
	resetToDefaultsForTest()
	t.Cleanup(resetToDefaultsForTest)

	// Violations baked in (at least three):
	//   1. duplicate domain id "devops"
	//   2. reserved folder prefix on "_reserved"
	//   3. invalid intent id charset "Bad_Intent"
	opts := ConfigureOptions{
		Domains: []ConfigureDomain{
			{ID: "devops", Folder: "Linux & DevOps"},
			{ID: "devops", Folder: "_reserved"},
		},
		Intents: []ConfigureIntent{
			{ID: "Bad_Intent"},
		},
	}
	err := Configure(opts)
	if err == nil {
		t.Fatal("expected aggregated error, got nil")
	}
	msg := err.Error()
	wantSubstrings := []string{"devops", "_reserved", "Bad_Intent"}
	for _, s := range wantSubstrings {
		if !strings.Contains(msg, s) {
			t.Fatalf("aggregated error %q does not mention %q", msg, s)
		}
	}
}
