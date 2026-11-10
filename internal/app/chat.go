package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ChatRoom is the LAN chat room hosted by each node. Every 子欣互传 node runs
// one room; browsers and other nodes join over websocket, so phones, desktops
// and the host UI all talk in the same place without any cloud dependency.
//
// The room keeps a bounded backlog so a tab that reconnects still sees the
// last messages, and the history is persisted next to the node state so it
// survives a restart.
type ChatRoom struct {
	mu       sync.RWMutex
	clients  map[*chatClient]struct{}
	backlog  []ChatMessage
	max      int
	store    *chatStore
	selfFP   string
	selfName string
}

// ChatMessage is one line in the room.
type ChatMessage struct {
	ID        string `json:"id"`
	From      string `json:"from"`            // fingerprint
	Alias     string `json:"alias"`           // display name
	Body      string `json:"body"`            // text
	Kind      string `json:"kind"`            // text | file | system
	Extra     string `json:"extra,omitempty"` // file name / note
	Timestamp int64  `json:"timestamp"`
}

type chatClient struct {
	conn  *websocket.Conn
	send  chan []byte
	alias string
	fp    string
	kind  string // "browser" | "node"
}

// chatInbound is what a client sends to the room.
type chatInbound struct {
	Type  string `json:"type"` // "message" | "ping"
	Body  string `json:"body"`
	Alias string `json:"alias"`
}

// NewChatRoom builds a room with the given history size.
func NewChatRoom(selfFP, selfAlias, historyPath string) *ChatRoom {
	r := &ChatRoom{
		clients:  map[*chatClient]struct{}{},
		max:      200,
		selfFP:   selfFP,
		selfName: selfAlias,
		store:    loadChatStore(historyPath),
	}
	r.backlog = r.store.load()
	return r
}

// History returns the recent messages, oldest first.
func (r *ChatRoom) History() []ChatMessage {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ChatMessage, len(r.backlog))
	copy(out, r.backlog)
	return out
}

// Post appends a message and broadcasts it. Used by both the websocket path
// and the plain HTTP path (so a node without a browser can still post).
func (r *ChatRoom) Post(m ChatMessage) {
	if m.Timestamp == 0 {
		m.Timestamp = time.Now().UnixMilli()
	}
	if m.ID == "" {
		m.ID = randomToken()[:12]
	}
	r.mu.Lock()
	r.backlog = append(r.backlog, m)
	if len(r.backlog) > r.max {
		r.backlog = r.backlog[len(r.backlog)-r.max:]
	}
	r.store.save(r.backlog)
	clients := make([]*chatClient, 0, len(r.clients))
	for c := range r.clients {
		clients = append(clients, c)
	}
	r.mu.Unlock()

	broadcast(clients, m)
}

func broadcast(clients []*chatClient, m ChatMessage) {
	raw, err := json.Marshal(map[string]any{
		"type": "chat",
		"data": m,
	})
	if err != nil {
		return
	}
	for _, c := range clients {
		select {
		case c.send <- raw:
		default: // slow consumer; drop rather than block everyone
		}
	}
}

// ------------------------------------------------------------------ HTTP

// handleChatHistory answers GET /api/chat/history.
func (s *Server) handleChatHistory(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"messages": s.chat.History()})
}

// relayChat pushes one message to every other 子欣互传 node we know about.
//
// Relay is a server responsibility on purpose: it uses the node's own device
// table, so a browser tab that has not finished its first scan yet cannot
// silently drop the fan-out. Nodes that receive a relayed message never
// relay it again, which keeps the traffic loop-free.
func (s *Server) relayChat(m ChatMessage) int {
	type target struct {
		addr string
		port int
	}
	seen := map[string]bool{s.selfFP: true}
	var targets []target
	for _, d := range s.registry.List() {
		if d.Address == "" || d.Port == 0 || seen[d.Fingerprint] {
			continue
		}
		seen[d.Fingerprint] = true
		targets = append(targets, target{addr: d.Address, port: d.Port})
	}
	if len(targets) == 0 {
		return 0
	}

	// A file announcement must point at this node's LAN address, not at
	// localhost or at whatever host the UI happened to be opened with.
	payload := m
	if m.Kind == "file" {
		if extra, err := parseFileExtra(m.Extra); err == nil {
			extra.URL = s.publicFileURL(extra.ID)
			if raw, err := json.Marshal(extra); err == nil {
				payload.Extra = string(raw)
			}
		}
	}
	body, err := json.Marshal(map[string]any{
		"from":  payload.From,
		"alias": payload.Alias,
		"body":  payload.Body,
		"kind":  payload.Kind,
		"extra": payload.Extra,
	})
	if err != nil {
		return 0
	}

	client := &http.Client{Timeout: 3 * time.Second}
	var mu sync.Mutex
	delivered := 0
	var wg sync.WaitGroup
	for _, t := range targets {
		wg.Add(1)
		go func(t target) {
			defer wg.Done()
			url := "http://" + net.JoinHostPort(t.addr, strconv.Itoa(t.port)) + "/api/chat/post"
			req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
			if err != nil {
				return
			}
			req.Header.Set("Content-Type", "application/json")
			resp, err := client.Do(req)
			if err != nil {
				return
			}
			defer resp.Body.Close()
			// Only count peers that actually run the chat endpoint. An older
			// build answers unknown /api paths with the SPA (200 + text/html),
			// which must not look like a delivery.
			ct := resp.Header.Get("Content-Type")
			if resp.StatusCode == http.StatusOK && strings.Contains(ct, "application/json") {
				mu.Lock()
				delivered++
				mu.Unlock()
			}
		}(t)
	}
	wg.Wait()
	return delivered
}

