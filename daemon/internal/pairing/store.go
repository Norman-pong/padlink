package pairing

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const storeVersion = 1

type Client struct {
	ID       string    `json:"id"`        // 8 位 hex，ctl unpair 定位用
	Name     string    `json:"name"`      // 手机显示名（配对时上报，可为空）
	DevID    string    `json:"dev_id"`    // 设备指纹：手机首次运行生成的 16B 随机值 hex（空 = 旧版手机未上报）
	TokenHex string    `json:"token_hex"` // 32B token 的 hex（文件 0600 保护）
	PairedAt time.Time `json:"paired_at"`
}

func (c Client) Token() ([]byte, error) {
	return hex.DecodeString(c.TokenHex)
}

// AuthEntry 认证索引条目：客户端与其 token 字节（hex 解码结果）。
type AuthEntry struct {
	Client Client
	Token  []byte
}

type storeFile struct {
	Version int      `json:"version"`
	Clients []Client `json:"clients"`
}

// Store 是 clients.json 的内存视图（并发安全）。
type Store struct {
	// MaxClients 已配对客户端上限（默认 4）。
	MaxClients int

	path         string
	mu           sync.Mutex
	clients      []Client
	auth         []AuthEntry // token 字节缓存，随 clients 变更重建（rebuildAuthLocked）
	permWarnings []string
}

// ConfigDir 返回配置目录路径（遵循 XDG_CONFIG_HOME，回落 ~/.config）。
// 主机身份等跨 IP 持久化的daemon 级文件与 clients.json 同目录。
func ConfigDir() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "padlink"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("pairing: 无法定位用户主目录: %w", err)
	}
	return filepath.Join(home, ".config", "padlink"), nil
}

// DefaultPath 返回默认存储路径（遵循 XDG_CONFIG_HOME，回落 ~/.config）。
func DefaultPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "clients.json"), nil
}

// Load 加载（或初始化）token 存储；文件不存在视为空存储。
// 既有文件权限宽于 0600 时记入告警（经 Warnings 取出）。
func Load(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("pairing: 创建配置目录: %w", err)
	}
	s := &Store{path: path, MaxClients: DefaultMaxClients}

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("pairing: 读取 %s: %w", path, err)
	}
	var sf storeFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return nil, fmt.Errorf("pairing: 解析 %s: %w", path, err)
	}
	if sf.Version > storeVersion {
		return nil, fmt.Errorf("pairing: %s 格式版本 %d 高于支持版本 %d，请升级 padlinkd", path, sf.Version, storeVersion)
	}
	s.clients = sf.Clients
	s.rebuildAuthLocked()

	if info, err := os.Stat(path); err == nil {
		if perm := info.Mode().Perm(); perm&0o077 != 0 {
			s.permWarnings = append(s.permWarnings,
				fmt.Sprintf("%s 权限 %04o 宽于 0600，建议执行 chmod 600 %s", path, perm, path))
		}
	}
	return s, nil
}

// Warnings 返回加载时产生的权限告警。
func (s *Store) Warnings() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.permWarnings...)
}

func (s *Store) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.clients)
}

// Clients 返回客户端列表快照。
func (s *Store) Clients() []Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Client, len(s.clients))
	copy(out, s.clients)
	return out
}

// AuthIndex 返回认证索引快照（供认证路径直接比对 HMAC，免逐包 hex 解码）。
func (s *Store) AuthIndex() []AuthEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.auth
}

// rebuildAuthLocked 重建认证索引（调用方须持锁或处于单线程构造期）；
// 每次分配新切片，先前返回的快照保持不可变；坏 hex 记录跳过。
func (s *Store) rebuildAuthLocked() {
	idx := make([]AuthEntry, 0, len(s.clients))
	for _, c := range s.clients {
		tok, err := hex.DecodeString(c.TokenHex)
		if err != nil {
			continue
		}
		idx = append(idx, AuthEntry{Client: c, Token: tok})
	}
	s.auth = idx
}

