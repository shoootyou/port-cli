package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/port-experimental/port-cli/internal/api"
)

// InvokeResult holds the processed output of an agent invocation.
type InvokeResult struct {
	// Output is the agent's final text response (from the "done" event).
	Output string
	// InvocationID is the identifier assigned by Port to this invocation.
	InvocationID string
	// AskUserQuestions is non-empty when the agent called ask_user_questions
	// and needs additional information from the user before continuing.
	AskUserQuestions []string
	// MonthlyQuotaUsage tracks invocation quota consumption (from "done" payload).
	MonthlyQuotaUsage map[string]any
	// RateLimitUsage tracks per-minute rate limit consumption (from "done" payload).
	RateLimitUsage map[string]any
	// ContextUsage tracks token consumption (from "done" payload).
	ContextUsage map[string]any
}

// InvokeOptions controls how the agent is invoked.
type InvokeOptions struct {
	// AgentID is the Port agent identifier (e.g. "triage_agent").
	AgentID string
	// Prompt is the user message to send to the agent.
	Prompt string
	// OnProgress is called for each SSE event as it arrives (optional).
	// Useful for streaming progress to the terminal.
	OnProgress func(eventType string, payload json.RawMessage)
}

// Invoke calls a Port AI Agent and returns the processed result.
// It streams SSE events internally, calling OnProgress for each one.
func Invoke(ctx context.Context, client *api.Client, opts InvokeOptions) (*InvokeResult, error) {
	events := make(chan api.SSEEvent, 64)
	result := &InvokeResult{}

	streamErr := make(chan error, 1)
	go func() {
		defer close(events)
		streamErr <- client.InvokeAgent(ctx, opts.AgentID, opts.Prompt, events)
	}()

	for event := range events {
		if opts.OnProgress != nil {
			opts.OnProgress(event.Type, event.Payload)
		}
		if err := processEvent(event, result); err != nil {
			return nil, fmt.Errorf("processing event %q: %w", event.Type, err)
		}
	}

	if err := <-streamErr; err != nil && !errors.Is(err, context.Canceled) {
		return nil, err
	}

	return result, nil
}

// processEvent extracts structured data from each SSE event into result.
func processEvent(event api.SSEEvent, result *InvokeResult) error {
	switch event.Type {
	case "invocationIdentifier":
		var p struct {
			InvocationIdentifier string `json:"invocationIdentifier"`
		}
		if err := json.Unmarshal(event.Payload, &p); err == nil {
			result.InvocationID = p.InvocationIdentifier
		}

	case "toolCall":
		var p struct {
			ToolName string          `json:"toolName"`
			Args     json.RawMessage `json:"args"`
		}
		if err := json.Unmarshal(event.Payload, &p); err != nil {
			return nil
		}
		if p.ToolName == "ask_user_questions" {
			var args struct {
				Questions []string `json:"questions"`
			}
			if err := json.Unmarshal(p.Args, &args); err == nil {
				result.AskUserQuestions = append(result.AskUserQuestions, args.Questions...)
			}
		}

	case "done":
		var p struct {
			Output            string         `json:"output"`
			MonthlyQuotaUsage map[string]any `json:"monthlyQuotaUsage"`
			RateLimitUsage    map[string]any `json:"rateLimitUsage"`
			ContextUsage      map[string]any `json:"contextUsage"`
		}
		if err := json.Unmarshal(event.Payload, &p); err != nil {
			return fmt.Errorf("failed to decode done payload: %w", err)
		}
		result.Output = p.Output
		result.MonthlyQuotaUsage = p.MonthlyQuotaUsage
		result.RateLimitUsage = p.RateLimitUsage
		result.ContextUsage = p.ContextUsage
	}
	return nil
}
