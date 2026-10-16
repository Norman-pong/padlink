// Package hostid 提供主机稳定身份：16B 随机值 hex，持久化于配置目录，跨网络环境不变。
// 身份仅作发现应答里的候选提示（PROTOCOL.md §4.10），不参与认证，不可用时省略字段。
package hostid

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"padlink/daemon/internal/pairing"
)

// FileName 身份文件名（与 clients.json 同目录，卸载 --purge 一并清理）。
const FileName = "host_id"

// LoadOrCreate 读取（或首次生成）主机身份；dir 为空时用 pairing 配置目录。
// 文件内容非法（截断/手改）时重新生成；环境不可写返回错误，调用方按无身份降级。
func LoadOrCreate(dir string) (string, error) {
	if dir == "" {
		var err error
		if dir, err = pairing.ConfigDir(); err != nil {
			return "", err
		}
	}
	path := filepath.Join(dir, FileName)
	if data, err := os.ReadFile(path); err == nil {
		if id := string(data); valid(id) {
			return id, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("hostid: 读取 %s: %w", path, err)
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("hostid: 生成身份: %w", err)
	}
	id := hex.EncodeToString(b)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("hostid: 创建配置目录: %w", err)
	}
	if err := os.WriteFile(path, []byte(id), 0o600); err != nil {
		return "", fmt.Errorf("hostid: 写入 %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return "", fmt.Errorf("hostid: chmod %s: %w", path, err)
	}
	return id, nil
}

func valid(id string) bool {
	if len(id) != 32 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}
