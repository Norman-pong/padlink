package update

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Relation 是当前版本与目标版本的关系。
type Relation int

const (
	RelUpgrade   Relation = iota // 目标比当前新
	RelSame                      // 同版本
	RelDowngrade                 // 目标比当前旧（显式指定 tag 时的回退）
	RelDev                       // 当前为开发构建，无法比较
)

// Result 是一次 Run 的结果；CheckOnly 时只填前四个字段。
type Result struct {
	Tag      string   // 目标 tag（统一带 v 前缀，显示用）
	Version  string   // 目标版本号（无 v 前缀）
	Current  string   // 调用方传入的当前版本
	Relation Relation // 当前与目标的关系
	UpToDate bool     // 未指定目标且已最新：未下载未安装即返回
	Asset    string   // 实际下载的资产名
}

// Options 是 Run 的入参；零值即「最新版 + 默认 GitHub」。
type Options struct {
	Repo      string    // "" 用 DefaultRepo
	Target    string    // 指定目标（v1.4.0 / 1.4.0）；空 = releases/latest
	Current   string    // ctl 自报版本（不可解析视为开发构建）
	CheckOnly bool      // 只解析与比较，不下载不安装
	BaseURL   string    // "" 用 https://github.com（测试注入 httptest 地址）
	Out       io.Writer // 进度与安装脚本输出落点（nil 用 io.Discard）
}

// Run 执行完整流程。验签失败发生在任何系统改动之前（下载与解压都在临时目录，
// 安装脚本在验签通过后才执行）；安装失败后的系统状态由包内脚本决定，
// 重跑本命令或一行安装器均安全（脚本幂等）。
func Run(opts Options) (Result, error) {
	out := opts.Out
	if out == nil {
		out = io.Discard
	}
	base := strings.TrimRight(opts.BaseURL, "/")
	if base == "" {
		base = "https://github.com"
	}
	repo := opts.Repo
	if repo == "" {
		repo = DefaultRepo
	}

	tag := opts.Target
	if tag == "" {
		var err error
		if tag, err = latestTag(base, repo); err != nil {
			return Result{Current: opts.Current}, err
		}
	}
	ver := NormalizeTag(tag)
	if ver == "" {
		return Result{Current: opts.Current}, fmt.Errorf("目标版本为空")
	}
	res := Result{Tag: "v" + ver, Version: ver, Current: opts.Current}

	cur := NormalizeTag(opts.Current)
	if _, err := ParseVersion(cur); err != nil {
		res.Relation = RelDev
	} else {
		c, err := CompareVersions(cur, ver)
		if err != nil {
			return res, err
		}
		switch {
		case c < 0:
			res.Relation = RelUpgrade
		case c > 0:
			res.Relation = RelDowngrade
		default:
			res.Relation = RelSame
		}
	}
	if opts.CheckOnly {
		return res, nil
	}
	if opts.Target == "" && res.Relation == RelSame {
		res.UpToDate = true
		return res, nil
	}

	switch res.Relation {
	case RelSame:
		fmt.Fprintf(out, "目标 %s 与当前版本相同（重装）\n", res.Tag)
	case RelDowngrade:
		fmt.Fprintf(out, "当前 %s → 目标 %s（回退）\n", opts.Current, res.Tag)
	default:
		fmt.Fprintf(out, "当前 %s → 目标 %s\n", opts.Current, res.Tag)
	}

	asset := AssetName(ver, runtime.GOOS, runtime.GOARCH)
	res.Asset = asset
	dlBase := base + "/" + repo + "/releases/download/" + tag

	tmp, err := os.MkdirTemp("", "padlink-update-*")
	if err != nil {
		return res, fmt.Errorf("创建临时目录: %w", err)
	}
	defer os.RemoveAll(tmp)

	client := newClient()
	assetPath := filepath.Join(tmp, asset)
	fmt.Fprintf(out, "下载 %s …\n", asset)
	if err := fetch(client, dlBase+"/"+asset, assetPath); err != nil {
		return res, err
	}
	checksumsPath := filepath.Join(tmp, "checksums.txt")
	if err := fetch(client, dlBase+"/checksums.txt", checksumsPath); err != nil {
		return res, err
	}
	checksums, err := os.ReadFile(checksumsPath)
	if err != nil {
		return res, fmt.Errorf("读取 checksums.txt: %w", err)
	}
	want, err := FindChecksum(checksums, asset)
	if err != nil {
		return res, err
	}
	got, err := sha256File(assetPath)
	if err != nil {
		return res, err
	}
	if want != got {
		return res, fmt.Errorf("SHA-256 校验失败（期望 %s，实得 %s）——已中止，系统未做任何改动", want, got)
	}

	pkgDir := filepath.Join(tmp, "pkg")
	if err := extractTarGz(assetPath, pkgDir); err != nil {
		return res, err
	}
	// 安装逻辑单源在包内安装脚本：unix 为 install.sh，windows 为 install.ps1
	// （复制二进制 + HKCU Run 键 + 防火墙 + 重启 daemon，见 packaging/windows/install.ps1）
	installScript, cmd := "install.sh", exec.Command("sh", "./install.sh")
	if runtime.GOOS == "windows" {
		installScript = "install.ps1"
		cmd = exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", "./install.ps1")
	}
	if _, err := os.Stat(filepath.Join(pkgDir, installScript)); err != nil {
		return res, fmt.Errorf("release 包内缺少 %s，拒绝安装", installScript)
	}
	fmt.Fprintf(out, "SHA-256 校验通过，执行 %s …\n", installScript)
	cmd.Dir = pkgDir
	// install.sh 的 sudo（Linux 首装 udev）需要读终端；update 路径下规则已存在一般不会走到
	cmd.Stdin = os.Stdin
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Run(); err != nil {
		return res, fmt.Errorf("%s 执行失败: %w（可重试 padlinkctl update，或用一行安装器重装）", installScript, err)
	}
	return res, nil
}