// Issue 签发客户端凭据（配对成功的唯一写入路径）：devID 命中既有记录则复用该记录
// （id 不变）并轮换 token（不占新名额），否则新增；超上限返回 ErrClientsFull。
// rotated 报告本次是否为同设备轮换；name 为空时不覆盖既有名称。
func (s *Store) Issue(name, devID string) (client Client, rotated bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.rebuildAuthLocked() // 覆盖全部返回路径（含 saveLocked 失败回滚后）
	if devID != "" {
		for i := range s.clients {
			if s.clients[i].DevID != devID {
				continue
			}
			prev := s.clients[i]
			next := prev
			if name != "" {
				next.Name = name
			}
			if next.TokenHex, err = newTokenHex(); err != nil {
				return Client{}, false, err
			}
			next.PairedAt = time.Now().UTC()
			s.clients[i] = next
			if err := s.saveLocked(); err != nil {
				s.clients[i] = prev
				return Client{}, false, err
			}
			return next, true, nil
		}
	}
	if len(s.clients) >= s.MaxClients {
		return Client{}, false, ErrClientsFull
	}
	id, err := randHex(4)
	if err != nil {
		return Client{}, false, fmt.Errorf("pairing: 生成客户端 id: %w", err)
	}
	tokenHex, err := newTokenHex()
	if err != nil {
		return Client{}, false, err
	}
	c := Client{ID: id, Name: name, DevID: devID, TokenHex: tokenHex, PairedAt: time.Now().UTC()}
	s.clients = append(s.clients, c)
	if err := s.saveLocked(); err != nil {
		s.clients = s.clients[:len(s.clients)-1]
		return Client{}, false, err
	}
	return c, false, nil
}

// Remove 删除指定客户端并持久化；id 不存在时 ok=false。
func (s *Store) Remove(id string) (Client, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.rebuildAuthLocked()
	for i := range s.clients {
		if s.clients[i].ID != id {
			continue
		}
		c := s.clients[i]
		s.clients = append(s.clients[:i], s.clients[i+1:]...)
		if err := s.saveLocked(); err != nil {
			s.clients = append(s.clients[:i], append([]Client{c}, s.clients[i:]...)...)
			return Client{}, false, err
		}
		return c, true, nil
	}
	return Client{}, false, nil
}

// HasDevID 报告是否存在该设备指纹的已配对记录（空指纹恒 false）；
// 满额时据此仍放行同设备重配（Issue 走轮换路径，不占新名额）。
func (s *Store) HasDevID(devID string) bool {
	if devID == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.clients {
		if c.DevID == devID {
			return true
		}
	}
	return false
}

// LookupToken 按 token 精确匹配客户端（常数时间比较）。
func (s *Store) LookupToken(token []byte) (Client, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.auth {
		if subtle.ConstantTimeCompare(e.Token, token) == 1 {
			return e.Client, true
		}
	}
	return Client{}, false
}

// saveLocked 以 0600 重写存储文件，调用方须已持锁；
// 显式 Chmod 防宽松 umask 使落盘权限过宽。
func (s *Store) saveLocked() error {
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("pairing: 写入 %s: %w", s.path, err)
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return fmt.Errorf("pairing: chmod %s: %w", s.path, err)
	}
	data, err := json.MarshalIndent(storeFile{Version: storeVersion, Clients: s.clients}, "", "  ")
	if err == nil {
		data = append(data, '\n')
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return fmt.Errorf("pairing: 写入 %s: %w", s.path, err)
	}
	if closeErr != nil {
		return fmt.Errorf("pairing: 关闭 %s: %w", s.path, closeErr)
	}
	return nil
}

func randHex(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func newTokenHex() (string, error) {
	token := make([]byte, TokenLen)
	if _, err := rand.Read(token); err != nil {
		return "", fmt.Errorf("pairing: 生成 token: %w", err)
	}
	return hex.EncodeToString(token), nil
}
