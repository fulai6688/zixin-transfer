package app

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"time"
)

// The mDNS channel is the primary discovery path, but it silently fails on
// hosts where inbound UDP 5353 is blocked (Windows firewall default) or where
// the router filters multicast. The subnet scan is the fallback: it speaks
// plain TCP to the node's own HTTP port, which the firewall rule we install
// already allows.
//
// The scan is deliberately conservative: a bounded number of ports, a bounded
// number of hosts, a short connect timeout and a hard deadline.

const (
	scanDialTimeout = 180 * time.Millisecond
	scanHTTPTimeout = 800 * time.Millisecond
	scanMaxHosts    = 1024
	scanWorkers     = 96
	// sweepBudget is the hard budget for one full sweep. It is deliberately
	// independent of the caller's timeout: a second browser asking for a
	// scan must not be able to cut an in-flight sweep short.
	sweepBudget = 20 * time.Second
)

// scanPorts returns the TCP ports worth probing on a neighbour: this node's
// own port, the default ports 子欣互传 uses, and anything we have seen before.
// The range matters because a second instance on the same machine often runs
// on 8081/8082 rather than 8080.
func (s *Server) scanPorts() []int {
	set := map[int]bool{s.cfg.Port: true}
	for p := 8080; p <= 8085; p++ {
		set[p] = true
	}
	for _, d := range s.registry.List() {
		if d.Port > 0 && d.Port < 65536 {
			set[d.Port] = true
		}
	}
	out := make([]int, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Ints(out)
	return out
}

// scanTargets returns the IPv4 addresses to probe: the loopback address
// (so several instances on one machine find each other) plus the /24 of
// every usable LAN interface.
func scanTargets() []string {
	seen := map[string]bool{}
	var out []string
	add := func(ip string) {
		if ip == "" || seen[ip] {
			return
		}
		seen[ip] = true
		out = append(out, ip)
	}

	add("127.0.0.1")

	ifaces, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifi.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipnet.IP.To4()
			if ip4 == nil {
				continue
			}
			ones, bits := ipnet.Mask.Size()
			if bits != 32 {
				// Only handle plain IPv4 networks; anything else is skipped
				// rather than guessed at.
				_ = ones
				continue
			}
			base := ip4.Mask(ipnet.Mask)
			for i := 1; i < 255; i++ {
				probe := make(net.IP, 4)
				copy(probe, base)
				probe[3] = byte(i)
				add(probe.String())
			}
		}
	}
	return out
}

