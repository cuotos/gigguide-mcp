package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"runtime/debug"
	"strings"
)

const protocolVersion = "2024-11-05"

func buildVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			return s.Value[:min(7, len(s.Value))]
		}
	}
	return "dev"
}

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      interface{}   `json:"id"`
	Result  interface{}   `json:"result,omitempty"`
	Error   *jsonRPCError `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func main() {
	log.SetOutput(os.Stderr)

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	writer := bufio.NewWriter(os.Stdout)

	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		var req jsonRPCRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			log.Printf("parse error: %v", err)
			continue
		}

		resp := handleRequest(&req)
		if resp == nil {
			continue // notification — no response needed
		}

		data, err := json.Marshal(resp)
		if err != nil {
			log.Printf("marshal error: %v", err)
			continue
		}
		fmt.Fprintf(writer, "%s\n", data)
		writer.Flush()
	}
}

func handleRequest(req *jsonRPCRequest) *jsonRPCResponse {
	switch req.Method {
	case "initialize":
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]interface{}{
				"protocolVersion": protocolVersion,
				"serverInfo": map[string]interface{}{
					"name":    "gigguide-mcp",
					"version": buildVersion(),
				},
				"capabilities": map[string]interface{}{
					"tools": map[string]interface{}{},
				},
			},
		}

	case "notifications/initialized":
		return nil

	case "tools/list":
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]interface{}{"tools": toolList()},
		}

	case "tools/call":
		return handleToolCall(req)

	default:
		return errResp(req.ID, -32601, fmt.Sprintf("method not found: %s", req.Method))
	}
}

func toolList() []map[string]interface{} {
	return []map[string]interface{}{
		{
			"name":        "search_gigs",
			"description": "Search for upcoming gigs and live music events from the Rock Regeneration gig guide, covering venues across southern England (Hampshire, Dorset, Wiltshire, Isle of Wight, etc). Returns structured event data. All filters are optional — omit to return everything.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"location": map[string]interface{}{
						"type":        "string",
						"description": "Filter by town or city. Automatically expands to include nearby towns in the same area (e.g. 'Southampton' also returns Eastleigh, Winchester, Hedge End, etc.).",
					},
					"artist": map[string]interface{}{
						"type":        "string",
						"description": "Filter by artist or band name — partial, case-insensitive match.",
					},
					"venue": map[string]interface{}{
						"type":        "string",
						"description": "Filter by venue name — partial, case-insensitive match.",
					},
					"from_date": map[string]interface{}{
						"type":        "string",
						"description": "Only return gigs on or after this date. Format: YYYY-MM-DD.",
					},
					"to_date": map[string]interface{}{
						"type":        "string",
						"description": "Only return gigs on or before this date. Format: YYYY-MM-DD.",
					},
				},
			},
		},
	}
}

type searchParams struct {
	Location string `json:"location"`
	Artist   string `json:"artist"`
	Venue    string `json:"venue"`
	FromDate string `json:"from_date"`
	ToDate   string `json:"to_date"`
}

func handleToolCall(req *jsonRPCRequest) *jsonRPCResponse {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errResp(req.ID, -32602, "invalid params")
	}
	if p.Name != "search_gigs" {
		return errResp(req.ID, -32602, fmt.Sprintf("unknown tool: %s", p.Name))
	}

	var args searchParams
	if len(p.Arguments) > 0 {
		if err := json.Unmarshal(p.Arguments, &args); err != nil {
			return errResp(req.ID, -32602, "invalid arguments")
		}
	}

	gigs, err := getGigs()
	if err != nil {
		return errResp(req.ID, -32603, fmt.Sprintf("failed to fetch gigs: %v", err))
	}

	results := filterGigs(gigs, args)

	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return errResp(req.ID, -32603, "failed to marshal results")
	}

	text := fmt.Sprintf("Found %d gig(s).\n\n%s", len(results), string(data))
	return &jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"content": []map[string]interface{}{
				{"type": "text", "text": text},
			},
		},
	}
}

func errResp(id interface{}, code int, msg string) *jsonRPCResponse {
	return &jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &jsonRPCError{Code: code, Message: msg},
	}
}
