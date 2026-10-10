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

	code, err := m.StartPairing("测试手机", "")
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
	again, err := m.StartPairing("另一台", "")
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
	if _, err := m.StartPairing("p", ""); err != nil {
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

	code, err := m.StartPairing("p", "")
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
		if _, _, err := store.Issue("c", ""); err != nil {
			t.Fatalf("Add #%d: %v", i+1, err)
		}
	}
	if _, _, err := store.Issue("c5", ""); !errors.Is(err, ErrClientsFull) {
		t.Errorf("第 5 个 Add: got %v, want ErrClientsFull", err)
	}
	if _, err := m.StartPairing("p", ""); !errors.Is(err, ErrClientsFull) {
		t.Errorf("满员后 StartPairing: got %v, want ErrClientsFull", err)
	}
}

func TestNameTooLong(t *testing.T) {
	m, _ := newTestManager(t, newTestStore(t))
	if _, err := m.StartPairing(strings.Repeat("x", MaxNameLen+1), ""); !errors.Is(err, ErrNameTooLong) {
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
			c, err := m.StartPairing("p", "")
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
	want, _, err := s1.Issue("手机A", "")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, _, err := s1.Issue("手机B", ""); err != nil {
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
	c1, _, _ := s.Issue("a", "")
	c2, _, _ := s.Issue("b", "")

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

// 设备指纹：空串（旧版手机）合法，其余必须恰为 32 位小写 hex。
func TestValidDevID(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", true},
		{strings.Repeat("a", 32), true},
		{"0123456789abcdef0123456789abcdef", true},
		{strings.Repeat("a", 31), false},
		{strings.Repeat("a", 33), false},
		{strings.Repeat("A", 32), false}, // 大写不接受：手机端恒小写，便于日志/展示比对
		{strings.Repeat("g", 32), false},
		{"0123456789abcdef0123456789abcde-", false},
	}
	for _, tc := range cases {
		if got := ValidDevID(tc.in); got != tc.want {
			t.Errorf("ValidDevID(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// 同设备指纹重配：复用既有记录（id 不变）、轮换 token、不新增名额；满额也放行。
func TestIssueRotatesSameDevice(t *testing.T) {
	store := newTestStore(t)
	dev := strings.Repeat("d", 32)

	first, rotated, err := store.Issue("手机A", dev)
	if err != nil {
		t.Fatalf("Issue #1: %v", err)
	}
	if rotated {
		t.Fatal("首次签发不应报告轮换")
	}
	second, rotated, err := store.Issue("手机A（改名）", dev)
	if err != nil {
		t.Fatalf("Issue #2: %v", err)
	}
	if !rotated {
		t.Error("同设备指纹应报告轮换")
	}
	if second.ID != first.ID {
		t.Errorf("ID 变化: %s → %s（同设备必须复用记录）", first.ID, second.ID)
	}
	if second.TokenHex == first.TokenHex {
		t.Error("token 未轮换")
	}
	if second.Name != "手机A（改名）" {
		t.Errorf("Name = %q, want 改名后的值", second.Name)
	}
	if store.Count() != 1 {
		t.Errorf("记录数 = %d, want 1", store.Count())
	}
	// 空名不覆盖既有名称
	third, _, err := store.Issue("", dev)
	if err != nil {
		t.Fatalf("Issue #3: %v", err)
	}
	if third.Name != second.Name {
		t.Errorf("空名覆盖了既有名称: %q", third.Name)
	}
	// 旧 token 失效、新 token 命中
	if _, ok := store.LookupToken(mustToken(t, first)); ok {
		t.Error("轮换后旧 token 仍命中")
	}
	if _, ok := store.LookupToken(mustToken(t, third)); !ok {
		t.Error("轮换后新 token 未命中")
	}
}

// 不同指纹新增记录；空指纹（旧版手机）每次都是新记录。
func TestIssueDistinctDevices(t *testing.T) {
	store := newTestStore(t)
	if _, _, err := store.Issue("A", strings.Repeat("1", 32)); err != nil {
		t.Fatalf("Issue A: %v", err)
	}
	if _, _, err := store.Issue("B", strings.Repeat("2", 32)); err != nil {
		t.Fatalf("Issue B: %v", err)
	}
	if store.Count() != 2 {
		t.Fatalf("记录数 = %d, want 2", store.Count())
	}
	// 空指纹两次 = 两条记录（无法识别为同一台，保持旧行为）
	if _, _, err := store.Issue("C", ""); err != nil {
		t.Fatalf("Issue C: %v", err)
	}
	if _, _, err := store.Issue("C again", ""); err != nil {
		t.Fatalf("Issue C again: %v", err)
	}
	if store.Count() != 4 {
		t.Fatalf("记录数 = %d, want 4", store.Count())
	}
}

// 满额时：新设备拒绝、同设备重配放行（StartPairing 与 Issue 两级都要放行）。
func TestSameDeviceAllowedWhenFull(t *testing.T) {
	store := newTestStore(t)
	m, _ := newTestManager(t, store)
	dev := strings.Repeat("e", 32)

	// 该手机先配对占一个名额（记录里带上指纹）
	code, err := m.StartPairing("手机X", dev)
	if err != nil {
		t.Fatalf("StartPairing(手机X): %v", err)
	}
	if _, err := m.Verify(code); err != nil {
		t.Fatalf("Verify(手机X): %v", err)
	}
	for i := 1; i < DefaultMaxClients; i++ {
		if _, _, err := store.Issue("c", ""); err != nil {
			t.Fatalf("Issue #%d: %v", i, err)
		}
	}
	if store.Count() != DefaultMaxClients {
		t.Fatalf("记录数 = %d, want %d", store.Count(), DefaultMaxClients)
	}
	if store.HasDevID("") {
		t.Error("空指纹不该命中 HasDevID")
	}
	if _, err := m.StartPairing("新手机", strings.Repeat("f", 32)); !errors.Is(err, ErrClientsFull) {
		t.Errorf("满额新设备 StartPairing: got %v, want ErrClientsFull", err)
	}
	if !store.HasDevID(dev) {
		t.Fatal("HasDevID 未命中已配对指纹")
	}
	code2, err := m.StartPairing("同设备", dev)
	if err != nil {
		t.Fatalf("满额同设备 StartPairing: %v", err)
	}
	if _, err := m.Verify(code2); err != nil {
		t.Fatalf("满额同设备 Verify: %v", err)
	}
	if store.Count() != DefaultMaxClients {
		t.Errorf("记录数 = %d, want %d（同设备不得新增）", store.Count(), DefaultMaxClients)
	}
}

// 端到端：同指纹两次配对轮换同一记录的 token（token 变化 + 记录数不变）。
func TestPairingSameDeviceRotates(t *testing.T) {
	store := newTestStore(t)
	m, _ := newTestManager(t, store)
	dev := strings.Repeat("9", 32)

	code1, err := m.StartPairing("手机", dev)
	if err != nil {
		t.Fatalf("StartPairing #1: %v", err)
	}
	c1, err := m.Verify(code1)
	if err != nil {
		t.Fatalf("Verify #1: %v", err)
	}
	code2, err := m.StartPairing("手机", dev)
	if err != nil {
		t.Fatalf("StartPairing #2: %v", err)
	}
	if code2 == code1 {
		t.Fatal("上一轮已结束，第二次发起必须生成新码")
	}
	c2, err := m.Verify(code2)
	if err != nil {
		t.Fatalf("Verify #2: %v", err)
	}
	if c1.ID != c2.ID || c1.TokenHex == c2.TokenHex {
		t.Errorf("同设备重配未复用记录/未轮换 token: %+v → %+v", c1.ID, c2.ID)
	}
	if store.Count() != 1 {
		t.Errorf("记录数 = %d, want 1", store.Count())
	}
}

// 非法指纹在发起阶段即拒绝（会话层映射 ERR(2)+关）。
func TestStartPairingRejectsBadDevID(t *testing.T) {
	store := newTestStore(t)
	m, _ := newTestManager(t, store)
	if _, err := m.StartPairing("p", strings.Repeat("Z", 32)); !errors.Is(err, ErrBadDevID) {
		t.Fatalf("非法指纹: got %v, want ErrBadDevID", err)
	}
}
