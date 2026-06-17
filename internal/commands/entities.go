package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/port-experimental/port-cli/internal/api"
	"github.com/port-experimental/port-cli/internal/config"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// testClient is a package-level seam for test-time client injection.
// When nil (production), commands use the normal auth/client flow.
// When non-nil (tests), commands use this client directly.
var testClient *api.Client

// RegisterEntities registers the entities command group under root.
func RegisterEntities(root *cobra.Command) {
	entitiesCmd := &cobra.Command{
		Use:   "entities",
		Short: "Entity CRUD operations",
		Long:  "Create, read, update, and delete entities in Port blueprints",
	}

	entitiesCmd.AddCommand(registerEntitiesList())
	entitiesCmd.AddCommand(registerEntitiesGet())
	entitiesCmd.AddCommand(registerEntitiesCreate())
	entitiesCmd.AddCommand(registerEntitiesUpdate())
	entitiesCmd.AddCommand(registerEntitiesDelete())

	root.AddCommand(entitiesCmd)
}

// registerEntitiesList registers the list subcommand.
func registerEntitiesList() *cobra.Command {
	var blueprint, output, org string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List entities in a blueprint",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateEntityIdentifier(blueprint); err != nil {
				if errors.Is(err, ErrInvalidIdentifier) {
					return fmt.Errorf("invalid blueprint identifier: %s", blueprint)
				}
				return err
			}

			client, err := getClientForEntities(cmd, org)
			if err != nil {
				return err
			}
			if testClient == nil {
				defer client.Close()
			}

			entities, err := client.GetEntities(cmd.Context(), blueprint, nil)
			if err != nil {
				return fmt.Errorf("failed to list entities: %w", err)
			}

			if len(entities) == 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "No entities found in blueprint %s\n", blueprint)
				return nil
			}

			switch output {
			case "table":
				return printEntitiesTable(cmd, entities)
			case "json":
				// Wrap in {"entities": [...]} for list output
				wrapped := map[string]interface{}{"entities": entities}
				return formatOutputToWriter(cmd.OutOrStdout(), wrapped, "json")
			case "yaml":
				// Wrap in {"entities": [...]} for list output
				wrapped := map[string]interface{}{"entities": entities}
				return formatOutputToWriter(cmd.OutOrStdout(), wrapped, "yaml")
			default:
				return fmt.Errorf("unsupported output format: %s", output)
			}
		},
	}

	cmd.Flags().StringVarP(&blueprint, "blueprint", "b", "", "Blueprint identifier (required)")
	cmd.MarkFlagRequired("blueprint")
	cmd.Flags().StringVarP(&output, "output", "o", "table", "Output format: table, json, yaml")
	cmd.Flags().StringVar(&org, "org", "", "Organization name (uses default if not specified)")

	return cmd
}

// registerEntitiesGet registers the get subcommand.
func registerEntitiesGet() *cobra.Command {
	var blueprint, output, org string

	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get a specific entity",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]

			if err := validateEntityIdentifier(id); err != nil {
				if errors.Is(err, ErrInvalidIdentifier) {
					return fmt.Errorf("invalid identifier: %s", id)
				}
				return err
			}

			if err := validateEntityIdentifier(blueprint); err != nil {
				if errors.Is(err, ErrInvalidIdentifier) {
					return fmt.Errorf("invalid blueprint identifier: %s", blueprint)
				}
				return err
			}

			client, err := getClientForEntities(cmd, org)
			if err != nil {
				return err
			}
			if testClient == nil {
				defer client.Close()
			}

			entity, err := client.GetEntity(cmd.Context(), blueprint, id)
			if err != nil {
				return fmt.Errorf("failed to get entity: %w", err)
			}

			switch output {
			case "json":
				// Wrap in {"entity": {...}} for get output
				wrapped := map[string]interface{}{"entity": entity}
				return formatOutputToWriter(cmd.OutOrStdout(), wrapped, "json")
			case "yaml":
				// Wrap in {"entity": {...}} for get output
				wrapped := map[string]interface{}{"entity": entity}
				return formatOutputToWriter(cmd.OutOrStdout(), wrapped, "yaml")
			default:
				return fmt.Errorf("unsupported output format: %s", output)
			}
		},
	}

	cmd.Flags().StringVarP(&blueprint, "blueprint", "b", "", "Blueprint identifier (required)")
	cmd.MarkFlagRequired("blueprint")
	cmd.Flags().StringVarP(&output, "output", "o", "json", "Output format: json, yaml")
	cmd.Flags().StringVar(&org, "org", "", "Organization name (uses default if not specified)")

	return cmd
}

