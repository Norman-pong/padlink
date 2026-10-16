package pairing

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func found(path string) func(string) (string, error) {
	return func(name string) (string, error) { return path, nil }
}

func TestDialogCmdDarwin(t *testing.T) {
	name, args, ok := dialogCmd("darwin", "1234", 60*time.Second, nil)
	if !ok || name != "osascript" {
		t.Fatalf("darwin 应走 osascript: %q %v %v", name, args, ok)
	}
	script := args[1]
	for _, want := range []string{`display dialog`, "1234", "giving up after 60"} {
		if !strings.Contains(script, want) {
			t.Errorf("脚本缺 %q: %s", want, script)
		}
	}
}

func TestDialogCmdLinuxZenity(t *testing.T) {
	name, args, ok := dialogCmd("linux", "1234", 60*time.Second, found("/usr/bin/zenity"))
	if !ok || name != "zenity" {
		t.Fatalf("linux 有 zenity 应走 zenity: %q %v %v", name, args, ok)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "1234") || !strings.Contains(joined, "--timeout=60") {
		t.Errorf("zenity 参数缺确认码或超时: %v", args)
	}
}

func TestDialogCmdLinuxNoZenity(t *testing.T) {
	_, _, ok := dialogCmd("linux", "1234", 60*time.Second, func(string) (string, error) {
		return "", errors.New("not found")
	})
	if ok {
		t.Error("无 zenity 应返回不可用（调用方回落通知）")
	}
}

func TestDialogCmdUnknownGOOS(t *testing.T) {
	if _, _, ok := dialogCmd("windows", "1234", 60*time.Second, found("x")); ok {
		t.Error("未知平台应返回不可用")
	}
}
