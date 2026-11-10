package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeFilename(t *testing.T) {
	cases := map[string]string{
		"../../etc/passwd":   "_.._etc_passwd",
		`C:\Windows\notepad`: "C:_Windows_notepad",
		"  空格 开头.bin":        "空格 开头.bin",
		"...hidden":          "hidden",
		"":                   "unnamed",
	}
	for in, want := range cases {
		if got := sanitizeFilename(in); got != want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", in, got, want)
		}
	}
	// 超长文件名必须保留扩展名
	long := strings.Repeat("a", 300) + ".zip"
	got := sanitizeFilename(long)
	if len(got) > 180 || !strings.HasSuffix(got, ".zip") {
		t.Errorf("long name not trimmed correctly: len=%d suffix=%q", len(got), filepath.Ext(got))
	}
}

func TestUniquePathDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(p, []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	next := uniquePath(p)
	if next == p {
		t.Fatalf("uniquePath returned the existing path %q", p)
	}
	if filepath.Dir(next) != dir {
		t.Fatalf("uniquePath escaped the directory: %q", next)
	}
	if !strings.HasSuffix(next, "(1).txt") {
		t.Fatalf("unexpected unique name: %q", next)
	}
	// 第二次仍然不能冲突
	if err := os.WriteFile(next, []byte("2"), 0o600); err != nil {
		t.Fatal(err)
	}
	third := uniquePath(p)
	if third == p || third == next {
		t.Fatalf("third candidate collides: %q", third)
	}
}

func TestParseFileExtra(t *testing.T) {
	raw := `{"id":"abc","name":"x.bin","size":3,"sha256":"ff","mime":"","url":"/api/chat/file/abc"}`
	e, err := parseFileExtra(raw)
	if err != nil {
		t.Fatalf("parseFileExtra: %v", err)
	}
	if e.ID != "abc" || e.Name != "x.bin" || e.Size != 3 {
		t.Fatalf("unexpected parse: %+v", e)
	}
	if _, err := parseFileExtra("not json"); err == nil {
		t.Fatal("expected error for malformed extra")
	}
}

func TestSessionBacklogIsBoundedAndOrdered(t *testing.T) {
	s := &Session{peers: map[Role]*Peer{}}
	for i := 0; i < 5; i++ {
		s.pushBacklog(1, []byte{byte(i)})
	}
	got := s.takeBacklog()
	if len(got) != 5 {
		t.Fatalf("backlog size = %d, want 5", len(got))
	}
	for i, f := range got {
		if f.kind != 1 || len(f.data) != 1 || f.data[0] != byte(i) {
			t.Fatalf("frame %d corrupted: %+v", i, f)
		}
	}
	if again := s.takeBacklog(); len(again) != 0 {
		t.Fatalf("backlog not drained: %d", len(again))
	}
}

func TestRegistryAliasOverrideWins(t *testing.T) {
	dir := t.TempDir()
	r := NewRegistry(filepath.Join(dir, "trusted.json"))
	fp := "0123456789abcdef0123456789abcdef"

	r.Upsert(Device{Fingerprint: fp, Alias: "对方的自称", Platform: "windows", Address: "10.0.0.9", Port: 8080, Channel: "lan", Online: true})
	if got := r.List()[0].DisplayName(); got != "对方的自称" {
		t.Fatalf("DisplayName = %q", got)
	}
	if _, ok := r.Rename(fp, "客厅台式机"); !ok {
		t.Fatal("Rename failed")
	}
	d := r.List()[0]
	if d.DisplayName() != "客厅台式机" {
		t.Fatalf("override not applied: %q", d.DisplayName())
	}
	// 清空备注后回落到对方自称
	r.Rename(fp, "")
	if got := r.List()[0].DisplayName(); got != "对方的自称" {
		t.Fatalf("override not cleared: %q", got)
	}
	// 重新加载（模拟重启）后备注仍在
	r.Rename(fp, "书房机")
	r2 := NewRegistry(filepath.Join(dir, "trusted.json"))
	if len(r2.List()) != 1 || r2.List()[0].DisplayName() != "书房机" {
		t.Fatalf("persisted alias lost: %+v", r2.List())
	}
}

func TestChatStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chat.json")
	st := loadChatStore(path)
	msgs := []ChatMessage{
		{ID: "1", From: "a", Alias: "A", Body: "你好", Kind: "text", Timestamp: 1},
		{ID: "2", From: "b", Alias: "B", Body: "file.bin", Kind: "file", Extra: `{"url":"/x"}`, Timestamp: 2},
	}
	st.save(msgs)
	got := loadChatStore(path).load()
	if len(got) != 2 || got[0].Body != "你好" || got[1].Kind != "file" {
		t.Fatalf("round trip mismatch: %+v", got)
	}
}
