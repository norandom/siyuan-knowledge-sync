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
type schemaDoc struct {
	Version      int             `json:"version"`
	Domain       schemaDomainDoc `json:"domain"`
	Intent       schemaIntentDoc `json:"intent"`
	RequiredKeys []string        `json:"required_keys"`
}

type schemaDomainDoc struct {
	Values  []string          `json:"values"`
	Folders map[string]string `json:"folders"`
}

type schemaIntentDoc struct {
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

	return nil
}
