// Package update 实现 padlinkctl update：解析目标版本 → 下载当前平台的 release
// tar.gz → SHA-256 校验 → 解压并执行包内安装脚本（Unix 为 install.sh，Windows 为 install.ps1）。
//
// 分层与 internal/service 一致：本文件是纯逻辑（版本比较、资产名、校验行匹配、
// Location 解析），不触网可单测；apply.go 负责网络下载与安装执行。
// 安装逻辑单源在 release 包内脚本，本包只做取包、验签、解压、执行四件事，
// 不复写任何安装步骤。
package update

import (
	"fmt"
	"strconv"
	"strings"
)

// DefaultRepo 是 GitHub 仓库名（releases/latest 与 /releases/download/ 路径前缀）。
const DefaultRepo = "Norman-pong/padlink"

// NormalizeTag 归一版本串：去首尾空白与 v/V 前缀（" v1.4.0 " → "1.4.0"）。
func NormalizeTag(tag string) string {
	tag = strings.TrimSpace(tag)
	tag = strings.TrimPrefix(tag, "v")
	tag = strings.TrimPrefix(tag, "V")
	return tag
}

// ParseVersion 解析三段数字版本号（1.4.0）；其他形态（含 0.1.0-dev）视为不可比较。
func ParseVersion(v string) ([3]int, error) {
	var out [3]int
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, fmt.Errorf("版本号 %q 不是三段式数字", v)
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, fmt.Errorf("版本号 %q 的第 %d 段 %q 不是非负整数", v, i+1, p)
		}
		out[i] = n
	}
	return out, nil
}

// CompareVersions 比较两个三段数字版本号，返回 -1/0/1；任一不可解析即报错
// （开发构建语义由调用方决定）。
func CompareVersions(a, b string) (int, error) {
	pa, err := ParseVersion(a)
	if err != nil {
		return 0, err
	}
	pb, err := ParseVersion(b)
	if err != nil {
		return 0, err
	}
	for i := range pa {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1, nil
			}
			return 1, nil
		}
	}
	return 0, nil
}

// AssetName 构造 goreleaser 产物名：padlink_<ver>_<os>_<arch>.tar.gz（与 .goreleaser.yaml
// 的 name_template 一致）。
func AssetName(version, goos, arch string) string {
	return fmt.Sprintf("padlink_%s_%s_%s.tar.gz", version, goos, arch)
}

// FindChecksum 从 goreleaser checksums.txt 内容中取指定文件的 SHA-256
// （行格式 "<hash>  <filename>"，两空格分隔）。
func FindChecksum(checksums []byte, filename string) (string, error) {
	suffix := "  " + filename
	for _, line := range strings.Split(string(checksums), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasSuffix(line, suffix) {
			if hash := strings.TrimSpace(strings.TrimSuffix(line, suffix)); hash != "" {
				return hash, nil
			}
		}
	}
	return "", fmt.Errorf("checksums.txt 中未找到 %s", filename)
}

// TagFromLocation 从 releases/latest 302 应答的 Location 解析最新 tag
// （…/releases/tag/v1.4.0 → v1.4.0）。与 script/install.sh 同一解析法，
// 不依赖 GitHub API（无 token 也不受 API 限流影响）。
func TagFromLocation(location string) (string, error) {
	const marker = "/tag/"
	i := strings.LastIndex(location, marker)
	if i < 0 {
		return "", fmt.Errorf("Location %q 中没有 %s 段，无法解析最新版本", location, marker)
	}
	tag := strings.TrimSpace(location[i+len(marker):])
	if tag == "" {
		return "", fmt.Errorf("Location %q 的 tag 段为空", location)
	}
	return tag, nil
}
