package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNormalizeTag(t *testing.T) {
	cases := map[string]string{
		" v1.4.0 ": "1.4.0",
		"V2.0.1":   "2.0.1",
		"1.3.2":    "1.3.2",
	}
	for in, want := range cases {
		if got := NormalizeTag(in); got != want {
			t.Errorf("NormalizeTag(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.3.0", "1.4.0", -1},
		{"1.4.0", "1.3.0", 1},
		{"1.10.0", "1.9.9", 1},
		{"2.0.0", "2.0.0", 0},
		{"1.4.1", "1.4.1", 0},
	}
	for _, c := range cases {
		got, err := CompareVersions(c.a, c.b)
		if err != nil {
			t.Fatalf("CompareVersions(%q, %q): %v", c.a, c.b, err)
		}
		if got != c.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
	if _, err := CompareVersions("0.1.0-dev", "1.4.0"); err == nil {
		t.Error("开发构建版本应不可比较")
	}
}

func TestAssetName(t *testing.T) {
	got := AssetName("1.4.0", "darwin", "arm64")
	if got != "padlink_1.4.0_darwin_arm64.tar.gz" {
		t.Errorf("AssetName = %q", got)
	}
}

func TestFindChecksum(t *testing.T) {
	body := []byte("aaa111  padlink_1.4.0_linux_amd64.tar.gz\nbbb222  padlink_1.4.0_darwin_arm64.tar.gz\n")
	got, err := FindChecksum(body, "padlink_1.4.0_darwin_arm64.tar.gz")
	if err != nil {
		t.Fatalf("FindChecksum: %v", err)
	}
	if got != "bbb222" {
		t.Errorf("FindChecksum = %q, want bbb222", got)
	}
	if _, err := FindChecksum(body, "missing.tar.gz"); err == nil {
		t.Error("缺失文件应报错")
	}
}

func TestTagFromLocation(t *testing.T) {
	got, err := TagFromLocation("https://github.com/Norman-pong/padlink/releases/tag/v1.4.1")
	if err != nil {
		t.Fatalf("TagFromLocation: %v", err)
	}
	if got != "v1.4.1" {
		t.Errorf("TagFromLocation = %q", got)
	}
	if _, err := TagFromLocation("https://example.com/other"); err == nil {
		t.Error("无 tag 段的 Location 应报错")
	}
}

// fakeRelease 起一个最小 GitHub Releases 形态的测试服务；hits 记录各端点命中次数。
type fakeRelease struct {
	srv  *httptest.Server
	hits map[string]int
}

// newFakeRelease：latest tag 固定 latestTag；assets 为资产名 → 内容；checksums 为
// checksums.txt 原文；missing 为 true 时资产端点一律 404。
func newFakeRelease(t *testing.T, latestTag string, assets map[string][]byte, checksums string, missing bool) *fakeRelease {
	t.Helper()
	f := &fakeRelease{hits: map[string]int{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/Norman-pong/padlink/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		f.hits["latest"]++
		w.Header().Set("Location", "/Norman-pong/padlink/releases/tag/"+latestTag)
		w.WriteHeader(http.StatusFound)
	})
	dl := "/Norman-pong/padlink/releases/download/"
	mux.HandleFunc(dl, func(w http.ResponseWriter, r *http.Request) {
		f.hits[r.URL.Path]++
		if missing {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		name := filepath.Base(r.URL.Path)
		if name == "checksums.txt" {
			w.Write([]byte(checksums))
			return
		}
		body, ok := assets[name]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Write(body)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		f.hits[r.URL.Path]++
		w.Write([]byte(checksums))
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

// makeTarGz 构造内存 release 包（常规文件，mode 0755）。
func makeTarGz(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	for name, data := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(data))}); err != nil {
			t.Fatalf("tar header %s: %v", name, err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatalf("tar write %s: %v", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

// setupAssets 构造含 install.sh 的包与配套 checksums，install.sh 会把 "done" 写到 marker。
func setupAssets(t *testing.T, marker string) (map[string][]byte, string) {
	t.Helper()
	pkg := makeTarGz(t, map[string][]byte{
		"install.sh": []byte("#!/bin/sh\necho done > " + marker + "\n"),
		"padlinkd":   []byte("#!/bin/sh\necho 9.9.9\n"),
	})
	asset := AssetName("1.4.1", runtime.GOOS, runtime.GOARCH)
	sum := sha256.Sum256(pkg)
	checksums := hex.EncodeToString(sum[:]) + "  " + asset + "\n"
	return map[string][]byte{asset: pkg}, checksums
}

func TestRunUpgradeFlow(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "marker")
	assets, checksums := setupAssets(t, marker)
	f := newFakeRelease(t, "v1.4.1", assets, checksums, false)

	var out bytes.Buffer
	res, err := Run(Options{BaseURL: f.srv.URL, Current: "1.4.0", Out: &out})
	if err != nil {
		t.Fatalf("Run: %v\n输出:\n%s", err, out.String())
	}
	if res.Tag != "v1.4.1" || res.Version != "1.4.1" || res.Relation != RelUpgrade {
		t.Errorf("Result = %+v", res)
	}
	if res.UpToDate {
		t.Error("升级流程不应报 UpToDate")
	}
	if string(mustRead(t, marker)) != "done\n" {
		t.Errorf("install.sh 未按预期执行（marker=%q）", mustRead(t, marker))
	}
	if f.hits["latest"] != 1 {
		t.Errorf("latest 端点命中 %d 次", f.hits["latest"])
	}
}

func TestRunUpToDateSkipsDownload(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "marker")
	assets, checksums := setupAssets(t, marker)
	f := newFakeRelease(t, "v1.4.1", assets, checksums, false)

	res, err := Run(Options{BaseURL: f.srv.URL, Current: "1.4.1", Out: &bytes.Buffer{}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.UpToDate || res.Relation != RelSame {
		t.Errorf("Result = %+v", res)
	}
	assetPath := "/Norman-pong/padlink/releases/download/v1.4.1/" + AssetName("1.4.1", runtime.GOOS, runtime.GOARCH)
	if f.hits[assetPath] != 0 {
		t.Error("已是最新时不应下载资产")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Error("已是最新时不应执行 install.sh")
	}
}

func TestRunChecksumMismatchAbortsBeforeInstall(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "marker")
	assets, _ := setupAssets(t, marker)
	f := newFakeRelease(t, "v1.4.1", assets, "deadbeef  padlink_1.4.1_darwin_arm64.tar.gz\n", false)

	var out bytes.Buffer
	if _, err := Run(Options{BaseURL: f.srv.URL, Current: "1.4.0", Out: &out}); err == nil {
		t.Fatal("校验失败应返回错误")
	} else if !bytes.Contains(out.Bytes(), []byte("下载")) {
		t.Error("校验失败前应已开始下载")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Error("校验失败时 install.sh 不得执行（系统不得被改动）")
	}
}

func TestRunCheckOnlyNoDownload(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "marker")
	assets, checksums := setupAssets(t, marker)
	f := newFakeRelease(t, "v1.4.1", assets, checksums, false)

	res, err := Run(Options{BaseURL: f.srv.URL, Current: "1.3.0", CheckOnly: true, Out: &bytes.Buffer{}})
	if err != nil {
		t.Fatalf("Run --check: %v", err)
	}
	if res.Relation != RelUpgrade || res.Tag != "v1.4.1" {
		t.Errorf("Result = %+v", res)
	}
	assetPath := "/Norman-pong/padlink/releases/download/v1.4.1/" + AssetName("1.4.1", runtime.GOOS, runtime.GOARCH)
	if f.hits[assetPath] != 0 {
		t.Error("--check 不应下载资产")
	}
}

func TestRunExplicitTagDowngrade(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "marker")
	assets, checksums := setupAssets(t, marker)
	// latest 指向旧 tag 的场景不重要——显式指定 tag 时不请求 latest
	f := newFakeRelease(t, "v1.4.1", assets, checksums, false)

	res, err := Run(Options{BaseURL: f.srv.URL, Current: "1.4.1", Target: "v1.4.1", Out: &bytes.Buffer{}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Relation != RelSame || res.UpToDate {
		t.Errorf("显式指定同版本应重装: %+v", res)
	}

	res, err = Run(Options{BaseURL: f.srv.URL, Current: "1.5.0", Target: "1.4.1", Out: &bytes.Buffer{}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Relation != RelDowngrade {
		t.Errorf("显式回退应报 RelDowngrade: %+v", res)
	}
}

func TestRunMissingAsset404(t *testing.T) {
	assets, checksums := setupAssets(t, filepath.Join(t.TempDir(), "marker"))
	// 包内容只含 1.4.1 资产，把 latest 指到没有资产的 v2.0.0 → 404 快速失败不重试
	f := newFakeRelease(t, "v2.0.0", assets, checksums, false)

	if _, err := Run(Options{BaseURL: f.srv.URL, Current: "1.4.0", Out: &bytes.Buffer{}}); err == nil {
		t.Fatal("404 应返回错误")
	}
}

func TestRunRejectsTarTraversal(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "marker")
	evil := makeTarGz(t, map[string][]byte{
		"../evil.sh": []byte("exit 0\n"),
		"install.sh": []byte("echo done > " + marker + "\n"),
	})
	asset := AssetName("1.4.1", runtime.GOOS, runtime.GOARCH)
	sum := sha256.Sum256(evil)
	checksums := hex.EncodeToString(sum[:]) + "  " + asset + "\n"
	f := newFakeRelease(t, "v1.4.1", map[string][]byte{asset: evil}, checksums, false)

	if _, err := Run(Options{BaseURL: f.srv.URL, Current: "1.4.0", Out: &bytes.Buffer{}}); err == nil {
		t.Fatal("路径逃逸条目应报错")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Error("解包中止后 install.sh 不得执行")
	}
}

func TestRunMissingInstallScript(t *testing.T) {
	pkg := makeTarGz(t, map[string][]byte{"padlinkd": []byte("x")})
	asset := AssetName("1.4.1", runtime.GOOS, runtime.GOARCH)
	sum := sha256.Sum256(pkg)
	checksums := fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), asset)
	f := newFakeRelease(t, "v1.4.1", map[string][]byte{asset: pkg}, checksums, false)

	if _, err := Run(Options{BaseURL: f.srv.URL, Current: "1.4.0", Out: &bytes.Buffer{}}); err == nil {
		t.Fatal("包内缺 install.sh 应报错")
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s: %v", path, err)
	}
	return b
}
