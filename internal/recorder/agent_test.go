package recorder

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"testdesk/internal/runner"
	"testdesk/internal/runtimeenv"
	"testdesk/internal/scripts"
	"testdesk/internal/settings"
)

const loginPage = `<!doctype html>
<html><head><title>login</title></head>
<body>
  <h1>登录</h1>
  <label for="user">用户名</label>
  <input id="user" type="text">
  <button onclick="document.getElementById('msg').textContent='已登录'">登录</button>
  <p id="msg"></p>
</body></html>`

var (
	textboxRef = regexp.MustCompile(`ref=(e\d+) role=textbox name="用户名"`)
	buttonRef  = regexp.MustCompile(`ref=(e\d+) role=button name="登录"`)
)

// fakeLLM plays back a fixed script of tool calls, filling in refs from the
// last tool result the way a real model would.
func fakeLLM(t *testing.T, pageURL string) http.Handler {
	step := 0
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []map[string]any `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		// Refs come from the most recent snapshot, not the most recent tool result.
		snapshot := ""
		for _, m := range req.Messages {
			if content, _ := m["content"].(string); m["role"] == "tool" && strings.Contains(content, "可交互元素") {
				snapshot = content
			}
		}

		findRef := func(re *regexp.Regexp) string {
			if m := re.FindStringSubmatch(snapshot); m != nil {
				return m[1]
			}
			t.Errorf("ref not found in snapshot:\n%s", snapshot)
			return "missing"
		}

		var name, args string
		switch step {
		case 0:
			name, args = "navigate", fmt.Sprintf(`{"url":%q,"note":"打开登录页"}`, pageURL)
		case 1:
			name, args = "snapshot", `{}`
		case 2:
			name, args = "fill", fmt.Sprintf(`{"ref":%q,"value":"alice","note":"输入用户名"}`, findRef(textboxRef))
		case 3:
			name, args = "click", fmt.Sprintf(`{"ref":%q,"note":"点击登录"}`, findRef(buttonRef))
		case 4:
			name, args = "assert_text", `{"text":"已登录","note":"检查登录成功提示"}`
		default:
			name, args = "finish", `{"summary":"登录成功"}`
		}

		resp := map[string]any{
			"id":      fmt.Sprintf("chatcmpl-%d", step),
			"object":  "chat.completion",
			"created": 0,
			"model":   "fake",
			"choices": []any{map[string]any{
				"index":         0,
				"finish_reason": "tool_calls",
				"message": map[string]any{
					"role":    "assistant",
					"content": nil,
					"tool_calls": []any{map[string]any{
						"id":       fmt.Sprintf("call_%d", step),
						"type":     "function",
						"function": map[string]any{"name": name, "arguments": args},
					}},
				},
			}},
		}
		step++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
}

// TestRecordAndReplayIntegration runs the whole pipeline for each language:
// fake LLM -> sidecar -> Chromium -> generated script -> runner. It needs an
// installed runtime, so set TESTDESK_RUNTIME_DIR. Python also needs the venv
// created by the runtime install (or TESTDESK_PYTHON).
func TestRecordAndReplayIntegration(t *testing.T) {
	runtimeDir := os.Getenv("TESTDESK_RUNTIME_DIR")
	if runtimeDir == "" {
		t.Skip("set TESTDESK_RUNTIME_DIR to an installed runtime to run this test")
	}

	cases := []struct {
		lang  scripts.Language
		wants []string
	}{
		{scripts.JavaScript, []string{
			"await page.goto(",
			`await page.getByRole("textbox", { name: "用户名", exact: true }).fill("alice")`,
			`await page.getByRole("button", { name: "登录", exact: true }).click()`,
			`await expect(page.locator("body")).toContainText("已登录")`,
		}},
		{scripts.Python, []string{
			"page.goto(",
			`page.get_by_role("textbox", name="用户名", exact=True).fill("alice")`,
			`page.get_by_role("button", name="登录", exact=True).click()`,
			`expect(page.locator("body")).to_contain_text("已登录")`,
		}},
	}

	for _, tc := range cases {
		t.Run(string(tc.lang), func(t *testing.T) {
			recordAndReplay(t, runtimeDir, tc.lang, tc.wants)
		})
	}
}

func recordAndReplay(t *testing.T, runtimeDir string, lang scripts.Language, wants []string) {
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(loginPage))
	}))
	defer page.Close()

	llm := httptest.NewServer(fakeLLM(t, page.URL))
	defer llm.Close()

	cfg := settings.Defaults()
	cfg.OpenAIBaseURL = llm.URL + "/v1"
	cfg.OpenAIAPIKey = "test-key"
	cfg.OpenAIModel = "fake-model"
	if py := os.Getenv("TESTDESK_PYTHON"); py != "" {
		cfg.PythonPath = py
	}

	agent := NewLLMAgent(func() (settings.Settings, error) { return cfg, nil }, runtimeenv.New(runtimeDir))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	code, err := agent.Record(ctx, Request{
		URL:      page.URL,
		Language: lang,
		Goal:     "用户可以登录",
	}, func(Event) {})
	if err != nil {
		t.Fatalf("record: %v", err)
	}

	for _, want := range wants {
		if !strings.Contains(code, want) {
			t.Errorf("generated script missing %q\n---\n%s", want, code)
		}
	}

	// The generated script must run on its own, without the agent.
	res := runner.Run(ctx, runner.Config{
		NodePath:   cfg.NodePath,
		PythonPath: cfg.PythonPath,
		RuntimeDir: runtimeDir,
	}, scripts.Script{Language: lang, Code: code},
		func(stream, line string) { t.Logf("[%s] %s", stream, line) })
	if res.ExitCode != 0 {
		t.Fatalf("replay failed: exit=%d error=%q\n---\n%s", res.ExitCode, res.Error, code)
	}
}
