package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/port-experimental/port-cli/internal/api"
	"github.com/port-experimental/port-cli/internal/config"
	"github.com/port-experimental/port-cli/internal/modules/skills/catalog"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// RegisterSkillsCatalog adds the "catalog" sub-command group to the skills command.
// It is called once from RegisterSkills — before rootCmd.AddCommand(skillsCmd).
func RegisterSkillsCatalog(skillsCmd *cobra.Command) {
	catalogCmd := &cobra.Command{
		Use:   "catalog",
		Short: "Provision skill entities in the Port catalog",
		Long: `Provision and manage skill entities stored in the Port catalog.

Unlike 'port skills sync' (which writes skill files to local AI tool directories),
the catalog sub-commands create, read, update, and delete the skill entities that
live in your Port organisation's "skill" blueprint.

Each skill is defined in a Markdown file with YAML frontmatter:

  ---
  identifier: my-skill      (required)
  title: My Skill           (optional — defaults to identifier)
  description: Short desc   (required)
  location: global          (optional — "global" (default) | "project")
  ---
  Markdown body becomes the skill's instructions field.

Run 'port skills catalog blueprint init' once per organisation to create the
"skill" blueprint before using the other catalog commands.`,
	}

	catalogCmd.AddCommand(registerCatalogCreate())
	catalogCmd.AddCommand(registerCatalogList())
	catalogCmd.AddCommand(registerCatalogGet())
	catalogCmd.AddCommand(registerCatalogUpdate())
	catalogCmd.AddCommand(registerCatalogDelete())
	catalogCmd.AddCommand(registerCatalogBlueprint())

	skillsCmd.AddCommand(catalogCmd)
}

// ---------------------------------------------------------------------------
// create
// ---------------------------------------------------------------------------

func registerCatalogCreate() *cobra.Command {
	var (
		file   string
		force  bool
		patch  bool
		yes    bool
		output string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create or update a skill entity from a Markdown file",
		Long: `Create or update a skill entity in the Port catalog from a Markdown file.

The file must contain YAML frontmatter followed by a Markdown body:

  ---
  identifier: my-skill      (required — unique key for the entity)
  title: My Skill           (optional — defaults to identifier)
  description: Short desc   (required — one-line summary shown in Port UI)
  location: global          (optional — "global" (default) | "project")
  ---
  Full Markdown instructions for the skill agent.

Flags:
  -f, --file     Path to the skill Markdown file (required)
  --force        Overwrite all existing properties (merge=false); cannot combine with --patch
  --patch        Partial update — only send the fields present (PATCH); cannot combine with --force
  --yes          Skip the confirmation prompt
  -o, --output   Output format: table (default), json, yaml`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			entity, err := catalog.ParseSkillFile(file)
			if err != nil {
				return fmt.Errorf("failed to parse skill file: %w", err)
			}

			opts := catalog.CreateOptions{Force: force, Patch: patch}

			if !yes {
				action := "create/update"
				if patch {
					action = "patch"
				} else if force {
					action = "force-overwrite"
				}
				ok, err := confirmPrompt(
					fmt.Sprintf("Provision skill %q (%s)?", entity.Identifier, action),
					fmt.Sprintf("This will %s skill entity %q in the Port catalog.", action, entity.Identifier),
				)
				if err != nil {
					return err
				}
				if !ok {
					fmt.Fprintln(os.Stderr, "Cancelled — no changes made.")
					return nil
				}
			}

			client, closer, err := newCatalogClient(ctx, cmd)
			if err != nil {
				return err
			}
			defer closer()

			if err := catalog.CreateSkill(ctx, client, entity, opts); err != nil {
				return err
			}

			return printSkillOutput(output, entity)
		},
	}

	cmd.Flags().StringVarP(&file, "file", "f", "", "Path to the skill Markdown file (required)")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite all properties (merge=false)")
	cmd.Flags().BoolVar(&patch, "patch", false, "Partial update via PATCH")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	cmd.Flags().StringVarP(&output, "output", "o", "table", "Output format: table, json, yaml")
	_ = cmd.MarkFlagRequired("file")

	return cmd
}

// ---------------------------------------------------------------------------
// list
// ---------------------------------------------------------------------------

func registerCatalogList() *cobra.Command {
	var output string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all skill entities in the Port catalog",
		Long:  `Fetch and display all skill entities from the Port catalog.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			client, closer, err := newCatalogClient(ctx, cmd)
			if err != nil {
				return err
			}
			defer closer()

			skills, err := catalog.ListSkills(ctx, client)
			if err != nil {
				return fmt.Errorf("failed to list skills: %w", err)
			}

			switch output {
			case "json":
				return json.NewEncoder(os.Stdout).Encode(skills)
			case "yaml":
				return yaml.NewEncoder(os.Stdout).Encode(skills)
			default:
				w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
				fmt.Fprintln(w, "IDENTIFIER\tTITLE\tLOCATION")
				for _, s := range skills {
					fmt.Fprintf(w, "%s\t%s\t%s\n", s.Identifier, s.Title, s.Location)
				}
				return w.Flush()
			}
		},
	}

	cmd.Flags().StringVarP(&output, "output", "o", "table", "Output format: table, json, yaml")
	return cmd
}

// ---------------------------------------------------------------------------
// get
// ---------------------------------------------------------------------------

func registerCatalogGet() *cobra.Command {
	var output string

	cmd := &cobra.Command{
		Use:   "get <identifier>",
		Short: "Get a single skill entity from the Port catalog",
		Long:  `Retrieve a single skill entity by identifier from the Port catalog.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			client, closer, err := newCatalogClient(ctx, cmd)
			if err != nil {
				return err
			}
			defer closer()

			skill, err := catalog.GetSkill(ctx, client, args[0])
			if err != nil {
				return fmt.Errorf("failed to get skill %q: %w", args[0], err)
			}

			return printSkillOutput(output, skill)
		},
	}

	cmd.Flags().StringVarP(&output, "output", "o", "table", "Output format: table, json, yaml")
	return cmd
}

