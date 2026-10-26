package recorder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/shared"

	"testdesk/internal/browser"
	"testdesk/internal/codegen"
	"testdesk/internal/runtimeenv"
	"testdesk/internal/settings"
)

const (
	maxSteps         = 60
	maxNoToolTurns   = 3
	maxSnapshotChars = 12000
)

const systemPrompt = `你是浏览器自动化测试录制员。使用提供的工具，在真实浏览器中完成用户给出的测试目标，并让每一步都可以被脚本重复执行。

规则：
1. 先 navigate 到起始网址，然后调用 snapshot 获取页面元素。
2. 点击和输入只能使用 snapshot 返回的 ref。页面发生变化后必须重新 snapshot。
3. 每个操作都带上简短的 note，说明这一步的意图，它会作为脚本注释。
4. 测试目标里的验证点必须用 assert_visible 或 assert_text 断言，不能只做操作。
5. 不要执行与测试目标无关的操作。
6. 目标完成后调用 finish，并在 summary 中简述结果。
7. 操作失败时阅读错误信息，换一种方式重试；同一操作失败两次后尝试其他路径。`

// LLMAgent records a test by letting an OpenAI-compatible model drive the browser.
type LLMAgent struct {
	loadSettings func() (settings.Settings, error)
	runtime      *runtimeenv.Manager
}

func NewLLMAgent(loadSettings func() (settings.Settings, error), runtime *runtimeenv.Manager) *LLMAgent {
	return &LLMAgent{loadSettings: loadSettings, runtime: runtime}
}

// toolArgs is the union of all tool arguments.
type toolArgs struct {
	URL     string `json:"url"`
	Ref     string `json:"ref"`
	Value   string `json:"value"`
	Key     string `json:"key"`
	Text    string `json:"text"`
	Note    string `json:"note"`
	Summary string `json:"summary"`
}

func (a *LLMAgent) Record(ctx context.Context, req Request, emit func(Event)) (string, error) {
	cfg, err := a.loadSettings()
	if err != nil {
		return "", err
	}
	if cfg.OpenAIAPIKey == "" {
		return "", errors.New("请先在设置中填写 API Key")
	}
	if cfg.OpenAIModel == "" {
		return "", errors.New("请先在设置中填写模型名称")
	}
	if rs := a.runtime.Status(ctx, cfg); !rs.Ready {
		return "", errors.New("运行环境未就绪，请先在设置中安装运行环境")
	}
	if err := a.runtime.WriteSidecar(); err != nil {
		return "", err
	}

	llm := openai.NewClient(
		option.WithAPIKey(cfg.OpenAIAPIKey),
		option.WithBaseURL(cfg.OpenAIBaseURL),
	)

	br, err := browser.Start(cfg.NodePath, a.runtime.Dir())
	if err != nil {
		return "", err
	}
	defer br.Close()

	if err := br.Launch(ctx, false); err != nil {
		return "", fmt.Errorf("启动浏览器失败：%w", err)
	}

	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(systemPrompt),
		openai.UserMessage(fmt.Sprintf("起始网址：%s\n测试目标：%s", req.URL, req.Goal)),
	}
	tools := toolDefinitions()

	var actions []codegen.Action
	noToolTurns := 0

	for step := 0; step < maxSteps; step++ {
		if ctx.Err() != nil {
			return "", errors.New("录制已停止")
		}

		resp, err := llm.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
			Model:    cfg.OpenAIModel,
			Messages: messages,
			Tools:    tools,
		})
		if err != nil {
			return "", fmt.Errorf("调用模型失败：%w", err)
		}
		if len(resp.Choices) == 0 {
			return "", errors.New("模型没有返回内容")
		}

		msg := resp.Choices[0].Message
		if strings.TrimSpace(msg.Content) != "" {
			emit(Event{Kind: EventMessage, Text: msg.Content})
		}
		messages = append(messages, msg.ToParam())

		if len(msg.ToolCalls) == 0 {
			noToolTurns++
			if noToolTurns >= maxNoToolTurns {
				return "", errors.New("模型连续没有调用工具，录制已中止")
			}
			messages = append(messages, openai.UserMessage("请使用提供的工具继续操作；完成全部步骤后调用 finish。"))
			continue
		}
		noToolTurns = 0

		for _, call := range msg.ToolCalls {
			result, finished := a.runTool(ctx, br, call, &actions, emit)
			messages = append(messages, openai.ToolMessage(result, call.ID))
			if finished {
				return codegen.Generate(req.Language, codegen.Script{
					Name:    req.Goal,
					Goal:    req.Goal,
					URL:     req.URL,
					Actions: actions,
				})
			}
		}
	}
	return "", fmt.Errorf("达到最大步数 %d，仍未完成录制", maxSteps)
}

