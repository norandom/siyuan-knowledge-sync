// Package migrate tests for the versioned MigrationPlan JSON contract
// (task 2.5, ontology-gate spec, Req 6.1/6.2/6.7/10.2).
package migrate

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"siyuan-knowledge-sync/internal/ontology"
)

// samplePlan builds a deterministic plan with one entry per op kind.
// time.Date is used (not time.Now) so JSON round-trip comparison is stable.
func samplePlan() MigrationPlan {
	return MigrationPlan{
		Version:     PlanV1,
		Source:      "/Users/mc/Source/wiki/Hosting",
		GeneratedAt: time.Date(2026, 6, 5, 16, 30, 0, 0, time.UTC),
		Entries: []PlanEntry{
			{
				Op:            OpKeep,
				SourcePath:    "Hosting/nginx.md",
				Domain:        ontology.DevOps,
				Intent:        ontology.IntentSOP,
				RewrittenBody: "# nginx SOP\n\nrewritten body\n",
				Notes:         "cobesy pass applied",
			},
			{
				Op:         OpDropLocal,
				SourcePath: "Hosting/scratch.md",
				Notes:      "obsolete scratch file",
			},
			{
				Op:          OpRetireSiyuan,
				SourcePath:  "Hosting/legacy.md",
				SiYuanDocID: "20240101120000-abcdefg",
				Notes:       "superseded by new doc",
			},
		},
	}
}

func TestMigrationPlan_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	plan := samplePlan()

	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got MigrationPlan
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !reflect.DeepEqual(plan, got) {
		t.Errorf("round-trip mismatch:\nwant: %#v\n got: %#v", plan, got)
	}
}

func TestMigrationPlan_MarshalStability(t *testing.T) {
	t.Parallel()

	plan := samplePlan()

	a, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal a: %v", err)
	}
	b, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal b: %v", err)
	}
	if string(a) != string(b) {
		t.Errorf("marshal not deterministic:\na=%s\nb=%s", a, b)
	}
}

func TestPlanEntry_OmitemptyOnKeep(t *testing.T) {
	t.Parallel()

	entry := PlanEntry{
		Op:         OpKeep,
		SourcePath: "x.md",
		Domain:     ontology.DevOps,
		Intent:     ontology.IntentSOP,
	}

	raw, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(raw)

	for _, forbidden := range []string{"rewritten_body", "siyuan_doc_id", "notes"} {
		if strings.Contains(s, forbidden) {
			t.Errorf("expected %q to be omitted (omitempty) from %s", forbidden, s)
		}
	}
}

func TestMigrationPlan_Validate_Happy(t *testing.T) {
	t.Parallel()

	plan := samplePlan()
	if err := plan.Validate(); err != nil {
		t.Errorf("expected nil validation error, got: %v", err)
	}
}

func TestMigrationPlan_Validate_EmptyEntries(t *testing.T) {
	t.Parallel()

	plan := MigrationPlan{
		Version: PlanV1,
		Source:  "/Users/mc/Source/wiki/Hosting",
	}
	if err := plan.Validate(); err != nil {
		t.Errorf("empty entries should validate, got: %v", err)
	}
}

func TestMigrationPlan_Validate_BadVersion(t *testing.T) {
	t.Parallel()

	for _, v := range []MigrationPlanVersion{0, 99} {
		plan := MigrationPlan{
			Version: v,
			Source:  "/Users/mc/Source/wiki/Hosting",
		}
		err := plan.Validate()
		if err == nil {
			t.Fatalf("version %d should reject", v)
		}
		msg := err.Error()
		// Error must mention the offending version and the supported version (1).
		// Use a numeric substring; tolerate either decimal form.
		if !strings.Contains(msg, "version") {
			t.Errorf("error %q missing 'version'", msg)
		}
		if !strings.Contains(msg, "1") {
			t.Errorf("error %q missing supported version 1", msg)
		}
	}
}

func TestMigrationPlan_Validate_MissingSource(t *testing.T) {
	t.Parallel()

	plan := MigrationPlan{
		Version: PlanV1,
		Source:  "",
	}
	err := plan.Validate()
	if err == nil {
		t.Fatal("missing source should reject")
	}
	if !strings.Contains(err.Error(), "source") {
		t.Errorf("error %q missing 'source'", err.Error())
	}
}

func TestMigrationPlan_Validate_UnknownOp(t *testing.T) {
	t.Parallel()

	plan := MigrationPlan{
		Version: PlanV1,
		Source:  "/x",
		Entries: []PlanEntry{
			{Op: PlanOp("purge"), SourcePath: "f.md"},
		},
	}
	err := plan.Validate()
	if err == nil {
		t.Fatal("unknown op should reject")
	}
	msg := err.Error()
	for _, want := range []string{"purge", "keep", "drop_local", "retire_siyuan"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q missing %q", msg, want)
		}
	}
}

func TestMigrationPlan_Validate_KeepMissingDomain(t *testing.T) {
	t.Parallel()

	plan := MigrationPlan{
		Version: PlanV1,
		Source:  "/x",
		Entries: []PlanEntry{
			{
				Op:         OpKeep,
				SourcePath: "f.md",
				Intent:     ontology.IntentSOP,
				// Domain missing
			},
		},
	}
	err := plan.Validate()
	if err == nil {
		t.Fatal("OpKeep without domain should reject")
	}
	if !strings.Contains(err.Error(), "domain") {
		t.Errorf("error %q missing 'domain'", err.Error())
	}
}

