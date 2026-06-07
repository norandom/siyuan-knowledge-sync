package main

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"siyuan-knowledge-sync/internal/ontology"
)

// TestSchemaJSON_TagsField_OmittedWhenVocabNil asserts the top-level shape
// guarantee in Requirement 5.3: when no vocabulary is configured, the
// marshaled JSON must not contain a `tags` key at all. This is what makes
// the default-user JSON byte-identical to the pre-tags-vocab era.
func TestSchemaJSON_TagsField_OmittedWhenVocabNil(t *testing.T) {
	ontology.ResetForTest()
	t.Cleanup(ontology.ResetForTest)

	doc := buildSchemaDoc()
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if bytes.Contains(out, []byte(`"tags"`)) {
		t.Fatalf("expected no \"tags\" key in JSON when vocabulary is unconfigured, got: %s", out)
	}

	// Cross-check by unmarshaling into a generic map.
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := m["tags"]; ok {
		t.Fatalf("unmarshaled map contains \"tags\" key but should not: %#v", m)
	}
}

// TestSchemaJSON_TagsField_PresentWhenVocabConfigured asserts Requirement 5.2:
// when Configure supplies a non-nil tag slice, the JSON output gains
// `tags.values` containing the configured entries in supplied order.
func TestSchemaJSON_TagsField_PresentWhenVocabConfigured(t *testing.T) {
	ontology.ResetForTest()
	t.Cleanup(ontology.ResetForTest)

	if err := ontology.Configure(ontology.ConfigureOptions{
		Domains: []ontology.ConfigureDomain{
			{ID: "personal", Folder: "Personal"},
			{ID: "work", Folder: "Work"},
		},
		Intents: []ontology.ConfigureIntent{
			{ID: "note"},
			{ID: "task"},
		},
		Tags: []string{"claude", "mcp"},
	}); err != nil {
		t.Fatalf("Configure: %v", err)
	}

	doc := buildSchemaDoc()
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	raw, ok := m["tags"]
	if !ok {
		t.Fatalf("expected \"tags\" key in JSON, got: %s", out)
	}
	tagsObj, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("tags is not a JSON object: %#v", raw)
	}
	valsRaw, ok := tagsObj["values"]
	if !ok {
		t.Fatalf("tags object missing \"values\" key: %#v", tagsObj)
	}
	vals, ok := valsRaw.([]any)
	if !ok {
		t.Fatalf("tags.values is not a JSON array: %#v", valsRaw)
	}
	got := make([]string, len(vals))
	for i, v := range vals {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("tags.values[%d] is not a string: %#v", i, v)
		}
		got[i] = s
	}
	want := []string{"claude", "mcp"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tags.values = %v, want %v", got, want)
	}
}

// TestSchemaJSON_TagsField_NonNilEmptyVocab_RendersEmptyArray asserts the
// nil-vs-empty signal is preserved end-to-end: a configured-but-empty
// vocabulary marshals as `"tags": {"values": []}`, not as `null` and not
// as an omitted field.
func TestSchemaJSON_TagsField_NonNilEmptyVocab_RendersEmptyArray(t *testing.T) {
	ontology.ResetForTest()
	t.Cleanup(ontology.ResetForTest)

	if err := ontology.Configure(ontology.ConfigureOptions{
		Domains: []ontology.ConfigureDomain{
			{ID: "personal", Folder: "Personal"},
		},
		Intents: []ontology.ConfigureIntent{
			{ID: "note"},
		},
		Tags: []string{},
	}); err != nil {
		t.Fatalf("Configure: %v", err)
	}

	doc := buildSchemaDoc()
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if !bytes.Contains(out, []byte(`"tags"`)) {
		t.Fatalf("expected \"tags\" key in JSON, got: %s", out)
	}
	// The empty slice must serialize as `[]`, not `null`.
	if !bytes.Contains(out, []byte(`"values":[]`)) {
		t.Fatalf("expected \"values\":[] in JSON, got: %s", out)
	}

	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	tagsObj, ok := m["tags"].(map[string]any)
	if !ok {
		t.Fatalf("tags is not a JSON object: %#v", m["tags"])
	}
	valsRaw, ok := tagsObj["values"]
	if !ok {
		t.Fatalf("tags object missing \"values\" key: %#v", tagsObj)
	}
	vals, ok := valsRaw.([]any)
	if !ok {
		t.Fatalf("tags.values is not a JSON array (got %T): %#v", valsRaw, valsRaw)
	}
	if len(vals) != 0 {
		t.Fatalf("tags.values = %#v, want []", vals)
	}
}

