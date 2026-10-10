package main

import (
	"fmt"
	"time"

	"padlink/daemon/internal/control"
	"padlink/daemon/internal/pairing"
	"padlink/daemon/internal/session"
)

// ctlBackend 聚合 pairing/session/hostinfo，向 control 包提供控制面视图。
type ctlBackend struct {
	start time.Time
	store *pairing.Store
	mgr   *pairing.Manager
	srv   *session.Server
	accel string // 启动时 accel-profile 检测结果（空 = 未检测到）
}

func (b *ctlBackend) Status() control.StatusData {
	st := b.srv.StatsSnapshot()
	code, expires, active := b.mgr.ActiveCode()
	pi := control.PairingInfo{Active: active}
	if active {
		pi.Code = code
		pi.ExpiresInSec = int(time.Until(expires).Seconds())
	}
	return control.StatusData{
		Version:        daemonVersion,
		UptimeSec:      time.Since(b.start).Seconds(),
		PairedClients:  b.store.Count(),
		ActiveSessions: b.srv.ActiveSessions(),
		Pairing:        pi,
		Stats:          control.StatsInfo{HMACFail: st.HMACFail, Dropped: st.Dropped},
		AccelProfile:   b.accel,
	}
}

// Pair 发起配对并返回确认码（ctl 终端展示路径）；进行中则复用同码。
// ctl 路径不带设备指纹（终端配对无手机指纹可用），按新设备处理。
func (b *ctlBackend) Pair() (string, error) {
	code, err := b.mgr.StartPairing("", "")
	if err != nil {
		return "", err
	}
	return code, nil
}

func (b *ctlBackend) Clients() []control.ClientInfo {
	online := b.srv.OnlineClients()
	clients := b.store.Clients()
	out := make([]control.ClientInfo, 0, len(clients))
	for _, c := range clients {
		out = append(out, control.ClientInfo{
			ID:       c.ID,
			Name:     c.Name,
			DevID:    c.DevID,
			PairedAt: c.PairedAt,
			Online:   online[c.ID],
		})
	}
	return out
}

// Unpair 删 token 并踢下线（会话收尾兜底补发未释放按键）。
func (b *ctlBackend) Unpair(id string) error {
	_, ok, err := b.store.Remove(id)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("客户端 %s 不存在", id)
	}
	b.srv.KickClient(id)
	return nil
}
