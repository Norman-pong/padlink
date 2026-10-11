package main

import (
	"encoding/binary"
	"math/rand"

	"padlink/daemon/internal/proto"
)

type mutateKind uint8

const (
	mBitFlip mutateKind = iota
	mTruncate
	mPayloadLen
	mHeaderField
	mUnknownType // type 改为 0 或 ≥14
	mOversize    // payload_len 1401..2048，实发等长
	mBadUTF8
	mRandomHMAC
	mKindCount // 哨兵，须在末尾
)

func (k mutateKind) String() string {
	switch k {
	case mBitFlip:
		return "bitflip"
	case mTruncate:
		return "truncate"
	case mPayloadLen:
		return "payload_len"
	case mHeaderField:
		return "header_field"
	case mUnknownType:
		return "unknown_type"
	case mOversize:
		return "oversize"
	case mBadUTF8:
		return "bad_utf8"
	case mRandomHMAC:
		return "random_hmac"
	default:
		return "unknown"
	}
}

// mutate 对合法线帧应用一种变异，返回新字节切片（不改入参）；入参须 ≥ HeaderSize。
func mutate(rng *rand.Rand, frame []byte, kind mutateKind) []byte {
	out := append([]byte(nil), frame...)
	switch kind {
	case mBitFlip:
		n := []int{1, 2, 4, 8}[rng.Intn(4)]
		flipped := make(map[int]bool, n)
		for i := 0; i < n && i < len(out)*8; i++ {
			var pos int
			for tries := 0; tries < 16; tries++ { // 保证 n 个不同字节各变一位
				pos = rng.Intn(len(out))
				if !flipped[pos] {
					break
				}
			}
			flipped[pos] = true
			out[pos] ^= 1 << rng.Intn(8)
		}
	case mTruncate:
		out = out[:rng.Intn(proto.HeaderSize)]
	case mPayloadLen:
		if len(out) >= proto.HeaderSize {
			binary.BigEndian.PutUint16(out[7:9], uint16(rng.Intn(1<<16)))
		}
	case mHeaderField:
		if len(out) >= proto.HeaderSize {
			off := []int{0, 1, 2, 4}[rng.Intn(4)] // magic[2]/ver/flags
			out[off] = byte(rng.Intn(256))
		}
	case mUnknownType:
		if len(out) >= proto.HeaderSize {
			if rng.Intn(4) == 0 {
				out[3] = 0
			} else {
				out[3] = byte(14 + rng.Intn(242))
			}
		}
	case mOversize:
		claim := proto.MaxPayload + 1 + rng.Intn(648) // 1401..2048
		payload := make([]byte, claim)
		rng.Read(payload)
		binary.BigEndian.PutUint16(out[7:9], uint16(claim))
		out = append(out[:proto.HeaderSize], payload...)
	case mBadUTF8:
		n := 1 + rng.Intn(64)
		payload := make([]byte, n)
		rng.Read(payload)
		payload[rng.Intn(n)] = 0xFF // 0xFF 恒为非法 UTF-8 序列
		out[3] = byte(proto.TypeText)
		binary.BigEndian.PutUint16(out[7:9], uint16(n))
		out = append(out[:proto.HeaderSize], payload...)
	case mRandomHMAC:
		out[4] |= proto.FlagAuth
		rng.Read(out[11:19])
	}
	return out
}