// TestSchemaJSON_ReflectsConfiguredDomainsAndIntents asserts Requirement 5.1:
// the JSON output's `domain.values`, `domain.folders`, and `intent.values`
// reflect the effective ontology in the supplied order after Configure.
func TestSchemaJSON_ReflectsConfiguredDomainsAndIntents(t *testing.T) {
	ontology.ResetForTest()
	t.Cleanup(ontology.ResetForTest)

	if err := ontology.Configure(ontology.ConfigureOptions{
		Domains: []ontology.ConfigureDomain{
			{ID: "personal", Folder: "Personal"},
			{ID: "work", Folder: "Work"},
		},
		Intents: []ontology.ConfigureIntent{
			{ID: "note"},
			{ID: "task"},
		},
	}); err != nil {
		t.Fatalf("Configure: %v", err)
	}

	doc := buildSchemaDoc()
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	domainObj, ok := m["domain"].(map[string]any)
	if !ok {
		t.Fatalf("domain is not a JSON object: %#v", m["domain"])
	}
	domainValsRaw, ok := domainObj["values"].([]any)
	if !ok {
		t.Fatalf("domain.values is not a JSON array: %#v", domainObj["values"])
	}
	gotDomainVals := make([]string, len(domainValsRaw))
	for i, v := range domainValsRaw {
		gotDomainVals[i] = v.(string)
	}
	wantDomainVals := []string{"personal", "work"}
	if !reflect.DeepEqual(gotDomainVals, wantDomainVals) {
		t.Fatalf("domain.values = %v, want %v", gotDomainVals, wantDomainVals)
	}

	foldersRaw, ok := domainObj["folders"].(map[string]any)
	if !ok {
		t.Fatalf("domain.folders is not a JSON object: %#v", domainObj["folders"])
	}
	if got, want := foldersRaw["personal"], "Personal"; got != want {
		t.Fatalf("domain.folders[personal] = %v, want %v", got, want)
	}
	if got, want := foldersRaw["work"], "Work"; got != want {
		t.Fatalf("domain.folders[work] = %v, want %v", got, want)
	}

	intentObj, ok := m["intent"].(map[string]any)
	if !ok {
		t.Fatalf("intent is not a JSON object: %#v", m["intent"])
	}
	intentValsRaw, ok := intentObj["values"].([]any)
	if !ok {
		t.Fatalf("intent.values is not a JSON array: %#v", intentObj["values"])
	}
	gotIntentVals := make([]string, len(intentValsRaw))
	for i, v := range intentValsRaw {
		gotIntentVals[i] = v.(string)
	}
	wantIntentVals := []string{"note", "task"}
	if !reflect.DeepEqual(gotIntentVals, wantIntentVals) {
		t.Fatalf("intent.values = %v, want %v", gotIntentVals, wantIntentVals)
	}
}

// TestSchemaJSON_TopLevelShapeUnchanged is the byte-identical-for-default-users
// guarantee (Requirement 5.3): without any Configure call the top-level JSON
// keys must be exactly {version, domain, intent, required_keys} — no `tags`,
// no extra fields, no removed fields.
func TestSchemaJSON_TopLevelShapeUnchanged(t *testing.T) {
	ontology.ResetForTest()
	t.Cleanup(ontology.ResetForTest)

	doc := buildSchemaDoc()
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	wantKeys := map[string]struct{}{
		"version":       {},
		"domain":        {},
		"intent":        {},
		"required_keys": {},
	}
	for k := range m {
		if _, ok := wantKeys[k]; !ok {
			t.Fatalf("unexpected top-level key %q in JSON: %s", k, out)
		}
	}
	for k := range wantKeys {
		if _, ok := m[k]; !ok {
			t.Fatalf("missing top-level key %q in JSON: %s", k, out)
		}
	}
	if len(m) != len(wantKeys) {
		t.Fatalf("top-level key count mismatch: got %d keys (%v), want %d (%v)",
			len(m), m, len(wantKeys), wantKeys)
	}
}
