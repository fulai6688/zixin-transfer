package app

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

// Config carries the runtime knobs for a 子欣互传 node.
type Config struct {
	Port        int
	DownloadDir string
	DataDir     string
	Alias       string
	Platform    string
	Version     string
	// Commit and BuildTime identify exactly which build is running; they are
	// injected at link time and shown in /api/health and the settings panel.
	Commit    string
	BuildTime string
	Announce  bool
	// RelayURL is the public address of the signalling relay that serves
	// the cross-network sessions (the cloud node). Clients use it to create
	// pickup codes and to open the signalling socket, so both peers meet on
	// the same server instead of their own local node.
	RelayURL string
}

// Server wires every subsystem together.
type Server struct {
	cfg      Config
	sessions *SessionManager
	registry *Registry
	lan      *LANBroker
	mdns     *MDNS
	settings *SettingsStore
	chat     *ChatRoom
	selfFP   string
	// localIP is this node's primary LAN address. Clients need it to hand
	// other devices a download URL that works from their side of the network
	// (a page opened at localhost would otherwise point peers at themselves).
	localIP  string
	upgrader websocket.Upgrader

	mu       sync.Mutex
	shutdown chan struct{}

	// scanMu guards the shared subnet sweep.
	scanMu     sync.Mutex
	scanFlight chan struct{}
	scanLast   []MDNSEntry
	scanAt     time.Time
}

// NewServer builds a server and its subsystems.
func NewServer(cfg Config) (*Server, error) {
	// Absolute paths everywhere: the UI shows this directory, and a relative
	// path would be resolved against whatever the working directory was.
	if cfg.DataDir != "" {
		if abs, err := filepath.Abs(cfg.DataDir); err == nil {
			cfg.DataDir = abs
		}
	}
	if cfg.DownloadDir == "" {
		cfg.DownloadDir = filepath.Join(cfg.DataDir, "Downloads")
	}
	if abs, err := filepath.Abs(cfg.DownloadDir); err == nil {
		cfg.DownloadDir = abs
	}
	if err := os.MkdirAll(cfg.DownloadDir, 0o755); err != nil {
		return nil, err
	}
	settings := NewSettingsStore(filepath.Join(cfg.DataDir, "settings.json"), cfg.DownloadDir)
	registry := NewRegistry(filepath.Join(cfg.DataDir, "trusted.json"))
	broker, err := NewLANBroker(registry, settings.DownloadDir(), filepath.Join(cfg.DataDir, "received.json"))
	if err != nil {
		return nil, err
	}
	fp, err := loadOrCreateFingerprint(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	localIP := ""
	if ip, _, err := primaryInterface(); err == nil && ip != nil {
		localIP = ip.String()
	}
	s := &Server{
		cfg:      cfg,
		sessions: NewSessionManager(time.Hour),
		localIP:  localIP,
		registry: registry,
		lan:      broker,
		settings: settings,
		chat:     NewChatRoom(fp, cfg.Alias, filepath.Join(cfg.DataDir, "chat.json")),
		selfFP:   fp,
		shutdown: make(chan struct{}),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1 << 16,
			WriteBufferSize: 1 << 16,
			CheckOrigin:     func(r *http.Request) bool { return true },
		},
	}
	if cfg.Announce {
		m, err := NewMDNS(cfg.Port, fp, cfg.Alias, cfg.Platform)
		if err == nil {
			if err := m.Start(); err == nil {
				s.mdns = m
			} else {
				log.Printf("zixin: mDNS announce unavailable: %v", err)
			}
		} else {
			log.Printf("zixin: mDNS unavailable: %v", err)
		}
	}
	// Staged room files are temporary; drop yesterday's leftovers.
	go s.sweepChatFiles(24 * time.Hour)
	return s, nil
}

// loadOrCreateFingerprint returns this node's stable identity. The value is
// persisted next to the node state: a fingerprint that changed on every start
// would make peers see a "new" device each time and lose their notes.
func loadOrCreateFingerprint(dataDir string) (string, error) {
	type identity struct {
		Fingerprint string `json:"fingerprint"`
	}
	path := filepath.Join(dataDir, "device.json")
	if b, err := os.ReadFile(path); err == nil {
		var id identity
		if json.Unmarshal(b, &id) == nil && len(id.Fingerprint) >= 16 {
			return id.Fingerprint, nil
		}
	}
	fp := newFingerprint()
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fp, err
	}
	b, err := json.MarshalIndent(identity{Fingerprint: fp}, "", "  ")
	if err != nil {
		return fp, err
	}
	// Best effort: a read-only data dir still gets a working (if unstable) id.
	_ = os.WriteFile(path, b, 0o600)
	return fp, nil
}

func newFingerprint() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("zixin: entropy source failed: " + err.Error())
	}
	const hexdigits = "0123456789abcdef"
	out := make([]byte, 32)
	for i, v := range b {
		out[i*2] = hexdigits[v>>4]
		out[i*2+1] = hexdigits[v&0x0f]
	}
	return string(out)
}

// ---------------------------------------------------------------- routing

