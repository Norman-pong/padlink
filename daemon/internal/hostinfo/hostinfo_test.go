package hostinfo

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func fakeRun(output string, err error) func(ctx context.Context, name string, args ...string) ([]byte, error) {
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name != "gsettings" || strings.Join(args, " ") != "get org.gnome.desktop.peripherals.mouse accel-profile" {
			return nil, errors.New("意外命令")
		}
		if err != nil {
			return nil, err
		}
		return []byte(output), nil
	}
}

func TestCheckWaylandMissing(t *testing.T) {
	rep := Check(Options{
		Env: func(string) string { return "" },
		Run: func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("not found") },
	})
	if len(rep.Findings) != 1 {
		t.Fatalf("findings = %d, want 1: %+v", len(rep.Findings), rep.Findings)
	}
	f := rep.Findings[0]
	if f.Severity != SevWarn || f.Check != "wayland" || !strings.Contains(f.Message, "降级") {
		t.Errorf("发现项不符: %+v", f)
	}
	if rep.AccelProfile != "" {
		t.Errorf("gsettings 不可用时 AccelProfile = %q, want 空（跳过不打扰）", rep.AccelProfile)
	}
}

func TestCheckAccelProfiles(t *testing.T) {
	cases := []struct {
		output  string
		want    string
		warning bool
	}{
		{"'flat'\n", "flat", false},
		{"'adaptive'\n", "adaptive", true},
		{"'custom'\n", "custom", true},
		{"flat\n", "flat", false},
	}
	for _, tc := range cases {
		rep := Check(Options{
			Env: func(k string) string {
				if k == "WAYLAND_DISPLAY" {
					return "wayland-0"
				}
				return ""
			},
			Run: fakeRun(tc.output, nil),
		})
		if rep.AccelProfile != tc.want {
			t.Errorf("output=%q: AccelProfile = %q, want %q", tc.output, rep.AccelProfile, tc.want)
		}
		hasWarn := false
		for _, f := range rep.Findings {
			if f.Check == "accel-profile" {
				hasWarn = true
				if !strings.Contains(f.Message, FlatHint) {
					t.Errorf("警告缺建议命令: %s", f.Message)
				}
			}
		}
		if hasWarn != tc.warning {
			t.Errorf("output=%q: 警告 = %v, want %v", tc.output, hasWarn, tc.warning)
		}
	}
}

func TestCheckTimeoutRespected(t *testing.T) {
	var gotTimeout time.Duration
	rep := Check(Options{
		Env: func(k string) string {
			if k == "WAYLAND_DISPLAY" {
				return "wayland-0"
			}
			return ""
		},
		Run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			d, _ := ctx.Deadline()
			gotTimeout = time.Until(d)
			return nil, ctx.Err()
		},
	})
	if gotTimeout <= 0 || gotTimeout > 5*time.Second {
		t.Errorf("超时预算 = %v, want ≤5s 且为正（3s 基线）", gotTimeout)
	}
	if rep.AccelProfile != "" || len(rep.Findings) != 0 {
		t.Errorf("查询失败应整体跳过: %+v", rep)
	}
}
