package main

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

func sampleFrames() []Frame {
	return []Frame{
		{RelMs: 0, Chan: ChanUDP, Data: make([]byte, 23)},
		{RelMs: 8, Chan: ChanTCP, Data: bytes.Repeat([]byte{0xAB}, 25)},
		{RelMs: 65536, Chan: ChanUDP, Data: bytes.Repeat([]byte{0x01}, MaxFrameLen)},
	}
}

func TestWriteReadRoundTrip(t *testing.T) {
	for _, sealed := range []bool{false, true} {
		var buf bytes.Buffer
		if err := WritePLREC(&buf, Header{Sealed: sealed}, sampleFrames()); err != nil {
			t.Fatalf("WritePLREC(sealed=%v): %v", sealed, err)
		}
		h, frames, err := ReadPLREC(bytes.NewReader(buf.Bytes()))
		if err != nil {
			t.Fatalf("ReadPLREC(sealed=%v): %v", sealed, err)
		}
		if h.Sealed != sealed {
			t.Errorf("Sealed = %v, want %v", h.Sealed, sealed)
		}
		want := sampleFrames()
		if len(frames) != len(want) {
			t.Fatalf("帧数 = %d, want %d", len(frames), len(want))
		}
		for i := range want {
			if frames[i].RelMs != want[i].RelMs || frames[i].Chan != want[i].Chan || !bytes.Equal(frames[i].Data, want[i].Data) {
				t.Errorf("帧 %d 不一致: got {%d %v %dB}, want {%d %v %dB}",
					i, frames[i].RelMs, frames[i].Chan, len(frames[i].Data),
					want[i].RelMs, want[i].Chan, len(want[i].Data))
			}
		}
	}
}

func TestHeaderLine(t *testing.T) {
	var buf bytes.Buffer
	if err := WritePLREC(&buf, Header{Sealed: true}, nil); err != nil {
		t.Fatalf("WritePLREC: %v", err)
	}
	if got, want := buf.String(), "PLREC 1 sealed\n"; got != want {
		t.Errorf("头行 = %q, want %q", got, want)
	}
}

func TestWriteRejectsBadFrames(t *testing.T) {
	if err := WritePLREC(&bytes.Buffer{}, Header{}, []Frame{{Chan: ChanUDP, Data: nil}}); err == nil {
		t.Error("空帧未报错")
	}
	if err := WritePLREC(&bytes.Buffer{}, Header{}, []Frame{{Chan: ChanUDP, Data: make([]byte, MaxFrameLen+1)}}); err == nil {
		t.Error("超长帧未报错")
	}
	if err := WritePLREC(&bytes.Buffer{}, Header{}, []Frame{{Chan: Channel(9), Data: []byte{1}}}); err == nil {
		t.Error("非法通道未报错")
	}
}

func TestReadRejectsBadHeader(t *testing.T) {
	cases := []struct {
		name string
		file string
	}{
		{"错 magic", "PLREK 1 raw\n"},
		{"版本过高", "PLREC 2 raw\n"},
		{"版本非数字", "PLREC x raw\n"},
		{"未知标记", "PLREC 1 half\n"},
		{"字段缺失", "PLREC 1\n"},
		{"头行过长", strings.Repeat("A", 600) + "\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, _, err := ReadPLREC(strings.NewReader(c.file)); err == nil {
				t.Fatal("未报错")
			}
		})
	}
}

func TestReadRejectsCorruptFrames(t *testing.T) {
	header := "PLREC 1 raw\n"
	cases := []struct {
		name string
		body []byte
	}{
		{"帧长 0", rec(0, 0, 0, nil)},
		{"帧长超限", rec(0, 0, MaxFrameLen+1, make([]byte, 100))},
		{"通道非法", rec(0, 7, 4, make([]byte, 4))},
		{"数据截断", rec(0, 1, 10, make([]byte, 3))},
		{"记录头截断", []byte{0, 0, 0}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data := append([]byte(header), c.body...)
			if _, _, err := ReadPLREC(bytes.NewReader(data)); err == nil {
				t.Fatal("未报错")
			}
		})
	}
}

func TestReadHeaderOnly(t *testing.T) {
	h, frames, err := ReadPLREC(strings.NewReader("PLREC 1 raw\n"))
	if err != nil {
		t.Fatalf("ReadPLREC: %v", err)
	}
	if h.Sealed || len(frames) != 0 {
		t.Errorf("h=%v frames=%d, want raw/0", h, len(frames))
	}
}

// rec 组装一条 12B 记录头 + 数据。
func rec(relMs uint64, chanID, length uint16, data []byte) []byte {
	b := make([]byte, 12)
	binary.BigEndian.PutUint64(b[0:8], relMs)
	binary.BigEndian.PutUint16(b[8:10], chanID)
	binary.BigEndian.PutUint16(b[10:12], length)
	return append(b, data...)
}
