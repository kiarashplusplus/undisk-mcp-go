// Package undisk provides a Go client for the Undisk MCP server —
// undo-first versioned file storage for AI agents.
//
// Usage:
//
//	client := undisk.NewClient("your-api-key")
//	ctx := context.Background()
//
//	err := client.Initialize(ctx)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	result, err := client.CallTool(ctx, "list_files", map[string]interface{}{"path": "/"})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Println(result.Text())
package undisk
