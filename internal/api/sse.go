package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/port-experimental/port-cli/internal/useragent"
)

// SSEEvent represents a single parsed Server-Sent Event from the Port Agent API.
type SSEEvent struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// ParseSSELine parses a single SSE line (e.g. "data: {...}") into an SSEEvent.
// Returns (nil, nil) for non-data lines (blank lines, comments, event/id fields).
func ParseSSELine(line string) (*SSEEvent, error) {
	if !strings.HasPrefix(line, "data: ") {
		return nil, nil
	}
	data := strings.TrimPrefix(line, "data: ")
	data = strings.TrimSpace(data)
	if data == "" || data == "[DONE]" {
		return nil, nil
	}
	var event SSEEvent
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		// Non-JSON payload — skip gracefully
		return nil, nil
	}
	return &event, nil
}

// requestStream makes an authenticated POST request and returns the raw *http.Response
// for SSE reading. The caller MUST close resp.Body when done.
//
// A dedicated http.Client with Timeout=0 is used so the stream is not cut by
// the default 300 s client timeout. Lifecycle is controlled via ctx.
func (c *Client) requestStream(ctx context.Context, method, path string, data any) (*http.Response, error) {
	token, err := c.getToken(ctx)
	if err != nil {
		return nil, err
	}

	endpoint := fmt.Sprintf("%s%s", c.apiURL, path)

	var reqBody io.Reader
	if data != nil {
		jsonData, err := json.Marshal(data)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal stream request body: %w", err)
		}
		reqBody = bytes.NewBuffer(jsonData)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create stream request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("User-Agent", useragent.String())

	// Dedicated client: no timeout (ctx controls lifecycle), no compression
	// (gzip-encoded SSE is not progressively decodable).
	streamClient := &http.Client{
		Transport: &http.Transport{
			DisableCompression:    true,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
		},
	}

	resp, err := streamClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("stream request failed: %w", err)
	}

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		// Try to extract just the error message from the JSON response body.
		var apiErr struct {
			Message string `json:"message"`
			Error   string `json:"error"`
		}
		if jsonErr := json.Unmarshal(body, &apiErr); jsonErr == nil {
			if apiErr.Message != "" {
				return nil, fmt.Errorf("agent invoke failed (%s): %s", resp.Status, apiErr.Message)
			}
			if apiErr.Error != "" {
				return nil, fmt.Errorf("agent invoke failed (%s): %s", resp.Status, apiErr.Error)
			}
		}
		return nil, fmt.Errorf("agent invoke failed (%s)", resp.Status)
	}

	return resp, nil
}

// InvokeAgent invokes a Port AI Agent and sends parsed SSE events to the provided
// channel. The channel is closed when the stream ends or ctx is cancelled.
// Returns nil on clean completion, ctx.Err() on cancellation, or a stream error.
func (c *Client) InvokeAgent(ctx context.Context, agentID, prompt string, events chan<- SSEEvent) error {
	body := map[string]string{"prompt": prompt}
	resp, err := c.requestStream(ctx, "POST", fmt.Sprintf("/agent/%s/invoke", url.PathEscape(agentID)), body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	// Increase scanner buffer to handle large SSE payloads (e.g. done events with HCL output).
	// Default 64 KiB is insufficient for AI agent responses; 1 MiB covers typical outputs.
	const maxSSELineBytes = 1 * 1024 * 1024 // 1 MiB
	scanner.Buffer(make([]byte, 0, 64*1024), maxSSELineBytes)
	for scanner.Scan() {
		line := scanner.Text()
		event, _ := ParseSSELine(line)
		if event == nil {
			continue
		}

		select {
		case events <- *event:
		case <-ctx.Done():
			return ctx.Err()
		}

		if event.Type == "done" {
			return nil
		}
	}

	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("SSE stream read error: %w", err)
	}

	return nil
}
