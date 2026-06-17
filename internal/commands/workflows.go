package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"text/tabwriter"

	"github.com/port-experimental/port-cli/internal/api"
	"github.com/port-experimental/port-cli/internal/config"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// testClient is the package-level seam for dependency injection in tests.
// In production (nil), commands use normal auth flow.
// In tests, set to httptest-backed client, then defer reset to nil.
var testClient *api.Client

// getClientForWorkflows returns the test client if set, otherwise creates a production client.
func getClientForWorkflows(cmd *cobra.Command, org string) (*api.Client, func(), error) {
	if testClient != nil {
		return testClient, func() {}, nil
	}

	flags := GetGlobalFlags(cmd.Context())
	configManager := config.NewConfigManager(flags.ConfigFile)

	cfg, err := configManager.LoadWithOverrides(
		flags.ClientID,
		flags.ClientSecret,
		flags.APIURL,
		org,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load configuration: %w", err)
	}

	useOrg := cfg.GetOrgOrDefault(org)
	orgConfig, err := cfg.GetOrgConfig(useOrg)
	if err != nil {
		return nil, nil, err
	}

	token, err := getOrRefreshCommandToken(cmd, configManager, useOrg)
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

	return client, func() { client.Close() }, nil
}

// RegisterWorkflows registers the workflows command group under root.
func RegisterWorkflows(rootCmd *cobra.Command) {
	workflowsCmd := &cobra.Command{
		Use:     "workflows",
		Aliases: []string{"workflow", "wf"},
		Short:   "Workflow operations",
		Long:    "Create, read, update, and delete Port workflows.",
	}

	workflowsCmd.AddCommand(registerWorkflowsList())
	workflowsCmd.AddCommand(registerWorkflowsGet())
	workflowsCmd.AddCommand(registerWorkflowsCreate())
	workflowsCmd.AddCommand(registerWorkflowsUpdate())
	workflowsCmd.AddCommand(registerWorkflowsDelete())

	rootCmd.AddCommand(workflowsCmd)
}

// registerWorkflowsList registers the workflows list command.
func registerWorkflowsList() *cobra.Command {
	var org, output string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all workflows",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, cleanup, err := getClientForWorkflows(cmd, org)
			if err != nil {
				return err
			}
			defer cleanup()

			workflows, err := client.GetWorkflows(cmd.Context())
			if err != nil {
				return fmt.Errorf("failed to list workflows: %w", err)
			}

			if len(workflows) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No workflows found")
				return nil
			}

			switch output {
			case "json":
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(workflows)
			case "yaml":
				encoder := yaml.NewEncoder(cmd.OutOrStdout())
				defer encoder.Close()
				return encoder.Encode(workflows)
			case "table":
				return renderWorkflowsTable(cmd, workflows)
			default:
				return fmt.Errorf("unsupported output format: %s", output)
			}
		},
	}

	cmd.Flags().StringVar(&org, "org", "", "Organization name (uses default if not specified)")
	cmd.Flags().StringVarP(&output, "output", "o", "table", "Output format: table, json, yaml")

	return cmd
}

// renderWorkflowsTable renders workflows as a table.
func renderWorkflowsTable(cmd *cobra.Command, workflows []map[string]interface{}) error {
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	defer w.Flush()

	fmt.Fprintln(w, "IDENTIFIER\tTITLE")

	for _, wf := range workflows {
		identifier := extractStringField(wf, "identifier")
		title := extractStringField(wf, "title")
		fmt.Fprintf(w, "%s\t%s\n", identifier, title)
	}

	return nil
}

// extractStringField safely extracts a string field from a map, returning "—" if missing.
func extractStringField(m map[string]interface{}, key string) string {
	v, ok := m[key]
	if !ok {
		return "—"
	}
	s, ok := v.(string)
	if !ok {
		return "—"
	}
	return s
}

