package app

import (
	"encoding/binary"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// 子欣互传 announces itself on the local network as a DNS-SD service and
// browses for peers doing the same. The implementation is deliberately
// self-contained: only the subset of DNS/MDNS needed for one service type
// is parsed.
//
// Service type: _zixin._tcp.local

const (
	mdnsGroup      = "224.0.0.251"
	mdnsPort       = 5353
	mdnsService    = "_zixin._tcp.local"
	mdnsTTL        = 120
	announceEvery  = 45 * time.Second
	browseEvery    = 5 * time.Second
	entryTTLExpiry = 150 * time.Second
)

var (
	typePTR uint16 = 12
	typeTXT uint16 = 16
	typeA   uint16 = 1
	typeSRV uint16 = 33
)

// MDNSEntry is one peer discovered on the local network.
type MDNSEntry struct {
	Instance    string            `json:"instance"`
	Host        string            `json:"host"`
	IPv4        string            `json:"ipv4"`
	Port        int               `json:"port"`
	TXT         map[string]string `json:"txt"`
	Fingerprint string            `json:"fingerprint"`
	Alias       string            `json:"alias"`
	Platform    string            `json:"platform"`
	LastSeen    int64             `json:"lastSeen"`
	Source      string            `json:"source,omitempty"`
}

// MDNS runs the announcement and discovery loops for one node.
type MDNS struct {
	instance string
	hostname string
	port     int
	fp       string
	alias    string
	platform string

	pc     *net.UDPConn
	tx     *net.UDPConn
	local  net.IP
	ifaces []net.Interface

	mu      sync.RWMutex
	entries map[string]*MDNSEntry

	stop chan struct{}
	once sync.Once
}

// NewMDNS prepares a broadcaster/browser. Call Start to run the loops.
func NewMDNS(port int, fingerprint, alias, platform string) (*MDNS, error) {
	iface, host, err := primaryInterface()
	if err != nil {
		return nil, err
	}
	m := &MDNS{
		instance: "Zixin-" + fingerprint[:min(6, len(fingerprint))],
		hostname: "zx-" + fingerprint[:min(8, len(fingerprint))],
		port:     port,
		fp:       fingerprint,
		alias:    alias,
		platform: platform,
		local:    iface,
		ifaces:   host,
		entries:  map[string]*MDNSEntry{},
		stop:     make(chan struct{}),
	}
	return m, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// primaryInterface picks the interface that carries the default route so
// announcements travel on the network the user actually cares about.
func primaryInterface() (net.IP, []net.Interface, error) {
	conn, err := net.Dial("udp4", "8.8.8.8:53")
	if err != nil {
		return nil, nil, err
	}
	local := conn.LocalAddr().(*net.UDPAddr).IP
	_ = conn.Close()

	var keep []net.Interface
	ifaces, err := net.Interfaces()
	if err != nil {
		return local, nil, err
	}
	for _, i := range ifaces {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagMulticast == 0 {
			continue
		}
		addrs, err := i.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.Equal(local) {
				keep = append(keep, i)
			}
		}
	}
	if len(keep) == 0 {
		for _, i := range ifaces {
			if i.Flags&net.FlagUp != 0 && i.Flags&net.FlagMulticast != 0 {
				keep = append(keep, i)
			}
		}
	}
	return local, keep, nil
}

// Start binds the multicast socket and launches the announce/browse loops.
//
// Everything is exchanged over the group address: queries, announcements and
// replies alike. That keeps a single socket per node (Go's net package has
// no portable multicast-join primitive without x/net/ipv4) and guarantees
// that every 子欣互传 node on the segment sees every reply.
func (m *MDNS) Start() error {
	gaddr := &net.UDPAddr{IP: net.ParseIP(mdnsGroup), Port: mdnsPort}
	var ifi *net.Interface
	if len(m.ifaces) > 0 {
		ifi = &m.ifaces[0]
	}
	pc, err := net.ListenMulticastUDP("udp4", ifi, gaddr)
	if err != nil {
		return fmt.Errorf("mdns listen: %w", err)
	}
	m.pc = pc

	// The sender is bound to this node's LAN address so that Windows and
	// Linux route the multicast datagram onto the interface the user is
	// actually on, instead of letting the stack pick a virtual adapter.
	local := &net.UDPAddr{IP: m.local, Port: 0}
	tx, err := net.DialUDP("udp4", local, gaddr)
	if err != nil {
		// Fall back to an unbound sender; some stacks route it correctly.
		tx, err = net.DialUDP("udp4", nil, gaddr)
		if err != nil {
			_ = pc.Close()
			return fmt.Errorf("mdns dial: %w", err)
		}
	}
	m.tx = tx

	go m.announceLoop()
	go m.browseLoop()
	go m.readLoop()
	go m.expireLoop()
	return nil
}

// reuseAddrControl lets several 子欣互传 nodes share port 5353 on one host,
// which is required for local testing with multiple instances.
func reuseAddrControl(network, address string, c syscall.RawConn) error {
	var sockErr error
	if err := c.Control(func(fd uintptr) {
		sockErr = setReuseAddr(fd)
	}); err != nil {
		return err
	}
	return sockErr
}

// Stop shuts the mDNS engine down.
func (m *MDNS) Stop() {
	m.once.Do(func() {
		close(m.stop)
		if m.pc != nil {
			_ = m.pc.Close()
		}
		if m.tx != nil {
			_ = m.tx.Close()
		}
	})
}

// Entries returns the live discovery table.
func (m *MDNS) Entries() []MDNSEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]MDNSEntry, 0, len(m.entries))
	for _, e := range m.entries {
		out = append(out, *e)
	}
	return out
}

