package agents

import (
	"encoding/json"
	"testing"

	"github.com/port-experimental/port-cli/internal/api"
)

func makeEvent(t *testing.T, eventType string, payload any) api.SSEEvent {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}
	return api.SSEEvent{Type: eventType, Payload: json.RawMessage(raw)}
}

func TestProcessEvent_InvocationIdentifier(t *testing.T) {
	result := &InvokeResult{}
	event := makeEvent(t, "invocationIdentifier", map[string]any{
		"invocationIdentifier": "inv_abc123",
	})
	if err := processEvent(event, result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.InvocationID != "inv_abc123" {
		t.Errorf("expected InvocationID %q, got %q", "inv_abc123", result.InvocationID)
	}
}

func TestProcessEvent_ToolCall_AskUserQuestions(t *testing.T) {
	result := &InvokeResult{}
	event := makeEvent(t, "toolCall", map[string]any{
		"toolName": "ask_user_questions",
		"args": map[string]any{
			"questions": []string{"What region?", "Which tier?"},
		},
	})
	if err := processEvent(event, result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.AskUserQuestions) != 2 {
		t.Fatalf("expected 2 questions, got %d", len(result.AskUserQuestions))
	}
	if result.AskUserQuestions[0] != "What region?" {
		t.Errorf("expected first question %q, got %q", "What region?", result.AskUserQuestions[0])
	}
	if result.AskUserQuestions[1] != "Which tier?" {
		t.Errorf("expected second question %q, got %q", "Which tier?", result.AskUserQuestions[1])
	}
}

func TestProcessEvent_ToolCall_OtherTool_NoChange(t *testing.T) {
	result := &InvokeResult{}
	event := makeEvent(t, "toolCall", map[string]any{
		"toolName": "search_entities",
		"args":     map[string]any{},
	})
	if err := processEvent(event, result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.AskUserQuestions) != 0 {
		t.Errorf("expected no questions, got %d", len(result.AskUserQuestions))
	}
	if result.Output != "" {
		t.Errorf("expected empty output, got %q", result.Output)
	}
}

func TestProcessEvent_Done(t *testing.T) {
	result := &InvokeResult{}
	event := makeEvent(t, "done", map[string]any{
		"output": "final answer here",
		"monthlyQuotaUsage": map[string]any{
			"used":  float64(5),
			"limit": float64(100),
		},
		"rateLimitUsage": map[string]any{
			"used": float64(1),
		},
		"contextUsage": map[string]any{
			"tokens": float64(1234),
		},
	})
	if err := processEvent(event, result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "final answer here" {
		t.Errorf("expected output %q, got %q", "final answer here", result.Output)
	}
	if result.MonthlyQuotaUsage == nil {
		t.Error("expected MonthlyQuotaUsage to be set")
	}
	if result.RateLimitUsage == nil {
		t.Error("expected RateLimitUsage to be set")
	}
	if result.ContextUsage == nil {
		t.Error("expected ContextUsage to be set")
	}
}

func TestProcessEvent_UnknownEventType_NoError(t *testing.T) {
	result := &InvokeResult{}
	event := makeEvent(t, "someUnknownEvent", map[string]any{"foo": "bar"})
	if err := processEvent(event, result); err != nil {
		t.Errorf("unexpected error for unknown event type: %v", err)
	}
	// No fields should be set
	if result.Output != "" || result.InvocationID != "" || len(result.AskUserQuestions) != 0 {
		t.Error("expected result to be unchanged for unknown event type")
	}
}

func TestProcessEvent_AskUserQuestions_MultipleCallsAppend(t *testing.T) {
	result := &InvokeResult{}

	// First toolCall
	event1 := makeEvent(t, "toolCall", map[string]any{
		"toolName": "ask_user_questions",
		"args": map[string]any{
			"questions": []string{"First question?"},
		},
	})
	if err := processEvent(event1, result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Second toolCall appends to existing questions
	event2 := makeEvent(t, "toolCall", map[string]any{
		"toolName": "ask_user_questions",
		"args": map[string]any{
			"questions": []string{"Second question?"},
		},
	})
	if err := processEvent(event2, result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.AskUserQuestions) != 2 {
		t.Errorf("expected 2 questions after two calls, got %d", len(result.AskUserQuestions))
	}
}
