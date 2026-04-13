# undisk-mcp Go SDK

Go client for [Undisk MCP](https://mcp.undisk.app) — undo-first versioned file storage for AI agents.

## Install

```bash
go get github.com/kiarashplusplus/undisk-mcp-go
```

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log"

    undisk "github.com/kiarashplusplus/undisk-mcp-go"
)

func main() {
    client := undisk.NewClient("your-api-key")
    ctx := context.Background()

    if err := client.Initialize(ctx); err != nil {
        log.Fatal(err)
    }

    // Write a file
    _, err := client.WriteFile(ctx, "hello.txt", "Hello from Go!")
    if err != nil {
        log.Fatal(err)
    }

    // Read it back
    result, err := client.ReadFile(ctx, "hello.txt")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(result.Text())
}
```

## Features

- **Zero dependencies** — uses only the Go standard library
- **Context-aware** — all methods accept `context.Context`
- **Typed tool inputs** — generated structs for all 24 MCP tools
- **Functional options** — `WithEndpoint()`, `WithMaxRetries()`, `WithHTTPClient()`
- **Retry with exponential backoff**
- **Session management** (automatic `Mcp-Session-Id` tracking)

## Code Generation

The `types.go` file is auto-generated from the canonical MCP tool schema:

```bash
cd packages/go-sdk
python3 scripts/generate_types.py
```

## License

MIT
