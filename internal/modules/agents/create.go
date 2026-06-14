package agents

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"charm.land/huh/v2"
	"github.com/charmbracelet/x/term"
	"github.com/port-experimental/port-cli/internal/api"
)

// Create creates, upserts, or patches a _ai_agent entity in Port from a .md file.
func Create(ctx context.Context, apiClient *api.Client, opts CreateOptions) (*CreateResult, error) {
	if opts.File == "" {
		return nil, errors.New("file is required")
	}

	spec, err := ParseAgentFile(opts.File)
	if err != nil {
		return nil, err
	}

	// Determine effective mode and prompt key.
	effectiveMode := opts.Mode
	promptKey := "prompt"

	switch opts.Mode {
	case CreateModeAuto:
		existing, getErr := apiClient.GetEntity(ctx, agentBlueprint, spec.Identifier)
		if getErr != nil {
			// Check if it's a 404 — if so, use create path.
			if is404Error(getErr) {
				effectiveMode = CreateModeCreate
				promptKey = "prompt"
			} else {
				return nil, getErr
			}
		} else {
			effectiveMode = CreateModeUpsert
			// Try to detect the prompt property from existing entity.
			existingAgent := parseAgentEntity(existing)
			if key, detectErr := detectPromptProperty(existingAgent); detectErr == nil {
				promptKey = key
			}
			// On detection failure, fall back to "prompt" silently.
		}

	case CreateModePatch:
		existing, getErr := apiClient.GetEntity(ctx, agentBlueprint, spec.Identifier)
		if getErr != nil {
			return nil, getErr
		}
		existingAgent := parseAgentEntity(existing)
		if key, detectErr := detectPromptProperty(existingAgent); detectErr == nil {
			promptKey = key
		}
		// On detection failure, fall back to "prompt" silently.

	case CreateModeCreate, CreateModeUpsert:
		// No GET needed; always use "prompt" as default key.
		promptKey = "prompt"

	default:
		promptKey = "prompt"
	}

	// Run confirmation prompt if not skipped.
	if !opts.Yes {
		if err := runConfirmation(spec, effectiveMode, opts.StdinReader); err != nil {
			return nil, err
		}
	}

	// Coerce nil tools to empty slice to avoid JSON null.
	tools := spec.Tools
	if tools == nil {
		tools = []string{}
	}

	var (
		raw    map[string]interface{}
		action string
		apiErr error
	)

	switch effectiveMode {
	case CreateModeCreate:
		body := buildCreateBody(spec, tools, promptKey)
		raw, apiErr = apiClient.CreateEntityWithParams(ctx, agentBlueprint, body, false, false)
		action = "created"

	case CreateModeUpsert:
		body := buildCreateBody(spec, tools, promptKey)
		raw, apiErr = apiClient.CreateEntityWithParams(ctx, agentBlueprint, body, true, false)
		action = "upserted"

	case CreateModePatch:
		patchBody := buildPatchBody(spec, tools, promptKey)
		var patchRaw api.Entity
		patchRaw, apiErr = apiClient.PatchEntity(ctx, agentBlueprint, spec.Identifier, api.Entity(patchBody))
		if patchRaw != nil {
			raw = map[string]interface{}(patchRaw)
		}
		action = "patched"
	}

	if apiErr != nil {
		return nil, apiErr
	}

	entity := parseAgentEntity(api.Entity(raw))

	return &CreateResult{
		Entity:    entity,
		Action:    action,
		ModeUsed:  effectiveMode,
		PromptKey: promptKey,
	}, nil
}

// buildCreateBody constructs the POST request body.
func buildCreateBody(spec *AgentFileSpec, tools []string, promptKey string) map[string]interface{} {
	properties := map[string]interface{}{
		"description":    spec.Description,
		"model":          spec.Model,
		"provider":       spec.Provider,
		"execution_mode": spec.ExecutionMode,
		"status":         spec.Status,
		"tools":          tools,
		promptKey:        spec.Prompt,
	}

	return map[string]interface{}{
		"identifier": spec.Identifier,
		"title":      spec.Title,
		"blueprint":  agentBlueprint,
		"properties": properties,
	}
}

