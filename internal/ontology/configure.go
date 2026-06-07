package ontology

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ConfigureDomain pairs a domain id with its canonical folder. It is the
// input shape Configure validates and stores into the package-level
// canonical-folder map.
type ConfigureDomain struct {
	ID     string
	Folder string
}

// ConfigureIntent carries a single intent identifier.
type ConfigureIntent struct {
	ID string
}

// ConfigureOptions is the validated input to Configure. Domains and Intents
// are required (a zero-domain or zero-intent ontology is rejected by
// construction — Requirement 1.3); both must be non-empty. The Tags axis
// (controlled tag vocabulary) is wired in by a later task and is omitted
// from this shape on purpose.
type ConfigureOptions struct {
	Domains []ConfigureDomain
	Intents []ConfigureIntent
}

// idCharset is the closed-enum-id charset shared by Domain and Intent
// identifiers. It enforces a lowercase ascii letter at position 0 followed
// by zero or more lowercase letters, digits, or hyphens. Empty strings do
// not match.
var idCharset = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Configure replaces the package-level ontology state with opts. It must
// be called at most once per process invocation, before any goroutine
// reads the ontology accessors; on error the state is unchanged.
//
// Configure runs every validation rule against opts and aggregates every
// failure via errors.Join, so a single invocation surfaces every problem.
// Validation classes:
//
//   - opts.Domains must be non-empty.
//   - opts.Intents must be non-empty.
//   - Every domain id must match `^[a-z][a-z0-9-]*$`.
//   - Every domain folder must be non-empty and must not start with `_`
//     or `/` (those prefixes are reserved by the engine).
//   - Domain ids and folder names must be pairwise unique.
//   - Every intent id must match `^[a-z][a-z0-9-]*$`.
//   - Intent ids must be pairwise unique.
//
// On nil return, AllDomains / AllIntents / Router.CanonicalFolder reflect
// opts in input order. On non-nil return, the previously-active state
// (compiled-in defaults if Configure had not run before) is preserved.
func Configure(opts ConfigureOptions) error {
	var errs []error

	if len(opts.Domains) == 0 {
		errs = append(errs, fmt.Errorf("ontology: at least one domain is required"))
	}
	if len(opts.Intents) == 0 {
		errs = append(errs, fmt.Errorf("ontology: at least one intent is required"))
	}

	// Per-domain field validation + uniqueness checks.
	seenDomainIDs := make(map[string]int, len(opts.Domains))
	seenDomainFolders := make(map[string]int, len(opts.Domains))
	for i, d := range opts.Domains {
		if !idCharset.MatchString(d.ID) {
			errs = append(errs, fmt.Errorf(
				"ontology: domain[%d] id %q does not match charset ^[a-z][a-z0-9-]*$",
				i, d.ID,
			))
		}
		if d.Folder == "" {
			errs = append(errs, fmt.Errorf(
				"ontology: domain[%d] (id=%q) has an empty folder",
				i, d.ID,
			))
		} else if strings.HasPrefix(d.Folder, "_") || strings.HasPrefix(d.Folder, "/") {
			errs = append(errs, fmt.Errorf(
				"ontology: domain[%d] (id=%q) folder %q starts with a reserved prefix (`_` or `/`)",
				i, d.ID, d.Folder,
			))
		}
		if prev, ok := seenDomainIDs[d.ID]; ok {
			errs = append(errs, fmt.Errorf(
				"ontology: duplicate domain id %q at entries [%d] and [%d]",
				d.ID, prev, i,
			))
		} else {
			seenDomainIDs[d.ID] = i
		}
		if d.Folder != "" {
			if prev, ok := seenDomainFolders[d.Folder]; ok {
				errs = append(errs, fmt.Errorf(
					"ontology: duplicate domain folder %q at entries [%d] (id=%q) and [%d] (id=%q)",
					d.Folder, prev, opts.Domains[prev].ID, i, d.ID,
				))
			} else {
				seenDomainFolders[d.Folder] = i
			}
		}
	}

	// Per-intent field validation + uniqueness checks.
	seenIntentIDs := make(map[string]int, len(opts.Intents))
	for i, in := range opts.Intents {
		if !idCharset.MatchString(in.ID) {
			errs = append(errs, fmt.Errorf(
				"ontology: intent[%d] id %q does not match charset ^[a-z][a-z0-9-]*$",
				i, in.ID,
			))
		}
		if prev, ok := seenIntentIDs[in.ID]; ok {
			errs = append(errs, fmt.Errorf(
				"ontology: duplicate intent id %q at entries [%d] and [%d]",
				in.ID, prev, i,
			))
		} else {
			seenIntentIDs[in.ID] = i
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	// All checks passed — replace package state atomically (sequentially,
	// inside this function; no goroutine reads the accessors during
	// startup per the Configure precondition).
	newDomains := make([]Domain, len(opts.Domains))
	newFolders := make(map[Domain]string, len(opts.Domains))
	for i, d := range opts.Domains {
		newDomains[i] = Domain(d.ID)
		newFolders[Domain(d.ID)] = d.Folder
	}
	newIntents := make([]Intent, len(opts.Intents))
	for i, in := range opts.Intents {
		newIntents[i] = Intent(in.ID)
	}

	allDomainsCanonical = newDomains
	allIntentsCanonical = newIntents
	canonicalFolders = newFolders
	return nil
}

// resetToDefaultsForTest restores the seeded compile-time defaults across
// every piece of package state Configure can mutate. It is package-private
// by design — tests in the ontology package call it via t.Cleanup to keep
// tests independent. Production code never calls it.
func resetToDefaultsForTest() {
	allDomainsCanonical = append([]Domain(nil), defaultDomains...)
	allIntentsCanonical = append([]Intent(nil), defaultIntents...)
	canonicalFolders = copyFolderMap(defaultCanonicalFolders)
}