// ---------------------------------------------------------------- announce

func (m *MDNS) announceLoop() {
	jitter := time.Duration(rand.Intn(3000)) * time.Millisecond
	select {
	case <-time.After(jitter):
	case <-m.stop:
		return
	}
	for {
		m.announce()
		select {
		case <-time.After(announceEvery + time.Duration(rand.Intn(5000))*time.Millisecond):
		case <-m.stop:
			return
		}
	}
}

func (m *MDNS) announce() {
	pkt := m.buildAnnouncement()
	_ = m.tx.SetWriteDeadline(time.Now().Add(2 * time.Second))
	n, err := m.tx.Write(pkt)
	if err != nil {
		log.Printf("mdns: announce failed: %v", err)
		return
	}
	log.Printf("mdns: announced %d bytes (%d records) from %s", n, 4, m.tx.LocalAddr())
}

func (m *MDNS) buildAnnouncement() []byte {
	var body []byte
	body = append(body, encodeName(mdnsService)...)

	// PTR: service -> instance
	body = append(body, uint16Bytes(typePTR, 1)...)
	body = append(body, uint32Bytes(mdnsTTL)...)
	rdata := encodeName(m.instance + ".local")
	body = append(body, uint16Bytes(uint16(len(rdata)), 0)...)
	body = append(body, rdata...)

	// SRV: instance -> host
	body = append(body, encodeName(m.instance+".local")...)
	body = append(body, uint16Bytes(typeSRV, 1)...)
	body = append(body, uint32Bytes(mdnsTTL)...)
	srv := []byte{0, 0, 0, 0}
	srv = append(srv, uint16Bytes(uint16(m.port), 0)...)
	srv = append(srv, encodeName(m.hostname+".local")...)
	body = append(body, uint16Bytes(uint16(len(srv)), 0)...)
	body = append(body, srv...)

	// TXT: metadata
	body = append(body, encodeName(m.instance+".local")...)
	body = append(body, uint16Bytes(typeTXT, 1)...)
	body = append(body, uint32Bytes(mdnsTTL)...)
	var txt []byte
	for _, kv := range []string{
		"v=1",
		"fp=" + m.fp,
		"alias=" + m.alias,
		"plat=" + m.platform,
		"port=" + strconv.Itoa(m.port),
	} {
		txt = append(txt, byte(len(kv)))
		txt = append(txt, kv...)
	}
	body = append(body, uint16Bytes(uint16(len(txt)), 0)...)
	body = append(body, txt...)

	// A: host -> ipv4
	if ip4 := m.local.To4(); ip4 != nil {
		body = append(body, encodeName(m.hostname+".local")...)
		body = append(body, uint16Bytes(typeA, 1)...)
		body = append(body, uint32Bytes(mdnsTTL)...)
		body = append(body, uint16Bytes(4, 0)...)
		body = append(body, ip4...)
	}

	head := []byte{}
	head = append(head, 0x00, 0x00) // id
	head = append(head, 0x84, 0x00) // authoritative response
	head = append(head, 0x00, 0x00) // qdcount
	head = append(head, uint16Bytes(4, 0)...)
	head = append(head, 0x00, 0x00) // nscount
	head = append(head, 0x00, 0x00) // arcount
	return append(head, body...)
}