// buildPatchBody constructs the PATCH request body — only non-empty fields.
func buildPatchBody(spec *AgentFileSpec, tools []string, promptKey string) map[string]interface{} {
	patchProps := map[string]interface{}{
		promptKey: spec.Prompt, // always included
	}

	if spec.Description != "" {
		patchProps["description"] = spec.Description
	}
	if spec.Model != "" {
		patchProps["model"] = spec.Model
	}
	if spec.Provider != "" {
		patchProps["provider"] = spec.Provider
	}
	if spec.ExecutionMode != "" {
		patchProps["execution_mode"] = spec.ExecutionMode
	}
	if spec.Status != "" {
		patchProps["status"] = spec.Status
	}
	if len(tools) > 0 {
		patchProps["tools"] = tools
	}

	// PATCH sends only "properties" — never identity fields (title, identifier).
	// Identity fields are immutable or unneeded on partial updates.
	return map[string]interface{}{
		"properties": patchProps,
	}
}

// runConfirmation shows the confirmation summary and prompts the user.
// stdinReader is the reader to use for input; if nil, os.Stdin is used.
// Returns ErrConfirmationDeclined if the user declines.
// Returns a non-nil error (not ErrConfirmationDeclined) if stdin is not a TTY
// and no reader is injected — callers should treat that as a hard failure (exit 1).
func runConfirmation(spec *AgentFileSpec, mode CreateMode, stdinReader io.Reader) error {
	// Print summary to stderr.
	promptPreview := spec.Prompt
	if promptPreview == "" {
		promptPreview = "(no prompt in file)"
	} else if len(promptPreview) > 100 {
		promptPreview = promptPreview[:100] + "…"
	}

	fmt.Fprintf(os.Stderr, "\nAgent to write\n")
	fmt.Fprintf(os.Stderr, "──────────────────────────────────────────\n")
	fmt.Fprintf(os.Stderr, "Identifier:     %s\n", spec.Identifier)
	fmt.Fprintf(os.Stderr, "Title:          %s\n", spec.Title)
	fmt.Fprintf(os.Stderr, "Mode:           %s\n", string(mode))
	fmt.Fprintf(os.Stderr, "Prompt preview: %s\n", promptPreview)
	fmt.Fprintf(os.Stderr, "──────────────────────────────────────────\n\n")

	// When a reader is injected (test seam): read one byte to detect EOF.
	// EOF → decline (simulates the user pressing Ctrl-D or supplying no input).
	// Any available byte → treat as a "yes" (only used in tests that need to
	// explicitly confirm; real interactive confirmation goes through huh below).
	if stdinReader != nil {
		buf := make([]byte, 1)
		n, readErr := stdinReader.Read(buf)
		if n == 0 || readErr == io.EOF {
			return ErrConfirmationDeclined
		}
		// Non-empty reader: treat first byte as confirmation signal.
		return nil
	}

	// Real interactive path: require a TTY; run huh confirmation form.
	if !term.IsTerminal(os.Stdin.Fd()) {
		return fmt.Errorf("stdin is not a terminal; use --yes to confirm non-interactively")
	}

	var confirmed bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(fmt.Sprintf("Apply changes to %s/%s?", agentBlueprint, spec.Identifier)).
				Value(&confirmed),
		),
	)

	if err := form.Run(); err != nil {
		return ErrConfirmationDeclined
	}

	if !confirmed {
		return ErrConfirmationDeclined
	}

	return nil
}

// is404Error checks whether an error from the API client represents a 404 Not Found.
// It matches the exact phrase "404 Not Found" that the client appends to error messages,
// avoiding false positives from URLs or body content that contain "404" as a substring.
func is404Error(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "404 Not Found")
}
