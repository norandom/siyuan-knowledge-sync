// Package migrate defines the versioned MigrationPlan JSON contract that the
// `siyuan-ontology` AI Skill produces and that `siyuan-knowledge-sync migrate
// apply` consumes (ontology-gate spec, Req 6.1/6.2/6.7/10.2).
//
// Apply (task 3.4) lives in `apply.go` and is intentionally NOT part of this
// file. This file is purely the type contract and the structural validator.
package migrate

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"siyuan-knowledge-sync/internal/ontology"
)

// MigrationPlanVersion is the schema version of the on-disk plan JSON. The
// loader rejects any value other than the constants defined below; on a
// future schema bump, the loader gains an explicit migration path before a
// new constant becomes acceptable.
type MigrationPlanVersion int

// PlanV1 is the only currently accepted MigrationPlan schema version.
const PlanV1 MigrationPlanVersion = 1

// MigrationPlan is a single JSON document produced by the Skill describing
// the per-file decisions for one source folder. It is consumed atomically
// by `migrate apply`: per-entry failures are recorded but do not abort the
// batch (see design `migrate/plan`, "atomicity" note).
type MigrationPlan struct {
	Version     MigrationPlanVersion `json:"version"`
	Source      string               `json:"source"`
	GeneratedAt time.Time            `json:"generated_at"`
	Entries     []PlanEntry          `json:"entries"`
}

// PlanOp is the discriminator for what `migrate apply` does with a given
// entry. It is a closed string enum; unknown values are rejected by
// MigrationPlan.Validate.
type PlanOp string

// Closed PlanOp enum.
const (
	// OpKeep applies the ontology to a source file, optionally replaces its
	// body with cobesy's rewritten output, routes it to the canonical folder,
	// and syncs to SiYuan. Requires Domain + Intent; forbids SiYuanDocID.
	OpKeep PlanOp = "keep"

	// OpDropLocal removes a source file from the local wiki tree (git rm +
	// commit). It performs NO SiYuan write. Forbids Domain/Intent/
	// RewrittenBody/SiYuanDocID.
	OpDropLocal PlanOp = "drop_local"

	// OpRetireSiyuan removes a document from the live SiYuan instance by
	// document ID. Requires SiYuanDocID; forbids Domain/Intent/RewrittenBody.
	OpRetireSiyuan PlanOp = "retire_siyuan"
)

// PlanEntry is one row in a MigrationPlan. The set of required vs forbidden
// fields depends on Op (see Validate).
type PlanEntry struct {
	Op            PlanOp           `json:"op"`
	SourcePath    string           `json:"source_path"`
	Domain        ontology.Domain  `json:"domain,omitempty"`
	Intent        ontology.Intent  `json:"intent,omitempty"`
	RewrittenBody string           `json:"rewritten_body,omitempty"`
	SiYuanDocID   string           `json:"siyuan_doc_id,omitempty"`
	Notes         string           `json:"notes,omitempty"`
}

// allowedOps is the canonical list used in error messages so the Skill (or a
// human) sees the legal values when an unknown op is rejected.
var allowedOps = []PlanOp{OpKeep, OpDropLocal, OpRetireSiyuan}

// allowedOpStrings returns allowedOps as a comma-joined string for error
// messages.
func allowedOpStrings() string {
	ss := make([]string, len(allowedOps))
	for i, op := range allowedOps {
		ss[i] = string(op)
	}
	return strings.Join(ss, ",")
}

