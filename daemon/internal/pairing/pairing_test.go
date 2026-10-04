package pairing

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Load(filepath.Join(t.TempDir(), "clients.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return s
}

func newTestManager(t *testing.T, store *Store) (*Manager, *[]string) {
	t.Helper()
	m := NewManager(store)
	m.CodeTTL = 60 * time.Second
	codes := &[]string{}
	m.SetNotify(func(code string) { *codes = append(*codes, code) })
	return m, codes
}

func TestStartAndVerify(t *testing.T) {
	store := newTestStore(t)
	m, notified := newTestManager(t, store)

	code, err := m.StartPairing("测试手机")
	if err != nil {
		t.Fatalf("StartPairing: %v", err)
	}
	if len(code) != CodeLen {
		t.Fatalf("确认码 %q 长度 %d, want %d", code, len(code), CodeLen)
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			t.Fatalf("确认码 %q 含非数字字符", code)
		}
	}
	if len(*notified) != 1 || (*notified)[0] != code {
		t.Fatalf("确认码未送达展示函数: %v", *notified)
	}

	// 幂等：进行中重复发起返回同一码
	again, err := m.StartPairing("另一台")
	if err != nil || again != code {
		t.Fatalf("进行中重复发起: got (%q,%v), want (%q,nil)", again, err, code)
	}

	client, err := m.Verify(code)
	if err != nil {
		t.Fatalf("Verify(正确码): %v", err)
	}
	if client.Name != "测试手机" { // 名称取自会话发起方
		t.Errorf("client.Name = %q, want %q", client.Name, "测试手机")
	}
	if client.PairedAt.IsZero() || time.Since(client.PairedAt) > 5*time.Second {
		t.Errorf("PairedAt 未绑定签发时间: %v", client.PairedAt)
	}
	if tok, err := client.Token(); err != nil || len(tok) != TokenLen {
		t.Errorf("token 长度 %d, want %d (err=%v)", len(tok), TokenLen, err)
	}

	// 消费后本轮作废
	if _, err := m.Verify(code); !errors.Is(err, ErrNoSession) {
		t.Errorf("消费后 Verify: got %v, want ErrNoSession", err)
	}
}

func TestWrongCodeFiveAttemptsLock(t *testing.T) {
	store := newTestStore(t)
	m, _ := newTestManager(t, store)
	if _, err := m.StartPairing("p"); err != nil {
		t.Fatalf("StartPairing: %v", err)
	}

	code, _, _ := m.ActiveCode()
	for i := 1; i <= 4; i++ {
		if _, err := m.Verify(wrongCode(code, i)); !errors.Is(err, ErrWrongCode) {
			t.Fatalf("第 %d 次错码: got %v, want ErrWrongCode", i, err)
		}
	}
	// 第 5 次错码：本轮作废
	if _, err := m.Verify(wrongCode(code, 5)); !errors.Is(err, ErrTooManyAttempts) {
		t.Fatalf("第 5 次错码: got %v, want ErrTooManyAttempts", err)
	}
	// 作废后即使正确码也不通过，须重新发起
	if _, err := m.Verify(code); !errors.Is(err, ErrNoSession) {
		t.Errorf("作废后正确码: got %v, want ErrNoSession", err)
	}
	if store.Count() != 0 {
		t.Errorf("作废轮不应签发 token, count=%d", store.Count())
	}
}

