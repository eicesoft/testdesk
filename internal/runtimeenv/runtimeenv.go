// Package runtimeenv installs and checks the Node/Python environment used for
// browser automation: Playwright, Chromium, and the sidecar script.
package runtimeenv

import (
	"bufio"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"testdesk/internal/proc"
	"testdesk/internal/settings"
)

//go:embed sidecar/server.mjs
var sidecarSource []byte

const (
	SidecarFile   = "sidecar.mjs"
	MinNodeMajor  = 18
	stepNode      = "node"
	stepPackages  = "packages"
	stepSidecar   = "sidecar"
	stepChromium  = "chromium"
	stepPython    = "python"
	statusRunning = "running"
	statusDone    = "done"
	statusFailed  = "failed"
	statusSkipped = "skipped"
	statusLog     = "log"
)

// Status describes what is installed. Ready means recording can run.
type Status struct {
	NodeVersion   string `json:"nodeVersion"`
	NodeOK        bool   `json:"nodeOk"`
	PlaywrightOK  bool   `json:"playwrightOk"`
	ChromiumOK    bool   `json:"chromiumOk"`
	PythonVersion string `json:"pythonVersion"`
	PythonOK      bool   `json:"pythonOk"`
	Ready         bool   `json:"ready"`
	RuntimeDir    string `json:"runtimeDir"`
}

// Progress is one step update during Install.
type Progress struct {
	Step    string `json:"step"`
	Status  string `json:"status"` // running | done | failed | skipped | log
	Message string `json:"message"`
}

// Manager owns the runtime directory.
type Manager struct {
	dir string

	mu         sync.Mutex
	installing bool
}

func New(dir string) *Manager {
	return &Manager{dir: dir}
}

func (m *Manager) Dir() string {
	return m.dir
}

// VenvPython returns the Python interpreter inside the runtime venv, or "" if it does not exist.
func VenvPython(runtimeDir string) string {
	if runtimeDir == "" {
		return ""
	}
	var p string
	if runtime.GOOS == "windows" {
		p = filepath.Join(runtimeDir, "py", "Scripts", "python.exe")
	} else {
		p = filepath.Join(runtimeDir, "py", "bin", "python")
	}
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

var versionPattern = regexp.MustCompile(`(\d+)\.(\d+)(\.\d+)?`)

// Status checks the current installation without changing anything.
func (m *Manager) Status(ctx context.Context, cfg settings.Settings) Status {
	st := Status{RuntimeDir: m.dir}

	if out, err := runOutput(ctx, cfg.NodePath, "--version"); err == nil {
		st.NodeVersion = strings.TrimSpace(out)
		if mm := versionPattern.FindStringSubmatch(st.NodeVersion); mm != nil {
			major, _ := strconv.Atoi(mm[1])
			st.NodeOK = major >= MinNodeMajor
		}
	}

	if _, err := os.Stat(filepath.Join(m.dir, "node_modules", "playwright", "package.json")); err == nil {
		st.PlaywrightOK = true
	}

	if st.NodeOK && st.PlaywrightOK {
		script := `import('playwright').then(m => console.log(m.chromium.executablePath()))`
		if out, err := runOutputIn(ctx, m.dir, cfg.NodePath, "--input-type=module", "-e", script); err == nil {
			if _, err := os.Stat(strings.TrimSpace(out)); err == nil {
				st.ChromiumOK = true
			}
		}
	}

	if py := VenvPython(m.dir); py != "" {
		if _, err := runOutputIn(ctx, m.dir, py, "-c", "import playwright"); err == nil {
			st.PythonOK = true
		}
	}
	if out, err := runOutput(ctx, cfg.PythonPath, "--version"); err == nil {
		st.PythonVersion = strings.TrimSpace(out)
	}

	st.Ready = st.NodeOK && st.PlaywrightOK && st.ChromiumOK
	return st
}

// WriteSidecar copies the embedded sidecar into the runtime directory.
// It runs on every install and before every recording so the app and sidecar stay in sync.
func (m *Manager) WriteSidecar() error {
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(m.dir, SidecarFile), sidecarSource, 0o644)
}

