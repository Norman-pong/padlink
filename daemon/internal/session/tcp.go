package session

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"padlink/daemon/internal/proto"
)

var (
	errClose    = errors.New("session: 关闭连接")
	errGraceful = errors.New("session: 对端 BYE")
)

type tcpSession struct {
	srv  *Server
	conn net.Conn

	limiter *rateBucket

	// mu 保护会话态（OnlineClients / 控制权裁决 / NOTICE 下发会跨 goroutine 读取）；
	// 锁序约束：持 mu 时禁止再取 Server.mu（Server.mu → mu 是允许方向）。
	mu sync.Mutex
	// wmu 串行化 conn 写（会话 goroutine 与 NOTICE 下发方并发写同一连接）。
	wmu sync.Mutex

	helloDone bool
	boundID   string // 认证命中的客户端 id（"" 未绑定）
	tokCache  []byte // 绑定后复用 token，避免每包 hex 解码
	txSeq     uint16

	badAuth      int
	dropWinStart time.Time
	dropWinCount int

	// pairWaitUntil 配对进行中的读超时宽限截止（零值=无宽限）。
	// PENDING_CODE 阶段客户端静默，宽限须覆盖码 TTL + 提交余量，否则超时断连即否决用户合法输码窗口。
	pairWaitUntil time.Time

	keysDown map[uint16]struct{} // 已按下未释放的 HID usage
	btnsDown map[uint8]struct{}  // 已按下未释放的按钮号
}

func (s *Server) acceptLoop(ln net.Listener) {
	defer s.internalWG.Done()
	for {
		c, err := ln.Accept()
		if err != nil {
			select {
			case <-s.closed:
			default:
				s.logf("接受 TCP 连接失败: %v", err)
			}
			return
		}
		sess := &tcpSession{
			srv:      s,
			conn:     c,
			limiter:  newRateBucket(s.cfg.RatePerSec, s.cfg.RateBurst),
			keysDown: make(map[uint16]struct{}),
			btnsDown: make(map[uint8]struct{}),
		}
		s.mu.Lock()
		// sessWG.Add 须与注册同临界区：Stop 在锁内快照会话后即 Wait，锁外 Add 违反 WaitGroup 契约
		//（且 Stop 后 finish 的 enqueueBlocking 可能撞上已关闭的 injCh）。
		if s.stopped {
			s.mu.Unlock()
			c.Close()
			return
		}
		s.sessions[sess] = struct{}{}
		s.sessWG.Add(1)
		s.mu.Unlock()
		go sess.run()
	}
}

func (t *tcpSession) run() {
	defer t.finish()
	for {
		frame, err := t.readFrame()
		if err != nil {
			switch {
			case errors.Is(err, net.ErrClosed):
			case errors.Is(err, os.ErrDeadlineExceeded):
				t.srv.logf("会话 %s 读取超时（%v 无任何包），判失联断开", t.conn.RemoteAddr(), t.srv.cfg.ReadTimeout)
			default:
				t.srv.vlogf("会话 %s 结束: %v", t.conn.RemoteAddr(), err)
			}
			return
		}

		now := time.Now()
		if t.rateLimited(now) {
			t.sendErr(ErrCodeRate)
			return
		}
		pkt, err := proto.Decode(frame)
		if err != nil {
			t.srv.stats.Dropped.Add(1)
			t.srv.vlogf("丢弃坏包（%s）: %v", t.conn.RemoteAddr(), err)
			continue
		}
		t.srv.vlogf("TCP ← %s %v seq=%d auth=%v plen=%d", t.conn.RemoteAddr(), pkt.Type, pkt.Seq, pkt.Flags&proto.FlagAuth != 0, len(pkt.Payload))
		if err := t.handle(pkt); err != nil {
			if errors.Is(err, errGraceful) {
				t.srv.vlogf("会话 %s 对端 BYE，优雅关闭", t.conn.RemoteAddr())
			} else if !errors.Is(err, errClose) {
				t.srv.vlogf("会话 %s 处理异常: %v", t.conn.RemoteAddr(), err)
			}
			return
		}
	}
}

