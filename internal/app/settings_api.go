package app

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// settingsPayload is what the UI needs to render the settings panel.
func (s *Server) settingsPayload() map[string]any {
	st := s.settings.Get()
	return map[string]any{
		"downloadDir":     s.settings.DownloadDir(),
		"defaultDir":      s.settings.FallbackDir(),
		"savedDir":        st.DownloadDir,
		"pickerSupported": true,
		"platform":        s.cfg.Platform,
	}
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.settingsPayload())
}

// handleSaveSettings changes the default receive directory.
func (s *Server) handleSaveSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DownloadDir string `json:"downloadDir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "malformed body"})
		return
	}
	dir, err := s.settings.SetDownloadDir(req.DownloadDir)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	s.lan.SetDownloadDir(dir)
	writeJSON(w, http.StatusOK, s.settingsPayload())
}

// handlePickFolder opens the host folder chooser (local requests only).
func (s *Server) handlePickFolder(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}
	start := s.settings.DownloadDir()
	var req struct {
		Start string `json:"start"`
	}
	if r.ContentLength > 0 {
		_ = json.NewDecoder(r.Body).Decode(&req)
		if strings.TrimSpace(req.Start) != "" {
			start = req.Start
		}
	}
	picked, err := pickFolder(start)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "path": picked, "cancelled": picked == ""})
}

// localPathAllowed limits shell actions to files this node has actually
// received (or the receive directories), so a stray request cannot open an
// arbitrary path on the user's machine.
func (s *Server) localPathAllowed(p string) (string, bool) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", false
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", false
	}
	abs = filepath.Clean(abs)
	if _, err := os.Stat(abs); err != nil {
		return abs, false
	}

	dirs := map[string]bool{
		filepath.Clean(s.settings.DownloadDir()): true,
		filepath.Clean(s.settings.FallbackDir()): true,
	}
	for _, f := range s.lan.Received() {
		if f.Path == "" {
			continue
		}
		rec := filepath.Clean(f.Path)
		if abs == rec {
			return abs, true
		}
		dirs[filepath.Dir(rec)] = true
	}
	for d := range dirs {
		if abs == d || strings.HasPrefix(abs, d+string(os.PathSeparator)) {
			return abs, true
		}
	}
	return abs, false
}

// handleReveal shows a received file in the OS file manager.
func (s *Server) handleReveal(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "malformed body"})
		return
	}
	abs, ok := s.localPathAllowed(req.Path)
	if !ok {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "只能打开本机已接收的文件或接收目录"})
		return
	}
	if err := revealPath(abs); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleOpenFile opens a received file with the default application.
func (s *Server) handleOpenFile(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "malformed body"})
		return
	}
	abs, ok := s.localPathAllowed(req.Path)
	if !ok {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "只能打开本机已接收的文件"})
		return
	}
	if err := openPath(abs); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleRenameDevice stores a user-chosen name for a device.
func (s *Server) handleRenameDevice(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Fingerprint string `json:"fingerprint"`
		Alias       string `json:"alias"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Fingerprint == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "malformed body"})
		return
	}
	d, ok := s.registry.Rename(req.Fingerprint, req.Alias)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "unknown device"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "device": d, "displayName": d.DisplayName()})
}
