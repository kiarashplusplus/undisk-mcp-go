package undisk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// DefaultEndpoint is the default Undisk MCP server URL.
	DefaultEndpoint = "https://mcp.undisk.app"
	// DefaultMaxRetries is the default number of retry attempts.
	DefaultMaxRetries = 3
	// DefaultRetryBase is the base delay for exponential backoff.
	DefaultRetryBase = 100 * time.Millisecond
	// DefaultTimeout is the default HTTP client timeout.
	DefaultTimeout = 60 * time.Second
	// Version is the SDK version.
	Version = "0.45.0"
)

// Client is the Undisk MCP client. It is safe for concurrent use.
type Client struct {
	endpoint   string
	apiKey     string
	maxRetries int
	retryBase  time.Duration
	httpClient *http.Client

	mu        sync.RWMutex
	sessionID string
	nextID    atomic.Int64
}

// Option configures a Client.
type Option func(*Client)

// WithEndpoint sets a custom endpoint (default: https://mcp.undisk.app).
func WithEndpoint(endpoint string) Option {
	return func(c *Client) {
		c.endpoint = strings.TrimRight(endpoint, "/")
	}
}

// WithMaxRetries sets the maximum number of retries (default: 3).
func WithMaxRetries(n int) Option {
	return func(c *Client) { c.maxRetries = n }
}

// WithRetryBase sets the base retry delay (default: 100ms).
func WithRetryBase(d time.Duration) Option {
	return func(c *Client) { c.retryBase = d }
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.httpClient = hc }
}

// NewClient creates a new Undisk MCP client.
func NewClient(apiKey string, opts ...Option) *Client {
	c := &Client{
		endpoint:   DefaultEndpoint,
		apiKey:     apiKey,
		maxRetries: DefaultMaxRetries,
		retryBase:  DefaultRetryBase,
		httpClient: &http.Client{Timeout: DefaultTimeout},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// ToolResult represents the result of an MCP tool call.
type ToolResult struct {
	Content []ContentItem `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// ContentItem is a single content block in a tool result.
type ContentItem struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// Text extracts all text content from the result.
func (r *ToolResult) Text() string {
	var parts []string
	for _, c := range r.Content {
		if c.Type == "text" && c.Text != "" {
			parts = append(parts, c.Text)
		}
	}
	return strings.Join(parts, "\n")
}

type jsonRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      *int64      `json:"id,omitempty"`
	Method  string      `json:"method"`
	Params  any `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// UndiskError is an error returned by the Undisk MCP server.
type UndiskError struct {
	Code    int
	Message string
	Data    json.RawMessage
}

func (e *UndiskError) Error() string {
	return fmt.Sprintf("undisk error %d: %s", e.Code, e.Message)
}

// Initialize starts an MCP session.
func (c *Client) Initialize(ctx context.Context) error {
	_, err := c.sendRPC(ctx, "initialize", map[string]any{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "undisk-mcp-go",
			"version": Version,
		},
	})
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}

	return c.sendNotification(ctx, "notifications/initialized", map[string]any{})
}

// ListTools returns the available tools from the server.
func (c *Client) ListTools(ctx context.Context) ([]ToolSchema, error) {
	raw, err := c.sendRPC(ctx, "tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}

	var result struct {
		Tools []ToolSchema `json:"tools"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("parse tools: %w", err)
	}
	return result.Tools, nil
}

// ToolSchema describes a single MCP tool.
type ToolSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// CallTool calls an MCP tool by name.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (*ToolResult, error) {
	raw, err := c.sendRPC(ctx, "tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	})
	if err != nil {
		return nil, err
	}

	var result ToolResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("parse result: %w", err)
	}
	return &result, nil
}

// ── Convenience methods ──

// ReadFile reads a file from the workspace.
func (c *Client) ReadFile(ctx context.Context, path string) (*ToolResult, error) {
	return c.CallTool(ctx, ToolReadFile, map[string]any{"path": path})
}

// WriteFile writes content to a file.
func (c *Client) WriteFile(ctx context.Context, path, content string) (*ToolResult, error) {
	return c.CallTool(ctx, ToolWriteFile, map[string]any{"path": path, "content": content})
}

// ListFiles lists files in the workspace.
func (c *Client) ListFiles(ctx context.Context, path string) (*ToolResult, error) {
	return c.CallTool(ctx, ToolListFiles, map[string]any{"path": path})
}

// DeleteFile deletes a file from the workspace.
func (c *Client) DeleteFile(ctx context.Context, path string) (*ToolResult, error) {
	return c.CallTool(ctx, ToolDeleteFile, map[string]any{"path": path})
}

// SearchFiles searches file contents.
func (c *Client) SearchFiles(ctx context.Context, pattern string) (*ToolResult, error) {
	return c.CallTool(ctx, ToolSearchFiles, map[string]any{"pattern": pattern})
}

// ListVersions gets the version history for a file.
func (c *Client) ListVersions(ctx context.Context, path string) (*ToolResult, error) {
	return c.CallTool(ctx, ToolListVersions, map[string]any{"path": path})
}

// RestoreVersion restores a file to a previous version.
func (c *Client) RestoreVersion(ctx context.Context, path, versionID string) (*ToolResult, error) {
	return c.CallTool(ctx, ToolRestoreVersion, map[string]any{"path": path, "version_id": versionID})
}

// ── Internal ──

func (c *Client) sendRPC(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      &id,
		Method:  method,
		Params:  params,
	}

	respBody, err := c.httpPost(ctx, req, false)
	if err != nil {
		return nil, err
	}

	var rpcResp jsonRPCResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if rpcResp.Error != nil {
		return nil, &UndiskError{Code: rpcResp.Error.Code, Message: rpcResp.Error.Message, Data: rpcResp.Error.Data}
	}

	return rpcResp.Result, nil
}

func (c *Client) sendNotification(ctx context.Context, method string, params any) error {
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	_, err := c.httpPost(ctx, req, true)
	return err
}

func (c *Client) httpPost(ctx context.Context, body any, isNotification bool) ([]byte, error) {
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := c.endpoint + "/mcp"

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+c.apiKey)

		c.mu.RLock()
		sid := c.sessionID
		c.mu.RUnlock()
		if sid != "" {
			req.Header.Set("Mcp-Session-Id", sid)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			if attempt >= c.maxRetries {
				return nil, fmt.Errorf("request failed: %w", err)
			}
			if err := c.backoff(ctx, attempt); err != nil {
				return nil, err
			}
			continue
		}

		if newSID := resp.Header.Get("Mcp-Session-Id"); newSID != "" {
			c.mu.Lock()
			c.sessionID = newSID
			c.mu.Unlock()
		}

		if resp.StatusCode >= 400 {
			errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			resp.Body.Close()
			// Only retry on server errors (5xx) and rate limits (429);
			// client errors (4xx) are not transient and should fail immediately.
			retryable := resp.StatusCode == 429 || resp.StatusCode >= 500
			if retryable && attempt < c.maxRetries {
				if err := c.backoff(ctx, attempt); err != nil {
					return nil, err
				}
				continue
			}
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(errBody))
		}

		if isNotification {
			resp.Body.Close()
			return nil, nil
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("read response: %w", err)
		}
		return respBody, nil
	}

	return nil, fmt.Errorf("max retries exceeded")
}

func (c *Client) backoff(ctx context.Context, attempt int) error {
	shift := attempt
	if shift > 30 {
		shift = 30
	}
	delay := c.retryBase * time.Duration(1<<uint(shift))
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay):
		return nil
	}
}