// ---------------------------------------------------------------------------
// update
// ---------------------------------------------------------------------------

func registerCatalogUpdate() *cobra.Command {
	var (
		file   string
		output string
	)

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Partially update a skill entity from a Markdown file (PATCH)",
		Long: `Partially update a skill entity in the Port catalog from a Markdown file.

Only the fields present in the file are updated (PATCH semantics).
Equivalent to 'port skills catalog create --patch'.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			entity, err := catalog.ParseSkillFile(file)
			if err != nil {
				return fmt.Errorf("failed to parse skill file: %w", err)
			}

			client, closer, err := newCatalogClient(ctx, cmd)
			if err != nil {
				return err
			}
			defer closer()

			if err := catalog.UpdateSkill(ctx, client, entity); err != nil {
				return err
			}

			return printSkillOutput(output, entity)
		},
	}

	cmd.Flags().StringVarP(&file, "file", "f", "", "Path to the skill Markdown file (required)")
	cmd.Flags().StringVarP(&output, "output", "o", "table", "Output format: table, json, yaml")
	_ = cmd.MarkFlagRequired("file")

	return cmd
}

// ---------------------------------------------------------------------------
// delete
// ---------------------------------------------------------------------------

func registerCatalogDelete() *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "delete <identifier>",
		Short: "Delete a skill entity from the Port catalog",
		Long:  `Delete a skill entity from the Port catalog by identifier. This operation is not idempotent — deleting a non-existent skill returns an error.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			identifier := args[0]

			if !yes {
				ok, err := confirmPrompt(
					fmt.Sprintf("Delete skill %q?", identifier),
					fmt.Sprintf("This will permanently remove skill entity %q from the Port catalog.", identifier),
				)
				if err != nil {
					return err
				}
				if !ok {
					fmt.Fprintln(os.Stderr, "Cancelled — skill not deleted.")
					return nil
				}
			}

			client, closer, err := newCatalogClient(ctx, cmd)
			if err != nil {
				return err
			}
			defer closer()

			if err := catalog.DeleteSkill(ctx, client, identifier); err != nil {
				return fmt.Errorf("failed to delete skill %q: %w", identifier, err)
			}

			fmt.Fprintf(os.Stdout, "Deleted skill %q from the Port catalog.\n", identifier)
			return nil
		},
	}

	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	return cmd
}

// ---------------------------------------------------------------------------
// blueprint (sub-group with "init" sub-command)
// ---------------------------------------------------------------------------

func registerCatalogBlueprint() *cobra.Command {
	blueprintCmd := &cobra.Command{
		Use:   "blueprint",
		Short: "Manage the Port 'skill' blueprint",
		Long:  `Manage the Port 'skill' blueprint that backs catalog skill entities.`,
	}

	blueprintCmd.AddCommand(registerCatalogBlueprintInit())
	return blueprintCmd
}

func registerCatalogBlueprintInit() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create the 'skill' blueprint in Port (idempotent)",
		Long: `Create the 'skill' blueprint in your Port organisation.

This is a one-time setup step required before using any 'port skills catalog'
commands. The operation is idempotent — running it when the blueprint already
exists is safe and produces no error.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			client, closer, err := newCatalogClient(ctx, cmd)
			if err != nil {
				return err
			}
			defer closer()

			if err := catalog.BootstrapBlueprint(ctx, client); err != nil {
				return fmt.Errorf("failed to bootstrap skill blueprint: %w", err)
			}

			fmt.Println("Skill blueprint is ready.")
			return nil
		},
	}
}

// ---------------------------------------------------------------------------
// shared helpers
// ---------------------------------------------------------------------------

// newCatalogClient builds an api.Client from the global flags, mirroring the
// pattern used by newSkillsModuleWithFlags in skills.go. Returns the client
// and a closer function (wraps client.Close for defer).
func newCatalogClient(ctx context.Context, cmd *cobra.Command) (*api.Client, func(), error) {
	flags := GetGlobalFlags(ctx)
	configManager := config.NewConfigManager(flags.ConfigFile)

	cfg, err := configManager.LoadWithOverrides(flags.ClientID, flags.ClientSecret, flags.APIURL, "")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load configuration: %w", err)
	}

	orgConfig, err := cfg.GetOrgConfig("")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get org config: %w", err)
	}

	token, err := getOrRefreshCommandToken(cmd, configManager, cfg.DefaultOrg)
	if err != nil {
		return nil, nil, err
	}

	client := api.NewClient(api.ClientOpts{
		Token:        token,
		ClientID:     orgConfig.ClientID,
		ClientSecret: orgConfig.ClientSecret,
		APIURL:       orgConfig.APIURL,
		Timeout:      0,
	})

	return client, func() { client.Close() }, nil //nolint:errcheck
}

// printSkillOutput writes a single SkillEntity in the requested format.
func printSkillOutput(format string, s catalog.SkillEntity) error {
	switch format {
	case "json":
		return json.NewEncoder(os.Stdout).Encode(s)
	case "yaml":
		return yaml.NewEncoder(os.Stdout).Encode(s)
	default:
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "IDENTIFIER\tTITLE\tLOCATION")
		fmt.Fprintf(w, "%s\t%s\t%s\n", s.Identifier, s.Title, s.Location)
		return w.Flush()
	}
}
