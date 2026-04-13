package undisk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewClient(t *testing.T) {
	c := NewClient("test-key")
	if c.endpoint != DefaultEndpoint {
		t.Errorf("expected %s, got %s", DefaultEndpoint, c.endpoint)
	}
	if c.apiKey != "test-key" {
		t.Errorf("expected test-key, got %s", c.apiKey)
	}
	if c.maxRetries != DefaultMaxRetries {
		t.Errorf("expected %d retries, got %d", DefaultMaxRetries, c.maxRetries)
	}
}

func TestNewClientWithOptions(t *testing.T) {
	c := NewClient("key", WithEndpoint("https://custom.example.com/"), WithMaxRetries(5))
	if c.endpoint != "https://custom.example.com" {
		t.Errorf("expected trimmed endpoint, got %s", c.endpoint)
	}
	if c.maxRetries != 5 {
		t.Errorf("expected 5 retries, got %d", c.maxRetries)
	}
}

func TestToolResultText(t *testing.T) {
	r := &ToolResult{
		Content: []ContentItem{
			{Type: "text", Text: "hello"},
			{Type: "image", Text: ""},
			{Type: "text", Text: "world"},
		},
	}
	if got := r.Text(); got != "hello\nworld" {
		t.Errorf("expected 'hello\\nworld', got %q", got)
	}
}

func TestToolResultTextEmpty(t *testing.T) {
	r := &ToolResult{Content: []ContentItem{}}
	if got := r.Text(); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestUndiskError(t *testing.T) {
	e := &UndiskError{Code: -32600, Message: "Invalid request"}
	expected := "undisk error -32600: Invalid request"
	if e.Error() != expected {
		t.Errorf("expected %q, got %q", expected, e.Error())
	}
}

func TestCallTool(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("missing auth header")
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("missing content-type")
		}

		var req jsonRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		w.Header().Set("Mcp-Session-Id", "test-session")
		w.Header().Set("Content-Type", "application/json")

		// Notifications have no ID — just return 202
		if req.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}

		resp := jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      *req.ID,
		}

		switch req.Method {
		case "initialize":
			result, _ := json.Marshal(map[string]any{
				"protocolVersion": "2025-03-26",
				"serverInfo":     map[string]any{"name": "test"},
			})
			resp.Result = result
		case "tools/call":
			result, _ := json.Marshal(ToolResult{
				Content: []ContentItem{{Type: "text", Text: "file content here"}},
			})
			resp.Result = result
		}

		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := NewClient("test-key", WithEndpoint(server.URL))
	ctx := context.Background()

	if err := c.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	if c.sessionID != "test-session" {
		t.Errorf("expected session ID 'test-session', got %q", c.sessionID)
	}

	result, err := c.CallTool(ctx, "read_file", map[string]any{"path": "test.txt"})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	if result.Text() != "file content here" {
		t.Errorf("expected 'file content here', got %q", result.Text())
	}
}

func TestCallToolError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCRequest
		json.NewDecoder(r.Body).Decode(&req)

		resp := jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      *req.ID,
			Error:   &jsonRPCError{Code: -32602, Message: "Path must be a string"},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := NewClient("key", WithEndpoint(server.URL))
	_, err := c.CallTool(context.Background(), "read_file", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	ue, ok := err.(*UndiskError)
	if !ok {
		t.Fatalf("expected UndiskError, got %T", err)
	}
	if ue.Code != -32602 {
		t.Errorf("expected code -32602, got %d", ue.Code)
	}
}

func TestConvenienceMethods(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCRequest
		json.NewDecoder(r.Body).Decode(&req)

		w.Header().Set("Content-Type", "application/json")
		resp := jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      *req.ID,
		}

		result, _ := json.Marshal(ToolResult{
			Content: []ContentItem{{Type: "text", Text: "ok"}},
		})
		resp.Result = result
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := NewClient("key", WithEndpoint(server.URL))
	ctx := context.Background()

	tests := []struct {
		name string
		fn   func() (*ToolResult, error)
	}{
		{"ReadFile", func() (*ToolResult, error) { return c.ReadFile(ctx, "test.txt") }},
		{"WriteFile", func() (*ToolResult, error) { return c.WriteFile(ctx, "test.txt", "data") }},
		{"ListFiles", func() (*ToolResult, error) { return c.ListFiles(ctx, "/") }},
		{"DeleteFile", func() (*ToolResult, error) { return c.DeleteFile(ctx, "test.txt") }},
		{"SearchFiles", func() (*ToolResult, error) { return c.SearchFiles(ctx, "pattern") }},
		{"ListVersions", func() (*ToolResult, error) { return c.ListVersions(ctx, "test.txt") }},
		{"RestoreVersion", func() (*ToolResult, error) { return c.RestoreVersion(ctx, "test.txt", "v1") }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.fn()
			if err != nil {
				t.Fatalf("%s failed: %v", tt.name, err)
			}
			if result.Text() != "ok" {
				t.Errorf("%s: expected 'ok', got %q", tt.name, result.Text())
			}
		})
	}
}

func TestAllToolNames(t *testing.T) {
	if len(AllToolNames) != 24 {
		t.Errorf("expected 24 tools, got %d", len(AllToolNames))
	}
}

func TestToolConstants(t *testing.T) {
	if ToolReadFile != "read_file" {
		t.Errorf("expected 'read_file', got %q", ToolReadFile)
	}
	if ToolWriteFile != "write_file" {
		t.Errorf("expected 'write_file', got %q", ToolWriteFile)
	}
	if ToolListFiles != "list_files" {
		t.Errorf("expected 'list_files', got %q", ToolListFiles)
	}
}
