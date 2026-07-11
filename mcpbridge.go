package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// startMCPBridge launches a local stdio MCP server (e.g. a UI-automation agent
// like terminator on Windows, or any filesystem/git server) as a child process,
// mirrors the tools it exposes onto our own MCP server, and serves that over the
// tailnet listener as Streamable HTTP at /mcp.
//
// tailtap adds nothing to the tools themselves; it is the transport and the
// auth. The tailnet ACL is the only gate, exactly like the SSH server, which is
// what these local MCP servers otherwise lack when exposed over a network.
//
// The child runs as whoever started tailtap, so on Windows a UI-automation
// server lands in that user's interactive desktop session, which is where it
// needs to be to drive the GUI.
func startMCPBridge(ln net.Listener, command, name string) error {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return fmt.Errorf("empty -mcp-cmd")
	}

	// 1. Spawn and initialize the upstream stdio MCP server. NewStdioMCPClient
	// starts the child process itself.
	up, err := client.NewStdioMCPClient(fields[0], nil, fields[1:]...)
	if err != nil {
		return fmt.Errorf("start upstream MCP server %q: %w", fields[0], err)
	}

	ctx := context.Background()
	var initReq mcp.InitializeRequest
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{Name: "tailtap", Version: version}
	initRes, err := up.Initialize(ctx, initReq)
	if err != nil {
		return fmt.Errorf("initialize upstream: %w", err)
	}
	infoLog("mcp: bridged upstream %q (%s %s)", command, initRes.ServerInfo.Name, initRes.ServerInfo.Version)

	// 2. Mirror the upstream tools onto our own server, forwarding every call.
	srv := server.NewMCPServer("tailtap-"+name, version)
	tools, err := up.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return fmt.Errorf("list upstream tools: %w", err)
	}
	for _, t := range tools.Tools {
		schema, err := json.Marshal(t.InputSchema)
		if err != nil {
			infoLog("mcp: skipping tool %q (bad schema: %v)", t.Name, err)
			continue
		}
		proxied := mcp.NewToolWithRawSchema(t.Name, t.Description, schema)
		srv.AddTool(proxied, forwardTool(up, t.Name))
	}
	infoLog("mcp: exposing %d tool(s) on the tailnet", len(tools.Tools))

	// 3. Serve Streamable HTTP on the tailnet listener. Localhost protection is
	// off because requests arrive by tailnet name, not localhost; the tailnet
	// ACL is what actually guards this endpoint.
	httpSrv := server.NewStreamableHTTPServer(srv,
		server.WithEndpointPath("/mcp"),
		server.WithDisableLocalhostProtection(true),
	)
	mux := http.NewServeMux()
	mux.Handle("/mcp", httpSrv)
	return http.Serve(ln, mux)
}

// forwardTool returns a handler that passes a tool call straight through to the
// upstream server and returns its result unchanged.
func forwardTool(up *client.Client, name string) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var fwd mcp.CallToolRequest
		fwd.Params.Name = name
		fwd.Params.Arguments = req.GetArguments()
		return up.CallTool(ctx, fwd)
	}
}
