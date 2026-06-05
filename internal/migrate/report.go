package migrate

// MigrationReport is the per-plan execution summary returned by Apply. It is
// the durable, machine-inspectable record of every plan entry's outcome —
// callers (e.g. `cmd migrate.go`, future log-shipping) render it as the user
// signal that the apply executor finished.
//
// Per-entry isolation guarantee (task 3.4, design `migrate/apply`):
// every PlanEntry in plan.Entries lands as exactly one EntryOutcome in
// Outcomes, in declaration order. A failure on one entry produces a
// StatusError outcome and the loop continues; Apply itself only returns a
// non-nil top-level error for pre-flight failures (plan validation or a
// context cancelation that beats the first entry).
type MigrationReport struct {
	// PlanSource is a copy of MigrationPlan.Source for downstream display
	// (so the rendered report carries provenance without re-attaching the
	// plan).
	PlanSource string `json:"plan_source"`

	// Outcomes is one EntryOutcome per PlanEntry, in plan order.
	Outcomes []EntryOutcome `json:"outcomes"`
}

// EntryOutcome is the result of applying a single PlanEntry.
type EntryOutcome struct {
	// SourcePath is the PlanEntry.SourcePath as submitted. It is preserved
	// verbatim even when routing moved the file, so the entry lines up
	// 1:1 with the plan that produced it.
	SourcePath string `json:"source_path"`

	// Op is the PlanEntry.Op as submitted.
	Op PlanOp `json:"op"`

	// Status is the per-entry outcome discriminator. See EntryStatus.
	Status EntryStatus `json:"status"`

	// NewPath records the post-route location for OpKeep entries that the
	// router actually moved (or the unchanged SourcePath when no move
	// happened). Empty for OpDropLocal / OpRetireSiyuan / StatusError.
	NewPath string `json:"new_path,omitempty"`

	// Error carries the human-readable failure message; non-empty only when
	// Status == StatusError. Lives next to Status so callers don't need a
	// second field to know whether to inspect it.
	Error string `json:"error,omitempty"`
}

// EntryStatus is the closed enum of per-entry outcomes.
type EntryStatus string

// EntryStatus values.
const (
	// StatusKept means an OpKeep entry was applied (ontology added, body
	// rewritten when requested, RouteAndSync succeeded).
	StatusKept EntryStatus = "kept"

	// StatusDropped means an OpDropLocal entry was applied (git rm +
	// commit). No SiYuan side effect occurred.
	StatusDropped EntryStatus = "dropped"

	// StatusRetired means an OpRetireSiyuan entry was applied (the SiYuan
	// removeDocByID call returned without error).
	StatusRetired EntryStatus = "retired"

	// StatusError captures any per-entry failure: read/write IO, git
	// subprocess failure, ontology rewrite refusal (ErrUnsafeRewrite), or
	// a SiYuan API rejection. Error carries the message.
	StatusError EntryStatus = "error"
)