// ------------------------------------------------------------------ browse

func (m *MDNS) browseLoop() {
	for {
		m.browse()
		select {
		case <-time.After(browseEvery):
		case <-m.stop:
			return
		}
	}
}

func (m *MDNS) browse() {
	pkt := []byte{}
	pkt = append(pkt, 0x00, 0x00)
	pkt = append(pkt, 0x00, 0x00) // standard query
	pkt = append(pkt, 0x00, 0x01) // qdcount
	pkt = append(pkt, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00)
	pkt = append(pkt, encodeName(mdnsService)...)
	pkt = append(pkt, uint16Bytes(typePTR, 0)...)
	pkt = append(pkt, uint16Bytes(1, 0)...)
	_ = m.tx.SetWriteDeadline(time.Now().Add(2 * time.Second))
	_, _ = m.tx.Write(pkt)
}

// ---------------------------------------------------------------- read loop

func (m *MDNS) readLoop() {
	buf := make([]byte, 4096)
	for {
		select {
		case <-m.stop:
			return
		default:
		}
		_ = m.pc.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, addr, err := m.pc.ReadFromUDP(buf)
		if err != nil {
			continue
		}
		log.Printf("mdns: received %d bytes from %s", n, addr)
		m.handlePacket(buf[:n])
	}
}

func (m *MDNS) handlePacket(pkt []byte) {
	if len(pkt) < 12 {
		return
	}
	flags := binary.BigEndian.Uint16(pkt[2:4])
	isResponse := flags&0x8000 != 0
	qd := int(binary.BigEndian.Uint16(pkt[4:6]))
	an := int(binary.BigEndian.Uint16(pkt[6:8]))
	ns := int(binary.BigEndian.Uint16(pkt[8:10]))
	ar := int(binary.BigEndian.Uint16(pkt[10:12]))
	off := 12

	queries := make([]string, 0, qd)
	for i := 0; i < qd; i++ {
		name, next, ok := parseName(pkt, off)
		if !ok {
			return
		}
		off = next + 4
		queries = append(queries, name)
	}

	if !isResponse {
		// A query: answer it with our full announcement so the asker can
		// learn our endpoint without waiting for the next periodic beacon.
		for _, q := range queries {
			if q == mdnsService || strings.HasSuffix(q, mdnsService) {
				m.announce()
				break
			}
		}
		return
	}

	total := an + ns + ar
	records := make([]dnsRecord, 0, total)
	for i := 0; i < total; i++ {
		rec, next, ok := parseRecord(pkt, off)
		if !ok {
			return
		}
		records = append(records, rec)
		off = next
	}
	m.mergeRecords(records)
}

type dnsRecord struct {
	name  string
	rtype uint16
	ttl   uint32
	rdata []byte
}

