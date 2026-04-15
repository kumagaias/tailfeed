package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os/exec"
)

type jsonrpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonrpcResponse struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Call spawns the MCP server, discovers the first available tool via tools/list,
// calls it with args, and returns the first text content from the response.
func Call(cfg *Config, args map[string]any) (string, error) {
	cmd := exec.Command(cfg.Command, cfg.Args...)
	cmd.Env = envSlice(cfg.Env)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start mcp server: %w", err)
	}
	defer func() { _ = cmd.Process.Kill() }()

	enc := json.NewEncoder(stdin)
	dec := json.NewDecoder(bufio.NewReader(stdout))

	// 1. initialize
	if err := enc.Encode(jsonrpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "initialize",
		Params: map[string]any{
			"protocolVersion": "2024-11-05",
			"clientInfo":      map[string]any{"name": "tailfeed", "version": "1.0"},
			"capabilities":    map[string]any{},
		},
	}); err != nil {
		return "", err
	}
	var initResp jsonrpcResponse
	if err := dec.Decode(&initResp); err != nil {
		return "", fmt.Errorf("initialize: %w", err)
	}
	if initResp.Error != nil {
		return "", fmt.Errorf("initialize: %s", initResp.Error.Message)
	}

	// 2. notifications/initialized
	_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})

	// 3. tools/list — pick the first tool
	if err := enc.Encode(jsonrpcRequest{JSONRPC: "2.0", ID: 2, Method: "tools/list"}); err != nil {
		return "", err
	}
	var listResp jsonrpcResponse
	if err := dec.Decode(&listResp); err != nil {
		return "", fmt.Errorf("tools/list: %w", err)
	}
	if listResp.Error != nil {
		return "", fmt.Errorf("tools/list: %s", listResp.Error.Message)
	}
	toolName, err := firstToolName(listResp.Result)
	if err != nil {
		return "", err
	}

	// 4. tools/call
	if err := enc.Encode(jsonrpcRequest{
		JSONRPC: "2.0", ID: 3, Method: "tools/call",
		Params: map[string]any{"name": toolName, "arguments": args},
	}); err != nil {
		return "", err
	}
	var callResp jsonrpcResponse
	if err := dec.Decode(&callResp); err != nil {
		return "", fmt.Errorf("tools/call: %w", err)
	}
	if callResp.Error != nil {
		return "", fmt.Errorf("tools/call: %s", callResp.Error.Message)
	}
	return extractText(callResp.Result)
}

func firstToolName(raw json.RawMessage) (string, error) {
	var result struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", err
	}
	if len(result.Tools) == 0 {
		return "", fmt.Errorf("no tools available")
	}
	return result.Tools[0].Name, nil
}

func extractText(raw json.RawMessage) (string, error) {
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", err
	}
	for _, c := range result.Content {
		if c.Type == "text" {
			return c.Text, nil
		}
	}
	return "", fmt.Errorf("no text content in response")
}

func envSlice(m map[string]string) []string {
	s := make([]string, 0, len(m))
	for k, v := range m {
		s = append(s, k+"="+v)
	}
	return s
}
