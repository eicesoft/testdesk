package recorder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"testdesk/internal/browser"
	"testdesk/internal/codegen"
	"testdesk/internal/runtimeenv"
	"testdesk/internal/settings"
)

// ManualAgent opens a visible browser and records whatever the user does in it.
// It ends when the user stops the recording or closes the browser window.
type ManualAgent struct {
	loadSettings func() (settings.Settings, error)
	runtime      *runtimeenv.Manager
}

func NewManualAgent(loadSettings func() (settings.Settings, error), runtime *runtimeenv.Manager) *ManualAgent {
	return &ManualAgent{loadSettings: loadSettings, runtime: runtime}
}

// recordedAction is the payload the sidecar sends for each user interaction.
type recordedAction struct {
	Tool    string           `json:"tool"`
	URL     string           `json:"url"`
	Value   string           `json:"value"`
	Key     string           `json:"key"`
	Locator *browser.Locator `json:"locator"`
}

func (a *ManualAgent) Record(ctx context.Context, req Request, emit func(Event)) (string, error) {
	cfg, err := a.loadSettings()
	if err != nil {
		return "", err
	}
	if rs := a.runtime.Status(ctx, cfg); !rs.Ready {
		return "", errors.New("运行环境未就绪，请先在设置中安装运行环境")
	}
	if err := a.runtime.WriteSidecar(); err != nil {
		return "", err
	}

	br, err := browser.Start(cfg.NodePath, a.runtime.Dir())
	if err != nil {
		return "", err
	}
	defer br.Close()

	if err := br.Launch(ctx, false); err != nil {
		return "", fmt.Errorf("启动浏览器失败：%w", err)
	}

	var (
		mu      sync.Mutex
		actions []codegen.Action
		closed  = make(chan struct{})
		once    sync.Once
	)
	br.OnEvent(func(name string, params json.RawMessage) {
		switch name {
		case "action":
			var rec recordedAction
			if err := json.Unmarshal(params, &rec); err != nil {
				return
			}
			act := codegen.Action{
				Tool:    rec.Tool,
				URL:     rec.URL,
				Value:   rec.Value,
				Key:     rec.Key,
				Locator: rec.Locator,
			}
			mu.Lock()
			actions = append(actions, act)
			mu.Unlock()
			emit(Event{Kind: EventAction, Text: describeAction(act)})
		case "closed":
			once.Do(func() { close(closed) })
		}
	})

	if err := br.StartRecording(ctx); err != nil {
		return "", err
	}
	if _, err := br.Navigate(ctx, req.URL); err != nil {
		return "", fmt.Errorf("打开起始网址失败：%w", err)
	}
	emit(Event{Kind: EventStatus, Text: "请在浏览器中操作，完成后点击「停止录制」"})

	select {
	case <-ctx.Done():
	case <-closed:
	}

	mu.Lock()
	recorded := append([]codegen.Action(nil), actions...)
	mu.Unlock()

	return codegen.Generate(req.Language, codegen.Script{
		Name:    req.Goal,
		Goal:    req.Goal,
		URL:     req.URL,
		Actions: recorded,
	})
}

// describeAction is the one-line summary shown in the progress list.
func describeAction(a codegen.Action) string {
	target := ""
	if a.Locator != nil {
		target = describeLocator(a.Locator)
	}
	switch a.Tool {
	case "navigate":
		return "打开 " + a.URL
	case "click":
		return "点击 " + target
	case "fill":
		return fmt.Sprintf("输入 %q 到 %s", a.Value, target)
	case "select_option":
		return fmt.Sprintf("选择 %q 于 %s", a.Value, target)
	case "press":
		return fmt.Sprintf("按下 %s", a.Key)
	default:
		return a.Tool
	}
}
