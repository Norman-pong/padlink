package main

import (
	"testing"

	"padlink/daemon/internal/proto"
)

func decodeFrames(t *testing.T, frames []Frame) []proto.Packet {
	t.Helper()
	out := make([]proto.Packet, 0, len(frames))
	for i, f := range frames {
		pkt, err := proto.Decode(f.Data)
		if err != nil {
			t.Fatalf("帧 %d 解码失败: %v", i, err)
		}
		out = append(out, pkt)
	}
	return out
}

func TestSynthCircle(t *testing.T) {
	const segs, gap = 90, 2
	frames, err := synthEvents(synthSpec{CircleR: 100, CircleSegs: segs, GapMs: gap})
	if err != nil {
		t.Fatalf("synthEvents: %v", err)
	}
	if len(frames) != segs {
		t.Fatalf("帧数 = %d, want %d", len(frames), segs)
	}
	for i, f := range frames {
		if f.Chan != ChanUDP {
			t.Errorf("帧 %d 通道 = %v, want UDP", i, f.Chan)
		}
		if f.RelMs != uint64(i*gap) {
			t.Errorf("帧 %d 时间戳 = %d, want %d", i, f.RelMs, i*gap)
		}
	}
	var sumX, sumY int64
	for i, pkt := range decodeFrames(t, frames) {
		if pkt.Type != proto.TypeMove {
			t.Fatalf("帧 %d 类型 = %v, want MOVE", i, pkt.Type)
		}
		if pkt.Seq != uint16(i+1) {
			t.Errorf("帧 %d seq = %d, want %d", i, pkt.Seq, i+1)
		}
		sumX += int64(pkt.Move.DX)
		sumY += int64(pkt.Move.DY)
	}
	if sumX != 0 || sumY != 0 {
		t.Errorf("圆周净位移 ΣX=%d ΣY=%d, want 0/0", sumX, sumY)
	}
}

func TestSynthScrollButtonsText(t *testing.T) {
	const notches, gap = 2, 8
	spec := synthSpec{ScrollNotches: notches, Buttons: true, Text: "你好", GapMs: gap}
	frames, err := synthEvents(spec)
	if err != nil {
		t.Fatalf("synthEvents: %v", err)
	}
	if len(frames) != notches+4+1 {
		t.Fatalf("帧数 = %d, want %d", len(frames), notches+4+1)
	}
	pkts := decodeFrames(t, frames)
	for i := 0; i < len(frames); i++ {
		if frames[i].RelMs != uint64(i*gap) {
			t.Errorf("帧 %d 时间戳 = %d, want %d", i, frames[i].RelMs, i*gap)
		}
	}
	for i := 0; i < notches; i++ {
		if frames[i].Chan != ChanUDP || pkts[i].Type != proto.TypeScroll || pkts[i].Scroll.DyHiRes != 120 {
			t.Errorf("滚动帧 %d = {%v %v %+v}, want UDP SCROLL +120", i, frames[i].Chan, pkts[i].Type, pkts[i].Scroll)
		}
	}
	wantBtns := [4]struct {
		btn  uint8
		down bool
	}{{1, true}, {1, false}, {3, true}, {3, false}}
	for i, want := range wantBtns {
		j := notches + i
		if frames[j].Chan != ChanTCP || pkts[j].Type != proto.TypeButton ||
			pkts[j].Button.Btn != want.btn || pkts[j].Button.Down != want.down {
			t.Errorf("按钮帧 %d = {%v %v %+v}, want TCP BUTTON %v", j, frames[j].Chan, pkts[j].Type, pkts[j].Button, want)
		}
	}
	last := len(frames) - 1
	if frames[last].Chan != ChanTCP || pkts[last].Type != proto.TypeText || pkts[last].Text != spec.Text {
		t.Errorf("文本帧 = {%v %v %q}, want TCP TEXT %q", frames[last].Chan, pkts[last].Type, pkts[last].Text, spec.Text)
	}
}

func TestSynthRejects(t *testing.T) {
	cases := []struct {
		name string
		spec synthSpec
	}{
		{"位移越界", synthSpec{CircleR: 30000, CircleSegs: 2, GapMs: 1}},
		{"非法 UTF-8", synthSpec{Text: "\xff", GapMs: 1}},
		{"缺半径", synthSpec{CircleSegs: 5, GapMs: 1}},
		{"负 gap", synthSpec{GapMs: -1}},
		{"负格数", synthSpec{ScrollNotches: -1, GapMs: 1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := synthEvents(c.spec); err == nil {
				t.Fatal("未报错")
			}
		})
	}
}

func TestSynthEmpty(t *testing.T) {
	frames, err := synthEvents(synthSpec{GapMs: 8})
	if err != nil {
		t.Fatalf("synthEvents: %v", err)
	}
	if len(frames) != 0 {
		t.Errorf("帧数 = %d, want 0", len(frames))
	}
}
