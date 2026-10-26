package browser

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

const testPage = `<!doctype html>
<html><head><title>login</title></head>
<body>
  <h1>欢迎来到登录页</h1>
  <label for="user">用户名</label>
  <input id="user" type="text">
  <button id="go" onclick="document.getElementById('msg').textContent='已登录'">登录</button>
  <p id="msg"></p>
</body></html>`

// TestSidecarIntegration drives real Chromium through the sidecar. It needs an
// installed runtime, so set TESTDESK_RUNTIME_DIR (and optionally TESTDESK_NODE).
func TestSidecarIntegration(t *testing.T) {
	runtimeDir := os.Getenv("TESTDESK_RUNTIME_DIR")
	if runtimeDir == "" {
		t.Skip("set TESTDESK_RUNTIME_DIR to an installed runtime to run this test")
	}
	nodePath := os.Getenv("TESTDESK_NODE")
	if nodePath == "" {
		nodePath = "node"
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(testPage))
	}))
	defer srv.Close()

	c, err := Start(nodePath, runtimeDir)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if err := c.Launch(ctx, true); err != nil {
		t.Fatalf("launch: %v", err)
	}
	if _, err := c.Navigate(ctx, srv.URL); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	snap, err := c.Snapshot(ctx)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	ref := findRef(snap, "button", "登录")
	if ref == "" {
		t.Fatalf("button 登录 not found in snapshot: %+v", snap.Elements)
	}

	loc, err := c.Act(ctx, "click", map[string]any{"ref": ref})
	if err != nil {
		t.Fatalf("click: %v", err)
	}
	if loc == nil || loc.By != "role" || loc.Role != "button" || loc.Name != "登录" {
		t.Errorf("click locator = %+v, want role button 登录", loc)
	}

	if _, err := c.Act(ctx, "assert_text", map[string]any{"text": "已登录"}); err != nil {
		t.Errorf("assert_text: %v", err)
	}

	// The fill target is the input labelled 用户名.
	ref = findRef(snap, "textbox", "用户名")
	if ref == "" {
		t.Fatalf("textbox 用户名 not found in snapshot: %+v", snap.Elements)
	}
	if _, err := c.Act(ctx, "fill", map[string]any{"ref": ref, "value": "alice"}); err != nil {
		t.Errorf("fill: %v", err)
	}

	// A bad ref must come back as an error, not a crash.
	if _, err := c.Act(ctx, "click", map[string]any{"ref": "e9999"}); err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Errorf("expected missing-ref error, got %v", err)
	}
}

func findRef(snap Snapshot, role, name string) string {
	for _, e := range snap.Elements {
		if e.Role == role && e.Name == name {
			return e.Ref
		}
	}
	return ""
}
