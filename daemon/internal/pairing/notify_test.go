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

func TestTextsForLocale(t *testing.T) {
	for _, loc := range []string{"zh_CN", "zh-Hans-CN", "ZH_cn", "zh"} {
		if got := textsForLocale(loc); got.ok != "好的" {
			t.Errorf("%q 应为中文文案，得到 %+v", loc, got)
		}
	}
	for _, loc := range []string{"en_US", "ja_JP", "", "  "} {
		if got := textsForLocale(loc); got.ok != "OK" {
			t.Errorf("%q 应回落英文文案，得到 %+v", loc, got)
		}
	}
}

func TestDialogCmdLinuxZenity(t *testing.T) {
	name, args, ok := dialogCmdLinux("1234", 60*time.Second, found("/usr/bin/zenity"), textsZh)
	if !ok || name != "zenity" {
		t.Fatalf("linux 有 zenity 应走 zenity: %q %v %v", name, args, ok)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "配对码：1234") || !strings.Contains(joined, "--timeout=60") {
		t.Errorf("zenity 参数缺配对码或超时: %v", args)
	}
	_, enArgs, _ := dialogCmdLinux("1234", 60*time.Second, found("/usr/bin/zenity"), textsEn)
	if !strings.Contains(strings.Join(enArgs, " "), "Pairing code: 1234") {
		t.Errorf("zenity 英文文案缺失: %v", enArgs)
	}
}

func TestDialogCmdLinuxNoZenity(t *testing.T) {
	_, _, ok := dialogCmdLinux("1234", 60*time.Second, func(string) (string, error) {
		return "", errors.New("not found")
	}, textsZh)
	if ok {
		t.Error("无 zenity 应返回不可用（调用方回落通知）")
	}
}
