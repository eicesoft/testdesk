// Package runner executes test scripts in a child process.
package runner

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"testdesk/internal/proc"
	"testdesk/internal/runtimeenv"
	"testdesk/internal/scripts"
)

// Config tells the runner where the interpreters and the runtime live.
type Config struct {
	NodePath   string
	PythonPath string
	RuntimeDir string
}

type Result struct {
	ExitCode   int    `json:"exitCode"`
	DurationMs int64  `json:"durationMs"`
	Error      string `json:"error,omitempty"`
}

// OutputFunc receives every line the script prints. stream is "stdout" or "stderr".
type OutputFunc func(stream, line string)

// Run writes sc to a file, executes it and blocks until it exits or ctx is cancelled.
//
// JavaScript scripts live inside the runtime directory so that `import 'playwright'`
// resolves against its node_modules. Python scripts run from a temp directory with
// the runtime venv interpreter when one exists.
func Run(ctx context.Context, cfg Config, sc scripts.Script, onOutput OutputFunc) Result {
	cmd, cleanup, err := buildCommand(ctx, cfg, sc)
	if err != nil {
		return Result{ExitCode: -1, Error: err.Error()}
	}
	defer cleanup()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{ExitCode: -1, Error: err.Error()}
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return Result{ExitCode: -1, Error: err.Error()}
	}

	start := time.Now()
	if err := cmd.Start(); err != nil {
		return Result{ExitCode: -1, Error: err.Error()}
	}

	// All pipe reads must finish before cmd.Wait closes them.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); pump(stdout, "stdout", onOutput) }()
	go func() { defer wg.Done(); pump(stderr, "stderr", onOutput) }()
	wg.Wait()

	res := Result{}
	if err := cmd.Wait(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			res.ExitCode = exitErr.ExitCode()
		} else {
			res.ExitCode = -1
			res.Error = err.Error()
		}
	}
	if ctx.Err() != nil {
		res.Error = "cancelled"
	}
	res.DurationMs = time.Since(start).Milliseconds()
	return res
}

func buildCommand(ctx context.Context, cfg Config, sc scripts.Script) (*exec.Cmd, func(), error) {
	var (
		cmd     *exec.Cmd
		file    *os.File
		workDir string
		err     error
	)

	if err := checkLanguage(sc); err != nil {
		return nil, nil, err
	}

	switch sc.Language {
	case scripts.Python:
		workDir, err = os.MkdirTemp("", "testdesk-run-")
		if err != nil {
			return nil, nil, err
		}
		file, err = os.CreateTemp(workDir, "main-*.py")
		if err != nil {
			os.RemoveAll(workDir)
			return nil, nil, err
		}
		python := runtimeenv.VenvPython(cfg.RuntimeDir)
		if python == "" {
			python = cfg.PythonPath
		}
		cmd = exec.CommandContext(ctx, python, "-u", file.Name())
	case scripts.JavaScript:
		if cfg.RuntimeDir == "" {
			return nil, nil, errors.New("运行环境未安装，请先在设置中安装运行环境")
		}
		workDir = filepath.Join(cfg.RuntimeDir, "runs")
		if err = os.MkdirAll(workDir, 0o755); err != nil {
			return nil, nil, err
		}
		// .mjs so import syntax and top-level await work.
		file, err = os.CreateTemp(workDir, "run-*.mjs")
		if err != nil {
			return nil, nil, err
		}
		cmd = exec.CommandContext(ctx, cfg.NodePath, file.Name())
	default:
		return nil, nil, fmt.Errorf("unsupported language: %s", sc.Language)
	}

	cleanup := func() {
		file.Close()
		os.Remove(file.Name())
		if sc.Language == scripts.Python {
			os.RemoveAll(workDir)
		}
	}

	if _, err := file.WriteString(sc.Code); err != nil {
		cleanup()
		return nil, nil, err
	}
	if err := file.Close(); err != nil {
		cleanup()
		return nil, nil, err
	}

	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), "PYTHONUNBUFFERED=1")
	proc.HideConsole(cmd)
	return cmd, cleanup, nil
}

// checkLanguage rejects code that was generated for the other language, e.g. a Python
// recording whose language was saved as javascript. Python comments start with '#',
// which is a syntax error in ESM.
func checkLanguage(sc scripts.Script) error {
	firstLine := strings.TrimSpace(strings.SplitN(sc.Code, "\n", 2)[0])
	switch sc.Language {
	case scripts.JavaScript:
		if strings.HasPrefix(firstLine, "#") && !strings.HasPrefix(firstLine, "#!") {
			return errors.New("脚本语言设置为 JavaScript，但代码是 Python，请在脚本页面把语言改回 Python 后保存")
		}
	case scripts.Python:
		if strings.HasPrefix(firstLine, "//") {
			return errors.New("脚本语言设置为 Python，但代码是 JavaScript，请在脚本页面把语言改回 JavaScript 后保存")
		}
	}
	return nil
}

func pump(r io.Reader, stream string, onOutput OutputFunc) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		onOutput(stream, sc.Text())
	}
}
