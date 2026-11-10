package app

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"math/big"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Role identifies which side of a transfer a peer is.
type Role string

const (
	RoleSender   Role = "sender"
	RoleReceiver Role = "receiver"
)

func (r Role) valid() bool { return r == RoleSender || r == RoleReceiver }

// Peer is one participant of a session.
type Peer struct {
	Role        Role
	Fingerprint string
	Alias       string
	conn        *websocket.Conn
	send        chan wsFrame
	closed      bool
	mu          sync.Mutex
}

// wsFrame keeps the original websocket frame type so relayed signal frames
// stay text frames and payload frames stay binary frames.
type wsFrame struct {
	kind int
	data []byte
}

func (p *Peer) writeJSON(v any) error {
	b, err := marshal(v)
	if err != nil {
		return err
	}
	return p.writeFrame(websocket.TextMessage, b)
}

// writeRaw relays an opaque payload frame.
func (p *Peer) writeRaw(b []byte) error {
	return p.writeFrame(websocket.BinaryMessage, b)
}

// writeFrame queues one frame for the peer.
func (p *Peer) writeFrame(kind int, b []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return errors.New("peer closed")
	}
	select {
	case p.send <- wsFrame{kind: kind, data: b}:
		return nil
	case <-time.After(10 * time.Second):
		return errors.New("peer send timeout")
	}
}

func (p *Peer) close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	close(p.send)
	p.mu.Unlock()
	_ = p.conn.Close()
}

// Session is a two-party transfer room, addressed both by opaque id and by a
// short human-typable code.
type Session struct {
	ID        string
	Code      string
	CreatedAt time.Time
	ExpiresAt time.Time

	mu    sync.Mutex
	peers map[Role]*Peer
	// backlog holds frames that arrived before the other peer joined. The
	// sender usually opens its socket first and starts the WebRTC handshake
	// immediately, so without a backlog the offer would be dropped and the
	// connection would hang until the user gave up.
	backlog []wsFrame
}

// backlogLimit caps the buffered bytes per session.
const backlogLimit = 2 << 20

// pushBacklog stores a frame for a peer that has not joined yet.
func (s *Session) pushBacklog(kind int, b []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.backlogSize() >= backlogLimit {
		return
	}
	cp := make([]byte, len(b))
	copy(cp, b)
	s.backlog = append(s.backlog, wsFrame{kind: kind, data: cp})
}

func (s *Session) backlogSize() int {
	n := 0
	for _, b := range s.backlog {
		n += len(b.data)
	}
	return n
}

// takeBacklog returns and clears the buffered frames.
func (s *Session) takeBacklog() []wsFrame {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.backlog
	s.backlog = nil
	return out
}

func (s *Session) peer(r Role) *Peer {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.peers[r]
}

func (s *Session) other(r Role) *Peer {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r == RoleSender {
		return s.peers[RoleReceiver]
	}
	return s.peers[RoleSender]
}

// SessionManager owns every live session in this process.
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	byCode   map[string]*Session
	ttl      time.Duration
}

// codeAlphabet omits 0/O and 1/I so codes stay readable over voice or screen.
const codeAlphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"

func randomCode(n int) string {
	out := make([]byte, n)
	max := big.NewInt(int64(len(codeAlphabet)))
	for i := range out {
		v, err := rand.Int(rand.Reader, max)
		if err != nil {
			// crypto/rand failure is unrecoverable for session codes.
			panic("zixin: entropy source failed: " + err.Error())
		}
		out[i] = codeAlphabet[v.Int64()]
	}
	return string(out)
}

func randomID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("zixin: entropy source failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// NewSessionManager returns a manager that expires sessions after ttl.
func NewSessionManager(ttl time.Duration) *SessionManager {
	m := &SessionManager{
		sessions: map[string]*Session{},
		byCode:   map[string]*Session{},
		ttl:      ttl,
	}
	go m.reaper()
	return m
}

func (m *SessionManager) reaper() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for range t.C {
		now := time.Now()
		m.mu.Lock()
		for id, s := range m.sessions {
			if now.After(s.ExpiresAt) {
				delete(m.sessions, id)
				delete(m.byCode, s.Code)
			}
		}
		m.mu.Unlock()
	}
}

// Create allocates a fresh session with a unique code.
func (m *SessionManager) Create() *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	var s *Session
	for attempt := 0; attempt < 64; attempt++ {
		code := randomCode(6)
		if _, exists := m.byCode[code]; exists {
			continue
		}
		s = &Session{
			ID:        randomID(),
			Code:      code,
			CreatedAt: time.Now(),
			ExpiresAt: time.Now().Add(m.ttl),
			peers:     map[Role]*Peer{},
		}
		m.sessions[s.ID] = s
		m.byCode[s.Code] = s
		return s
	}
	panic("zixin: unable to allocate a unique session code")
}

// Get returns a session by id.
func (m *SessionManager) Get(id string) *Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[id]
}

// GetByCode returns a session by its short code.
func (m *SessionManager) GetByCode(code string) *Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.byCode[code]
}

// Destroy removes a session and disconnects its peers.
func (m *SessionManager) Destroy(id string) {
	m.mu.Lock()
	s := m.sessions[id]
	delete(m.sessions, id)
	if s != nil {
		delete(m.byCode, s.Code)
	}
	m.mu.Unlock()
	if s == nil {
		return
	}
	s.mu.Lock()
	for _, p := range s.peers {
		p.close()
	}
	s.peers = map[Role]*Peer{}
	s.mu.Unlock()
}

// Join attaches a peer to a session under the given role.
func (m *SessionManager) Join(s *Session, p *Peer) error {
	s.mu.Lock()
	if existing, taken := s.peers[p.Role]; taken && existing != nil {
		s.mu.Unlock()
		return errors.New("role already occupied")
	}
	s.peers[p.Role] = p
	other := s.peers[RoleSender]
	if other == nil {
		other = s.peers[RoleReceiver]
	}
	backlog := s.takeBacklogLocked()
	s.mu.Unlock()

	// Flush anything the other side sent before we joined.
	for _, b := range backlog {
		_ = p.writeFrame(b.kind, b.data)
	}

	if other != nil && other != p {
		_ = other.writeJSON(map[string]any{
			"type": "peer-joined",
			"from": p.Role,
			"to":   other.Role,
			"payload": map[string]any{
				"role":        p.Role,
				"fingerprint": p.Fingerprint,
				"alias":       p.Alias,
			},
		})
	}
	return nil
}

// takeBacklogLocked returns and clears the buffered frames. The caller must
// hold s.mu.
func (s *Session) takeBacklogLocked() []wsFrame {
	out := s.backlog
	s.backlog = nil
	return out
}

// Leave detaches a peer from its session.
func (m *SessionManager) Leave(s *Session, p *Peer) {
	s.mu.Lock()
	if cur, ok := s.peers[p.Role]; ok && cur == p {
		delete(s.peers, p.Role)
	}
	other := s.peers[RoleSender]
	if other == nil {
		other = s.peers[RoleReceiver]
	}
	s.mu.Unlock()
	if other != nil && other != p {
		_ = other.writeJSON(map[string]any{
			"type": "disconnection",
			"from": p.Role,
			"to":   other.Role,
			"payload": map[string]any{
				"role": p.Role,
			},
		})
	}
}
