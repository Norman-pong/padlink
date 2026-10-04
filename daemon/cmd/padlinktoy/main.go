// padlinktoy 是 PadLink 联调工具三件套（PRD §2.2 tools/ 的 daemon 侧承载）：
// record/replay 合成录制回放、fuzz 协议变异模糊测试、latency ECHO RTT 压测。
// 与 daemon 同模块、共用 internal/proto 协议实现，零第三方依赖。
// token 一律经 -token（hex）或环境变量 PADLINK_TOKEN 提供，不写日志、不入录制文件。
package main

import (
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const defaultPort = 53021

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "record":
		err = recordMain(os.Args[2:])
	case "replay":
		err = replayMain(os.Args[2:])
	case "fuzz":
		err = fuzzMain(os.Args[2:])
	case "latency":
		err = latencyMain(os.Args[2:])
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "未知子命令 %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "padlinktoy: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `padlinktoy — PadLink 联调工具（录制回放 / 协议 fuzz / 延迟压测）

用法: padlinktoy <子命令> [flags]

子命令:
  record   生成合成事件录制，写入 .plrec 文件（默认 stdout）
  replay   将 .plrec 回放到目标 daemon
  fuzz     协议变异模糊测试，结束后做 daemon 活性判定
  latency  ECHO 往返时延（RTT）压测，可叠加 MOVE 背景负载

token 一律经 -token（hex）或环境变量 PADLINK_TOKEN 提供；不写日志、不入录制文件。
各子命令 -h 查看完整 flags。
`)
}

// resolveToken 取 -token 或环境变量 PADLINK_TOKEN（前者优先）；皆空返回 nil，
// 是否必需由调用方决定。token 只作返回值，不打印。
func resolveToken(flagVal string) ([]byte, error) {
	v := flagVal
	if v == "" {
		v = os.Getenv("PADLINK_TOKEN")
	}
	if v == "" {
		return nil, nil
	}
	tok, err := hex.DecodeString(v)
	if err != nil {
		return nil, fmt.Errorf("token 不是合法 hex: %w", err)
	}
	return tok, nil
}

// parseTwoInts 解析 "a b" 双整型 flag 值。
func parseTwoInts(s string) (int, int, error) {
	parts := strings.Fields(s)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("需要两个空格分隔的整数，实得 %q", s)
	}
	a, errA := strconv.Atoi(parts[0])
	b, errB := strconv.Atoi(parts[1])
	if errA != nil || errB != nil {
		return 0, 0, fmt.Errorf("需要两个空格分隔的整数，实得 %q", s)
	}
	return a, b, nil
}
