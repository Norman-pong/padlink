package session

import (
	"testing"
	"time"

	"padlink/daemon/internal/proto"
)

// ---- 纯逻辑：裁决表 ----

func TestArbiterSingleDevice(t *testing.T) {
	a := newControlArbiter(time.Second, time.Second)
	now := time.Unix(1000, 0)
	for i := 0; i < 3; i++ {
		if res := a.admit("A", now); !res.Allowed {
			t.Fatalf("首个设备第 %d 次事件被拒", i+1)
		}
		now = now.Add(10 * time.Millisecond)
	}
	if a.primaryID() != "A" {
		t.Fatalf("持权设备 = %q, want A", a.primaryID())
	}
}

func TestArbiterBlocksSecondDevice(t *testing.T) {
	a := newControlArbiter(15*time.Second, time.Second)
	now := time.Unix(1000, 0)
	a.admit("A", now)

	// 持权设备 1s 内有操作 → B 被拒 + 首次提示
	res := a.admit("B", now.Add(10*time.Millisecond))
	if res.Allowed {
		t.Fatal("持权设备操作中，第二设备不应放行")
	}
	if !res.FirstDenied || res.RetryAfter != 15*time.Second {
		t.Fatalf("首次被拒 = %+v, want {FirstDenied:true RetryAfter:15s}", res)
	}
	// 冷静期内再试：仍被拒，但不再重复提示
	res = a.admit("B", now.Add(2*time.Second))
	if res.Allowed || res.FirstDenied {
		t.Fatalf("冷静期内 = %+v, want 被拒且不重复提示", res)
	}
	if res.RetryAfter <= 0 || res.RetryAfter > 15*time.Second {
		t.Fatalf("剩余冷静期 = %v, want (0,15s]", res.RetryAfter)
	}
	if a.primaryID() != "A" {
		t.Fatalf("持权设备被顶替: %q", a.primaryID())
	}
}

func TestArbiterTakeoverWhenPrimaryIdle(t *testing.T) {
	cooldown, idle := 100*time.Millisecond, 20*time.Millisecond
	a := newControlArbiter(cooldown, idle)
	now := time.Unix(1000, 0)
	a.admit("A", now)
	// A 操作中 → B 被拒并进入冷静期
	if res := a.admit("B", now); res.Allowed {
		t.Fatal("A 操作中 B 不应放行")
	}
	// 冷静期满 + A 空闲 → B 接管
	okAt := now.Add(cooldown + idle + time.Millisecond)
	if res := a.admit("B", okAt); !res.Allowed {
		t.Fatalf("冷静期满且 A 空闲，B 应接管，实得 %+v", res)
	}
	if a.primaryID() != "B" {
		t.Fatalf("持权设备 = %q, want B", a.primaryID())
	}
	// 角色互换：A 现在成了第二设备
	if res := a.admit("A", okAt.Add(time.Millisecond)); res.Allowed {
		t.Fatal("接管后 A 应立即成为被拒方")
	}
	if a.primaryID() != "B" {
		t.Fatalf("持权设备被顶替: %q", a.primaryID())
	}
}

func TestArbiterReBlocksWhilePrimaryActive(t *testing.T) {
	cooldown, idle := 50*time.Millisecond, 20*time.Millisecond
	a := newControlArbiter(cooldown, idle)
	now := time.Unix(1000, 0)
	a.admit("A", now)
	a.admit("B", now) // 首次被拒

	// A 持续操作到冷静期满之后：B 再次被拒，并重新进入冷静期（再次提示）
	late := now.Add(cooldown + time.Millisecond)
	a.admit("A", late)
	res := a.admit("B", late)
	if res.Allowed {
		t.Fatal("A 仍在操作，B 不应放行")
	}
	if !res.FirstDenied || res.RetryAfter != cooldown {
		t.Fatalf("重新进入冷静期 = %+v", res)
	}
}

// 按住的按键/按钮同样算"在操作"：A 空闲但按住 Ctrl 时，B 不能接管。
func TestArbiterHeldKeysKeepPriority(t *testing.T) {
	cooldown, idle := 30*time.Millisecond, 10*time.Millisecond
	a := newControlArbiter(cooldown, idle)
	now := time.Unix(1000, 0)
	a.admit("A", now)
	a.admit("B", now)
	a.setHeld("A", true)

	res := a.admit("B", now.Add(cooldown+idle+time.Second))
	if res.Allowed {
		t.Fatal("A 仍按住按键，B 不应接管")
	}
	a.setHeld("A", false)
	if res := a.admit("B", now.Add(cooldown+idle+2*time.Second)); !res.Allowed {
		t.Fatalf("A 已松手且空闲，B 应接管，实得 %+v", res)
	}
}

func TestArbiterReleaseOnDisconnect(t *testing.T) {
	a := newControlArbiter(time.Hour, time.Hour)
	now := time.Unix(1000, 0)
	a.admit("A", now)
	a.admit("B", now) // B 进冷静期
	a.release("A")    // A 断开：控制权释放
	if a.primaryID() != "" {
		t.Fatalf("释放后持权设备 = %q, want 空", a.primaryID())
	}
	if res := a.admit("B", now.Add(time.Millisecond)); !res.Allowed {
		t.Fatalf("无持权设备时 B 的首个事件应放行，实得 %+v", res)
	}
	if a.primaryID() != "B" {
		t.Fatalf("持权设备 = %q, want B", a.primaryID())
	}
	if a.isHeld("B") {
		t.Fatal("release 不应把 B 标记为按住（B 从未上报）")
	}
}