// registerEntitiesCreate registers the create subcommand.
func registerEntitiesCreate() *cobra.Command {
	var blueprint, file, org string
	var patch, force bool

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new entity with auto-detect upsert",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateEntityIdentifier(blueprint); err != nil {
				if errors.Is(err, ErrInvalidIdentifier) {
					return fmt.Errorf("invalid blueprint identifier: %s", blueprint)
				}
				return err
			}

			entity, err := parseEntityFile(file)
			if err != nil {
				if errors.Is(err, ErrSymlink) {
					return fmt.Errorf("symlinks are not supported")
				}
				if errors.Is(err, ErrFileTooLarge) {
					return fmt.Errorf("file exceeds 1MB limit")
				}
				if errors.Is(err, ErrUnsupportedFormat) {
					return fmt.Errorf("unsupported file format (use .json, .yaml, or .yml)")
				}
				return fmt.Errorf("failed to parse entity file: %w", err)
			}

			identifier, ok := entity["identifier"].(string)
			if !ok || identifier == "" {
				return fmt.Errorf("entity file must contain an 'identifier' field")
			}

			if err := validateEntityIdentifier(identifier); err != nil {
				if errors.Is(err, ErrInvalidIdentifier) {
					return fmt.Errorf("invalid identifier: %s", identifier)
				}
				return err
			}

			// Detect and warn about unknown fields
			if unknownFields := detectUnknownEntityFields(entity); len(unknownFields) > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "Warning: unknown fields detected: %s\n", strings.Join(unknownFields, ", "))
			}

			client, err := getClientForEntities(cmd, org)
			if err != nil {
				return err
			}
			if testClient == nil {
				defer client.Close()
			}

			// Existence probe: try to GET the entity
			_, getErr := client.GetEntity(cmd.Context(), blueprint, identifier)
			entityExists := getErr == nil

			var upsert, merge bool
			var successMessage string

			if !entityExists {
				// 404 path: create new entity
				upsert = false
				merge = false
				successMessage = fmt.Sprintf("Created entity %s", identifier)
			} else {
				// Entity exists: confirm before overwriting (unless --force)
				confirmed, err := confirmAction(
					fmt.Sprintf("Entity %s exists. Overwrite?", identifier),
					force,
					cmd.InOrStdin(),
				)
				if err != nil {
					return fmt.Errorf("confirmation failed: %w", err)
				}
				if !confirmed {
					return fmt.Errorf("operation cancelled")
				}

				// Decide upsert/merge based on --patch flag
				upsert = true
				merge = patch
				if patch {
					successMessage = fmt.Sprintf("Merged entity %s", identifier)
				} else {
					successMessage = fmt.Sprintf("Replaced entity %s", identifier)
				}
			}

			// POST with upsert/merge params
			_, err = client.CreateEntityWithParams(cmd.Context(), blueprint, entity, upsert, merge)
			if err != nil {
				return fmt.Errorf("failed to create entity: %w", err)
			}

			fmt.Fprintln(cmd.OutOrStdout(), successMessage)
			return nil
		},
	}

	cmd.Flags().StringVarP(&blueprint, "blueprint", "b", "", "Blueprint identifier (required)")
	cmd.MarkFlagRequired("blueprint")
	cmd.Flags().StringVarP(&file, "file", "f", "", "Entity file (JSON or YAML) (required)")
	cmd.MarkFlagRequired("file")
	cmd.Flags().BoolVar(&patch, "patch", false, "Merge with existing entity if it exists")
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")
	cmd.Flags().StringVar(&org, "org", "", "Organization name (uses default if not specified)")

	return cmd
}

// registerEntitiesUpdate registers the update subcommand.
func registerEntitiesUpdate() *cobra.Command {
	var blueprint, file, org string
	var force bool

	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update an existing entity (PATCH)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]

			if err := validateEntityIdentifier(id); err != nil {
				if errors.Is(err, ErrInvalidIdentifier) {
					return fmt.Errorf("invalid identifier: %s", id)
				}
				return err
			}

			if err := validateEntityIdentifier(blueprint); err != nil {
				if errors.Is(err, ErrInvalidIdentifier) {
					return fmt.Errorf("invalid blueprint identifier: %s", blueprint)
				}
				return err
			}

			entity, err := parseEntityFile(file)
			if err != nil {
				if errors.Is(err, ErrSymlink) {
					return fmt.Errorf("symlinks are not supported")
				}
				if errors.Is(err, ErrFileTooLarge) {
					return fmt.Errorf("file exceeds 1MB limit")
				}
				if errors.Is(err, ErrUnsupportedFormat) {
					return fmt.Errorf("unsupported file format (use .json, .yaml, or .yml)")
				}
				return fmt.Errorf("failed to parse entity file: %w", err)
			}

			// Check identifier mismatch
			fileIdentifier, ok := entity["identifier"].(string)
			if ok && fileIdentifier != "" && fileIdentifier != id {
				return fmt.Errorf("Identifier mismatch: file has '%s', expected '%s'", fileIdentifier, id)
			}

			client, err := getClientForEntities(cmd, org)
			if err != nil {
				return err
			}
			if testClient == nil {
				defer client.Close()
			}

			// PATCH the entity
			_, err = client.PatchEntity(cmd.Context(), blueprint, id, entity)
			if err != nil {
				return fmt.Errorf("failed to update entity: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Updated entity %s\n", id)
			return nil
		},
	}

	cmd.Flags().StringVarP(&blueprint, "blueprint", "b", "", "Blueprint identifier (required)")
	cmd.MarkFlagRequired("blueprint")
	cmd.Flags().StringVarP(&file, "file", "f", "", "Entity file (JSON or YAML) (required)")
	cmd.MarkFlagRequired("file")
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation (reserved for future use)")
	cmd.Flags().StringVar(&org, "org", "", "Organization name (uses default if not specified)")

	return cmd
}