// Validate reports all structural problems with the plan as a single joined
// error (use errors.Is on any future sentinels). It does not touch the
// filesystem and does not check that SourcePath exists; it only enforces
// the JSON contract.
//
// Rules:
//   - Version must equal PlanV1.
//   - Source must be non-empty.
//   - Each entry's SourcePath must be non-empty.
//   - Op must be one of the closed enum values.
//   - OpKeep requires Domain (enum-valid) and Intent (enum-valid); forbids
//     SiYuanDocID.
//   - OpDropLocal forbids Domain, Intent, RewrittenBody, SiYuanDocID.
//   - OpRetireSiyuan requires SiYuanDocID; forbids Domain, Intent,
//     RewrittenBody.
//
// An empty Entries slice is valid (a no-op plan).
func (p MigrationPlan) Validate() error {
	var errs []error

	if p.Version != PlanV1 {
		errs = append(errs, fmt.Errorf(
			"unsupported plan version %d: only version %d is supported",
			p.Version, PlanV1,
		))
	}

	if p.Source == "" {
		errs = append(errs, errors.New("missing required field: source"))
	}

	for i, entry := range p.Entries {
		for _, e := range validateEntry(i, entry) {
			errs = append(errs, e)
		}
	}

	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

// validateEntry returns every structural violation in a single PlanEntry.
// The index is included in each error message so a multi-entry plan can be
// debugged from the joined output.
func validateEntry(idx int, e PlanEntry) []error {
	var errs []error

	prefix := fmt.Sprintf("entries[%d]", idx)

	if e.SourcePath == "" {
		errs = append(errs, fmt.Errorf("%s: missing required field: source_path", prefix))
	}

	switch e.Op {
	case OpKeep:
		// Domain required + enum-valid.
		if e.Domain == "" {
			errs = append(errs, fmt.Errorf("%s: op=keep requires domain", prefix))
		} else if v := ontology.ValidateDomain(string(e.Domain)); v != nil {
			errs = append(errs, fmt.Errorf(
				"%s: op=keep has invalid domain %q (allowed: %s)",
				prefix, string(e.Domain), strings.Join(v.Allowed, ","),
			))
		}
		// Intent required + enum-valid.
		if e.Intent == "" {
			errs = append(errs, fmt.Errorf("%s: op=keep requires intent", prefix))
		} else if v := ontology.ValidateIntent(string(e.Intent)); v != nil {
			errs = append(errs, fmt.Errorf(
				"%s: op=keep has invalid intent %q (allowed: %s)",
				prefix, string(e.Intent), strings.Join(v.Allowed, ","),
			))
		}
		// Forbidden cross-op field.
		if e.SiYuanDocID != "" {
			errs = append(errs, fmt.Errorf(
				"%s: op=keep forbids siyuan_doc_id (only retire_siyuan uses it)",
				prefix,
			))
		}

	case OpDropLocal:
		if e.Domain != "" {
			errs = append(errs, fmt.Errorf("%s: op=drop_local forbids domain", prefix))
		}
		if e.Intent != "" {
			errs = append(errs, fmt.Errorf("%s: op=drop_local forbids intent", prefix))
		}
		if e.RewrittenBody != "" {
			errs = append(errs, fmt.Errorf("%s: op=drop_local forbids rewritten_body", prefix))
		}
		if e.SiYuanDocID != "" {
			errs = append(errs, fmt.Errorf("%s: op=drop_local forbids siyuan_doc_id", prefix))
		}

	case OpRetireSiyuan:
		if e.SiYuanDocID == "" {
			errs = append(errs, fmt.Errorf("%s: op=retire_siyuan requires siyuan_doc_id", prefix))
		}
		if e.Domain != "" {
			errs = append(errs, fmt.Errorf("%s: op=retire_siyuan forbids domain", prefix))
		}
		if e.Intent != "" {
			errs = append(errs, fmt.Errorf("%s: op=retire_siyuan forbids intent", prefix))
		}
		if e.RewrittenBody != "" {
			errs = append(errs, fmt.Errorf("%s: op=retire_siyuan forbids rewritten_body", prefix))
		}

	default:
		errs = append(errs, fmt.Errorf(
			"%s: unknown op %q (allowed: %s)",
			prefix, string(e.Op), allowedOpStrings(),
		))
	}

	return errs
}