// Handler builds the HTTP routing table.
func (s *Server) Handler(static fs.FS) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("POST /api/session", s.handleCreateSession)
	mux.HandleFunc("GET /api/session/{id}", s.handleSessionInfo)
	mux.HandleFunc("GET /api/session/code/{code}", s.handleSessionByCode)
	mux.HandleFunc("DELETE /api/session/{id}", s.handleDestroySession)
	mux.HandleFunc("GET /api/devices", s.handleDevices)
	mux.HandleFunc("POST /api/pair", s.handlePair)

	mux.HandleFunc("GET /api/lan/devices", s.handleLANDevices)
	mux.HandleFunc("GET /api/lan/scan", s.handleLANScan)
	mux.HandleFunc("POST /api/lan/inquiry", s.lan.HandleInquiry)
	mux.HandleFunc("GET /api/lan/inbox/{id}", s.lan.HandleInboxState)
	mux.HandleFunc("POST /api/lan/accept", s.lan.HandleAccept)
	mux.HandleFunc("POST /api/lan/upload", s.lan.HandleUpload)
	mux.HandleFunc("GET /api/lan/pending", s.lan.HandlePending)
	mux.HandleFunc("GET /api/lan/received", s.lan.HandleReceived)
	mux.HandleFunc("POST /api/inbox/save", s.handleInboxSave)

	mux.HandleFunc("GET /api/ws/session", s.handleSessionSocket)

	mux.HandleFunc("GET /api/firewall/status", s.handleFirewallStatus)
	mux.HandleFunc("POST /api/firewall/install", s.handleFirewallInstall)

	mux.HandleFunc("GET /api/chat/history", s.handleChatHistory)
	mux.HandleFunc("POST /api/chat/post", s.handleChatPost)
	mux.HandleFunc("POST /api/chat/upload", s.handleChatUpload)
	mux.HandleFunc("GET /api/chat/file/{id}", s.handleChatFile)
	mux.HandleFunc("GET /api/chat/ws", s.handleChatSocket)

	mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	mux.HandleFunc("POST /api/settings", s.handleSaveSettings)
	mux.HandleFunc("POST /api/system/pick-folder", s.handlePickFolder)
	mux.HandleFunc("POST /api/system/reveal", s.handleReveal)
	mux.HandleFunc("POST /api/system/open", s.handleOpenFile)
	mux.HandleFunc("POST /api/devices/rename", s.handleRenameDevice)

	// Unknown API paths must not fall through to the SPA: a 200 + index.html
	// would convince clients (and the chat fan-out) that an endpoint exists.
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": "unknown endpoint",
			"path":  r.URL.Path,
		})
	})

	if static != nil {
		mux.Handle("/", spaHandler(static))
	}
	return corsMiddleware(logging(mux))
}

func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if !strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api/health" {
			return
		}
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}

// spaHandler serves the embedded single page app with history fallback.
func spaHandler(static fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(static))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(filepath.ToSlash(r.URL.Path), "/")
		if p == "" {
			p = "index.html"
		}
		if f, err := static.Open(p); err == nil {
			_ = f.Close()
			if strings.HasPrefix(p, "_next/static/") || strings.HasSuffix(p, ".js") || strings.HasSuffix(p, ".css") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			fileServer.ServeHTTP(w, r)
			return
		}
		// History API fallback.
		b, err := fs.ReadFile(static, "index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(b)
	})
}

// ------------------------------------------------------------------ API

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"version":  s.cfg.Version,
		"alias":    s.cfg.Alias,
		"platform": s.cfg.Platform,
		"device":   s.selfFP,
		"port":     s.cfg.Port,
		"announce": s.mdns != nil,
		"firewall": FirewallInstalled(),
		"relay":    s.cfg.RelayURL,
		"commit":   s.cfg.Commit,
		"builtAt":  s.cfg.BuildTime,
		"download": s.settings.DownloadDir(),
		"local":    isLoopback(r),
		"address":  s.localIP,
	})
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	sess := s.sessions.Create()
	writeJSON(w, http.StatusOK, map[string]any{
		"id":        sess.ID,
		"code":      sess.Code,
		"expiresAt": sess.ExpiresAt.UnixMilli(),
	})
}

func (s *Server) handleSessionInfo(w http.ResponseWriter, r *http.Request) {
	sess := s.sessions.Get(r.PathValue("id"))
	if sess == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "unknown session"})
		return
	}
	sess.mu.Lock()
	hasSender := sess.peers[RoleSender] != nil
	hasReceiver := sess.peers[RoleReceiver] != nil
	sess.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"id":             sess.ID,
		"code":           sess.Code,
		"expiresAt":      sess.ExpiresAt.UnixMilli(),
		"senderOnline":   hasSender,
		"receiverOnline": hasReceiver,
	})
}

func (s *Server) handleDestroySession(w http.ResponseWriter, r *http.Request) {
	s.sessions.Destroy(r.PathValue("id"))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleDevices(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"devices": s.registry.List()})
}

