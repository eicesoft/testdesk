// Package settings persists user preferences as a JSON file.
package settings

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type Settings struct {
	OpenAIBaseURL string `json:"openaiBaseUrl"`
	OpenAIAPIKey  string `json:"openaiApiKey"`
	OpenAIModel   string `json:"openaiModel"`
	// NodePath is the node executable used for the browser sidecar and JS scripts.
	NodePath string `json:"nodePath"`
	// PythonPath is the interpreter used to create the Python runtime.
	PythonPath string `json:"pythonPath"`
}

func Defaults() Settings {
	return Settings{
		OpenAIBaseURL: "https://api.openai.com/v1",
		NodePath:      "node",
		PythonPath:    "python",
	}
}

// Store reads and writes a single settings file.
type Store struct {
	path string
}

func NewStore(path string) *Store {
	return &Store{path: path}
}

// Load returns the saved settings merged over the defaults.
func (s *Store) Load() (Settings, error) {
	cfg := Defaults()
	data, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	if cfg.NodePath == "" {
		cfg.NodePath = Defaults().NodePath
	}
	if cfg.PythonPath == "" {
		cfg.PythonPath = Defaults().PythonPath
	}
	return cfg, nil
}

func (s *Store) Save(cfg Settings) error {
	cfg.OpenAIBaseURL = strings.TrimRight(strings.TrimSpace(cfg.OpenAIBaseURL), "/")
	cfg.OpenAIAPIKey = strings.TrimSpace(cfg.OpenAIAPIKey)
	cfg.OpenAIModel = strings.TrimSpace(cfg.OpenAIModel)
	cfg.NodePath = strings.TrimSpace(cfg.NodePath)
	cfg.PythonPath = strings.TrimSpace(cfg.PythonPath)

	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