// registerWorkflowsGet registers the workflows get command.
func registerWorkflowsGet() *cobra.Command {
	var org, output string

	cmd := &cobra.Command{
		Use:   "get <identifier>",
		Short: "Get a workflow by identifier",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			identifier := args[0]

			if err := validateWorkflowIdentifier(identifier); err != nil {
				return err
			}

			client, cleanup, err := getClientForWorkflows(cmd, org)
			if err != nil {
				return err
			}
			defer cleanup()

			workflow, err := client.GetWorkflow(cmd.Context(), identifier)
			if err != nil {
				return err
			}

			switch output {
			case "json":
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(workflow)
			case "yaml":
				encoder := yaml.NewEncoder(cmd.OutOrStdout())
				defer encoder.Close()
				return encoder.Encode(workflow)
			default:
				return fmt.Errorf("unsupported output format: %s", output)
			}
		},
	}

	cmd.Flags().StringVar(&org, "org", "", "Organization name (uses default if not specified)")
	cmd.Flags().StringVarP(&output, "output", "o", "json", "Output format: json, yaml")

	return cmd
}

// registerWorkflowsCreate registers the workflows create command.
func registerWorkflowsCreate() *cobra.Command {
	var org, file string
	var force bool

	cmd := &cobra.Command{
		Use:   "create --file <path>",
		Short: "Create a workflow from a file",
		RunE: func(cmd *cobra.Command, args []string) error {
			if file == "" {
				return fmt.Errorf("--file flag is required")
			}

			// Parse the workflow file
			body, identifier, err := parseWorkflowFile(file)
			if err != nil {
				return err
			}

			// Validate identifier
			if err := validateWorkflowIdentifier(identifier); err != nil {
				return err
			}

			client, cleanup, err := getClientForWorkflows(cmd, org)
			if err != nil {
				return err
			}
			defer cleanup()

			ctx := cmd.Context()

			// GET probe to check if workflow exists
			existingWorkflow, err := client.GetWorkflow(ctx, identifier)
			if err != nil {
				// If not found, create directly
				if errors.Is(err, api.ErrWorkflowNotFound) {
					if _, err := client.CreateWorkflow(ctx, body); err != nil {
						return fmt.Errorf("failed to create workflow: %w", err)
					}
					fmt.Fprintf(cmd.OutOrStdout(), "Created workflow %s\n", identifier)
					return nil
				}
				// Other error
				return err
			}

			// Workflow exists — enter recreate mode
			return recreateWorkflow(cmd, client, identifier, body, existingWorkflow, force, true)
		},
	}

	cmd.Flags().StringVar(&org, "org", "", "Organization name (uses default if not specified)")
	cmd.Flags().StringVarP(&file, "file", "f", "", "Path to workflow file (JSON or YAML)")
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")

	return cmd
}

// recreateWorkflow implements the recreate-and-rollback logic for create and update.
func recreateWorkflow(cmd *cobra.Command, client *api.Client, identifier string, newBody, oldWorkflow map[string]interface{}, force bool, isCreate bool) error {
	ctx := cmd.Context()

	// Confirm unless --force
	prompt := fmt.Sprintf("Workflow %s already exists. Replace it? This operation is non-atomic (delete then create). (y/n): ", identifier)
	confirmed, err := confirmAction(prompt, force, cmd.InOrStdin())
	if err != nil {
		return fmt.Errorf("failed to read confirmation: %w", err)
	}
	if !confirmed {
		fmt.Fprintln(cmd.OutOrStdout(), "Aborted")
		return nil
	}

	// DELETE
	if err := client.DeleteWorkflow(ctx, identifier); err != nil {
		return fmt.Errorf("failed to delete existing workflow: %w", err)
	}

	// POST (new body)
	if _, err := client.CreateWorkflow(ctx, newBody); err != nil {
		// Rollback: attempt to restore the old workflow
		rollbackErr := rollbackWorkflow(ctx, client, identifier, oldWorkflow)
		if rollbackErr != nil {
			return fmt.Errorf("workflow %s was deleted, recreate failed (%w), and rollback also failed (%v). Manual restoration required", identifier, err, rollbackErr)
		}
		return fmt.Errorf("workflow %s was deleted, but recreate failed: %w. Successfully restored the previous version", identifier, err)
	}

	// Success
	if isCreate {
		fmt.Fprintf(cmd.OutOrStdout(), "Replaced workflow %s\n", identifier)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "Updated workflow %s\n", identifier)
	}
	return nil
}