// latestTag HEAD releases/latest（关闭自动跟随，取 302 的 Location）解析最新 tag。
// 弱网下 HEAD 超时常见（与 script/install.sh 的 --retry 2 同因），瞬时错误重试 2 次。
func latestTag(base, repo string) (string, error) {
	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	url := base + "/" + repo + "/releases/latest"
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * time.Second)
		}
		tag, retryable, err := resolveLatest(client, url)
		if err == nil {
			return tag, nil
		}
		if !retryable {
			return "", err
		}
		lastErr = err
	}
	return "", fmt.Errorf("查询最新版本失败（已重试 2 次）: %w", lastErr)
}

// resolveLatest 返回（tag, 是否瞬时错误, 错误）。
func resolveLatest(client *http.Client, url string) (string, bool, error) {
	req, err := http.NewRequest(http.MethodHead, url, nil)
	if err != nil {
		return "", false, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", true, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		return "", false, fmt.Errorf("查询最新版本失败: HTTP %d（预期 302 跳转）", resp.StatusCode)
	}
	loc, err := resp.Location()
	if err != nil {
		return "", false, fmt.Errorf("302 应答缺少有效 Location: %v", err)
	}
	tag, err := TagFromLocation(loc.String())
	return tag, false, err
}

// newClient 构造下载用客户端：连接 15s 超时、单请求总 5min 上限、走环境代理。
func newClient() *http.Client {
	return &http.Client{
		Timeout: 5 * time.Minute,
		Transport: &http.Transport{
			Proxy:               http.ProxyFromEnvironment,
			DialContext:         (&net.Dialer{Timeout: 15 * time.Second}).DialContext,
			TLSHandshakeTimeout: 10 * time.Second,
		},
	}
}

// statusError 是非 2xx 应答；404 与其他状态的处理策略不同（见 fetch）。
type statusError struct{ code int }

func (e *statusError) Error() string { return "HTTP " + strconv.Itoa(e.code) }

// fetch 下载 url 到 dest：404 直接失败（平台包未发布，重试无意义）；
// 网络错误与 5xx/429 视为瞬时错误，重试 2 次、线性退避；其余 4xx 立即失败。
func fetch(client *http.Client, url, dest string) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * time.Second)
		}
		err := fetchOnce(client, url, dest)
		if err == nil {
			return nil
		}
		var se *statusError
		if errors.As(err, &se) {
			if se.code == http.StatusNotFound {
				return fmt.Errorf("下载失败 404: %s（该平台的安装包可能尚未发布）", url)
			}
			if se.code < 500 && se.code != http.StatusTooManyRequests {
				return fmt.Errorf("下载失败: %w（%s）", err, url)
			}
		}
		lastErr = err
	}
	return fmt.Errorf("下载失败（已重试 2 次）: %w（%s）", lastErr, url)
}

func fetchOnce(client *http.Client, url, dest string) error {
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &statusError{resp.StatusCode}
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return nil
}

// extractTarGz 解包 release 包到 destDir。只接受常规文件（goreleaser 产物不含
// 其他类型，出现即视为包异常），并拒绝绝对路径与 ..（防御纵深，包本身已过验签）。
func extractTarGz(src, destDir string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("打开 gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("读取 tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			return fmt.Errorf("包内含非常规条目 %q（type %q），中止解包", hdr.Name, string(rune(hdr.Typeflag)))
		}
		name := filepath.Clean(hdr.Name)
		if filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, ".."+string(filepath.Separator)) {
			return fmt.Errorf("包内条目路径可疑: %q", hdr.Name)
		}
		target := filepath.Join(destDir, name)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		outFile, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(hdr.Mode)&0o777)
		if err != nil {
			return err
		}
		_, err = io.Copy(outFile, tr)
		if closeErr := outFile.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return fmt.Errorf("写出 %s: %w", hdr.Name, err)
		}
	}
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
