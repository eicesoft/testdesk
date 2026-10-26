package main

import (
	"context"
	"os"
	"path/filepath"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"testdesk/internal/recorder"
	"testdesk/internal/runner"
	"testdesk/internal/runtimeenv"
	"testdesk/internal/scripts"
	"testdesk/internal/settings"
)

// Event names emitted to the frontend. Keep in sync with frontend/src/lib/api.ts.
const (
	EventRunOutput     = "run:output"
	EventRunDone       = "run:done"
	EventRecorderEvent = "recorder:event"
	EventSetupProgress = "setup:progress"
)

type RunOutput struct {
	RunID  string `json:"runId"`
	Stream string `json:"stream"`
	Line   string `json:"line"`
}

type RunDone struct {
	RunID    string        `json:"runId"`
	ScriptID string        `json:"scriptId"`
	Result   runner.Result `json:"result"`
}

// App is bound to the frontend. Exported methods become JS functions.
type App struct {
	ctx      context.Context
	store    *scripts.Store
	settings *settings.Store
	runtime  *runtimeenv.Manager
	recorder *recorder.Recorder

	runsMu sync.Mutex
	runs   map[string]context.CancelFunc
}

func NewApp() (*App, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	root := filepath.Join(base, "testdesk")

	store, err := scripts.NewStore(filepath.Join(root, "scripts"))
	if err != nil {
		return nil, err
	}

	a := &App{
		store:    store,
		settings: settings.NewStore(filepath.Join(root, "settings.json")),
		runtime:  runtimeenv.New(filepath.Join(root, "runtime")),
		runs:     map[string]context.CancelFunc{},
	}
	ai := recorder.NewLLMAgent(a.settings.Load, a.runtime)
	manual := recorder.NewManualAgent(a.settings.Load, a.runtime)
	a.recorder = recorder.New(ai, manual, func(ev recorder.Event) {
		a.emit(EventRecorderEvent, ev)
	})
	return a, nil
}

// startup is called by Wails once the window is ready. The context is needed for runtime events.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

func (a *App) emit(name string, data any) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, name, data)
	}
}

// ---- settings ----

func (a *App) GetSettings() (settings.Settings, error) {
	return a.settings.Load()
}

func (a *App) SaveSettings(cfg settings.Settings) (settings.Settings, error) {
	if err := a.settings.Save(cfg); err != nil {
		return settings.Settings{}, err
	}
	return a.settings.Load()
}

// ---- runtime ----

func (a *App) GetRuntimeStatus() (runtimeenv.Status, error) {
	cfg, err := a.settings.Load()
	if err != nil {
		return runtimeenv.Status{}, err
	}
	return a.runtime.Status(a.context(), cfg), nil
}

// InstallRuntime runs the setup in the background. Progress arrives on EventSetupProgress.
func (a *App) InstallRuntime() error {
	cfg, err := a.settings.Load()
	if err != nil {
		return err
	}
	go func() {
		err := a.runtime.Install(a.context(), cfg, func(p runtimeenv.Progress) {
			a.emit(EventSetupProgress, p)
		})
		if err == nil {
			a.emit(EventSetupProgress, runtimeenv.Progress{Step: "all", Status: "done", Message: "运行环境已就绪"})
		}
	}()
	return nil
}

// ---- scripts ----

func (a *App) ListScripts() ([]scripts.Script, error) {
	return a.store.List()
}

func (a *App) GetScript(id string) (scripts.Script, error) {
	return a.store.Get(id)
}

func (a *App) SaveScript(sc scripts.Script) (scripts.Script, error) {
	return a.store.Save(sc)
}

func (a *App) DeleteScript(id string) error {
	return a.store.Delete(id)
}

// ---- run ----

// RunScript starts the script in the background and returns the run id.
// Output arrives on EventRunOutput, completion on EventRunDone.
func (a *App) RunScript(id string) (string, error) {
	sc, err := a.store.Get(id)
	if err != nil {
		return "", err
	}
	cfg, err := a.settings.Load()
	if err != nil {
		return "", err
	}
	runID, err := scripts.NewID()
	if err != nil {
		return "", err
	}

	runCfg := runner.Config{
		NodePath:   cfg.NodePath,
		PythonPath: cfg.PythonPath,
		RuntimeDir: a.runtime.Dir(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	a.runsMu.Lock()
	a.runs[runID] = cancel
	a.runsMu.Unlock()

	go func() {
		res := runner.Run(ctx, runCfg, sc, func(stream, line string) {
			a.emit(EventRunOutput, RunOutput{RunID: runID, Stream: stream, Line: line})
		})

		a.runsMu.Lock()
		delete(a.runs, runID)
		a.runsMu.Unlock()
		cancel()

		a.emit(EventRunDone, RunDone{RunID: runID, ScriptID: id, Result: res})
	}()
	return runID, nil
}

func (a *App) StopRun(runID string) {
	a.runsMu.Lock()
	defer a.runsMu.Unlock()
	if cancel, ok := a.runs[runID]; ok {
		cancel()
	}
}

// ---- recording ----

func (a *App) StartRecording(req recorder.Request) error {
	return a.recorder.Start(req)
}

func (a *App) StopRecording() {
	a.recorder.Stop()
}

// context returns the Wails context, or a background context before startup.
func (a *App) context() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}