// registerEntitiesDelete registers the delete subcommand.
func registerEntitiesDelete() *cobra.Command {
	var blueprint, org string
	var force bool

	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete an entity",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]

			if err := validateEntityIdentifier(id); err != nil {
				if errors.Is(err, ErrInvalidIdentifier) {
					return fmt.Errorf("invalid identifier: %s", id)
				}
				return err
			}

			if err := validateEntityIdentifier(blueprint); err != nil {
				if errors.Is(err, ErrInvalidIdentifier) {
					return fmt.Errorf("invalid blueprint identifier: %s", blueprint)
				}
				return err
			}

			// Confirm deletion (unless --force)
			confirmed, err := confirmAction(
				fmt.Sprintf("Delete entity %s?", id),
				force,
				cmd.InOrStdin(),
			)
			if err != nil {
				return fmt.Errorf("confirmation failed: %w", err)
			}
			if !confirmed {
				return fmt.Errorf("operation cancelled")
			}

			client, err := getClientForEntities(cmd, org)
			if err != nil {
				return err
			}
			if testClient == nil {
				defer client.Close()
			}

			if err := client.DeleteEntity(cmd.Context(), blueprint, id); err != nil {
				return fmt.Errorf("failed to delete entity: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Deleted entity %s\n", id)
			return nil
		},
	}

	cmd.Flags().StringVarP(&blueprint, "blueprint", "b", "", "Blueprint identifier (required)")
	cmd.MarkFlagRequired("blueprint")
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")
	cmd.Flags().StringVar(&org, "org", "", "Organization name (uses default if not specified)")

	return cmd
}

// getClientForEntities returns a client for entity operations.
// If testClient is set, it returns that. Otherwise, it creates a new client with auth.
func getClientForEntities(cmd *cobra.Command, org string) (*api.Client, error) {
	// Test seam: if testClient is set, use it directly
	if testClient != nil {
		return testClient, nil
	}

	// Production path: normal auth flow
	flags := GetGlobalFlags(cmd.Context())
	configManager := config.NewConfigManager(flags.ConfigFile)

	cfg, err := configManager.LoadWithOverrides(
		flags.ClientID,
		flags.ClientSecret,
		flags.APIURL,
		org,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}

	useOrg := cfg.GetOrgOrDefault(org)
	orgConfig, err := cfg.GetOrgConfig(useOrg)
	if err != nil {
		return nil, err
	}

	token, err := getOrRefreshCommandToken(cmd, configManager, useOrg)
	if err != nil {
		return nil, err
	}

	client := api.NewClient(api.ClientOpts{
		Token:        token,
		ClientID:     orgConfig.ClientID,
		ClientSecret: orgConfig.ClientSecret,
		APIURL:       orgConfig.APIURL,
		Timeout:      0,
	})

	return client, nil
}

// printEntitiesTable prints entities in a table format.
func printEntitiesTable(cmd *cobra.Command, entities []api.Entity) error {
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	defer w.Flush()

	// Header
	fmt.Fprintln(w, "IDENTIFIER\tTITLE\tTEAM")

	// Rows
	for _, e := range entities {
		identifier := getStringOrDash(e, "identifier")
		title := getStringOrDash(e, "title")
		team := getStringOrDash(e, "team")
		fmt.Fprintf(w, "%s\t%s\t%s\n", identifier, title, team)
	}

	return nil
}

// getStringOrDash extracts a string field from an entity or returns "—" if missing.
func getStringOrDash(entity api.Entity, key string) string {
	if val, ok := entity[key].(string); ok {
		return val
	}
	return "—"
}

// formatOutputToWriter formats and writes output to a writer (for json/yaml).
func formatOutputToWriter(w interface{ Write([]byte) (int, error) }, data interface{}, format string) error {
	switch format {
	case "json":
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(data)
	case "yaml":
		encoder := yaml.NewEncoder(w)
		defer encoder.Close()
		return encoder.Encode(data)
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}
}