// publicFileURL is the address other devices should use to fetch a shared
// file: this node's LAN IP and port.
func (s *Server) publicFileURL(id string) string {
	host := s.localIP
	if host == "" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(s.cfg.Port)) + "/api/chat/file/" + id
}

// fileExtra is the JSON stored in ChatMessage.Extra for file messages.
type fileExtra struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	MIME   string `json:"mime"`
	URL    string `json:"url"`
}

func parseFileExtra(raw string) (fileExtra, error) {
	var e fileExtra
	err := json.Unmarshal([]byte(raw), &e)
	return e, err
}

// handleChatPost answers POST /api/chat/post. The local UI sets relay=true;
// a peer relaying a message leaves it false so nothing loops.
func (s *Server) handleChatPost(w http.ResponseWriter, r *http.Request) {
	var req struct {
		From  string `json:"from"`
		Alias string `json:"alias"`
		Body  string `json:"body"`
		Kind  string `json:"kind"`
		Extra string `json:"extra"`
		Relay bool   `json:"relay"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Body == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "malformed body"})
		return
	}
	if len(req.Body) > 4000 {
		req.Body = req.Body[:4000]
	}
	kind := req.Kind
	if kind != "file" {
		kind = "text"
	}
	m := ChatMessage{
		ID:        randomToken()[:12],
		From:      req.From,
		Alias:     req.Alias,
		Body:      req.Body,
		Kind:      kind,
		Extra:     req.Extra,
		Timestamp: time.Now().UnixMilli(),
	}
	if m.From == "" {
		m.From = s.selfFP
	}
	if m.Alias == "" {
		m.Alias = m.From
	}
	if kind == "file" && m.Extra == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "file message needs extra"})
		return
	}
	s.chat.Post(m)

	delivered := 0
	if req.Relay {
		delivered = s.relayChat(m)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": m.ID, "delivered": delivered})
}

// chatFileDir is where files shared into the room are staged.
func (s *Server) chatFileDir() string {
	return filepath.Join(s.cfg.DataDir, "chatfiles")
}

// handleChatUpload stores one file and announces it to the room. The bytes are
// served back by handleChatFile, so a room with ten members still uploads the
// file once instead of pushing it ten times.
func (s *Server) handleChatUpload(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	mime := r.URL.Query().Get("mime")
	if name == "" {
		name = "file"
	}
	name = sanitizeFilename(name)

	dir := s.chatFileDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	id := randomToken()[:16]
	target := filepath.Join(dir, id+"-"+name)
	f, err := os.Create(target)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), r.Body)
	cerr := f.Close()
	if err != nil || cerr != nil {
		_ = os.Remove(target)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "write failed"})
		return
	}

	extra := fileExtra{
		ID:     id,
		Name:   name,
		Size:   n,
		SHA256: hex.EncodeToString(h.Sum(nil)),
		MIME:   mime,
		URL:    "/api/chat/file/" + id,
	}
	rawExtra, _ := json.Marshal(extra)
	msg := ChatMessage{
		ID:        randomToken()[:12],
		From:      s.selfFP,
		Alias:     s.cfg.Alias,
		Body:      name,
		Kind:      "file",
		Extra:     string(rawExtra),
		Timestamp: time.Now().UnixMilli(),
	}
	s.chat.Post(msg)
	delivered := s.relayChat(msg)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"message":   msg,
		"file":      extra,
		"delivered": delivered,
	})
}

// handleChatFile serves a staged file back by its random id.
func (s *Server) handleChatFile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" || strings.ContainsAny(id, "/\\") {
		http.NotFound(w, r)
		return
	}
	entries, err := os.ReadDir(s.chatFileDir())
	if err != nil {
		http.NotFound(w, r)
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), id+"-") {
			continue
		}
		full := filepath.Join(s.chatFileDir(), e.Name())
		f, err := os.Open(full)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			http.NotFound(w, r)
			return
		}
		download := strings.TrimPrefix(e.Name(), id+"-")
		w.Header().Set("Content-Disposition", "attachment; filename=\""+download+"\"")
		w.Header().Set("Content-Type", "application/octet-stream")
		http.ServeContent(w, r, download, info.ModTime(), f)
		return
	}
	http.NotFound(w, r)
}

// sweepChatFiles deletes shared files older than ttl so the staging area does
// not grow without bound.
func (s *Server) sweepChatFiles(ttl time.Duration) {
	dir := s.chatFileDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-ttl)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// handleChatSocket upgrades GET /api/chat/ws into the room.
func (s *Server) handleChatSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &chatClient{
		conn:  conn,
		send:  make(chan []byte, 64),
		alias: r.URL.Query().Get("alias"),
		fp:    r.URL.Query().Get("fingerprint"),
		kind:  r.URL.Query().Get("kind"),
	}
	if c.alias == "" {
		c.alias = c.fp
	}

	s.chat.mu.Lock()
	s.chat.clients[c] = struct{}{}
	// Replay the recent history so a reconnecting tab is not lost.
	history := make([]ChatMessage, len(s.chat.backlog))
	copy(history, s.chat.backlog)
	clients := make([]*chatClient, 0, len(s.chat.clients))
	for cl := range s.chat.clients {
		clients = append(clients, cl)
	}
	s.chat.mu.Unlock()

	go c.writePump()
	// history snapshot first
	if raw, err := json.Marshal(map[string]any{"type": "history", "data": history}); err == nil {
		select {
		case c.send <- raw:
		default:
		}
	}
	// announce
	if raw, err := json.Marshal(map[string]any{
		"type": "presence",
		"data": map[string]any{"event": "join", "alias": c.alias, "count": len(clients)},
	}); err == nil {
		for _, cl := range clients {
			select {
			case cl.send <- raw:
			default:
			}
		}
	}

	defer func() {
		s.chat.mu.Lock()
		delete(s.chat.clients, c)
		rest := make([]*chatClient, 0, len(s.chat.clients))
		for cl := range s.chat.clients {
			rest = append(rest, cl)
		}
		s.chat.mu.Unlock()
		close(c.send)
		if raw, err := json.Marshal(map[string]any{
			"type": "presence",
			"data": map[string]any{"event": "leave", "alias": c.alias, "count": len(rest)},
		}); err == nil {
			for _, cl := range rest {
				select {
				case cl.send <- raw:
				default:
				}
			}
		}
	}()

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var in chatInbound
		if json.Unmarshal(data, &in) != nil {
			continue
		}
		switch in.Type {
		case "ping":
			if raw, err := json.Marshal(map[string]any{"type": "pong"}); err == nil {
				select {
				case c.send <- raw:
				default:
				}
			}
		case "message":
			if in.Body == "" {
				continue
			}
			if len(in.Body) > 4000 {
				in.Body = in.Body[:4000]
			}
			m := ChatMessage{
				From:      c.fp,
				Alias:     c.alias,
				Body:      in.Body,
				Kind:      "text",
				Timestamp: time.Now().UnixMilli(),
			}
			if m.From == "" {
				m.From = "anon"
			}
			m.ID = randomToken()[:12]
			s.chat.Post(m)
			go s.relayChat(m)
		}
	}
}

func (c *chatClient) writePump() {
	for raw := range c.send {
		_ = c.conn.SetWriteDeadline(time.Now().Add(20 * time.Second))
		if c.conn.WriteMessage(websocket.TextMessage, raw) != nil {
			return
		}
	}
}

// ------------------------------------------------------------- persistence

type chatStore struct {
	path string
}

func loadChatStore(path string) *chatStore { return &chatStore{path: path} }

func (s *chatStore) load() []ChatMessage {
	if s.path == "" {
		return nil
	}
	raw, err := readFileUTF8(s.path)
	if err != nil {
		return nil
	}
	var msgs []ChatMessage
	if json.Unmarshal([]byte(raw), &msgs) != nil {
		return nil
	}
	if len(msgs) > 200 {
		msgs = msgs[len(msgs)-200:]
	}
	return msgs
}

func (s *chatStore) save(msgs []ChatMessage) {
	if s.path == "" {
		return
	}
	raw, err := json.MarshalIndent(msgs, "", "  ")
	if err != nil {
		return
	}
	writeFileUTF8(s.path, raw)
}