// ---- 端到端：两会话 + 注入捕获 + NOTICE ----

// authedClient 完成 HELLO 握手并持有 token 的测试客户端。
type authedClient struct {
	*tcpClient
	token []byte
	seq   uint16
}

func dialAuthed(t *testing.T, port int, token []byte) *authedClient {
	t.Helper()
	c := dialTCP(t, port)
	pl := make([]byte, 2)
	pl[0], pl[1] = 0, byte(proto.Ver)
	c.send(proto.NewRaw(proto.TypeHello, 1, pl))
	if pkt, err := c.recv(2 * time.Second); err != nil || pkt.Type != proto.TypeHello {
		t.Fatalf("HELLO 握手: pkt=%v err=%v", pkt.Type, err)
	}
	return &authedClient{tcpClient: c, token: token, seq: 1}
}

func (c *authedClient) sendSealed(pkt proto.Packet) {
	c.t.Helper()
	proto.Seal(&pkt, c.token)
	c.send(pkt)
}

func (c *authedClient) nextSeq() uint16 {
	c.seq++
	return c.seq
}

// 多设备控制权端到端：A 控制中 B 的输入被丢弃并收到 NOTICE；
// 冷静期满且 A 空闲后 B 的输入放行（接管）。
func TestMultiDevicePriorityEndToEnd(t *testing.T) {
	cooldown, idle := 150*time.Millisecond, 40*time.Millisecond
	ts := newTestServer(t, func(c *Config) {
		c.PreemptCooldown = cooldown
		c.PreemptIdle = idle
	})
	tokA := addClient(t, ts.store, "手机A")
	tokB := addClient(t, ts.store, "手机B")
	a := dialAuthed(t, ts.srv.Port(), tokA)
	b := dialAuthed(t, ts.srv.Port(), tokB)

	// A 先动：取得控制权并真实注入
	a.sendSealed(proto.NewMove(a.nextSeq(), 10, 0))
	waitFor(t, "A 的 MOVE 注入", func() bool { return ts.cap.count(evRel, 0, 10) == 1 })

	// B 立刻动：被拒（不注入），并收到 NOTICE(0, 剩余秒数)
	b.sendSealed(proto.NewMove(b.nextSeq(), 99, 0))
	pkt, err := b.recv(2 * time.Second)
	if err != nil {
		t.Fatalf("等待 NOTICE: %v", err)
	}
	if pkt.Type != proto.TypeNotice {
		t.Fatalf("下行类型 = %v, want NOTICE", pkt.Type)
	}
	if !proto.VerifyHMAC(&pkt, tokB) {
		t.Fatal("NOTICE 未用该客户端 token 封签")
	}
	if pkt.Notice.Code != proto.NoticeControlBusy {
		t.Fatalf("NOTICE code = %d, want %d", pkt.Notice.Code, proto.NoticeControlBusy)
	}
	if pkt.Notice.Arg == 0 || pkt.Notice.Arg > 15 {
		t.Fatalf("NOTICE arg = %d, want (0,15]", pkt.Notice.Arg)
	}
	if n := ts.cap.count(evRel, 0, 99); n != 0 {
		t.Fatalf("被拒设备的 MOVE 被注入 %d 次（应为 0）", n)
	}

	// 冷静期内重试：仍被拒，且不再下发 NOTICE（无下行可读）
	b.sendSealed(proto.NewMove(b.nextSeq(), 98, 0))
	if pkt, err := b.recv(200 * time.Millisecond); err == nil {
		t.Fatalf("冷静期内不应重复下发提示，实得 %v", pkt.Type)
	}
	if n := ts.cap.count(evRel, 0, 98); n != 0 {
		t.Fatalf("冷静期内 MOVE 被注入 %d 次（应为 0）", n)
	}

	// 冷静期满 + A 空闲 → B 接管
	time.Sleep(cooldown + idle + 30*time.Millisecond)
	b.sendSealed(proto.NewMove(b.nextSeq(), 77, 0))
	waitFor(t, "B 接管后的 MOVE 注入", func() bool { return ts.cap.count(evRel, 0, 77) == 1 })

	// 接管后角色互换：A 立即被拒
	a.sendSealed(proto.NewMove(a.nextSeq(), 55, 0))
	if pkt, err := a.recv(2 * time.Second); err != nil || pkt.Type != proto.TypeNotice {
		t.Fatalf("接管后 A 应收到 NOTICE: pkt=%v err=%v", pkt.Type, err)
	}
	if n := ts.cap.count(evRel, 0, 55); n != 0 {
		t.Fatalf("接管后被拒设备的 MOVE 被注入 %d 次（应为 0）", n)
	}
	if ts.srv.StatsSnapshot().Preempted == 0 {
		t.Fatal("Preempted 计数未累加")
	}
}
