// Package browser drives Chromium through the Playwright sidecar (sidecar.mjs),
// which runs as a child Node process and speaks one JSON object per line.
package browser

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"testdesk/internal/proc"
)

const (
	callTimeout  = 60 * time.Second
	closeTimeout = 5 * time.Second
	stderrLines  = 20
)

// Locator describes how a recorded action found its element. It is also what the
// code generator turns into Python / JavaScript.
type Locator struct {
	By       string `json:"by"` // role | text | css
	Role     string `json:"role,omitempty"`
	Name     string `json:"name,omitempty"`
	Nth      *int   `json:"nth,omitempty"`
	Text     string `json:"text,omitempty"`
	Selector string `json:"selector,omitempty"`
}

type Element struct {
	Ref      string `json:"ref"`
	Role     string `json:"role"`
	Name     string `json:"name"`
	Disabled bool   `json:"disabled,omitempty"`
}

type Snapshot struct {
	URL      string    `json:"url"`
	Title    string    `json:"title"`
	Elements []Element `json:"elements"`
	Text     string    `json:"text"`
}

// rpcResponse is either a reply to a call (ID set) or an event pushed by the sidecar (Event set).
type rpcResponse struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  string          `json:"error"`
	Event  string          `json:"event"`
	Params json.RawMessage `json:"params"`
}

// Client owns one sidecar process. Calls are serialized.
type Client struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser

	responses chan rpcResponse
	exited    chan struct{}
	exitErr   error

	mu     sync.Mutex // serializes calls
	nextID int

	evMu    sync.Mutex
	onEvent func(name string, params json.RawMessage)

	stderrMu sync.Mutex
	stderr   []string
}

// Start launches `node <runtimeDir>/sidecar.mjs`.
func Start(nodePath, runtimeDir string) (*Client, error) {
	cmd := exec.Command(nodePath, filepath.Join(runtimeDir, "sidecar.mjs"))
	cmd.Dir = runtimeDir
	proc.HideConsole(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动浏览器侧车失败：%w", err)
	}

	c := &Client{
		cmd:       cmd,
		stdin:     stdin,
		responses: make(chan rpcResponse, 4),
		exited:    make(chan struct{}),
	}
	go c.readResponses(stdout)
	go c.readStderr(stderr)
	go func() {
		c.exitErr = cmd.Wait()
		close(c.exited)
	}()
	return c, nil
}

func (c *Client) readResponses(r io.Reader) {
	defer close(c.responses)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024) // snapshots can be large
	for sc.Scan() {
		var resp rpcResponse
		if err := json.Unmarshal(sc.Bytes(), &resp); err != nil {
			continue // not a protocol line
		}
		if resp.Event != "" {
			c.dispatchEvent(resp.Event, resp.Params)
			continue
		}
		c.responses <- resp
	}
}

// OnEvent sets the handler for events the sidecar pushes, such as user actions during manual
// recording. The handler runs on the reader goroutine, so it must not call the sidecar.
func (c *Client) OnEvent(fn func(name string, params json.RawMessage)) {
	c.evMu.Lock()
	defer c.evMu.Unlock()
	c.onEvent = fn
}

func (c *Client) dispatchEvent(name string, params json.RawMessage) {
	c.evMu.Lock()
	fn := c.onEvent
	c.evMu.Unlock()
	if fn != nil {
		fn(name, params)
	}
}

func (c *Client) readStderr(r io.Reader) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		c.stderrMu.Lock()
		c.stderr = append(c.stderr, sc.Text())
		if len(c.stderr) > stderrLines {
			c.stderr = c.stderr[len(c.stderr)-stderrLines:]
		}
		c.stderrMu.Unlock()
	}
}

// Call sends one request and decodes the result into out (out may be nil).
func (c *Client) Call(ctx context.Context, method string, params any, out any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	c.nextID++
	id := c.nextID
	data, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		return err
	}
	if _, err := c.stdin.Write(append(data, '\n')); err != nil {
		return c.processError(err)
	}

	select {
	case resp, ok := <-c.responses:
		if !ok {
			return c.processError(errors.New("侧车已关闭"))
		}
		if resp.ID != id {
			return fmt.Errorf("侧车响应错乱（期望 %d，收到 %d）", id, resp.ID)
		}
		if resp.Error != "" {
			return errors.New(resp.Error)
		}
		if out != nil && len(resp.Result) > 0 {
			return json.Unmarshal(resp.Result, out)
		}
		return nil
	case <-c.exited:
		return c.processError(errors.New("侧车进程已退出"))
	case <-ctx.Done():
		// The response for this call would arrive later and confuse the next one, so stop the process.
		c.kill()
		return fmt.Errorf("调用 %s 超时或被取消：%w", method, ctx.Err())
	}
}

// Close asks the sidecar to close the browser, then makes sure the process is gone.
func (c *Client) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
	defer cancel()
	_ = c.Call(ctx, "close", nil, nil)

	c.stdin.Close()
	select {
	case <-c.exited:
	case <-time.After(closeTimeout):
		c.kill()
		<-c.exited
	}
	return nil
}

func (c *Client) kill() {
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
}

// processError adds the last stderr lines, which usually explain why the sidecar died.
func (c *Client) processError(err error) error {
	c.stderrMu.Lock()
	tail := strings.Join(c.stderr, "\n")
	c.stderrMu.Unlock()
	if tail != "" {
		return fmt.Errorf("%w\n%s", err, tail)
	}
	return err
}

// Launch starts Chromium. Recording shows the window, so headless is false by default.
func (c *Client) Launch(ctx context.Context, headless bool) error {
	return c.Call(ctx, "launch", map[string]any{"headless": headless}, nil)
}

func (c *Client) Navigate(ctx context.Context, url string) (Snapshot, error) {
	var res struct {
		URL   string `json:"url"`
		Title string `json:"title"`
	}
	if err := c.Call(ctx, "navigate", map[string]any{"url": url}, &res); err != nil {
		return Snapshot{}, err
	}
	return Snapshot{URL: res.URL, Title: res.Title}, nil
}

func (c *Client) Snapshot(ctx context.Context) (Snapshot, error) {
	var snap Snapshot
	err := c.Call(ctx, "snapshot", nil, &snap)
	return snap, err
}

// StartRecording makes the sidecar report user actions on the current page as events.
func (c *Client) StartRecording(ctx context.Context) error {
	return c.Call(ctx, "record_start", nil, nil)
}

// Act runs one action method (click, fill, press, select_option, wait_for,
// assert_visible, assert_text) and returns the locator it used.
func (c *Client) Act(ctx context.Context, method string, params map[string]any) (*Locator, error) {
	var res struct {
		Locator *Locator `json:"locator"`
	}
	if err := c.Call(ctx, method, params, &res); err != nil {
		return nil, err
	}
	return res.Locator, nil
}
