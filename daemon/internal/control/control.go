// Package control 实现 padlinkctl ↔ padlinkd 的本机控制通道：
// Unix socket（Linux $XDG_RUNTIME_DIR/padlinkd/control.sock；
// macOS $TMPDIR/padlinkd/control.sock）上的行分隔 JSON 协议。
package control

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// 控制通道参数。
const (
	sockDir      = "padlinkd"
	sockName     = "control.sock"
	readTimeout  = 5 * time.Second
	maxLineBytes = 64 * 1024
)

// ErrDisabled 表示无可用运行目录（Linux 上 XDG_RUNTIME_DIR 未设置）：控制通道禁用（调用方告警后继续）。
var ErrDisabled = errors.New("无运行目录（XDG_RUNTIME_DIR 未设置），控制通道禁用（ctl 与配对终端展示不可用）")

// SocketPath 返回控制 socket 路径。Linux 用 $XDG_RUNTIME_DIR（缺失即 ErrDisabled）；
// macOS 无 XDG 概念，回落 os.TempDir()（launchd 代理与终端同用户共享 TMPDIR，
// 两侧各自计算可收敛到同一路径）。
func SocketPath() (string, error) {
	rd := os.Getenv("XDG_RUNTIME_DIR")
	if rd == "" {
		if runtime.GOOS == "darwin" {
			rd = os.TempDir()
		} else {
			return "", ErrDisabled
		}
	}
	return filepath.Join(rd, sockDir, sockName), nil
}

// Request 是 ctl → daemon 的请求。
type Request struct {
	Cmd string `json:"cmd"` // status | pair | clients | unpair
	Arg string `json:"arg,omitempty"`
}

// Response 是 daemon → ctl 的响应。
type Response struct {
	OK    bool   `json:"ok"`
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

// PairingInfo 进行中的配对（ctl status 展示）。
type PairingInfo struct {
	Active       bool   `json:"active"`
	Code         string `json:"code,omitempty"`
	ExpiresInSec int    `json:"expires_in_sec,omitempty"`
}

// StatsInfo 丢包/认证失败/控制权抢占计数。
type StatsInfo struct {
	HMACFail  uint64 `json:"hmac_fail"`
	Dropped   uint64 `json:"dropped"`
	Preempted uint64 `json:"preempted"` // 多设备控制权裁决拒绝的控制事件数
}

// StatusData 是 status 命令的 data。
type StatusData struct {
	Version        string      `json:"version"`
	UptimeSec      float64     `json:"uptime_sec"`
	PairedClients  int         `json:"paired_clients"`
	ActiveSessions int         `json:"active_sessions"`
	Pairing        PairingInfo `json:"pairing"`
	Stats          StatsInfo   `json:"stats"`
	AccelProfile   string      `json:"accel_profile"`
}

// ClientInfo 是 clients 命令的单行。
type ClientInfo struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	DevID    string    `json:"dev_id,omitempty"` // 设备指纹（空 = 旧版手机未上报）
	PairedAt time.Time `json:"paired_at"`
	Online   bool      `json:"online"`
}

// Backend 由 daemon 侧装配（pairing + session + hostinfo 的聚合视图）。
type Backend interface {
	Status() StatusData
	Pair() (code string, err error)
	Clients() []ClientInfo
	Unpair(id string) error
}

// Server 是控制通道服务端。经 Start 创建。
type Server struct {
	path    string
	ln      net.Listener
	backend Backend
	wg      sync.WaitGroup
}

// Start 监听控制 socket。XDG_RUNTIME_DIR 未设置时返回 (nil, ErrDisabled)。
func Start(backend Backend) (*Server, error) {
	path, err := SocketPath()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("control: 创建运行时目录: %w", err)
	}
	os.Remove(path) // 崩溃残留的 socket 文件（此时无监听者，安全清除）

	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("control: 监听 %s: %w", path, err)
	}
	s := &Server{path: path, ln: ln, backend: backend}
	s.wg.Add(1)
	go s.serve()
	return s, nil
}

// Path 返回 socket 路径（测试用）。
func (s *Server) Path() string { return s.path }

// Stop 关闭 socket 并等待全部连接处理完毕。
func (s *Server) Stop() {
	s.ln.Close()
	s.wg.Wait()
	os.Remove(s.path)
}

func (s *Server) serve() {
	defer s.wg.Done()
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleConn(c)
		}()
	}
}

func (s *Server) handleConn(c net.Conn) {
	defer c.Close()
	c.SetDeadline(time.Now().Add(readTimeout))
	sc := bufio.NewScanner(c)
	sc.Buffer(make([]byte, 0, 4096), maxLineBytes)
	w := bufio.NewWriter(c)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var resp Response
		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			resp = Response{Error: fmt.Sprintf("请求不是合法 JSON: %v", err)}
		} else {
			resp = s.dispatch(req)
		}
		out, err := json.Marshal(resp)
		if err != nil {
			out = []byte(`{"ok":false,"error":"响应序列化失败"}`)
		}
		if _, err := w.Write(append(out, '\n')); err != nil {
			return
		}
		if err := w.Flush(); err != nil {
			return
		}
	}
}

func (s *Server) dispatch(req Request) Response {
	switch req.Cmd {
	case "status":
		return Response{OK: true, Data: s.backend.Status()}
	case "pair":
		code, err := s.backend.Pair()
		if err != nil {
			return Response{Error: err.Error()}
		}
		return Response{OK: true, Data: map[string]string{"code": code}}
	case "clients":
		return Response{OK: true, Data: s.backend.Clients()}
	case "unpair":
		if req.Arg == "" {
			return Response{Error: "unpair 需要客户端 id（arg 字段，见 clients）"}
		}
		if err := s.backend.Unpair(req.Arg); err != nil {
			return Response{Error: err.Error()}
		}
		return Response{OK: true, Data: map[string]string{"unpaired": req.Arg}}
	default:
		return Response{Error: fmt.Sprintf("未知命令 %q（支持 status/pair/clients/unpair）", req.Cmd)}
	}
}
