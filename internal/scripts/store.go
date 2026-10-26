// Package scripts stores recorded test scripts on disk.
package scripts

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type Language string

const (
	Python     Language = "python"
	JavaScript Language = "javascript"
)

func (l Language) Valid() bool {
	return l == Python || l == JavaScript
}

type Script struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Language  Language  `json:"language"`
	Code      string    `json:"code"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

var (
	ErrNotFound = errors.New("script not found")
	idPattern   = regexp.MustCompile(`^[a-f0-9]{16}$`)
)

// NewID returns a random 16-char hex id. It is also used for run ids.
func NewID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Store keeps each script as <dir>/<id>.json.
type Store struct {
	dir string
	mu  sync.RWMutex
}

func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create script dir: %w", err)
	}
	return &Store{dir: dir}, nil
}

// List returns all scripts, most recently updated first.
func (s *Store) List() ([]Script, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}

	list := make([]Script, 0, len(entries))
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if e.IsDir() || !ok || !idPattern.MatchString(id) {
			continue
		}
		sc, err := s.read(id)
		if err != nil {
			return nil, err
		}
		list = append(list, sc)
	}

	sort.Slice(list, func(i, j int) bool {
		return list[i].UpdatedAt.After(list[j].UpdatedAt)
	})
	return list, nil
}

func (s *Store) Get(id string) (Script, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.read(id)
}

// Save creates a script when sc.ID is empty, otherwise overwrites the existing one.
func (s *Store) Save(sc Script) (Script, error) {
	if strings.TrimSpace(sc.Name) == "" {
		return Script{}, errors.New("script name is required")
	}
	if !sc.Language.Valid() {
		return Script{}, fmt.Errorf("unsupported language %q", sc.Language)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	sc.UpdatedAt = now

	if sc.ID == "" {
		id, err := NewID()
		if err != nil {
			return Script{}, err
		}
		sc.ID = id
		sc.CreatedAt = now
	} else {
		if !idPattern.MatchString(sc.ID) {
			return Script{}, fmt.Errorf("invalid script id %q", sc.ID)
		}
		existing, err := s.read(sc.ID)
		switch {
		case err == nil:
			sc.CreatedAt = existing.CreatedAt
		case errors.Is(err, ErrNotFound):
			sc.CreatedAt = now
		default:
			return Script{}, err
		}
	}

	data, err := json.MarshalIndent(sc, "", "  ")
	if err != nil {
		return Script{}, err
	}

	// Write to a temp file first so a crash never leaves a half-written script.
	path := s.path(sc.ID)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return Script{}, err
	}
	if err := os.Rename(tmp, path); err != nil {
		return Script{}, err
	}
	return sc, nil
}

func (s *Store) Delete(id string) error {
	if !idPattern.MatchString(id) {
		return fmt.Errorf("invalid script id %q", id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	err := os.Remove(s.path(id))
	if errors.Is(err, fs.ErrNotExist) {
		return ErrNotFound
	}
	return err
}

// read must be called with s.mu held.
func (s *Store) read(id string) (Script, error) {
	if !idPattern.MatchString(id) {
		return Script{}, fmt.Errorf("invalid script id %q", id)
	}
	data, err := os.ReadFile(s.path(id))
	if errors.Is(err, fs.ErrNotExist) {
		return Script{}, ErrNotFound
	}
	if err != nil {
		return Script{}, err
	}
	var sc Script
	if err := json.Unmarshal(data, &sc); err != nil {
		return Script{}, fmt.Errorf("parse script %s: %w", id, err)
	}
	return sc, nil
}

func (s *Store) path(id string) string {
	return filepath.Join(s.dir, id+".json")
}
