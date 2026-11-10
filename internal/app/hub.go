package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// marshal is the single JSON encoder used by the signalling layer so every
// frame keeps the same envelope shape.
func marshal(v any) ([]byte, error) { return json.Marshal(v) }

// Device is a peer visible to this node, either on the local network or
// through the cloud relay.
type Device struct {
	Fingerprint string `json:"fingerprint"`
	Alias       string `json:"alias"`
	// AliasOverride is the name the user gave this device; it wins over the
	// name the peer reports about itself.
	AliasOverride string `json:"aliasOverride,omitempty"`
	Platform      string `json:"platform"`
	Address       string `json:"address,omitempty"`
	Port          int    `json:"port,omitempty"`
	Channel       string `json:"channel"`
	Online        bool   `json:"online"`
	Paired        bool   `json:"paired"`
	LastSeen      int64  `json:"lastSeen"`
}

// DisplayName returns the name to show in the UI.
func (d Device) DisplayName() string {
	if d.AliasOverride != "" {
		return d.AliasOverride
	}
	if d.Alias != "" {
		return d.Alias
	}
	if len(d.Fingerprint) >= 8 {
		return d.Fingerprint[:8]
	}
	return d.Fingerprint
}

// Registry tracks known devices and persists the trusted ("paired") subset.
type Registry struct {
	mu      sync.RWMutex
	devices map[string]*Device // keyed by fingerprint
	trusted map[string]bool

	path string
}

// NewRegistry creates a registry persisting trust state under path.
func NewRegistry(path string) *Registry {
	r := &Registry{
		devices: map[string]*Device{},
		trusted: map[string]bool{},
		path:    path,
	}
	r.load()
	return r
}

type persistedState struct {
	Trusted map[string]bool   `json:"trusted"`
	Devices map[string]Device `json:"devices,omitempty"`
}

func (r *Registry) load() {
	if r.path == "" {
		return
	}
	b, err := os.ReadFile(r.path)
	if err != nil {
		return
	}
	var st persistedState
	if json.Unmarshal(b, &st) != nil {
		return
	}
	for k, v := range st.Trusted {
		if v {
			r.trusted[k] = true
		}
	}
	// Known devices survive a restart, so the device list does not empty out
	// just because nobody answered a scan yet.
	for fp, d := range st.Devices {
		if fp == "" {
			continue
		}
		cp := d
		cp.Fingerprint = fp
		cp.Online = false
		r.devices[fp] = &cp
	}
}

func (r *Registry) save() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.saveLocked()
}

// Upsert records a device sighting and returns the merged record.
func (r *Registry) Upsert(d Device) Device {
	r.mu.Lock()
	defer r.mu.Unlock()
	if d.LastSeen == 0 {
		d.LastSeen = time.Now().UnixMilli()
	}
	cur, ok := r.devices[d.Fingerprint]
	if !ok {
		cp := d
		cp.Paired = r.trusted[d.Fingerprint]
		r.devices[d.Fingerprint] = &cp
		r.saveLocked()
		return cp
	}
	if d.Alias != "" {
		cur.Alias = d.Alias
	}
	if d.Platform != "" {
		cur.Platform = d.Platform
	}
	if d.Address != "" {
		cur.Address = d.Address
	}
	if d.Port != 0 {
		cur.Port = d.Port
	}
	cur.Channel = d.Channel
	cur.Online = d.Online
	cur.LastSeen = d.LastSeen
	cur.Paired = r.trusted[d.Fingerprint]
	r.saveLocked()
	return *cur
}

// Rename stores a user-chosen name for a device. An empty name clears it.
func (r *Registry) Rename(fingerprint, alias string) (Device, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.devices[fingerprint]
	if !ok {
		return Device{}, false
	}
	d.AliasOverride = strings.TrimSpace(alias)
	r.saveLocked()
	return *d, true
}

func (r *Registry) saveLocked() {
	// save() takes the lock itself, so inline the persistence here instead of
	// calling it with the lock held.
	st := persistedState{Trusted: map[string]bool{}, Devices: map[string]Device{}}
	for k, v := range r.trusted {
		st.Trusted[k] = v
	}
	for fp, d := range r.devices {
		keep := d.AliasOverride != "" || st.Trusted[fp] || d.LastSeen > 0
		if !keep {
			continue
		}
		cp := *d
		cp.Online = false
		st.Devices[fp] = cp
	}
	if r.path == "" {
		return
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(r.path), 0o755)
	tmp := r.path + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, r.path)
	}
}

// List returns every known device, newest sighting first.
func (r *Registry) List() []Device {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Device, 0, len(r.devices))
	for _, d := range r.devices {
		out = append(out, *d)
	}
	return out
}

// Trust marks a fingerprint as paired so future transfers skip confirmation.
func (r *Registry) Trust(fingerprint string) {
	r.mu.Lock()
	r.trusted[fingerprint] = true
	if d, ok := r.devices[fingerprint]; ok {
		d.Paired = true
	}
	r.mu.Unlock()
	r.save()
}

// Untrust revokes pairing.
func (r *Registry) Untrust(fingerprint string) {
	r.mu.Lock()
	delete(r.trusted, fingerprint)
	if d, ok := r.devices[fingerprint]; ok {
		d.Paired = false
	}
	r.mu.Unlock()
	r.save()
}

// IsTrusted reports whether a fingerprint is paired.
func (r *Registry) IsTrusted(fingerprint string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.trusted[fingerprint]
}
