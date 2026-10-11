package main

import (
	"fmt"
	"math"
	"unicode/utf8"

	"padlink/daemon/internal/proto"
)

// synthSpec 描述一次合成录制；各段固定按 圆周→滚动→按钮→文本 顺序拼接。
type synthSpec struct {
	CircleR       int // 圆周半径；0 = 不生成
	CircleSegs    int
	ScrollNotches int // SCROLL 格数（每格 +120 hi-res）
	Buttons       bool
	Text          string // 空 = 不生成
	GapMs         int    // 相邻事件间隔（毫秒）
}

// synthEvents 生成未封签帧序列（seq 从 1 全局自增）；MOVE/SCROLL 走 UDP，BUTTON/TEXT/KEY 走 TCP。
func synthEvents(spec synthSpec) ([]Frame, error) {
	if spec.GapMs < 0 {
		return nil, fmt.Errorf("gap-ms 不能为负")
	}
	if spec.CircleR < 0 || spec.CircleR > 30000 {
		return nil, fmt.Errorf("circle 半径取值 1..30000")
	}
	if spec.CircleSegs < 0 {
		return nil, fmt.Errorf("circle 段数不能为负")
	}
	if spec.ScrollNotches < 0 {
		return nil, fmt.Errorf("scroll 格数不能为负")
	}
	if spec.Text != "" && !utf8.ValidString(spec.Text) {
		return nil, fmt.Errorf("文本内容不是合法 UTF-8")
	}

	var frames []Frame
	var seq uint16
	ms := uint64(0)
	add := func(ch Channel, pkt *proto.Packet) error {
		pkt.Seq = seq + 1
		buf, err := proto.Encode(pkt)
		if err != nil {
			return err
		}
		frames = append(frames, Frame{RelMs: ms, Chan: ch, Data: buf})
		seq++
		ms += uint64(spec.GapMs)
		return nil
	}

	if spec.CircleSegs > 0 || spec.CircleR > 0 {
		if spec.CircleR <= 0 || spec.CircleSegs <= 0 {
			return nil, fmt.Errorf("--circle 需要 \"半径 段数\" 且均 > 0")
		}
		// 圆周相邻点差分（与 padlinkd --test 同法）：末点回到起点，净位移恒为零。
		prevX, prevY := float64(spec.CircleR), 0.0
		for i := 1; i <= spec.CircleSegs; i++ {
			a := 2 * math.Pi * float64(i) / float64(spec.CircleSegs)
			x := math.Round(float64(spec.CircleR) * math.Cos(a))
			y := math.Round(float64(spec.CircleR) * math.Sin(a))
			dx, dy := x-prevX, y-prevY
			if math.Abs(dx) > 32767 || math.Abs(dy) > 32767 {
				return nil, fmt.Errorf("圆周段位移越界（r=%d segs=%d），请增大段数", spec.CircleR, spec.CircleSegs)
			}
			pkt := proto.NewMove(0, int16(dx), int16(dy))
			if err := add(ChanUDP, &pkt); err != nil {
				return nil, err
			}
			prevX, prevY = x, y
		}
	}
	for i := 0; i < spec.ScrollNotches; i++ {
		pkt := proto.NewScroll(0, 120)
		if err := add(ChanUDP, &pkt); err != nil {
			return nil, err
		}
	}
	if spec.Buttons {
		for _, b := range [4]struct {
			btn  uint8
			down bool
		}{{1, true}, {1, false}, {3, true}, {3, false}} {
			pkt := proto.NewButton(0, b.btn, b.down)
			if err := add(ChanTCP, &pkt); err != nil {
				return nil, err
			}
		}
	}
	if spec.Text != "" {
		pkt := proto.NewText(0, spec.Text)
		if err := add(ChanTCP, &pkt); err != nil {
			return nil, err
		}
	}
	return frames, nil
}