// rollbackWorkflow attempts to restore the old workflow after a failed recreate.
func rollbackWorkflow(ctx context.Context, client *api.Client, identifier string, oldWorkflow map[string]interface{}) error {
	_, err := client.CreateWorkflow(ctx, oldWorkflow)
	return err
}

// registerWorkflowsUpdate registers the workflows update command.
func registerWorkflowsUpdate() *cobra.Command {
	var org, file string
	var force bool

	cmd := &cobra.Command{
		Use:   "update <identifier> --file <path>",
		Short: "Update a workflow (recreate: delete then create)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			identifier := args[0]

			if file == "" {
				return fmt.Errorf("--file flag is required")
			}

			// Parse the workflow file
			body, fileIdentifier, err := parseWorkflowFile(file)
			if err != nil {
				return err
			}

			// Validate identifier
			if err := validateWorkflowIdentifier(identifier); err != nil {
				return err
			}

			// Check for identifier mismatch (before any API call)
			if fileIdentifier != identifier {
				return fmt.Errorf("identifier mismatch: argument is %q but file contains %q", identifier, fileIdentifier)
			}

			client, cleanup, err := getClientForWorkflows(cmd, org)
			if err != nil {
				return err
			}
			defer cleanup()

			ctx := cmd.Context()

			// GET probe to check if workflow exists and stash the old definition
			existingWorkflow, err := client.GetWorkflow(ctx, identifier)
			if err != nil {
				// If not found, return a clear "not found" error
				if errors.Is(err, api.ErrWorkflowNotFound) {
					return fmt.Errorf("workflow %s not found", identifier)
				}
				// Other error
				return err
			}

			// Workflow exists — enter recreate mode with rollback
			return recreateWorkflow(cmd, client, identifier, body, existingWorkflow, force, false)
		},
	}

	cmd.Flags().StringVar(&org, "org", "", "Organization name (uses default if not specified)")
	cmd.Flags().StringVarP(&file, "file", "f", "", "Path to workflow file (JSON or YAML)")
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")

	return cmd
}

// registerWorkflowsDelete registers the workflows delete command.
func registerWorkflowsDelete() *cobra.Command {
	var org string
	var force bool

	cmd := &cobra.Command{
		Use:   "delete <identifier>",
		Short: "Delete a workflow",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			identifier := args[0]

			if err := validateWorkflowIdentifier(identifier); err != nil {
				return err
			}

			client, cleanup, err := getClientForWorkflows(cmd, org)
			if err != nil {
				return err
			}
			defer cleanup()

			// Confirm unless --force
			prompt := fmt.Sprintf("Delete workflow %s? This action cannot be undone. (y/n): ", identifier)
			confirmed, err := confirmAction(prompt, force, cmd.InOrStdin())
			if err != nil {
				return fmt.Errorf("failed to read confirmation: %w", err)
			}
			if !confirmed {
				fmt.Fprintln(cmd.OutOrStdout(), "Aborted")
				return nil
			}

			if err := client.DeleteWorkflow(cmd.Context(), identifier); err != nil {
				return fmt.Errorf("failed to delete workflow: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Deleted workflow %s\n", identifier)
			return nil
		},
	}

	cmd.Flags().StringVar(&org, "org", "", "Organization name (uses default if not specified)")
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")

	return cmd
}
