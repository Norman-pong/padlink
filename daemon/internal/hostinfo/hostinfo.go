// Package hostinfo 实现 daemon 启动时的主机环境自检：
// Wayland 会话缺失与 GNOME 指针加速 profile（PRD §4.2 基线 / RESEARCH-WAYLAND.md §3-§4）。
package hostinfo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Severity 发现项严重级别。
type Severity string

const SevWarn Severity = "warning"

// Finding 是一条自检结论。
type Finding struct {
	Severity Severity
	Check    string
	Message  string
}

// Report 汇总自检结果；AccelProfile 为空表示未检测到（gsettings 不可用，跳过不打扰）。
type Report struct {
	Findings     []Finding
	AccelProfile string
}

// Options 注入环境查询与命令执行（测试用；零值取真实环境）。
type Options struct {
	Env func(key string) string
	Run func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// accelCheckTimeout gsettings 查询超时。
const accelCheckTimeout = 3 * time.Second

// FlatHint 设置 flat 加速基线的建议命令。
const FlatHint = "gsettings set org.gnome.desktop.peripherals.mouse accel-profile 'flat'"

// Check 执行自检。/dev/uinput 存在性不在此查（uinput.Open 的错误文案已覆盖）。
func Check(opts Options) Report {
	if opts.Env == nil {
		opts.Env = os.Getenv
	}
	if opts.Run == nil {
		opts.Run = defaultRun
	}
	var rep Report

	if opts.Env("WAYLAND_DISPLAY") == "" {
		rep.Findings = append(rep.Findings, Finding{
			Severity: SevWarn,
			Check:    "wayland",
			Message:  "WAYLAND_DISPLAY 未设置：文本注入（剪贴板+CtrlV）不可用，语音听写功能降级；触摸板/键盘注入不受影响",
		})
	}

	profile, err := accelProfile(opts.Run)
	rep.AccelProfile = profile
	if err == nil && profile != "" && profile != "flat" {
		rep.Findings = append(rep.Findings, Finding{
			Severity: SevWarn,
			Check:    "accel-profile",
			Message:  fmt.Sprintf("鼠标加速 profile=%q，非 flat 会与手机端加速曲线叠加（双重加速）。建议基线：%s", profile, FlatHint),
		})
	}
	return rep
}

// accelProfile 读取 GNOME 全局鼠标加速 profile；命令不存在等错误由调用方跳过。
func accelProfile(run func(ctx context.Context, name string, args ...string) ([]byte, error)) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), accelCheckTimeout)
	defer cancel()
	out, err := run(ctx, "gsettings", "get", "org.gnome.desktop.peripherals.mouse", "accel-profile")
	if err != nil {
		return "", err
	}
	v := strings.Trim(strings.TrimSpace(string(out)), `'"`)
	if v == "" {
		return "", errors.New("gsettings 输出为空")
	}
	return v, nil
}

func defaultRun(ctx context.Context, name string, args ...string) ([]byte, error) {
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
