package service

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile 建一个测试用文件（路径含 padlink 以通过路径守卫）。
func writeFile(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("建测试文件: %v", err)
	}
	return path
}

func TestRunDryRunHasNoSideEffects(t *testing.T) {
	dir := t.TempDir()
	target := writeFile(t, dir, "padlink-dryrun.txt")
	created := filepath.Join(dir, "padlink-created.txt")
	plan := Plan{Action: "uninstall", Steps: []Step{
		{Kind: StepRunCommand, Cmd: "touch", Args: []string{created}},
		{Kind: StepRemoveFile, Path: target, Guard: dir},
		{Kind: StepNotice, Notice: NoticeManual, Text: "需手动执行的命令"},
	}}

	var buf bytes.Buffer
	res, err := Run(plan, true, &buf)
	if err != nil {
		t.Fatalf("Run(dry-run): %v", err)
	}
	if !res.DryRun {
		t.Error("Result.DryRun 应为 true")
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("dry-run 不得删除文件: %v", err)
	}
	if _, err := os.Stat(created); err == nil {
		t.Fatal("dry-run 不得执行命令")
	}
	out := buf.String()
	for _, want := range []string{
		"[dry-run] 将执行: touch " + created,
		"[dry-run] 将删除文件: " + target,
		"需手动处理: 需手动执行的命令",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run 输出缺少 %q\n实际输出:\n%s", want, out)
		}
	}
	if len(res.Removed) != 1 || res.Removed[0] != target {
		t.Errorf("Removed = %q，期望 [%q]", res.Removed, target)
	}
	if len(res.Manual) != 1 || res.Manual[0] != "需手动执行的命令" {
		t.Errorf("Manual = %q", res.Manual)
	}
}

func TestRunEnforcesGuard(t *testing.T) {
	dir := t.TempDir()
	target := writeFile(t, dir, "padlink-guard.txt")
	// 守卫前缀与实际目录不符：执行层必须拒绝删除并报错
	plan := Plan{Steps: []Step{{Kind: StepRemoveFile, Path: target, Guard: filepath.Join(dir, "sub")}}}

	_, err := Run(plan, false, io.Discard)
	if err == nil {
		t.Fatal("期望路径守卫拒绝，实际成功")
	}
	mustContain(t, "错误消息", err.Error(), "不在允许前缀")
	if _, statErr := os.Stat(target); statErr != nil {
		t.Fatalf("守卫拒绝后文件应仍在: %v", statErr)
	}
}

func TestRunReportsManualCommandOnRemoveFailure(t *testing.T) {
	dir := t.TempDir()
	nonEmpty := filepath.Join(dir, "padlink-config") // 目录名含 padlink
	if err := os.MkdirAll(nonEmpty, 0o700); err != nil {
		t.Fatalf("建目录: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nonEmpty, "extra.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("建文件: %v", err)
	}
	plan := Plan{Steps: []Step{{Kind: StepRemoveDir, Path: nonEmpty, Guard: dir}}}

	var buf bytes.Buffer
	_, err := Run(plan, false, &buf)
	if err == nil {
		t.Fatal("目录非空时应报错（不得用 RemoveAll 吞掉）")
	}
	mustContain(t, "错误消息", err.Error(), "rm -rf "+nonEmpty)
	mustContain(t, "输出", buf.String(), "错误:")
	if _, statErr := os.Stat(nonEmpty); statErr != nil {
		t.Fatalf("删除失败后目录应仍在: %v", statErr)
	}
}

func TestRunSkipsMissingPaths(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "padlink-missing.txt")
	res, err := Run(Plan{Steps: []Step{{Kind: StepRemoveFile, Path: missing, Guard: dir}}}, false, io.Discard)
	if err != nil {
		t.Fatalf("目标已不存在不应报错: %v", err)
	}
	if len(res.Skipped) != 1 || res.Skipped[0] != missing {
		t.Fatalf("Skipped = %q，期望 [%q]", res.Skipped, missing)
	}
	if len(res.Removed) != 0 {
		t.Fatalf("Removed = %q，期望为空", res.Removed)
	}
}

func TestRunOptionalCommandFailureIsWarning(t *testing.T) {
	plan := Plan{Steps: []Step{
		{Kind: StepRunCommand, Cmd: "sh", Args: []string{"-c", "exit 3"}, Optional: true},
	}}
	var buf bytes.Buffer
	res, err := Run(plan, false, &buf)
	if err != nil {
		t.Fatalf("可选命令失败不应中断: %v", err)
	}
	if len(res.Warnings) != 1 {
		t.Fatalf("Warnings = %q，期望 1 条", res.Warnings)
	}
	mustContain(t, "警告输出", buf.String(), "警告:")
}

func TestRunAggregatesFailuresAndContinues(t *testing.T) {
	dir := t.TempDir()
	target := writeFile(t, dir, "padlink-after-failure.txt")
	plan := Plan{Steps: []Step{
		{Kind: StepRunCommand, Cmd: "sh", Args: []string{"-c", "exit 1"}},
		{Kind: StepRemoveFile, Path: target, Guard: dir},
	}}
	var buf bytes.Buffer
	res, err := Run(plan, false, &buf)
	if err == nil {
		t.Fatal("非可选命令失败应返回错误")
	}
	mustContain(t, "错误消息", err.Error(), "执行失败")
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Fatalf("前一步失败后仍应继续执行删除: %v", statErr)
	}
	if len(res.Removed) != 1 {
		t.Fatalf("Removed = %q，期望 [%q]", res.Removed, target)
	}
}

func TestRunExecutesCommandAndRemovesFile(t *testing.T) {
	dir := t.TempDir()
	target := writeFile(t, dir, "padlink-real.txt")
	plan := Plan{Steps: []Step{
		{Kind: StepRemoveFile, Path: target, Guard: dir},
		{Kind: StepRunCommand, Cmd: "sh", Args: []string{"-c", "true"}},
		{Kind: StepNotice, Notice: NoticeKeep, Text: "保留项"},
	}}
	var buf bytes.Buffer
	res, err := Run(plan, false, &buf)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Fatalf("文件应已删除: %v", statErr)
	}
	if len(res.Removed) != 1 || len(res.Commands) != 1 || len(res.Kept) != 1 {
		t.Fatalf("Result = %+v", res)
	}
	out := buf.String()
	for _, want := range []string{"已删除文件: " + target, "已执行: sh -c true", "保留: 保留项"} {
		if !strings.Contains(out, want) {
			t.Errorf("输出缺少 %q\n实际输出:\n%s", want, out)
		}
	}
}

func TestDaemonRunningWithoutSocket(t *testing.T) {
	// 只验证探测函数不 panic 且返回布尔值（真实环境相关，不做断言）
	_ = DaemonRunning()
}