func TestMigrationPlan_Validate_KeepInvalidDomainString(t *testing.T) {
	t.Parallel()

	plan := MigrationPlan{
		Version: PlanV1,
		Source:  "/x",
		Entries: []PlanEntry{
			{
				Op:         OpKeep,
				SourcePath: "f.md",
				Domain:     ontology.Domain("bogus"),
				Intent:     ontology.IntentSOP,
			},
		},
	}
	err := plan.Validate()
	if err == nil {
		t.Fatal("OpKeep with invalid domain should reject")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("error %q missing offending domain 'bogus'", err.Error())
	}
}

func TestMigrationPlan_Validate_KeepInvalidIntentString(t *testing.T) {
	t.Parallel()

	plan := MigrationPlan{
		Version: PlanV1,
		Source:  "/x",
		Entries: []PlanEntry{
			{
				Op:         OpKeep,
				SourcePath: "f.md",
				Domain:     ontology.DevOps,
				Intent:     ontology.Intent("braindump"),
			},
		},
	}
	err := plan.Validate()
	if err == nil {
		t.Fatal("OpKeep with invalid intent should reject")
	}
	if !strings.Contains(err.Error(), "braindump") {
		t.Errorf("error %q missing offending intent 'braindump'", err.Error())
	}
}

func TestMigrationPlan_Validate_RetireMissingDocID(t *testing.T) {
	t.Parallel()

	plan := MigrationPlan{
		Version: PlanV1,
		Source:  "/x",
		Entries: []PlanEntry{
			{
				Op:         OpRetireSiyuan,
				SourcePath: "f.md",
				// SiYuanDocID missing
			},
		},
	}
	err := plan.Validate()
	if err == nil {
		t.Fatal("OpRetireSiyuan without doc id should reject")
	}
	if !strings.Contains(err.Error(), "siyuan_doc_id") {
		t.Errorf("error %q missing 'siyuan_doc_id'", err.Error())
	}
}

func TestMigrationPlan_Validate_MissingSourcePath(t *testing.T) {
	t.Parallel()

	plan := MigrationPlan{
		Version: PlanV1,
		Source:  "/x",
		Entries: []PlanEntry{
			{
				Op:     OpDropLocal,
				// SourcePath missing
			},
		},
	}
	err := plan.Validate()
	if err == nil {
		t.Fatal("missing source_path should reject")
	}
	if !strings.Contains(err.Error(), "source_path") {
		t.Errorf("error %q missing 'source_path'", err.Error())
	}
}

func TestMigrationPlan_Validate_KeepForbidsSiYuanDocID(t *testing.T) {
	t.Parallel()

	plan := MigrationPlan{
		Version: PlanV1,
		Source:  "/x",
		Entries: []PlanEntry{
			{
				Op:          OpKeep,
				SourcePath:  "f.md",
				Domain:      ontology.DevOps,
				Intent:      ontology.IntentSOP,
				SiYuanDocID: "20240101120000-abcdefg",
			},
		},
	}
	err := plan.Validate()
	if err == nil {
		t.Fatal("OpKeep with SiYuanDocID should reject")
	}
	if !strings.Contains(err.Error(), "siyuan_doc_id") {
		t.Errorf("error %q missing 'siyuan_doc_id'", err.Error())
	}
}

func TestMigrationPlan_Validate_DropLocalForbidsDomain(t *testing.T) {
	t.Parallel()

	plan := MigrationPlan{
		Version: PlanV1,
		Source:  "/x",
		Entries: []PlanEntry{
			{
				Op:         OpDropLocal,
				SourcePath: "f.md",
				Domain:     ontology.DevOps,
			},
		},
	}
	err := plan.Validate()
	if err == nil {
		t.Fatal("OpDropLocal with Domain should reject")
	}
	if !strings.Contains(err.Error(), "domain") {
		t.Errorf("error %q missing 'domain'", err.Error())
	}
}

func TestMigrationPlan_Validate_RetireForbidsDomain(t *testing.T) {
	t.Parallel()

	plan := MigrationPlan{
		Version: PlanV1,
		Source:  "/x",
		Entries: []PlanEntry{
			{
				Op:          OpRetireSiyuan,
				SourcePath:  "f.md",
				SiYuanDocID: "20240101120000-abcdefg",
				Domain:      ontology.DevOps,
			},
		},
	}
	err := plan.Validate()
	if err == nil {
		t.Fatal("OpRetireSiyuan with Domain should reject")
	}
	if !strings.Contains(err.Error(), "domain") {
		t.Errorf("error %q missing 'domain'", err.Error())
	}
}

func TestMigrationPlan_Validate_AggregatesMultiple(t *testing.T) {
	t.Parallel()

	// One entry that violates THREE rules:
	//   - OpKeep without Domain (missing required)
	//   - OpKeep without Intent (missing required)
	//   - OpKeep with SiYuanDocID (forbidden cross-op field)
	plan := MigrationPlan{
		Version: PlanV1,
		Source:  "/x",
		Entries: []PlanEntry{
			{
				Op:          OpKeep,
				SourcePath:  "f.md",
				SiYuanDocID: "20240101120000-abcdefg",
			},
		},
	}
	err := plan.Validate()
	if err == nil {
		t.Fatal("entry with three violations should reject")
	}
	msg := err.Error()
	for _, want := range []string{"domain", "intent", "siyuan_doc_id"} {
		if !strings.Contains(msg, want) {
			t.Errorf("aggregated error %q missing %q", msg, want)
		}
	}
}