// Install runs the setup steps. It reports progress through progress and returns
// the first fatal error. Python is optional: when it is missing that step is skipped.
func (m *Manager) Install(ctx context.Context, cfg settings.Settings, progress func(Progress)) error {
	m.mu.Lock()
	if m.installing {
		m.mu.Unlock()
		return errors.New("运行环境正在安装中")
	}
	m.installing = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.installing = false
		m.mu.Unlock()
	}()

	emit := func(step, status, msg string) {
		progress(Progress{Step: step, Status: status, Message: msg})
	}
	// Fail records the error for the step and returns it for the caller.
	fail := func(step string, err error) error {
		emit(step, statusFailed, err.Error())
		return err
	}

	// 1. Node
	emit(stepNode, statusRunning, "检查 Node.js")
	out, err := runOutput(ctx, cfg.NodePath, "--version")
	if err != nil {
		return fail(stepNode, fmt.Errorf("未找到 Node.js（%s）。请先安装 Node.js 18 或更高版本，或在设置中指定 node 路径", cfg.NodePath))
	}
	mm := versionPattern.FindStringSubmatch(out)
	if mm == nil {
		return fail(stepNode, fmt.Errorf("无法识别 Node.js 版本：%s", strings.TrimSpace(out)))
	}
	if major, _ := strconv.Atoi(mm[1]); major < MinNodeMajor {
		return fail(stepNode, fmt.Errorf("Node.js 版本过低（%s），需要 %d 或更高", strings.TrimSpace(out), MinNodeMajor))
	}
	emit(stepNode, statusDone, "Node.js "+strings.TrimSpace(out))

	// 2. npm packages
	emit(stepPackages, statusRunning, "安装 playwright 与 @playwright/test")
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return fail(stepPackages, err)
	}
	pkg := []byte("{\n  \"name\": \"testdesk-runtime\",\n  \"private\": true,\n  \"type\": \"module\"\n}\n")
	if err := os.WriteFile(filepath.Join(m.dir, "package.json"), pkg, 0o644); err != nil {
		return fail(stepPackages, err)
	}
	if err := runStreaming(ctx, m.dir, progress, stepPackages, npmCommand(cfg.NodePath),
		"install", "--no-audit", "--no-fund", "playwright", "@playwright/test"); err != nil {
		return fail(stepPackages, fmt.Errorf("npm install 失败：%w", err))
	}
	emit(stepPackages, statusDone, "npm 包已安装")

	// 3. Sidecar script
	emit(stepSidecar, statusRunning, "写入侧车脚本")
	if err := m.WriteSidecar(); err != nil {
		return fail(stepSidecar, err)
	}
	emit(stepSidecar, statusDone, "侧车脚本已写入")

	// 4. Chromium
	emit(stepChromium, statusRunning, "下载 Chromium（首次较慢）")
	cli := filepath.Join(m.dir, "node_modules", "playwright", "cli.js")
	if err := runStreaming(ctx, m.dir, progress, stepChromium, cfg.NodePath, cli, "install", "chromium"); err != nil {
		return fail(stepChromium, fmt.Errorf("安装 Chromium 失败：%w", err))
	}
	emit(stepChromium, statusDone, "Chromium 已安装")

	// 5. Python (optional)
	emit(stepPython, statusRunning, "检查 Python")
	if _, err := runOutput(ctx, cfg.PythonPath, "--version"); err != nil {
		emit(stepPython, statusSkipped, "未找到 Python，跳过。Python 脚本将无法运行，可在设置中指定路径后重装")
		return nil
	}
	venv := filepath.Join(m.dir, "py")
	if VenvPython(m.dir) == "" {
		if err := runStreaming(ctx, m.dir, progress, stepPython, cfg.PythonPath, "-m", "venv", venv); err != nil {
			return fail(stepPython, fmt.Errorf("创建 Python 虚拟环境失败：%w", err))
		}
	}
	py := VenvPython(m.dir)
	if py == "" {
		return fail(stepPython, errors.New("虚拟环境中找不到 Python"))
	}
	if err := runStreaming(ctx, m.dir, progress, stepPython, py, "-m", "pip", "install", "--disable-pip-version-check", "playwright"); err != nil {
		return fail(stepPython, fmt.Errorf("pip install playwright 失败：%w", err))
	}
	if err := runStreaming(ctx, m.dir, progress, stepPython, py, "-m", "playwright", "install", "chromium"); err != nil {
		return fail(stepPython, fmt.Errorf("安装 Python 版 Chromium 失败：%w", err))
	}
	emit(stepPython, statusDone, "Python 运行环境已就绪")
	return nil
}

// npmCommand prefers the npm that ships next to the configured node binary.
func npmCommand(nodePath string) string {
	if filepath.IsAbs(nodePath) {
		name := "npm"
		if runtime.GOOS == "windows" {
			name = "npm.cmd"
		}
		candidate := filepath.Join(filepath.Dir(nodePath), name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return "npm"
}

// runOutput runs a command and returns its stdout. It times out after 15 seconds.
func runOutput(ctx context.Context, name string, args ...string) (string, error) {
	return runOutputIn(ctx, "", name, args...)
}

func runOutputIn(ctx context.Context, dir, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	proc.HideConsole(cmd)
	out, err := cmd.Output()
	return string(out), err
}

// runStreaming runs a command and forwards every output line as a log progress event.
func runStreaming(ctx context.Context, dir string, progress func(Progress), step, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PLAYWRIGHT_DOWNLOAD_PROGRESS=1")
	proc.HideConsole(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout // interleave stderr into the same stream
	if err := cmd.Start(); err != nil {
		return err
	}
	pump(stdout, func(line string) {
		progress(Progress{Step: step, Status: statusLog, Message: line})
	})
	return cmd.Wait()
}

func pump(r io.Reader, fn func(string)) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			fn(line)
		}
	}
}
