package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/evan-william/codepack-lz/internal/format"
	packpkg "github.com/evan-william/codepack-lz/internal/pack"
	"github.com/evan-william/codepack-lz/internal/prune"
	"github.com/evan-william/codepack-lz/internal/stats"
	"github.com/evan-william/codepack-lz/internal/tokens"
	"github.com/evan-william/codepack-lz/internal/version"
	"github.com/evan-william/codepack-lz/internal/walk"
)

func newMCPCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Run a stdio MCP server",
		Long:  "Run a minimal stdio Model Context Protocol server exposing pack_codebase and stats_codepack tools.",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return runMCP(os.Stdin, os.Stdout)
		},
	}
}

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func runMCP(in io.Reader, out io.Writer) error {
	br := bufio.NewReader(in)
	for {
		msg, err := readRPCMessage(br)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if len(msg.ID) == 0 {
			continue // notification
		}
		result, rpcErr := handleMCP(msg)
		if err := writeRPCResponse(out, msg.ID, result, rpcErr); err != nil {
			return err
		}
	}
}

func handleMCP(msg rpcMessage) (any, *rpcError) {
	switch msg.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]string{
				"name":    "codepack-lz",
				"version": version.Version,
			},
		}, nil
	case "tools/list":
		return map[string]any{"tools": []any{mcpPackTool(), mcpStatsTool()}}, nil
	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return nil, newRPCError(-32602, err.Error())
		}
		switch params.Name {
		case "pack_codebase":
			text, err := mcpPackCodebase(params.Arguments)
			if err != nil {
				return nil, newRPCError(-32000, err.Error())
			}
			return mcpTextResult(text), nil
		case "stats_codepack":
			text, err := mcpStatsCodepack(params.Arguments)
			if err != nil {
				return nil, newRPCError(-32000, err.Error())
			}
			return mcpTextResult(text), nil
		default:
			return nil, newRPCError(-32602, "unknown tool "+strconv.Quote(params.Name))
		}
	default:
		return nil, newRPCError(-32601, "method not found")
	}
}

func mcpPackTool() map[string]any {
	return map[string]any{
		"name":        "pack_codebase",
		"description": "Pack a local directory into md, xml, txt, or codepack output.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":           map[string]any{"type": "string", "default": "."},
				"format":         map[string]any{"type": "string", "enum": format.Kinds(), "default": format.KindMarkdown},
				"include":        map[string]any{"type": "array", "items": map[string]string{"type": "string"}},
				"exclude":        map[string]any{"type": "array", "items": map[string]string{"type": "string"}},
				"max_file_size":  map[string]any{"type": "string", "default": "1MiB"},
				"redact":         map[string]any{"type": "boolean", "default": false},
				"compress":       map[string]any{"type": "boolean", "default": false},
				"count_tokens":   map[string]any{"type": "string", "enum": []string{"est", "off"}, "default": "est"},
				"strip_comments": map[string]any{"type": "boolean", "default": false},
			},
		},
	}
}

func mcpStatsTool() map[string]any {
	return map[string]any{
		"name":        "stats_codepack",
		"description": "Inspect a codepack output file header or readable output kind.",
		"inputSchema": map[string]any{
			"type":       "object",
			"required":   []string{"path"},
			"properties": map[string]any{"path": map[string]string{"type": "string"}},
		},
	}
}

func mcpTextResult(text string) map[string]any {
	return map[string]any{
		"content": []map[string]string{{"type": "text", "text": text}},
	}
}

func mcpPackCodebase(raw json.RawMessage) (string, error) {
	var args struct {
		Path          string   `json:"path"`
		Format        string   `json:"format"`
		Include       []string `json:"include"`
		Exclude       []string `json:"exclude"`
		MaxFileSize   string   `json:"max_file_size"`
		Redact        bool     `json:"redact"`
		Compress      bool     `json:"compress"`
		CountTokens   string   `json:"count_tokens"`
		StripComments bool     `json:"strip_comments"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", err
		}
	}
	if args.Path == "" {
		args.Path = "."
	}
	if args.Format == "" {
		args.Format = format.KindMarkdown
	}
	if args.MaxFileSize == "" {
		args.MaxFileSize = "1MiB"
	}
	if args.CountTokens == "" {
		args.CountTokens = "est"
	}
	if args.Format == format.KindEnvelope && (args.Compress || args.StripComments) {
		return "", fmt.Errorf("codepack envelope cannot transform content")
	}
	maxFileSize, err := parseSize(args.MaxFileSize)
	if err != nil {
		return "", err
	}
	opts := packpkg.Options{
		Walk: walk.Options{
			Include:     args.Include,
			Exclude:     args.Exclude,
			MaxFileSize: maxFileSize,
		},
		SecretScan:    true,
		Redact:        args.Redact,
		StripComments: args.StripComments,
	}
	if args.Compress {
		opts.Pruner = prune.NewHeuristic()
	}
	switch args.CountTokens {
	case "off":
	case "est":
		if args.Format != format.KindEnvelope {
			opts.Counter, err = tokens.NewEstimator()
			if err != nil {
				return "", err
			}
		}
	default:
		return "", fmt.Errorf("count_tokens must be est or off")
	}

	p, err := packpkg.Build(args.Path, opts)
	if err != nil {
		return "", err
	}
	r, err := format.New(args.Format)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := r.Render(&buf, p); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func mcpStatsCodepack(raw json.RawMessage) (string, error) {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	if args.Path == "" {
		return "", fmt.Errorf("path is required")
	}
	var buf bytes.Buffer
	if err := stats.Print(&buf, args.Path); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func readRPCMessage(br *bufio.Reader) (rpcMessage, error) {
	contentLength := -1
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return rpcMessage{}, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			return rpcMessage{}, fmt.Errorf("malformed MCP header line %q", line)
		}
		if strings.EqualFold(strings.TrimSpace(key), "content-length") {
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return rpcMessage{}, err
			}
			contentLength = n
		}
	}
	if contentLength < 0 {
		return rpcMessage{}, fmt.Errorf("missing Content-Length header")
	}
	body := make([]byte, contentLength)
	if _, err := io.ReadFull(br, body); err != nil {
		return rpcMessage{}, err
	}
	var msg rpcMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		return rpcMessage{}, err
	}
	return msg, nil
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func newRPCError(code int, msg string) *rpcError {
	return &rpcError{Code: code, Message: msg}
}

func writeRPCResponse(out io.Writer, id json.RawMessage, result any, rpcErr *rpcError) error {
	resp := map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id)}
	if rpcErr != nil {
		resp["error"] = rpcErr
	} else {
		resp["result"] = result
	}
	body, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		return err
	}
	_, err = out.Write(body)
	return err
}
