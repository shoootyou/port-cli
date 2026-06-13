package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/port-experimental/port-cli/internal/api"
	"github.com/port-experimental/port-cli/internal/config"
	"github.com/port-experimental/port-cli/internal/modules/agents"
	"github.com/port-experimental/port-cli/internal/styles"
	"github.com/spf13/cobra"
)

// RegisterAgents registers the "port agents" command group.
func RegisterAgents(rootCmd *cobra.Command) {
	agentsCmd := &cobra.Command{
		Use:   "agents",
		Short: "Invoke and manage Port AI Agents",
		Long:  "Invoke Port AI Agents and manage their configuration.",
	}
	agentsCmd.AddCommand(registerAgentInvoke())
	rootCmd.AddCommand(agentsCmd)
}

func registerAgentInvoke() *cobra.Command {
	var (
		org    string
		raw    bool
		output string
	)

	cmd := &cobra.Command{
		Use:   "invoke <agent-id> <prompt>",
		Short: "Invoke a Port AI Agent",
		Long: `Invoke a Port AI Agent with a prompt and stream the response.

Progress events are written to stderr; the final output goes to stdout.
Use --raw to dump all SSE events as JSON (useful for debugging).
Use --output json to get a structured JSON result instead of plain text.

Examples:
  port agents invoke triage_agent "Storage account for the payments system"
  port agents invoke triage_agent "Virtual network for PCI workloads" --output json
  port agents invoke triage_agent "Key Vault for SOC logs" --raw`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			agentID := args[0]
			prompt := args[1]

			flags := GetGlobalFlags(cmd.Context())
			configManager := config.NewConfigManager(flags.ConfigFile)

			cfg, err := configManager.LoadWithOverrides(
				flags.ClientID,
				flags.ClientSecret,
				flags.APIURL,
				org,
			)
			if err != nil {
				return fmt.Errorf("failed to load configuration: %w", err)
			}

			useOrg := cfg.GetOrgOrDefault(org)
			orgConfig, err := cfg.GetOrgConfig(useOrg)
			if err != nil {
				return err
			}

			token, err := getOrRefreshCommandToken(cmd, configManager, useOrg)
			if err != nil {
				return err
			}

			client := api.NewClient(api.ClientOpts{
				Token:        token,
				ClientID:     orgConfig.ClientID,
				ClientSecret: orgConfig.ClientSecret,
				APIURL:       orgConfig.APIURL,
			})
			defer client.Close()

			lipgloss.Fprintf(os.Stderr, "%s Invoking agent %s…\n",
				styles.CheckMark, styles.Bold.Render(agentID))

			var onProgress func(string, json.RawMessage)
			if raw {
				onProgress = func(eventType string, payload json.RawMessage) {
					enc := json.NewEncoder(os.Stderr)
					_ = enc.Encode(map[string]any{
						"type":    eventType,
						"payload": payload,
					})
				}
			} else {
				onProgress = func(eventType string, payload json.RawMessage) {
					switch eventType {
					case "waiting":
						lipgloss.Fprintf(os.Stderr, "  %s waiting…\n", styles.Circle)
					case "execution":
						lipgloss.Fprintf(os.Stderr, "  %s executing…\n", styles.Circle)
					case "toolPrep", "toolCall":
						var p struct {
							ToolName string `json:"toolName"`
						}
						if json.Unmarshal(payload, &p) == nil && p.ToolName != "" {
							lipgloss.Fprintf(os.Stderr, "  %s tool: %s\n",
								styles.Circle, styles.Bold.Render(p.ToolName))
						}
					case "done":
						lipgloss.Fprintf(os.Stderr, "%s done\n", styles.CheckMark)
					}
				}
			}

			result, err := agents.Invoke(cmd.Context(), client, agents.InvokeOptions{
				AgentID:    agentID,
				Prompt:     prompt,
				OnProgress: onProgress,
			})
			if err != nil {
				return fmt.Errorf("agent invocation failed: %w", err)
			}

			// Surface ask_user_questions if the agent needs more input
			if len(result.AskUserQuestions) > 0 {
				lipgloss.Fprintf(os.Stderr,
					"\n%s The agent needs more information:\n", styles.QuestionMark)
				for i, q := range result.AskUserQuestions {
					lipgloss.Fprintf(os.Stderr, "  %d. %s\n", i+1, q)
				}
				lipgloss.Fprintf(os.Stderr,
					"\nRe-invoke with a prompt that answers the questions above.\n")
			}

			switch strings.ToLower(output) {
			case "json":
				return formatOutput(map[string]any{
					"output":            result.Output,
					"invocationId":      result.InvocationID,
					"askUserQuestions":  result.AskUserQuestions,
					"monthlyQuotaUsage": result.MonthlyQuotaUsage,
					"rateLimitUsage":    result.RateLimitUsage,
					"contextUsage":      result.ContextUsage,
				}, "json")
			default:
				fmt.Println(result.Output)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&org, "org", "", "Organization name (uses default if not specified)")
	cmd.Flags().BoolVar(&raw, "raw", false, "Dump all SSE events as JSON to stderr (for debugging)")
	cmd.Flags().StringVarP(&output, "output", "o", "text", "Output format: text, json")

	return cmd
}