func TestCodeExpiry(t *testing.T) {
	store := newTestStore(t)
	m, _ := newTestManager(t, store)
	m.CodeTTL = 30 * time.Millisecond

	code, err := m.StartPairing("p")
	if err != nil {
		t.Fatalf("StartPairing: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if _, err := m.Verify(code); !errors.Is(err, ErrExpired) {
		t.Fatalf("过期后 Verify: got %v, want ErrExpired", err)
	}
	// 过期会话已清除
	if _, err := m.Verify(code); !errors.Is(err, ErrNoSession) {
		t.Errorf("过期清除后 Verify: got %v, want ErrNoSession", err)
	}
}

func TestVerifyWithoutSession(t *testing.T) {
	m, _ := newTestManager(t, newTestStore(t))
	if _, err := m.Verify("1234"); !errors.Is(err, ErrNoSession) {
		t.Errorf("got %v, want ErrNoSession", err)
	}
}

func TestMaxFourClients(t *testing.T) {
	store := newTestStore(t)
	m, _ := newTestManager(t, store)
	for i := 0; i < DefaultMaxClients; i++ {
		if _, err := store.Add("c"); err != nil {
			t.Fatalf("Add #%d: %v", i+1, err)
		}
	}
	if _, err := store.Add("c5"); !errors.Is(err, ErrClientsFull) {
		t.Errorf("第 5 个 Add: got %v, want ErrClientsFull", err)
	}
	if _, err := m.StartPairing("p"); !errors.Is(err, ErrClientsFull) {
		t.Errorf("满员后 StartPairing: got %v, want ErrClientsFull", err)
	}
}

func TestNameTooLong(t *testing.T) {
	m, _ := newTestManager(t, newTestStore(t))
	if _, err := m.StartPairing(strings.Repeat("x", MaxNameLen+1)); !errors.Is(err, ErrNameTooLong) {
		t.Errorf("got %v, want ErrNameTooLong", err)
	}
}

func TestConcurrentPairingMutualExclusion(t *testing.T) {
	store := newTestStore(t)
	m, _ := newTestManager(t, store)

	const n = 20
	codes := make([]string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c, err := m.StartPairing("p")
			if err != nil {
				t.Errorf("并发 StartPairing #%d: %v", i, err)
				return
			}
			codes[i] = c
		}(i)
	}
	wg.Wait()
	for i, c := range codes {
		if c == "" || c != codes[0] {
			t.Fatalf("并发会话出现多个确认码: codes[%d]=%q codes[0]=%q", i, c, codes[0])
		}
	}

	// 并发提交正确码：恰有一个成功签发
	var success atomic.Int64
	var verifyWG sync.WaitGroup
	for i := 0; i < n; i++ {
		verifyWG.Add(1)
		go func() {
			defer verifyWG.Done()
			if _, err := m.Verify(codes[0]); err == nil {
				success.Add(1)
			}
		}()
	}
	verifyWG.Wait()
	if got := success.Load(); got != 1 {
		t.Errorf("并发 Verify 成功次数 = %d, want 1", got)
	}
	if store.Count() != 1 {
		t.Errorf("签发客户端数 = %d, want 1", store.Count())
	}
}

func TestStoreRoundTripAndPerm(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clients.json")

	s1, err := Load(path)
	if err != nil {
		t.Fatalf("Load(新): %v", err)
	}
	want, err := s1.Add("手机A")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := s1.Add("手机B"); err != nil {
		t.Fatalf("Add B: %v", err)
	}

	// 权限必须 0600（不依赖 umask）
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("存储文件权限 = %04o, want 0600", got)
	}

	// 往返：重载后 token 精确可用
	s2, err := Load(path)
	if err != nil {
		t.Fatalf("Load(重载): %v", err)
	}
	if s2.Count() != 2 {
		t.Fatalf("重载 count = %d, want 2", s2.Count())
	}
	tok, err := want.Token()
	if err != nil {
		t.Fatalf("Token(): %v", err)
	}
	got, ok := s2.LookupToken(tok)
	if !ok || got.ID != want.ID || got.Name != want.Name || !got.PairedAt.Equal(want.PairedAt) {
		t.Errorf("token 往返不一致: got %+v ok=%v, want %+v", got, ok, want)
	}
	if _, ok := s2.LookupToken(make([]byte, TokenLen)); ok {
		t.Error("错误 token 不应命中")
	}
}

func TestLoadWarnsLoosePerm(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clients.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"clients":[]}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	warns := s.Warnings()
	if len(warns) != 1 || !strings.Contains(warns[0], "0600") {
		t.Errorf("权限告警 = %v, want 恰一条含 0600", warns)
	}
}

func TestStoreRemove(t *testing.T) {
	s := newTestStore(t)
	c1, _ := s.Add("a")
	c2, _ := s.Add("b")

	if _, ok, err := s.Remove(c1.ID); err != nil || !ok {
		t.Fatalf("Remove(%s): ok=%v err=%v", c1.ID, ok, err)
	}
	if _, ok, err := s.Remove("不存在"); err != nil || ok {
		t.Errorf("Remove(不存在): ok=%v err=%v", ok, err)
	}
	if s.Count() != 1 {
		t.Fatalf("count = %d, want 1", s.Count())
	}
	if _, ok := s.LookupToken(mustToken(t, c2)); !ok {
		t.Error("未删除的客户端 token 应仍命中")
	}
	if _, ok := s.LookupToken(mustToken(t, c1)); ok {
		t.Error("已删除的客户端 token 不应命中")
	}
}

func wrongCode(code string, delta int) string {
	d := int(code[3]-'0') + delta
	d %= 10
	return code[:3] + string(rune('0'+d))
}

func mustToken(t *testing.T, c Client) []byte {
	t.Helper()
	tok, err := c.Token()
	if err != nil {
		t.Fatalf("Token(): %v", err)
	}
	return tok
}