func (m *MDNS) mergeRecords(records []dnsRecord) {
	// First pass: PTR records tell us which instances exist.
	instances := map[string]bool{}
	for _, r := range records {
		if r.rtype == typePTR && strings.HasSuffix(r.name, mdnsService) {
			inst, _, ok := parseName(r.rdata, 0)
			if ok {
				instances[inst] = true
			}
		}
	}
	if len(instances) == 0 {
		return
	}

	for inst := range instances {
		e := &MDNSEntry{
			Instance: inst,
			TXT:      map[string]string{},
			LastSeen: time.Now().UnixMilli(),
			Source:   "mdns",
		}
		for _, r := range records {
			if r.name != inst {
				continue
			}
			switch r.rtype {
			case typeSRV:
				if len(r.rdata) >= 6 {
					e.Port = int(binary.BigEndian.Uint16(r.rdata[4:6]))
					if host, _, ok := parseName(r.rdata, 6); ok {
						e.Host = host
					}
				}
			case typeTXT:
				for _, kv := range parseTXT(r.rdata) {
					k, v, found := strings.Cut(kv, "=")
					if found {
						e.TXT[k] = v
					}
				}
			case typeA:
				if len(r.rdata) == 4 {
					e.IPv4 = net.IP(r.rdata).String()
				}
			}
		}
		if e.Fingerprint == "" {
			e.Fingerprint = e.TXT["fp"]
		}
		if e.Alias == "" {
			e.Alias = e.TXT["alias"]
		}
		if e.Platform == "" {
			e.Platform = e.TXT["plat"]
		}
		if e.Fingerprint == "" || e.IPv4 == "" || e.Port == 0 {
			continue
		}
		if e.Fingerprint == m.fp {
			continue // our own announcement
		}
		m.mu.Lock()
		m.entries[e.Fingerprint] = e
		m.mu.Unlock()
	}
}

func (m *MDNS) expireLoop() {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-t.C:
			cutoff := time.Now().Add(-entryTTLExpiry).UnixMilli()
			m.mu.Lock()
			for k, e := range m.entries {
				if e.LastSeen < cutoff {
					delete(m.entries, k)
				}
			}
			m.mu.Unlock()
		}
	}
}

// ------------------------------------------------------------ DNS encoding

func uint16Bytes(v uint16, _ int) []byte {
	return []byte{byte(v >> 8), byte(v)}
}

func uint32Bytes(v uint32) []byte {
	return []byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}
}

func encodeName(name string) []byte {
	var out []byte
	name = strings.TrimSuffix(name, ".")
	for _, label := range strings.Split(name, ".") {
		if label == "" {
			continue
		}
		out = append(out, byte(len(label)))
		out = append(out, label...)
	}
	return append(out, 0x00)
}

func parseName(b []byte, off int) (string, int, bool) {
	var labels []string
	jumped := false
	orig := off
	limit := 0
	for {
		if off >= len(b) {
			return "", 0, false
		}
		l := int(b[off])
		if l == 0 {
			off++
			if !jumped {
				orig = off
			}
			break
		}
		if l&0xC0 == 0xC0 {
			if off+1 >= len(b) {
				return "", 0, false
			}
			ptr := int(binary.BigEndian.Uint16(b[off:off+2]) & 0x3FFF)
			if !jumped {
				orig = off + 2
				jumped = true
			}
			off = ptr
			limit++
			if limit > 32 {
				return "", 0, false
			}
			continue
		}
		off++
		if off+l > len(b) {
			return "", 0, false
		}
		labels = append(labels, string(b[off:off+l]))
		off += l
	}
	return strings.Join(labels, "."), orig, true
}

func parseRecord(b []byte, off int) (dnsRecord, int, bool) {
	name, next, ok := parseName(b, off)
	if !ok || next+10 > len(b) {
		return dnsRecord{}, 0, false
	}
	rtype := binary.BigEndian.Uint16(b[next : next+2])
	ttl := binary.BigEndian.Uint32(b[next+4 : next+8])
	rdlen := int(binary.BigEndian.Uint16(b[next+8 : next+10]))
	off = next + 10
	if off+rdlen > len(b) {
		return dnsRecord{}, 0, false
	}
	rdata := make([]byte, rdlen)
	copy(rdata, b[off:off+rdlen])
	return dnsRecord{name: name, rtype: rtype, ttl: ttl, rdata: rdata}, off + rdlen, true
}

func parseTXT(rdata []byte) []string {
	var out []string
	for i := 0; i < len(rdata); {
		l := int(rdata[i])
		i++
		if i+l > len(rdata) {
			break
		}
		out = append(out, string(rdata[i:i+l]))
		i += l
	}
	return out
}

// hostLabel returns a hostname-safe label for this machine.
func hostLabel() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "flux"
	}
	var b strings.Builder
	for _, r := range h {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + 32)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}