// runTool executes one tool call. The returned string is sent back to the model.
// Failed browser actions are not logged; their error text goes back to the model so it can retry.
func (a *LLMAgent) runTool(ctx context.Context, br *browser.Client, call openai.ChatCompletionMessageToolCall,
	actions *[]codegen.Action, emit func(Event)) (string, bool) {

	name := call.Function.Name
	var args toolArgs
	if call.Function.Arguments != "" {
		if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
			return "ERROR: 参数不是合法的 JSON：" + err.Error(), false
		}
	}

	switch name {
	case "navigate":
		if args.URL == "" {
			return "ERROR: 缺少 url", false
		}
		snap, err := br.Navigate(ctx, args.URL)
		if err != nil {
			return "ERROR: " + err.Error(), false
		}
		*actions = append(*actions, codegen.Action{Tool: name, URL: args.URL, Note: args.Note})
		emit(Event{Kind: EventAction, Text: "navigate " + args.URL})
		return fmt.Sprintf("已打开 %s（标题：%s）。请调用 snapshot 查看页面。", snap.URL, snap.Title), false

	case "snapshot":
		snap, err := br.Snapshot(ctx)
		if err != nil {
			return "ERROR: " + err.Error(), false
		}
		return formatSnapshot(snap), false

	case "click", "fill", "press", "select_option", "wait_for", "assert_visible", "assert_text":
		if msg := validateAction(name, args); msg != "" {
			return "ERROR: " + msg, false
		}
		params := map[string]any{}
		if args.Ref != "" {
			params["ref"] = args.Ref
		}
		if args.Text != "" {
			params["text"] = args.Text
		}
		if name == "fill" || name == "select_option" {
			params["value"] = args.Value
		}
		if name == "press" {
			params["key"] = args.Key
		}

		loc, err := br.Act(ctx, name, params)
		if err != nil {
			return "ERROR: " + err.Error() + "。请重新 snapshot 后再试。", false
		}
		*actions = append(*actions, codegen.Action{
			Tool:    name,
			Value:   args.Value,
			Key:     args.Key,
			Text:    args.Text,
			Note:    args.Note,
			Locator: loc,
		})
		emit(Event{Kind: EventAction, Text: fmt.Sprintf("%s %s", name, describeLocator(loc))})
		return "OK", false

	case "finish":
		if len(*actions) == 0 {
			return "ERROR: 还没有任何操作，不能结束。", false
		}
		emit(Event{Kind: EventMessage, Text: "完成：" + args.Summary})
		return "录制完成。", true

	default:
		return "ERROR: 未知工具 " + name, false
	}
}

func validateAction(name string, args toolArgs) string {
	switch name {
	case "assert_text":
		if args.Text == "" {
			return "assert_text 需要 text"
		}
		return ""
	case "fill", "select_option":
		if args.Ref == "" {
			return name + " 需要 ref"
		}
		if args.Value == "" && name == "select_option" {
			return "select_option 需要 value"
		}
		return ""
	case "press":
		if args.Ref == "" {
			return "press 需要 ref"
		}
		if args.Key == "" {
			return "press 需要 key"
		}
		return ""
	default:
		if args.Ref == "" && args.Text == "" {
			return name + " 需要 ref 或 text"
		}
		return ""
	}
}

