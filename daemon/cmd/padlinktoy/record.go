package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"padlink/daemon/internal/proto"
)

type recordOptions struct {
	Out    io.Writer // .plrec 内容（二进制）
	ErrOut io.Writer // 摘要/告警（保证 Out 可安全重定向）
	Token  []byte    // 非空时帧就地封签（sealed 模式）
	Spec   synthSpec
}

// runRecord 生成合成录制并写出；人读摘要走 ErrOut。
func runRecord(o recordOptions) error {
	frames, err := synthEvents(o.Spec)
	if err != nil {
		return err
	}
	sealed := len(o.Token) > 0
	if sealed {
		for i := range frames {
			pkt, err := proto.Decode(frames[i].Data)
			if err != nil {
				return fmt.Errorf("内部错误：生成帧 %d 无法解码: %w", i, err)
			}
			proto.Seal(&pkt, o.Token)
			buf, err := proto.Encode(&pkt)
			if err != nil {
				return err
			}
			frames[i].Data = buf
		}
	}
	if err := WritePLREC(o.Out, Header{Sealed: sealed}, frames); err != nil {
		return err
	}

	counts := make(map[proto.Type]int, 4)
	for _, f := range frames {
		counts[proto.Type(f.Data[3])]++
	}
	mode := "raw"
	if sealed {
		mode = "sealed"
	}
	fmt.Fprintf(o.ErrOut, "已录制 %d 帧（%s）", len(frames), mode)
	for _, t := range []proto.Type{proto.TypeMove, proto.TypeScroll, proto.TypeButton, proto.TypeKey, proto.TypeText} {
		if counts[t] > 0 {
			fmt.Fprintf(o.ErrOut, " %s=%d", t, counts[t])
		}
	}
	fmt.Fprintln(o.ErrOut)
	return nil
}

func recordMain(args []string) error {
	fs := flag.NewFlagSet("padlinktoy record", flag.ExitOnError)
	out := fs.String("o", "", "输出文件路径（默认 stdout）")
	circle := fs.String("circle", "", "圆周 MOVE 段：\"半径 段数\"（如 \"200 120\"）")
	scroll := fs.Int("scroll", 0, "SCROLL 格数（每格 +120 hi-res）")
	text := fs.String("type", "", "TEXT 文本内容")
	buttons := fs.Bool("buttons", false, "附加左右键各单击一次")
	gapMs := fs.Int("gap-ms", 8, "相邻事件间隔（毫秒）")
	tokenHex := fs.String("token", "", "封签 token（hex，缺省读 PADLINK_TOKEN；提供则录制 sealed，否则 raw）")
	fs.Parse(args)
	// 支持 "--circle 半径 段数" 双值写法：flag 包在首个位置参数处停摆，
	// 此处把紧随其后的整数摘出并入 --circle 后重新解析剩余 flags。
	rest := fs.Args()
	if *circle != "" && !strings.ContainsAny(*circle, " \t") && len(rest) > 0 {
		if _, err := strconv.Atoi(rest[0]); err == nil {
			*circle = *circle + " " + rest[0]
			rest = rest[1:]
		}
	}
	if len(rest) > 0 {
		fs.Parse(rest)
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("多余参数 %q", fs.Args())
	}
	tok, err := resolveToken(*tokenHex)
	if err != nil {
		return err
	}

	spec := synthSpec{GapMs: *gapMs, ScrollNotches: *scroll, Buttons: *buttons, Text: *text}
	if *circle != "" {
		r, segs, err := parseTwoInts(*circle)
		if err != nil {
			return fmt.Errorf("--circle: %w", err)
		}
		spec.CircleR, spec.CircleSegs = r, segs
	}
	if spec.CircleR == 0 && spec.CircleSegs == 0 && spec.ScrollNotches == 0 && spec.Text == "" && !spec.Buttons {
		// 未指定任何生成段：默认对齐 padlinkd --test 的合成序列
		spec.CircleR, spec.CircleSegs = 200, 120
		spec.ScrollNotches = 3
		spec.Buttons = true
		spec.Text = "hello padlink"
	}

	var w io.Writer = os.Stdout
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			return fmt.Errorf("创建 %s: %w", *out, err)
		}
		defer f.Close()
		w = f
	}
	return runRecord(recordOptions{Out: w, ErrOut: os.Stderr, Token: tok, Spec: spec})
}
