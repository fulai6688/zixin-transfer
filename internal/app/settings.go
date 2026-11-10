package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Settings is the persisted node configuration.
type Settings struct {
	// DownloadDir is where received files are written by default.
	DownloadDir string `json:"downloadDir"`
}

// SettingsStore persists the node settings next to the node state.
type SettingsStore struct {
	mu       sync.RWMutex
	path     string
	fallback string
	cur      Settings
}

// NewSettingsStore loads settings from path, falling back to fallbackDir.
func NewSettingsStore(path, fallbackDir string) *SettingsStore {
	s := &SettingsStore{path: path, fallback: fallbackDir}
	s.cur = Settings{DownloadDir: fallbackDir}
	s.load()
	return s
}

func (s *SettingsStore) load() {
	if s.path == "" {
		return
	}
	b, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var st Settings
	if json.Unmarshal(b, &st) != nil {
		return
	}
	if st.DownloadDir != "" {
		s.cur.DownloadDir = st.DownloadDir
	}
}

func (s *SettingsStore) saveLocked() error {
	if s.path == "" {
		return nil
	}
	b, err := json.MarshalIndent(s.cur, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Get returns the current settings.
func (s *SettingsStore) Get() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cur
}

// DownloadDir returns the effective receive directory.
func (s *SettingsStore) DownloadDir() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cur.DownloadDir == "" {
		return s.fallback
	}
	return s.cur.DownloadDir
}

// FallbackDir returns the platform default receive directory.
func (s *SettingsStore) FallbackDir() string { return s.fallback }

// SetDownloadDir validates, creates and persists a new receive directory.
func (s *SettingsStore) SetDownloadDir(dir string) (string, error) {
	dir = strings.TrimSpace(strings.Trim(dir, `"`))
	if dir == "" {
		return "", fmt.Errorf("路径不能为空")
	}
	if !filepath.IsAbs(dir) {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return "", fmt.Errorf("路径无效: %w", err)
		}
		dir = abs
	}
	dir = filepath.Clean(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("无法创建目录: %w", err)
	}
	// Prove we can actually write there instead of failing on the first transfer.
	probe := filepath.Join(dir, ".zixin-transfer-write-test")
	if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
		return "", fmt.Errorf("目录不可写: %w", err)
	}
	_ = os.Remove(probe)

	s.mu.Lock()
	defer s.mu.Unlock()
	prev := s.cur.DownloadDir
	s.cur.DownloadDir = dir
	if err := s.saveLocked(); err != nil {
		s.cur.DownloadDir = prev
		return "", fmt.Errorf("保存设置失败: %w", err)
	}
	return dir, nil
}
