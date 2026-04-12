package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"

	"go-palace/internal/config"
	"go-palace/internal/kg"
	"go-palace/internal/palace"
	"go-palace/version"
)

// OpenKGForMCP opens the KG database. Exposed for cmd/mempalace to call
// without importing kg directly (since mcp already imports it).
func OpenKGForMCP(path string) (*kg.KG, error) {
	return kg.Open(path)
}

// Server is a hand-rolled MCP server. Construct via NewServer.
type Server struct {
	palace     *palace.Palace
	kg         *kg.KG
	config     *config.Config
	palacePath string
}

// Request is a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      any             `json:"id,omitempty"`
}

// Response is a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id,omitempty"`
	Result  any    `json:"result,omitempty"`
	Error   any    `json:"error,omitempty"`
}

// ToolDef describes one MCP tool for the tools/list response.
type ToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// NewServer creates an MCP server backed by the given palace and KG.
func NewServer(palacePath string, p *palace.Palace, kgDB *kg.KG, cfg *config.Config) *Server {
	return &Server{
		palace:     p,
		kg:         kgDB,
		config:     cfg,
		palacePath: palacePath,
	}
}

// Serve reads line-delimited JSON-RPC from stdin and writes responses to stdout.
func (s *Server) Serve(stdin io.Reader, stdout io.Writer) error {
	scanner := bufio.NewScanner(stdin)
	// Allow up to 10 MB lines for large tool arguments.
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var req Request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			slog.Error("mcp: json parse error", "error", err)
			continue
		}
		resp := s.handleRequest(req)
		if resp == nil {
			continue // notification, no response
		}
		data, err := json.Marshal(resp)
		if err != nil {
			slog.Error("mcp: json marshal error", "error", err)
			continue
		}
		fmt.Fprintf(stdout, "%s\n", data)
		if f, ok := stdout.(interface{ Flush() }); ok {
			f.Flush()
		}
	}
	return scanner.Err()
}

func (s *Server) handleRequest(req Request) *Response {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "notifications/initialized":
		return nil
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(req)
	default:
		return &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   map[string]any{"code": -32601, "message": fmt.Sprintf("Unknown method: %s", req.Method)},
		}
	}
}

func (s *Server) handleInitialize(req Request) *Response {
	var params map[string]any
	if len(req.Params) > 0 {
		_ = json.Unmarshal(req.Params, &params)
	}
	clientVersion, _ := params["protocolVersion"].(string)
	if clientVersion == "" {
		clientVersion = SupportedProtocolVersions[len(SupportedProtocolVersions)-1]
	}

	negotiated := SupportedProtocolVersions[0]
	for _, v := range SupportedProtocolVersions {
		if v == clientVersion {
			negotiated = clientVersion
			break
		}
	}

	return &Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"protocolVersion": negotiated,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "mempalace", "version": version.Version},
		},
	}
}

func (s *Server) handleToolsList(req Request) *Response {
	defs := toolDefinitions()
	tools := make([]map[string]any, len(defs))
	for i, d := range defs {
		tools[i] = map[string]any{
			"name":        d.Name,
			"description": d.Description,
			"inputSchema": d.InputSchema,
		}
	}
	return &Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  map[string]any{"tools": tools},
	}
}

func (s *Server) handleToolsCall(req Request) *Response {
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return &Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   map[string]any{"code": -32602, "message": "Invalid params"},
			}
		}
	}

	handler, schema := lookupTool(params.Name)
	if handler == nil {
		return &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   map[string]any{"code": -32601, "message": fmt.Sprintf("Unknown tool: %s", params.Name)},
		}
	}

	// Type coercion based on input_schema.
	args := params.Arguments
	if args == nil {
		args = map[string]any{}
	}
	coerceArgs(args, schema)

	result := handler(s, args)
	resultJSON, _ := json.Marshal(result)

	return &Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": string(resultJSON)},
			},
		},
	}
}

// coerceArgs converts string values to int/float where the schema declares
// "integer" or "number" types.
func coerceArgs(args map[string]any, schema map[string]any) {
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		return
	}
	for key, val := range args {
		propSchema, ok := props[key].(map[string]any)
		if !ok {
			continue
		}
		declaredType, _ := propSchema["type"].(string)
		switch declaredType {
		case "integer":
			switch v := val.(type) {
			case float64:
				args[key] = int(v)
			case string:
				if n, err := strconv.Atoi(v); err == nil {
					args[key] = n
				}
			}
		case "number":
			if v, ok := val.(string); ok {
				if f, err := strconv.ParseFloat(v, 64); err == nil {
					args[key] = f
				}
			}
		}
	}
}