// probeHealth asks a candidate node for its identity.
func probeHealth(ctx context.Context, ip string, port int) (MDNSEntry, bool) {
	dialer := &net.Dialer{Timeout: scanDialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(ip, strconv.Itoa(port)))
	if err != nil {
		return MDNSEntry{}, false
	}
	_ = conn.Close()

	reqCtx, cancel := context.WithTimeout(ctx, scanHTTPTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet,
		"http://"+net.JoinHostPort(ip, strconv.Itoa(port))+"/api/health", nil)
	if err != nil {
		return MDNSEntry{}, false
	}
	client := &http.Client{Timeout: scanHTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return MDNSEntry{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return MDNSEntry{}, false
	}
	var h struct {
		OK       bool   `json:"ok"`
		Version  string `json:"version"`
		Alias    string `json:"alias"`
		Platform string `json:"platform"`
		Device   string `json:"device"`
		Port     int    `json:"port"`
	}
	if json.NewDecoder(resp.Body).Decode(&h) != nil || h.Device == "" {
		return MDNSEntry{}, false
	}
	fp := h.Device
	return MDNSEntry{
		Instance:    "Zixin-" + fp[:min(6, len(fp))],
		Host:        ip,
		IPv4:        ip,
		Port:        h.Port,
		Fingerprint: fp,
		Alias:       h.Alias,
		Platform:    h.Platform,
		LastSeen:    time.Now().UnixMilli(),
		Source:      "scan",
	}, true
}

// scanLAN probes the local network and returns every 子欣互传 node it finds.
//
// Several UI tabs (or two nodes on one machine) ask for a scan at the same
// time, and a full sweep costs a few hundred TCP dials. Requests therefore
// share one in-flight sweep, and a completed sweep result is reused briefly.
func (s *Server) scanLAN(ctx context.Context, _ time.Duration) []MDNSEntry {
	s.scanMu.Lock()
	if time.Since(s.scanAt) < scanCacheFor {
		out := append([]MDNSEntry(nil), s.scanLast...)
		s.scanMu.Unlock()
		return out
	}
	if flight := s.scanFlight; flight != nil {
		s.scanMu.Unlock()
		select {
		case <-flight:
			s.scanMu.Lock()
			out := append([]MDNSEntry(nil), s.scanLast...)
			s.scanMu.Unlock()
			return out
		case <-ctx.Done():
			// The caller gave up waiting; the sweep keeps running for the
			// next request instead of being cancelled with it.
			return nil
		}
	}
	flight := make(chan struct{})
	s.scanFlight = flight
	s.scanMu.Unlock()

	runCtx, cancel := context.WithTimeout(context.Background(), sweepBudget)
	out, complete := s.sweepLAN(runCtx)
	cancel()

	s.scanMu.Lock()
	// Only a sweep that ran to completion is worth caching; a truncated one
	// would hide peers for the whole cache window.
	if complete {
		s.scanLast = out
		s.scanAt = time.Now()
	}
	s.scanFlight = nil
	s.scanMu.Unlock()
	close(flight)

	log.Printf("zixin: subnet scan found %d peer(s), complete=%v", len(out), complete)
	return out
}

// scanCacheFor is how long a completed sweep is reused.
const scanCacheFor = 5 * time.Second

// sweepLAN performs one full subnet sweep. It reports whether the sweep ran
// to completion (false when the budget ran out).
func (s *Server) sweepLAN(ctx context.Context) ([]MDNSEntry, bool) {
	targets := scanTargets()
	ports := s.scanPorts()
	if len(targets) > scanMaxHosts {
		targets = targets[:scanMaxHosts]
	}

	type job struct {
		ip   string
		port int
	}
	jobs := make(chan job, len(targets)*len(ports))
	for _, ip := range targets {
		for _, p := range ports {
			jobs <- job{ip: ip, port: p}
		}
	}
	close(jobs)

	results := make([]MDNSEntry, 0, 8)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < scanWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				if ctx.Err() != nil {
					return
				}
				entry, ok := probeHealth(ctx, j.ip, j.port)
				if !ok {
					continue
				}
				if entry.Fingerprint == s.selfFP {
					continue // ourselves
				}
				mu.Lock()
				results = append(results, entry)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	// De-duplicate by fingerprint. A node reachable over both loopback and
	// its LAN address is kept under the LAN address, which is what other
	// devices (and other tabs on the network) can actually reach.
	byFP := map[string]MDNSEntry{}
	rank := func(e MDNSEntry) int {
		if ip := net.ParseIP(e.IPv4); ip != nil && ip.IsLoopback() {
			return 0
		}
		return 1
	}
	for _, e := range results {
		prev, ok := byFP[e.Fingerprint]
		if !ok || rank(e) > rank(prev) || (rank(e) == rank(prev) && e.LastSeen > prev.LastSeen) {
			byFP[e.Fingerprint] = e
		}
	}
	out := make([]MDNSEntry, 0, len(byFP))
	for _, e := range byFP {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Alias < out[j].Alias })

	complete := ctx.Err() == nil

	// Remember what we saw so the next scan can reuse the ports.
	for _, e := range out {
		s.registry.Upsert(Device{
			Fingerprint: e.Fingerprint,
			Alias:       e.Alias,
			Platform:    e.Platform,
			Address:     e.IPv4,
			Port:        e.Port,
			Channel:     "lan",
			Online:      true,
		})
	}
	return out, complete
}

// handleLANScan answers GET /api/lan/scan with the discovered peers.
func (s *Server) handleLANScan(w http.ResponseWriter, r *http.Request) {
	devices := s.scanLAN(r.Context(), 0)
	if devices == nil {
		devices = []MDNSEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": devices})
}

// handleFirewallInstall triggers the elevated helper that opens the node's
// TCP and UDP ports, including mDNS on 5353.
func (s *Server) handleFirewallInstall(w http.ResponseWriter, r *http.Request) {
	if err := InstallFirewall(); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleFirewallStatus reports whether the firewall rules look present.
func (s *Server) handleFirewallStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"installed": FirewallInstalled(),
		"supported": runtime.GOOS == "windows",
	})
}
