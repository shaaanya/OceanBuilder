package libreterrain

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// FindBridge locates the optional libreterrain-mcp binary: explicit path, then
// $LIBRETERRAIN_MCP, then ../LibreTerrain/libreterrain-mcp, then $PATH. "" if absent.
func FindBridge(explicit string) string {
	for _, c := range []string{explicit, os.Getenv("LIBRETERRAIN_MCP"), filepath.Join("..", "LibreTerrain", "libreterrain-mcp")} {
		if c == "" {
			continue
		}
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			abs, _ := filepath.Abs(c)
			return abs
		}
	}
	if p, err := exec.LookPath("libreterrain-mcp"); err == nil {
		return p
	}
	return ""
}

// Client speaks MCP (newline-delimited JSON-RPC over stdio) to `libreterrain-mcp --project f`,
// which serves a project file headlessly. LibreTerrain stays an external, optional tool.
type Client struct {
	cmd    *exec.Cmd
	in     io.WriteCloser
	out    *bufio.Reader
	stderr bytes.Buffer
	id     int
}

// Open starts the bridge on a project file (JSON or native .lterrain) and performs the MCP handshake.
func Open(bridge, project string) (*Client, error) {
	c := &Client{cmd: exec.Command(bridge, "--project", project)}
	c.cmd.Stderr = &c.stderr
	in, err := c.cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := c.cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	c.in, c.out = in, bufio.NewReaderSize(out, 1<<20)
	if err := c.cmd.Start(); err != nil {
		return nil, err
	}
	if _, err := c.rpc("initialize", map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "ocean", "version": "0.1"}}); err != nil {
		c.Close()
		return nil, err
	}
	if _, err := fmt.Fprintln(c.in, `{"jsonrpc":"2.0","method":"notifications/initialized"}`); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

func (c *Client) rpc(method string, params any) (json.RawMessage, error) {
	c.id++
	req, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": c.id, "method": method, "params": params})
	if _, err := fmt.Fprintf(c.in, "%s\n", req); err != nil {
		return nil, fmt.Errorf("%w: %s", err, c.stderr.String())
	}
	for {
		line, err := c.out.ReadBytes('\n')
		if err != nil {
			return nil, fmt.Errorf("libreterrain-mcp closed: %v: %s", err, c.stderr.String())
		}
		var r struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(line, &r) != nil || r.ID != c.id {
			continue // notification or unrelated message
		}
		if r.Error != nil {
			return nil, fmt.Errorf("%s: %s", method, r.Error.Message)
		}
		return r.Result, nil
	}
}

// Tool calls an MCP tool and returns its structured result. A tool-level error is returned as an error.
func (c *Client) Tool(name string, args map[string]any) (map[string]any, error) {
	if args == nil {
		args = map[string]any{}
	}
	raw, err := c.rpc("tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return nil, err
	}
	var res struct {
		IsError    bool           `json:"isError"`
		Structured map[string]any `json:"structuredContent"`
		Content    []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, err
	}
	if res.IsError {
		msg := ""
		if len(res.Content) > 0 {
			msg = res.Content[0].Text
		}
		return nil, fmt.Errorf("%s: %s", name, msg)
	}
	if res.Structured == nil && len(res.Content) > 0 {
		_ = json.Unmarshal([]byte(res.Content[0].Text), &res.Structured)
	}
	return res.Structured, nil
}

// Revision returns the project's current graph revision, which every edit must quote.
func (c *Client) Revision() (string, error) {
	g, err := c.Tool("get_graph", nil)
	if err != nil {
		return "", err
	}
	s, _ := g["revision"].(string)
	if s == "" {
		return "", fmt.Errorf("get_graph returned no revision")
	}
	return s, nil
}

// Edit applies graph operations and returns the ids of added nodes keyed by ref.
func (c *Client) Edit(ops ...map[string]any) (map[string]string, error) {
	rev, err := c.Revision()
	if err != nil {
		return nil, err
	}
	r, err := c.Tool("edit_graph", map[string]any{"revision": rev, "operations": ops})
	if err != nil {
		return nil, err
	}
	refs := map[string]string{}
	if m, ok := r["refs"].(map[string]any); ok {
		for k, v := range m {
			refs[k], _ = v.(string)
		}
	}
	return refs, nil
}

// MarkExport marks a node's output for export in the given formats.
func (c *Client) MarkExport(node string, formats ...string) error {
	rev, err := c.Revision()
	if err != nil {
		return err
	}
	_, err = c.Tool("mark_export", map[string]any{"revision": rev, "node": node, "formats": formats})
	return err
}

// Build runs start_build and waits for it. LibreTerrain reports a refused or failed
// build as an idle state with no files, which is treated as an error here.
func (c *Client) Build(resolution int, timeout time.Duration) ([]string, error) {
	if _, err := c.Tool("start_build", map[string]any{"resolution": resolution}); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		s, err := c.Tool("get_build_status", nil)
		if err != nil {
			return nil, err
		}
		if s["state"] == "running" {
			time.Sleep(500 * time.Millisecond)
			continue
		}
		if e, ok := s["error"]; ok && e != nil {
			return nil, fmt.Errorf("libreterrain build failed: %v", e)
		}
		var files []string
		if l, ok := s["files"].([]any); ok {
			for _, f := range l {
				files = append(files, fmt.Sprint(f))
			}
		}
		if len(files) == 0 {
			return nil, fmt.Errorf("libreterrain build finished without writing files: %v", s)
		}
		return files, nil
	}
	return nil, fmt.Errorf("libreterrain build did not finish within %s", timeout)
}

// Save writes the project in LibreTerrain's native format.
func (c *Client) Save(path string) error {
	_, err := c.Tool("save_project", map[string]any{"path": path})
	return err
}

// Close stops the bridge.
func (c *Client) Close() {
	c.in.Close()
	done := make(chan struct{})
	go func() { c.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		c.cmd.Process.Kill()
	}
}
