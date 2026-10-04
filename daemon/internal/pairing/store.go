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

// storeVersion 便于后续格式迁移。
const storeVersion = 1

// Client 是一台已配对手机：token 签发时绑定名称与时间戳。
type Client struct {
	ID       string    `json:"id"`        // 8 位 hex，ctl unpair 定位用
	Name     string    `json:"name"`      // 手机显示名（配对时上报，可为空）
	TokenHex string    `json:"token_hex"` // 32B token 的 hex（文件权限 0600 保护）
	PairedAt time.Time `json:"paired_at"`
}

// Token 返回解码后的 token 字节。
func (c Client) Token() ([]byte, error) {
	return hex.DecodeString(c.TokenHex)
}

type storeFile struct {
	Version int      `json:"version"`
	Clients []Client `json:"clients"`
}

// Store 是 clients.json 的内存视图（并发安全）。
type Store struct {
	// MaxClients 已配对客户端上限（默认 4，Add 时强制）。
	MaxClients int

	path         string
	mu           sync.Mutex
	clients      []Client
	permWarnings []string
}

// DefaultPath 返回默认存储路径：$XDG_CONFIG_HOME/padlink/clients.json，
// 无 XDG_CONFIG_HOME 时 ~/.config/padlink/clients.json。
func DefaultPath() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "padlink", "clients.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("pairing: 无法定位用户主目录: %w", err)
	}
	return filepath.Join(home, ".config", "padlink", "clients.json"), nil
}

// Load 加载（或初始化）token 存储。文件不存在视为空存储；
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

// Count 返回已配对客户端数。
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

// Add 签发新客户端：crypto/rand 32B token，绑定名称与当前时间，持久化后返回。
// 超过上限返回 ErrClientsFull。
func (s *Store) Add(name string) (Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.clients) >= s.MaxClients {
		return Client{}, ErrClientsFull
	}
	id, err := randHex(4)
	if err != nil {
		return Client{}, fmt.Errorf("pairing: 生成客户端 id: %w", err)
	}
	token := make([]byte, TokenLen)
	if _, err := rand.Read(token); err != nil {
		return Client{}, fmt.Errorf("pairing: 生成 token: %w", err)
	}
	c := Client{ID: id, Name: name, TokenHex: hex.EncodeToString(token), PairedAt: time.Now().UTC()}
	s.clients = append(s.clients, c)
	if err := s.saveLocked(); err != nil {
		s.clients = s.clients[:len(s.clients)-1]
		return Client{}, err
	}
	return c, nil
}

// Remove 删除指定客户端并持久化；id 不存在时 ok=false。
func (s *Store) Remove(id string) (Client, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
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

// LookupToken 按 token 精确匹配客户端（常数时间比较）。
func (s *Store) LookupToken(token []byte) (Client, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.clients {
		raw, err := hex.DecodeString(c.TokenHex)
		if err != nil {
			continue
		}
		if subtle.ConstantTimeCompare(raw, token) == 1 {
			return c, true
		}
	}
	return Client{}, false
}

// saveLocked 以 0600 权限重写存储文件；调用方须已持锁。
// 显式 Chmod 防宽松 umask 导致落盘权限过宽。
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
