package runner

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"testdesk/internal/scripts"
)

func TestRunJavaScriptStreamsOutput(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not installed")
	}

	var mu sync.Mutex
	var lines []string
	res := Run(context.Background(), Config{NodePath: "node", RuntimeDir: t.TempDir()}, scripts.Script{
		Language: scripts.JavaScript,
		Code:     "console.log('hello'); console.error('oops'); process.exit(3)",
	}, func(stream, line string) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, stream+":"+line)
	})

	if res.ExitCode != 3 {
		t.Errorf("exit code = %d, want 3 (error=%q)", res.ExitCode, res.Error)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "stdout:hello") || !strings.Contains(joined, "stderr:oops") {
		t.Errorf("missing output, got:\n%s", joined)
	}
}

func TestRunJavaScriptWithoutRuntimeFails(t *testing.T) {
	res := Run(context.Background(), Config{NodePath: "node"}, scripts.Script{
		Language: scripts.JavaScript,
		Code:     "console.log(1)",
	}, func(string, string) {})

	if res.Error == "" {
		t.Error("expected an error when the runtime directory is missing")
	}
}

func TestRunMissingInterpreter(t *testing.T) {
	res := Run(context.Background(), Config{PythonPath: "no-such-python-binary", RuntimeDir: t.TempDir()}, scripts.Script{
		Language: scripts.Python,
		Code:     "print(1)",
	}, func(string, string) {})

	if res.Error == "" {
		t.Error("expected an error when the interpreter does not exist")
	}
}
