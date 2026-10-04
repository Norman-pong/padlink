package main

import (
	"bytes"
	"encoding/binary"
	"math/rand"
	"testing"
	"unicode/utf8"

	"padlink/daemon/internal/proto"
)

func baseFrame(t *testing.T) []byte {
	t.Helper()
	pkt := proto.NewMove(1, 3, -2)
	b, err := proto.Encode(&pkt)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	return b
}

func requireSameLen(t *testing.T, in, out []byte) {
	t.Helper()
	if len(out) != len(in) {
		t.Fatalf("长度改变: %d → %d", len(in), len(out))
	}
}

func TestMutateKindInvariants(t *testing.T) {
	base := baseFrame(t)
	cases := []struct {
		kind  mutateKind
		check func(t *testing.T, in, out []byte)
	}{
		{mBitFlip, func(t *testing.T, in, out []byte) {
			requireSameLen(t, in, out)
			if bytes.Equal(in, out) {
				t.Fatal("位翻转未改变帧")
			}
		}},
		{mTruncate, func(t *testing.T, in, out []byte) {
			if len(out) >= proto.HeaderSize {
				t.Fatalf("截断长度 = %d, want < %d", len(out), proto.HeaderSize)
			}
		}},
		{mPayloadLen, requireSameLen},
		{mHeaderField, requireSameLen},
		{mUnknownType, func(t *testing.T, in, out []byte) {
			requireSameLen(t, in, out)
			if out[3] != 0 && out[3] < 14 {
				t.Fatalf("type = %d, want 0 或 ≥14", out[3])
			}
		}},
		{mOversize, func(t *testing.T, in, out []byte) {
			claim := int(binary.BigEndian.Uint16(out[7:9]))
			if claim < proto.MaxPayload+1 || claim > 2048 {
				t.Fatalf("payload_len = %d, want 1401..2048", claim)
			}
			if len(out) != proto.HeaderSize+claim {
				t.Fatalf("实长 = %d, want %d", len(out), proto.HeaderSize+claim)
			}
		}},
		{mBadUTF8, func(t *testing.T, in, out []byte) {
			if proto.Type(out[3]) != proto.TypeText {
				t.Fatalf("type = %v, want TEXT", proto.Type(out[3]))
			}
			n := int(binary.BigEndian.Uint16(out[7:9]))
			if n != len(out)-proto.HeaderSize {
				t.Fatalf("payload_len = %d, 实得 %dB", n, len(out)-proto.HeaderSize)
			}
			if utf8.Valid(out[proto.HeaderSize:]) {
				t.Fatal("payload 是合法 UTF-8")
			}
		}},
		{mRandomHMAC, func(t *testing.T, in, out []byte) {
			requireSameLen(t, in, out)
			if out[4]&proto.FlagAuth == 0 {
				t.Fatal("FlagAuth 未置位")
			}
			if bytes.Equal(in[11:19], out[11:19]) {
				t.Fatal("hmac 未随机化")
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.kind.String(), func(t *testing.T) {
			rng := rand.New(rand.NewSource(42)) // 固定种子，结果可复现
			for i := 0; i < 200; i++ {
				out := mutate(rng, base, c.kind)
				c.check(t, base, out)
			}
		})
	}
}

func TestMutateDoesNotModifyInput(t *testing.T) {
	base := baseFrame(t)
	snapshot := append([]byte(nil), base...)
	rng := rand.New(rand.NewSource(1))
	for k := mutateKind(0); k < mKindCount; k++ {
		mutate(rng, base, k)
	}
	if !bytes.Equal(base, snapshot) {
		t.Fatal("mutate 修改了入参")
	}
}

func TestMutateKindNames(t *testing.T) {
	seen := make(map[string]bool, int(mKindCount))
	for k := mutateKind(0); k < mKindCount; k++ {
		name := k.String()
		if name == "" || name == "unknown" {
			t.Errorf("策略 %d 名称异常: %q", k, name)
		}
		if seen[name] {
			t.Errorf("策略名重复: %q", name)
		}
		seen[name] = true
	}
}
