package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"siyuan-knowledge-sync/internal/ontology"
)

// schemaVersion is the contract version embedded in the JSON output. The
// SKILL.md consults this field to detect cached-output drift across CLI
// upgrades. It matches the MigrationPlan V1 contract version.
const schemaVersion = 1

// schemaDoc is the canonical JSON document emitted by `schema --json`. It
// is the single source of truth the siyuan-ontology AI Skill reads on every
// session — the SKILL.md does NOT hardcode the enums.
//
// Tags is a pointer with omitempty so the top-level JSON shape stays
// byte-identical for users without a configured tag vocabulary
// (Requirement 5.3): nil pointer → key absent. A non-nil pointer means the
// vocabulary is closed; an empty Values slice inside means closed-but-empty
// (every tag is unrecognized).
type schemaDoc struct {
	Version      int             `json:"version"`
	Domain       schemaDomainDoc `json:"domain"`
	Intent       schemaIntentDoc `json:"intent"`
	RequiredKeys []string        `json:"required_keys"`
	Tags         *schemaTagsDoc  `json:"tags,omitempty"`
}

type schemaDomainDoc struct {
	Values  []string          `json:"values"`
	Folders map[string]string `json:"folders"`
}

type schemaIntentDoc struct {
	Values []string `json:"values"`
}

// schemaTagsDoc surfaces the configured controlled tag vocabulary
// (Requirement 5.2). Values is a non-nil slice when the surrounding pointer
// is non-nil so JSON renders `"values": []` rather than `null` for the
// closed-but-empty case — preserving the nil-vs-non-nil-empty distinction
// observable in `ontology.AllowedTags()`.
type schemaTagsDoc struct {
	Values []string `json:"values"`
}

// newSchemaCommand returns the `schema` cobra subcommand. It deliberately
// takes no config-path pointer: the schema is compiled into the binary via
// the `ontology` package and is config-independent.
func newSchemaCommand() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "schema",
		Short: "Print the ontology schema (closed enums + canonical folder map)",
		Long: `Print the closed ontology schema — domain enum, intent enum, canonical folder map.
The --json flag emits the machine-readable form used by the siyuan-ontology AI Skill
as its single source of truth for schema enforcement.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return printSchema(cmd.OutOrStdout(), jsonOut)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit JSON instead of a human summary")
	return cmd
}

// buildSchemaDoc populates the JSON document from the ontology package.
// It is the only place that walks AllDomains / AllIntents / the router so
// downstream tests can exercise the same path the JSON output does.
func buildSchemaDoc() schemaDoc {
	router := ontology.Router{}

	domains := ontology.AllDomains()
	domainValues := make([]string, 0, len(domains))
	folders := make(map[string]string, len(domains))
	for _, d := range domains {
		s := string(d)
		domainValues = append(domainValues, s)
		folders[s] = router.CanonicalFolder(d)
	}

	intents := ontology.AllIntents()
	intentValues := make([]string, 0, len(intents))
	for _, i := range intents {
		intentValues = append(intentValues, string(i))
	}

	// Optional tag vocabulary: ontology.AllowedTags() returns nil for the
	// open vocabulary (no `tags:` section configured) and a non-nil slice
	// when the operator pinned a controlled vocabulary. We preserve that
	// distinction in the JSON output by leaving schemaDoc.Tags as nil in
	// the open case (omitempty drops the key) and pointing it at a
	// non-nil Values slice — possibly empty — otherwise.
	allowed := ontology.AllowedTags()
	var tags *schemaTagsDoc
	if allowed != nil {
		// make() with len(allowed)==0 yields a non-nil empty slice so
		// JSON renders `"values": []` not `null` for the
		// closed-but-empty case.
		values := make([]string, len(allowed))
		copy(values, allowed)
		tags = &schemaTagsDoc{Values: values}
	}

	return schemaDoc{
		Version: schemaVersion,
		Domain: schemaDomainDoc{
			Values:  domainValues,
			Folders: folders,
		},
		Intent: schemaIntentDoc{
			Values: intentValues,
		},
		RequiredKeys: []string{"domain", "intent"},
		Tags:         tags,
	}
}

// printSchema writes the schema either as indented JSON (when jsonOut is
// true) or as a human-readable summary suitable for help-style inspection.
func printSchema(out io.Writer, jsonOut bool) error {
	doc := buildSchemaDoc()

	if jsonOut {
		b, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal schema: %w", err)
		}
		if _, err := out.Write(b); err != nil {
			return err
		}
		_, err = fmt.Fprintln(out)
		return err
	}

	fmt.Fprintf(out, "Ontology Schema (version: %d)\n\n", doc.Version)

	fmt.Fprintln(out, "Required frontmatter keys:")
	for _, k := range doc.RequiredKeys {
		fmt.Fprintf(out, "  - %s\n", k)
	}

	fmt.Fprintln(out, "\ndomain (closed enum):")
	for _, v := range doc.Domain.Values {
		fmt.Fprintf(out, "  - %s -> %s\n", v, doc.Domain.Folders[v])
	}

	fmt.Fprintln(out, "\nintent (closed enum):")
	for _, v := range doc.Intent.Values {
		fmt.Fprintf(out, "  - %s\n", v)
	}

	// Tag vocabulary section: only emitted when the operator pinned a
	// vocabulary (doc.Tags != nil). The unconfigured/open-vocabulary case
	// stays silent so the human output remains visually identical to the
	// pre-tags-vocab era for default users.
	if doc.Tags != nil {
		fmt.Fprintln(out, "\ntag vocabulary (closed):")
		if len(doc.Tags.Values) == 0 {
			fmt.Fprintln(out, "  (configured but empty — every tag is unrecognized)")
		} else {
			for _, v := range doc.Tags.Values {
				fmt.Fprintf(out, "  - %s\n", v)
			}
		}
	}

	return nil
}
