package session

import (
	"fmt"
	"time"

	"padlink/daemon/internal/proto"
)

func (t *tcpSession) enqueueInput(pkt proto.Packet) {
	switch pkt.Type {
	case proto.TypeButton:
		t.trackButton(pkt.Button.Btn, pkt.Button.Down)
	case proto.TypeKey:
		t.trackKey(pkt.Key.HIDUsage, pkt.Key.Down)
	}
	if action := t.srv.inputAction(pkt, t.bound(), ""); action != nil {
		t.srv.enqueueBlocking(action)
	}
}

func (s *Server) inputAction(pkt proto.Packet, clientID, source string) func() {
	switch pkt.Type {
	case proto.TypeMove:
		dx, dy := pkt.Move.DX, pkt.Move.DY
		return s.doInj(source+"MOVE", func() error { return s.inj.Move(dx, dy) })
	case proto.TypeScroll:
		dy := pkt.Scroll.DyHiRes
		return s.doInj(source+"SCROLL", func() error { return s.inj.Scroll(dy) })
	case proto.TypeButton:
		btn, down := pkt.Button.Btn, pkt.Button.Down
		return s.doInj(fmt.Sprintf("BUTTON btn=%d", btn), func() error { return s.inj.Button(btn, down) })
	case proto.TypeKey:
		hid, down := pkt.Key.HIDUsage, pkt.Key.Down
		return s.doInj(fmt.Sprintf("KEY hid=0x%04X", hid), func() error { return s.inj.Key(hid, down) })
	case proto.TypeText:
		fn := s.onText
		if fn == nil {
			s.stats.Dropped.Add(1)
			s.vlogf("TEXT 到达但文本注入未配置，丢弃（长度 %d）", len(pkt.Text))
			return nil
		}
		text := pkt.Text
		return s.doInj("TEXT", func() error { return fn(clientID, text) })
	default:
		return nil
	}
}

// admitControl 裁决一次控制事件（MOVE/SCROLL/BUTTON/KEY/TEXT，TCP 与 UDP 共用）；
// 被拒时下发一次 NOTICE 并兜底释放该设备残留的按键（异常时序防御）。
func (s *Server) admitControl(clientID string) bool {
	if clientID == "" {
		return true // 未绑定：不参与多设备裁决
	}
	res := s.arb.admit(clientID, time.Now())
	if res.Allowed {
		return true
	}
	s.stats.Preempted.Add(1)
	if res.FirstDenied {
		s.vlogf("控制权在设备 %s，已拒绝 %s 的输入并进入冷静期（剩余 %.0fs）",
			s.arb.primaryID(), clientID, res.RetryAfter.Seconds())
		s.notifyControlBusy(clientID, res.RetryAfter)
		if s.arb.isHeld(clientID) {
			s.dropHeld(clientID) // 被拒设备若仍按住按键/按钮（竞态兜底），先补发释放
		}
	}
	return false
}

// releaseInputs 兜底补发按住未释放的按键/按钮 up；what 标记来源（零残留红线，入注入队列保序）。
func (s *Server) releaseInputs(keys []uint16, btns []uint8, what string) {
	if len(keys) == 0 && len(btns) == 0 {
		return
	}
	s.enqueueBlocking(func() {
		for _, hid := range keys {
			if err := s.inj.Key(hid, false); err != nil {
				s.logf("%s：按键释放失败 hid=0x%04X: %v", what, hid, err)
			}
		}
		for _, b := range btns {
			if err := s.inj.Button(b, false); err != nil {
				s.logf("%s：按钮释放失败 btn=%d: %v", what, b, err)
			}
		}
	})
}

// dropHeld 释放某设备残留的按键/按钮（被拒时的防御路径，零残留红线）。
func (s *Server) dropHeld(clientID string) {
	s.mu.Lock()
	targets := make([]*tcpSession, 0, 1)
	for sess := range s.sessions {
		if sess.bound() == clientID {
			targets = append(targets, sess)
		}
	}
	s.mu.Unlock()
	for _, sess := range targets {
		if keys, btns := sess.takeHeld(); len(keys) > 0 || len(btns) > 0 {
			s.releaseInputs(keys, btns, "被拒设备残留")
			s.logf("设备 %s 被拒控制权时仍按住输入，已补发释放", clientID)
		}
	}
}

func (s *Server) injectLoop() {
	defer s.internalWG.Done()
	for f := range s.injCh {
		f()
	}
}

// enqueueBlocking 在注入队列入队（TCP 路径：背压保序；服务停止时放行）。
func (s *Server) enqueueBlocking(f func()) {
	select {
	case s.injCh <- f:
	case <-s.closed:
	}
}

// tryEnqueue 非阻塞入队（UDP 路径：满即丢弃计数）。
func (s *Server) tryEnqueue(f func()) bool {
	select {
	case s.injCh <- f:
		return true
	default:
		s.stats.Dropped.Add(1)
		return false
	}
}

// doInj 包装注入调用，失败记录日志（token/文本内容不进日志）。
func (s *Server) doInj(what string, f func() error) func() {
	return func() {
		if err := f(); err != nil {
			s.logf("注入 %s 失败: %v", what, err)
		}
	}
}

// trackButton/trackKey 记录按住状态，"按住数 0↔非0"翻转时同步给控制权裁决器（按住的按键/按钮也算"在操作"）。
func (t *tcpSession) trackButton(btn uint8, down bool) {
	t.mu.Lock()
	before := len(t.keysDown) + len(t.btnsDown)
	if down {
		t.btnsDown[btn] = struct{}{}
	} else {
		delete(t.btnsDown, btn)
	}
	after := len(t.keysDown) + len(t.btnsDown)
	id := t.boundID
	t.mu.Unlock()
	t.syncHeld(id, before, after)
}

func (t *tcpSession) trackKey(hid uint16, down bool) {
	t.mu.Lock()
	before := len(t.keysDown) + len(t.btnsDown)
	if down {
		t.keysDown[hid] = struct{}{}
	} else {
		delete(t.keysDown, hid)
	}
	after := len(t.keysDown) + len(t.btnsDown)
	id := t.boundID
	t.mu.Unlock()
	t.syncHeld(id, before, after)
}

func (t *tcpSession) syncHeld(id string, before, after int) {
	if (before == 0) != (after == 0) {
		t.srv.arb.setHeld(id, after > 0)
	}
}

// takeHeld 取出并清空按住集合（收尾兜底释放 / 被拒设备残留释放用）。
func (t *tcpSession) takeHeld() ([]uint16, []uint8) {
	t.mu.Lock()
	keys := make([]uint16, 0, len(t.keysDown))
	for hid := range t.keysDown {
		keys = append(keys, hid)
	}
	btns := make([]uint8, 0, len(t.btnsDown))
	for b := range t.btnsDown {
		btns = append(btns, b)
	}
	t.keysDown = make(map[uint16]struct{})
	t.btnsDown = make(map[uint8]struct{})
	id := t.boundID
	t.mu.Unlock()
	t.srv.arb.setHeld(id, false)
	return keys, btns
}