// readFrame 按头声明的 payload_len 精确读取一帧。
func (t *tcpSession) readFrame() ([]byte, error) {
	t.conn.SetReadDeadline(time.Now().Add(effectiveReadTimeout(t.srv.cfg.ReadTimeout, t.pairWaitUntil, time.Now())))
	var head [proto.HeaderSize]byte
	if _, err := io.ReadFull(t.conn, head[:]); err != nil {
		return nil, err
	}
	plen := int(binary.BigEndian.Uint16(head[7:9]))
	if plen > proto.MaxPayload {
		return nil, fmt.Errorf("payload_len=%d 超限", plen)
	}
	frame := make([]byte, 0, proto.HeaderSize+plen)
	frame = append(frame, head[:]...)
	if plen > 0 {
		payload := make([]byte, plen)
		if _, err := io.ReadFull(t.conn, payload); err != nil {
			return nil, err
		}
		frame = append(frame, payload...)
	}
	return frame, nil
}

// rateLimited 令牌桶超限时丢弃并计数；1s 窗口内丢弃数超上限返回 true（调用方 ERR+断连）。
func (t *tcpSession) rateLimited(now time.Time) bool {
	if t.limiter.allow(now) {
		return false
	}
	t.srv.stats.Dropped.Add(1)
	if t.dropWinStart.IsZero() || now.Sub(t.dropWinStart) > rateDropWindow {
		t.dropWinStart, t.dropWinCount = now, 0
	}
	t.dropWinCount++
	return t.dropWinCount > t.srv.cfg.RateDropMax
}

// bound 返回认证命中的客户端 id（"" 未绑定）。
func (t *tcpSession) bound() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.boundID
}

// token 返回绑定 token（未绑定为 nil）；仅作只读使用。
func (t *tcpSession) token() []byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.tokCache
}

func (t *tcpSession) dispatch(pkt proto.Packet) error {
	// 多设备控制权：非持权设备的控制事件一律丢弃（TCP/UDP 同闸）；ECHO/BYE 不受门控。
	if isControlType(pkt.Type) && !t.srv.admitControl(t.bound()) {
		return nil
	}
	switch pkt.Type {
	case proto.TypeMove, proto.TypeScroll, proto.TypeButton, proto.TypeKey, proto.TypeText:
		t.enqueueInput(pkt)
	case proto.TypeEcho:
		reply := proto.NewEcho(t.nextSeq(), pkt.Echo.TsMs)
		proto.Seal(&reply, t.token())
		return t.writePkt(reply)
	case proto.TypeBye:
		return errGraceful
	default:
		// PAIR_OK/PAIR_NAK/ERR 不应入站
		t.srv.stats.Dropped.Add(1)
	}
	return nil
}

// finish 会话收尾：补发本会话期间按下未释放的按键与按钮 up（零残留的服务端兜底），
// 再注销并关连接。
func (t *tcpSession) finish() {
	keys, btns := t.takeHeld()
	if len(keys) > 0 || len(btns) > 0 {
		t.srv.releaseInputs(keys, btns, "会话结束兜底")
		t.srv.logf("会话结束兜底：补发 %d 个按键 / %d 个按钮 up", len(keys), len(btns))
	}
	t.srv.mu.Lock()
	delete(t.srv.sessions, t)
	lastOfClient := t.boundID != "" && !t.srv.hasSessionLocked(t.boundID)
	t.srv.mu.Unlock()
	if lastOfClient {
		t.srv.arb.release(t.boundID) // 该客户端已无会话：控制权释放给其他设备
	}
	t.conn.Close()
	t.srv.sessWG.Done()
}

// hasSessionLocked 报告是否还有其他会话绑定该客户端（调用方须持 Server.mu）。
func (s *Server) hasSessionLocked(clientID string) bool {
	for sess := range s.sessions {
		if sess.bound() == clientID {
			return true
		}
	}
	return false
}

func (t *tcpSession) nextSeq() uint16 {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.txSeq++
	return t.txSeq
}

func (t *tcpSession) writePkt(pkt proto.Packet) error {
	buf, err := proto.Encode(&pkt)
	if err != nil {
		return err
	}
	t.wmu.Lock()
	defer t.wmu.Unlock()
	t.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	_, err = t.conn.Write(buf)
	return err
}

func (t *tcpSession) sendErr(code uint8) {
	_ = t.writePkt(proto.NewRaw(proto.TypeErr, t.nextSeq(), []byte{code}))
}

func (t *tcpSession) sendNak(reason uint8) {
	_ = t.writePkt(proto.NewRaw(proto.TypePairNak, t.nextSeq(), []byte{reason}))
}