func formatSnapshot(s browser.Snapshot) string {
	var b strings.Builder
	fmt.Fprintf(&b, "URL: %s\nTitle: %s\n\n可交互元素（用 ref 引用）：\n", s.URL, s.Title)
	if len(s.Elements) == 0 {
		b.WriteString("（没有找到可交互元素）\n")
	}
	for _, e := range s.Elements {
		name := e.Name
		if name == "" {
			name = "(无名称)"
		}
		disabled := ""
		if e.Disabled {
			disabled = " [disabled]"
		}
		fmt.Fprintf(&b, "- ref=%s role=%s name=%q%s\n", e.Ref, e.Role, name, disabled)
	}
	b.WriteString("\n页面文本：\n")
	b.WriteString(s.Text)

	out := b.String()
	if len(out) > maxSnapshotChars {
		out = out[:maxSnapshotChars] + "\n...（已截断）"
	}
	return out
}

func describeLocator(l *browser.Locator) string {
	if l == nil {
		return ""
	}
	switch l.By {
	case "role":
		return fmt.Sprintf("%s %q", l.Role, l.Name)
	case "text":
		return fmt.Sprintf("text %q", l.Text)
	default:
		return l.Selector
	}
}

func toolDefinitions() []openai.ChatCompletionToolParam {
	note := prop("string", "这一步的简短说明，会写入脚本注释")
	ref := prop("string", "snapshot 返回的元素 ref，例如 e3")

	return []openai.ChatCompletionToolParam{
		tool("navigate", "打开一个网址。", schema([]string{"url"}, map[string]any{
			"url":  prop("string", "完整网址，包含 http:// 或 https://"),
			"note": note,
		})),
		tool("snapshot", "获取当前页面的可交互元素列表（带 ref）和页面文本。点击或输入前、页面变化后都应调用。", schema(nil, map[string]any{})),
		tool("click", "点击一个元素。", schema([]string{"ref"}, map[string]any{
			"ref":  ref,
			"note": note,
		})),
		tool("fill", "在输入框中填入文本。", schema([]string{"ref", "value"}, map[string]any{
			"ref":   ref,
			"value": prop("string", "要填入的文本"),
			"note":  note,
		})),
		tool("press", "对元素按一个键，例如 Enter。", schema([]string{"ref", "key"}, map[string]any{
			"ref":  ref,
			"key":  prop("string", "键名，例如 Enter、Tab、Escape"),
			"note": note,
		})),
		tool("select_option", "在下拉框中选择选项（按选项文本或值）。", schema([]string{"ref", "value"}, map[string]any{
			"ref":   ref,
			"value": prop("string", "选项的文本或值"),
			"note":  note,
		})),
		tool("wait_for", "等待元素可见。", schema([]string{"ref"}, map[string]any{
			"ref":  ref,
			"note": note,
		})),
		tool("assert_visible", "断言元素可见。这是测试的验证点。", schema([]string{"ref"}, map[string]any{
			"ref":  ref,
			"note": note,
		})),
		tool("assert_text", "断言元素（不填 ref 时为整个页面）包含指定文本。这是测试的验证点。", schema([]string{"text"}, map[string]any{
			"ref":  prop("string", "可选，snapshot 返回的元素 ref"),
			"text": prop("string", "期望出现的文本"),
			"note": note,
		})),
		tool("finish", "测试目标已完成，结束录制。", schema([]string{"summary"}, map[string]any{
			"summary": prop("string", "简述完成了哪些操作、验证了什么"),
		})),
	}
}

func tool(name, description string, params map[string]any) openai.ChatCompletionToolParam {
	return openai.ChatCompletionToolParam{
		Function: shared.FunctionDefinitionParam{
			Name:        name,
			Description: openai.String(description),
			Parameters:  shared.FunctionParameters(params),
		},
	}
}

func schema(required []string, properties map[string]any) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{
		"type":       "object",
		"properties": properties,
		"required":   required,
	}
}

func prop(typ, description string) map[string]any {
	return map[string]any{"type": typ, "description": description}
}
