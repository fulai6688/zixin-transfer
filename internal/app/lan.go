package app

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Inquiry is a request from a peer asking permission to send a file.
type Inquiry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	MIME        string `json:"mime"`
	Fingerprint string `json:"fingerprint"`
	Alias       string `json:"alias"`
	Channel     string `json:"channel"`
	// SavePath is the directory this particular transfer should land in. It
	// is chosen by the receiver, so a one-off file can go somewhere else
	// without changing the default.
	SavePath  string `json:"savePath,omitempty"`
	CreatedAt int64  `json:"createdAt"`
}

// InboxState describes the current disposition of an inquiry.
type InboxState struct {
	Status string `json:"status"` // pending | accepted | rejected | done | error
	Token  string `json:"token,omitempty"`
	Error  string `json:"error,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// ReceivedFile is a file that landed on this node.
type ReceivedFile struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	From        string `json:"from"`
	ReceivedAt  int64  `json:"receivedAt"`
	Fingerprint string `json:"fingerprint"`
}

// LANBroker handles the direct, server-less transfer path between two nodes
// on the same network. The receiving node owns the HTTP endpoint; the
// sending browser streams straight to it.
type LANBroker struct {
	mu             sync.RWMutex
	pending        map[string]*Inquiry
	tokens         map[string]*Inquiry
	tokenByInquiry map[string]string
	received       []ReceivedFile
	registry       *Registry
	downloadDir    string
	receivedPath   string
}

// NewLANBroker creates a broker writing into downloadDir. receivedPath, when
// set, is where the received-file index is persisted.
func NewLANBroker(registry *Registry, downloadDir, receivedPath string) (*LANBroker, error) {
	if err := os.MkdirAll(downloadDir, 0o755); err != nil {
		return nil, err
	}
	b := &LANBroker{
		pending:        map[string]*Inquiry{},
		tokens:         map[string]*Inquiry{},
		tokenByInquiry: map[string]string{},
		registry:       registry,
		downloadDir:    downloadDir,
		receivedPath:   receivedPath,
	}
	b.loadReceived()
	return b, nil
}

func (b *LANBroker) loadReceived() {
	if b.receivedPath == "" {
		return
	}
	raw, err := os.ReadFile(b.receivedPath)
	if err != nil {
		return
	}
	var files []ReceivedFile
	if json.Unmarshal(raw, &files) != nil {
		return
	}
	if len(files) > 200 {
		files = files[len(files)-200:]
	}
	b.received = files
}

// persistReceived writes the index so the "received" panel survives a restart.
// The caller must hold b.mu (write lock).
func (b *LANBroker) persistReceivedLocked() {
	if b.receivedPath == "" {
		return
	}
	raw, err := json.MarshalIndent(b.received, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(b.receivedPath), 0o755)
	tmp := b.receivedPath + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) == nil {
		_ = os.Rename(tmp, b.receivedPath)
	}
}

// SetDownloadDir changes where new files are written.
func (b *LANBroker) SetDownloadDir(dir string) {
	b.mu.Lock()
	b.downloadDir = dir
	b.mu.Unlock()
}

// DownloadDir reports the current default receive directory.
func (b *LANBroker) DownloadDir() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.downloadDir
}

func randomToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("zixin: entropy source failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// Inquire registers an incoming send request.
func (b *LANBroker) Inquire(in Inquiry) Inquiry {
	if in.ID == "" {
		in.ID = randomToken()[:16]
	}
	if in.CreatedAt == 0 {
		in.CreatedAt = time.Now().UnixMilli()
	}
	in.Channel = "lan"
	b.mu.Lock()
	b.pending[in.ID] = &in
	b.mu.Unlock()
	return in
}

// InboxStateFor reports the disposition of one inquiry.
func (b *LANBroker) InboxStateFor(id string) (InboxState, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if token, ok := b.tokenByInquiry[id]; ok {
		return InboxState{Status: "accepted", Token: token}, true
	}
	if _, ok := b.pending[id]; ok {
		return InboxState{Status: "pending"}, true
	}
	return InboxState{}, false
}

// Pending lists inquiries still awaiting a decision.
func (b *LANBroker) Pending() []Inquiry {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]Inquiry, 0, len(b.pending))
	for _, in := range b.pending {
		out = append(out, *in)
	}
	return out
}

// Accept approves an inquiry and returns the one-shot upload token.
// savePath, when set, sends this file to a directory other than the default.
func (b *LANBroker) Accept(id string, accept bool, reason string, savePath string) (InboxState, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	in, ok := b.pending[id]
	if !ok {
		return InboxState{}, false
	}
	delete(b.pending, id)
	if !accept {
		return InboxState{Status: "rejected", Reason: reason}, true
	}
	if strings.TrimSpace(savePath) != "" {
		if err := ensureDir(savePath); err != nil {
			return InboxState{Status: "error", Error: err.Error()}, true
		}
		in.SavePath = filepath.Clean(strings.TrimSpace(savePath))
	}
	token := randomToken()
	b.tokens[token] = in
	b.tokenByInquiry[id] = token
	return InboxState{Status: "accepted", Token: token}, true
}

// Received returns the files received so far.
func (b *LANBroker) Received() []ReceivedFile {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]ReceivedFile, len(b.received))
	copy(out, b.received)
	return out
}

// Store writes an uploaded stream to disk and records it.
func (b *LANBroker) Store(token, filename string, r io.Reader) (ReceivedFile, error) {
	b.mu.RLock()
	in, ok := b.tokens[token]
	dir := b.downloadDir
	b.mu.RUnlock()
	if !ok {
		return ReceivedFile{}, fmt.Errorf("invalid or expired upload token")
	}
	if filename == "" {
		filename = in.Name
	}
	filename = sanitizeFilename(filename)

	// A per-transfer directory wins over the default.
	if in.SavePath != "" {
		dir = in.SavePath
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ReceivedFile{}, err
	}
	target := filepath.Join(dir, filename)
	target = uniquePath(target)

	f, err := os.Create(target)
	if err != nil {
		return ReceivedFile{}, err
	}
	defer f.Close()

	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), r)
	if err != nil {
		_ = os.Remove(target)
		return ReceivedFile{}, err
	}

	rec := ReceivedFile{
		Name:        filepath.Base(target),
		Path:        target,
		Size:        n,
		SHA256:      hex.EncodeToString(h.Sum(nil)),
		From:        in.Alias,
		ReceivedAt:  time.Now().UnixMilli(),
		Fingerprint: in.Fingerprint,
	}
	b.mu.Lock()
	delete(b.tokens, token)
	b.received = append(b.received, rec)
	if len(b.received) > 200 {
		b.received = b.received[len(b.received)-200:]
	}
	b.persistReceivedLocked()
	b.mu.Unlock()
	return rec, nil
}

func sanitizeFilename(name string) string {
	name = strings.ReplaceAll(name, "\\", "_")
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.TrimSpace(name)
	name = strings.TrimLeft(name, ".")
	if name == "" {
		name = "unnamed"
	}
	if len(name) > 180 {
		ext := filepath.Ext(name)
		if len(ext) > 20 {
			ext = ""
		}
		name = name[:180-len(ext)] + ext
	}
	return name
}

func uniquePath(p string) string {
	if _, err := os.Stat(p); err != nil {
		return p
	}
	ext := filepath.Ext(p)
	base := strings.TrimSuffix(p, ext)
	for i := 1; i < 1000; i++ {
		candidate := fmt.Sprintf("%s (%d)%s", base, i, ext)
		if _, err := os.Stat(candidate); err != nil {
			return candidate
		}
	}
	return p
}

// SaveLocal writes a stream that arrived over the signalling path (P2P or
// relay) straight to the download directory without an upload token. The
// caller is the local UI, so this is a trusted path.
func (b *LANBroker) SaveLocal(filename, mime string, r io.Reader) (ReceivedFile, error) {
	return b.SaveLocalTo("", filename, mime, r)
}

// SaveLocalTo writes a stream into dir (default directory when dir is empty).
func (b *LANBroker) SaveLocalTo(dir, filename, mime string, r io.Reader) (ReceivedFile, error) {
	b.mu.RLock()
	def := b.downloadDir
	b.mu.RUnlock()
	if dir == "" {
		dir = def
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ReceivedFile{}, err
	}
	if filename == "" {
		filename = "received-" + time.Now().Format("20060102-150405")
	}
	filename = sanitizeFilename(filename)
	target := uniquePath(filepath.Join(dir, filename))

	f, err := os.Create(target)
	if err != nil {
		return ReceivedFile{}, err
	}
	defer f.Close()

	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), r)
	if err != nil {
		_ = os.Remove(target)
		return ReceivedFile{}, err
	}

	rec := ReceivedFile{
		Name:       filepath.Base(target),
		Path:       target,
		Size:       n,
		SHA256:     hex.EncodeToString(h.Sum(nil)),
		From:       "P2P",
		ReceivedAt: time.Now().UnixMilli(),
	}
	b.mu.Lock()
	b.received = append(b.received, rec)
	if len(b.received) > 200 {
		b.received = b.received[len(b.received)-200:]
	}
	b.persistReceivedLocked()
	b.mu.Unlock()
	return rec, nil
}

// ------------------------------------------------------------------ HTTP

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,OPTIONS,DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type,X-Flux-Token,X-Flux-Device")
		w.Header().Set("Access-Control-Max-Age", "600")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// HandleInquiry accepts a send request from a peer on the local network.
func (b *LANBroker) HandleInquiry(w http.ResponseWriter, r *http.Request) {
	var in Inquiry
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "malformed inquiry"})
		return
	}
	if in.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing filename"})
		return
	}
	out := b.Inquire(in)
	writeJSON(w, http.StatusOK, map[string]any{"id": out.ID, "status": "pending"})
}

// HandleInboxState reports the disposition of one inquiry.
func (b *LANBroker) HandleInboxState(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	st, ok := b.InboxStateFor(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"status": "unknown"})
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// HandleAccept records the user's decision on an inquiry.
func (b *LANBroker) HandleAccept(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID       string `json:"id"`
		Accept   bool   `json:"accept"`
		Reason   string `json:"reason"`
		SavePath string `json:"savePath"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "malformed body"})
		return
	}
	st, ok := b.Accept(req.ID, req.Accept, req.Reason, req.SavePath)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "unknown inquiry"})
		return
	}
	if st.Status == "error" {
		writeJSON(w, http.StatusBadRequest, st)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// HandleUpload receives a file body and writes it to disk.
func (b *LANBroker) HandleUpload(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		token = r.Header.Get("X-Flux-Token")
	}
	if token == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "missing token"})
		return
	}
	name := r.URL.Query().Get("name")
	rec, err := b.Store(token, name, r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

// HandlePending lists inbound requests waiting for approval.
func (b *LANBroker) HandlePending(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"pending": b.Pending()})
}

// HandleReceived lists files this node has received.
func (b *LANBroker) HandleReceived(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"files": b.Received()})
}
