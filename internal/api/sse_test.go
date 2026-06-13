package api

import (
	"encoding/json"
	"testing"
)

func TestParseSSELine_ValidDataLine(t *testing.T) {
	line := `data: {"type":"execution","payload":{"content":"thinking..."}}`
	event, err := ParseSSELine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event == nil {
		t.Fatal("expected non-nil event, got nil")
	}
	if event.Type != "execution" {
		t.Errorf("expected type %q, got %q", "execution", event.Type)
	}
}

func TestParseSSELine_WaitingEvent(t *testing.T) {
	line := `data: {"type":"waiting","payload":{}}`
	event, err := ParseSSELine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event == nil {
		t.Fatal("expected non-nil event, got nil")
	}
	if event.Type != "waiting" {
		t.Errorf("expected type %q, got %q", "waiting", event.Type)
	}
}

func TestParseSSELine_BlankLine(t *testing.T) {
	event, err := ParseSSELine("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event != nil {
		t.Errorf("expected nil event for blank line, got %+v", event)
	}
}

func TestParseSSELine_EventFieldLine(t *testing.T) {
	event, err := ParseSSELine("event: message")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event != nil {
		t.Errorf("expected nil event for event: field, got %+v", event)
	}
}

func TestParseSSELine_CommentLine(t *testing.T) {
	event, err := ParseSSELine(": foo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event != nil {
		t.Errorf("expected nil event for comment line, got %+v", event)
	}
}

func TestParseSSELine_DoneMarker(t *testing.T) {
	event, err := ParseSSELine("data: [DONE]")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event != nil {
		t.Errorf("expected nil event for [DONE], got %+v", event)
	}
}

func TestParseSSELine_EmptyDataAfterPrefix(t *testing.T) {
	event, err := ParseSSELine("data: ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event != nil {
		t.Errorf("expected nil event for empty data, got %+v", event)
	}
}

func TestParseSSELine_NonJSONPayload(t *testing.T) {
	event, err := ParseSSELine("data: not-json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event != nil {
		t.Errorf("expected nil event for non-JSON data, got %+v", event)
	}
}

func TestParseSSELine_DoneEventWithOutput(t *testing.T) {
	line := `data: {"type":"done","payload":{"output":"hello"}}`
	event, err := ParseSSELine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event == nil {
		t.Fatal("expected non-nil event, got nil")
	}
	if event.Type != "done" {
		t.Errorf("expected type %q, got %q", "done", event.Type)
	}
	// Verify the payload is present and contains the output field.
	var payload struct {
		Output string `json:"output"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}
	if payload.Output != "hello" {
		t.Errorf("expected output %q, got %q", "hello", payload.Output)
	}
}

func TestParseSSELine_InvocationIdentifierEvent(t *testing.T) {
	line := `data: {"type":"invocationIdentifier","payload":{"invocationIdentifier":"inv_abc"}}`
	event, err := ParseSSELine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event == nil {
		t.Fatal("expected non-nil event, got nil")
	}
	if event.Type != "invocationIdentifier" {
		t.Errorf("expected type %q, got %q", "invocationIdentifier", event.Type)
	}
}
