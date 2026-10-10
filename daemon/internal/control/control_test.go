package control

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

type fakeBackend struct {
	pairErr   error
	unpairErr error
	unpaired  []string
}

func (f *fakeBackend) Status() StatusData {
	return StatusData{
		Version:        "test-1",
		UptimeSec:      12.5,
		PairedClients:  2,
		ActiveSessions: 1,
		Pairing:        PairingInfo{Active: true, Code: "4321", ExpiresInSec: 42},
		Stats:          StatsInfo{HMACFail: 3, Dropped: 9},
		AccelProfile:   "flat",
	}
}

func (f *fakeBackend) Pair() (string, error) {
	if f.pairErr != nil {
		return "", f.pairErr
	}
	return "1234", nil
}

func (f *fakeBackend) Clients() []ClientInfo {
	return []ClientInfo{
		{ID: "aa1", Name: "手机A", PairedAt: time.Unix(1700000000, 0), Online: true},
		{ID: "bb2", Name: "手机B", PairedAt: time.Unix(1700000001, 0), Online: false},
	}
}

func (f *fakeBackend) Unpair(id string) error {
	if f.unpairErr != nil {
		return f.unpairErr
	}
	f.unpaired = append(f.unpaired, id)
	return nil
}

// request 往返一次请求-响应。
func request(t *testing.T, path string, req Request) Response {
	t.Helper()
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		t.Fatalf("编码请求: %v", err)
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		t.Fatalf("读响应: %v", err)
	}
	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		t.Fatalf("解析响应 %q: %v", line, err)
	}
	return resp
}

// shortRuntimeDir 返回短路径的运行时目录（macOS unix socket 路径 ≤104 字节限制）。
func shortRuntimeDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "plctl")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func TestSocketRoundTripFourCommands(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", shortRuntimeDir(t))
	backend := &fakeBackend{}
	srv, err := Start(backend)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(srv.Stop)

	// status
	resp := request(t, srv.Path(), Request{Cmd: "status"})
	if !resp.OK {
		t.Fatalf("status: %v", resp.Error)
	}
	data, err := json.Marshal(resp.Data)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var st StatusData
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if st.Version != "test-1" || st.PairedClients != 2 || st.ActiveSessions != 1 ||
		st.Stats.HMACFail != 3 || st.Stats.Dropped != 9 || st.AccelProfile != "flat" {
		t.Errorf("status 数据不符: %+v", st)
	}
	if !st.Pairing.Active || st.Pairing.Code != "4321" || st.Pairing.ExpiresInSec != 42 {
		t.Errorf("pairing 数据不符: %+v", st.Pairing)
	}

	// pair
	resp = request(t, srv.Path(), Request{Cmd: "pair"})
	if !resp.OK {
		t.Fatalf("pair: %v", resp.Error)
	}
	var pairData struct {
		Code string `json:"code"`
	}
	data, _ = json.Marshal(resp.Data)
	if err := json.Unmarshal(data, &pairData); err != nil || pairData.Code != "1234" {
		t.Errorf("pair data = %s (%v)", data, err)
	}

	// clients
	resp = request(t, srv.Path(), Request{Cmd: "clients"})
	if !resp.OK {
		t.Fatalf("clients: %v", resp.Error)
	}
	data, _ = json.Marshal(resp.Data)
	var clients []ClientInfo
	if err := json.Unmarshal(data, &clients); err != nil || len(clients) != 2 {
		t.Fatalf("clients = %s (%v)", data, err)
	}
	if clients[0].ID != "aa1" || clients[0].Name != "手机A" || !clients[0].Online || clients[1].Online {
		t.Errorf("clients 内容不符: %+v", clients)
	}

	// unpair
	resp = request(t, srv.Path(), Request{Cmd: "unpair", Arg: "aa1"})
	if !resp.OK {
		t.Fatalf("unpair: %v", resp.Error)
	}
	if len(backend.unpaired) != 1 || backend.unpaired[0] != "aa1" {
		t.Errorf("Unpair 收到 %v, want [aa1]", backend.unpaired)
	}

	// unpair 缺 arg / 后端错误 / 未知命令
	if resp = request(t, srv.Path(), Request{Cmd: "unpair"}); resp.OK {
		t.Error("unpair 缺 arg 应失败")
	}
	backend.unpairErr = errors.New("客户端不存在")
	if resp = request(t, srv.Path(), Request{Cmd: "unpair", Arg: "zz"}); resp.OK || resp.Error != "客户端不存在" {
		t.Errorf("unpair 错误透传: %+v", resp)
	}
	backend.unpairErr = nil
	if resp = request(t, srv.Path(), Request{Cmd: "bogus"}); resp.OK || resp.Error == "" {
		t.Errorf("未知命令应失败: %+v", resp)
	}
}

func TestDisabledWithoutXDGRuntimeDir(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
	if runtime.GOOS == "darwin" {
		// darwin 回落 os.TempDir()，控制通道保持可用
		srv, err := Start(&fakeBackend{})
		if err != nil {
			t.Fatalf("darwin 回落 TMPDIR 后 Start: %v", err)
		}
		t.Cleanup(srv.Stop)
		want := filepath.Join(os.TempDir(), sockDir, sockName)
		if srv.Path() != want {
			t.Errorf("darwin socket 路径 = %q, want %q", srv.Path(), want)
		}
		return
	}
	if _, err := Start(&fakeBackend{}); !errors.Is(err, ErrDisabled) {
		t.Fatalf("got %v, want ErrDisabled", err)
	}
}

func TestStaleSocketRemoved(t *testing.T) {
	dir := shortRuntimeDir(t)
	t.Setenv("XDG_RUNTIME_DIR", dir)
	stale := filepath.Join(dir, sockDir, sockName)
	if err := os.MkdirAll(filepath.Dir(stale), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(stale, []byte("junk"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	srv, err := Start(&fakeBackend{})
	if err != nil {
		t.Fatalf("Start(残留 socket): %v", err)
	}
	t.Cleanup(srv.Stop)
	if resp := request(t, srv.Path(), Request{Cmd: "status"}); !resp.OK {
		t.Errorf("status 失败: %v", resp.Error)
	}
}