func (s *Server) handlePair(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Fingerprint string `json:"fingerprint"`
		Alias       string `json:"alias"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Fingerprint == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "malformed body"})
		return
	}
	s.registry.Trust(req.Fingerprint)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "fingerprint": req.Fingerprint})
}

func (s *Server) handleLANDevices(w http.ResponseWriter, r *http.Request) {
	entries := []MDNSEntry{}
	if s.mdns != nil {
		entries = s.mdns.Entries()
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": entries})
}

// handleSessionByCode resolves a short pickup code into a session.
func (s *Server) handleSessionByCode(w http.ResponseWriter, r *http.Request) {
	code := strings.ToUpper(strings.TrimSpace(r.PathValue("code")))
	sess := s.sessions.GetByCode(code)
	if sess == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "取件码无效或已过期"})
		return
	}
	sess.mu.Lock()
	hasSender := sess.peers[RoleSender] != nil
	hasReceiver := sess.peers[RoleReceiver] != nil
	sess.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"id":             sess.ID,
		"code":           sess.Code,
		"expiresAt":      sess.ExpiresAt.UnixMilli(),
		"senderOnline":   hasSender,
		"receiverOnline": hasReceiver,
	})
}

// handleInboxSave stores a payload that arrived over the signalling path.
func (s *Server) handleInboxSave(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	mime := r.URL.Query().Get("mime")
	dir := r.URL.Query().Get("dir")
	if dir != "" && !isLoopback(r) {
		dir = "" // a remote peer may not choose where local files land
	}
	rec, err := s.lan.SaveLocalTo(dir, name, mime, r.Body)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

// ------------------------------------------------------- websocket session

type signalFrame struct {
	Type    string          `json:"type"`
	From    string          `json:"from"`
	To      string          `json:"to"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

func (s *Server) handleSessionSocket(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	sessID := q.Get("session")
	role := Role(q.Get("role"))
	if sessID == "" || !role.valid() {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "session and role are required"})
		return
	}
	sess := s.sessions.Get(sessID)
	if sess == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "unknown session"})
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	peer := &Peer{
		Role:        role,
		Fingerprint: q.Get("fingerprint"),
		Alias:       q.Get("alias"),
		conn:        conn,
		send:        make(chan wsFrame, 64),
	}
	if err := s.sessions.Join(sess, peer); err != nil {
		_ = conn.WriteJSON(map[string]any{"type": "error", "message": err.Error()})
		_ = conn.Close()
		return
	}

	go peer.writePump()
	peer.writeJSON(map[string]any{
		"type":    "ready",
		"from":    string(role),
		"payload": map[string]any{"session": sess.ID, "role": string(role)},
	})

	defer func() {
		s.sessions.Leave(sess, peer)
		peer.close()
	}()

	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		switch msgType {
		case websocket.TextMessage, websocket.BinaryMessage:
			// Signal frames are relayed verbatim; the server never inspects
			// SDP, ICE candidates or relay payloads.
			out := data
			if msgType == websocket.TextMessage {
				var f signalFrame
				if json.Unmarshal(data, &f) == nil {
					f.From = string(role)
					f.To = string(sess.other(role).Role)
					if enc, err := json.Marshal(f); err == nil {
						out = enc
					}
				}
			}
			target := sess.other(role)
			if target == nil {
				// The other peer has not joined yet: hold the frame so the
				// handshake still completes once it shows up.
				sess.pushBacklog(msgType, out)
				continue
			}
			_ = target.writeFrame(msgType, out)
		}
	}
}

func (p *Peer) writePump() {
	for {
		f, ok := <-p.send
		if !ok {
			return
		}
		_ = p.conn.SetWriteDeadline(time.Now().Add(20 * time.Second))
		if err := p.conn.WriteMessage(f.kind, f.data); err != nil {
			return
		}
	}
}

// --------------------------------------------------------------- lifecycle

// Run starts the HTTP server and blocks until the process is asked to stop.
func (s *Server) Run(static fs.FS) error {
	srv := &http.Server{
		Addr:              ":" + itoa(s.cfg.Port),
		Handler:           s.Handler(static),
		ReadHeaderTimeout: 15 * time.Second,
		// Long-lived websockets must not be cut off by a write timeout.
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("zixin %s listening on http://0.0.0.0:%d", s.cfg.Version, s.cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-errCh:
		return err
	case <-stop:
		log.Println("zixin: shutting down")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	if s.mdns != nil {
		s.mdns.Stop()
	}
	close(s.shutdown)
	return nil
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// RelayFrame is the wire format used when a transfer falls back to the
// server relay: an 8 byte big-endian offset followed by the payload.
type RelayFrame struct {
	Offset uint64
	Data   []byte
}

// EncodeRelayFrame prepends the offset header.
func EncodeRelayFrame(offset uint64, payload []byte) []byte {
	out := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint64(out[:8], offset)
	copy(out[8:], payload)
	return out
}

// DecodeRelayFrame splits a relay frame into offset and payload.
func DecodeRelayFrame(b []byte) (uint64, []byte, bool) {
	if len(b) < 8 {
		return 0, nil, false
	}
	return binary.BigEndian.Uint64(b[:8]), b[8:], true
}

var _ = io.Discard
