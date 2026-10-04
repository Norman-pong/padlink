// Package proto 实现 PadLink 线协议 v1 的编解码（19 字节头 + payload，大端序）。
// 一致性以 protocol/testvectors.json 黄金向量为单源。
package proto

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"fmt"
	"unicode/utf8"
)

// 线协议常量（PROTOCOL.md §2）。
const (
	HeaderSize = 19   // 固定头长度
	MaxPayload = 1400 // payload 上限（UDP 分片安全值）
	Ver        = 1    // 协议版本
	FlagAuth   = 0x01 // flags bit0：hmac_trunc 有效
)

// magic 恒为 'PL'。
var magic = [2]byte{'P', 'L'}

// Type 事件类型枚举（PROTOCOL.md §3）。
type Type uint8

const (
	TypeHello        Type = 1
	TypeDiscoverResp Type = 2
	TypePairReq      Type = 3
	TypePairOK       Type = 4
	TypePairNak      Type = 5
	TypeMove         Type = 6
	TypeScroll       Type = 7
	TypeButton       Type = 8
	TypeKey          Type = 9
	TypeText         Type = 10
	TypeEcho         Type = 11
	TypeBye          Type = 12
	TypeErr          Type = 13
)

func (t Type) String() string {
	switch t {
	case TypeHello:
		return "HELLO"
	case TypeDiscoverResp:
		return "DISCOVER_RESP"
	case TypePairReq:
		return "PAIR_REQ"
	case TypePairOK:
		return "PAIR_OK"
	case TypePairNak:
		return "PAIR_NAK"
	case TypeMove:
		return "MOVE"
	case TypeScroll:
		return "SCROLL"
	case TypeButton:
		return "BUTTON"
	case TypeKey:
		return "KEY"
	case TypeText:
		return "TEXT"
	case TypeEcho:
		return "ECHO"
	case TypeBye:
		return "BYE"
	case TypeErr:
		return "ERR"
	default:
		return fmt.Sprintf("UNKNOWN(0x%02X)", uint8(t))
	}
}

// 哨兵错误，用 errors.Is 判断；上下文细节经 %w 包装在同一错误链上。
var (
	ErrBadMagic    = errors.New("bad magic")
	ErrBadVersion  = errors.New("unsupported protocol version")
	ErrLenRange    = errors.New("payload length out of range")
	ErrTruncated   = errors.New("truncated packet")
	ErrUnknownType = errors.New("unknown event type")
	ErrPayloadLen  = errors.New("payload length mismatch for event type")
	ErrBadHMAC     = errors.New("HMAC verification failed")
	ErrBadUTF8     = errors.New("TEXT payload is not valid UTF-8")
)

// 定长 payload 的类型化字段。
type (
	// MOVE：相对指针位移，单位为计数。
	Move struct {
		DX, DY int16
	}
	// SCROLL：高分辨率滚轮，单位 = 1/120 滚轮格。
	Scroll struct {
		DyHiRes int32
	}
	// BUTTON：btn 1=左 2=中 3=右。
	Button struct {
		Btn  uint8
		Down bool
	}
	// KEY：USB HID 键盘页 usage，由注入层映射 Linux KEY_*。
	Key struct {
		HIDUsage uint16
		Down     bool
	}
	// ECHO：发送方 Unix 毫秒时间戳，对端原样回传。
	Echo struct {
		TsMs int64
	}
)

// Packet 是一次解码后的完整报文。类型化字段仅在与 Type 匹配时有效；
// 会话层类型（HELLO/DISCOVER_RESP/PAIR_*/BYE/ERR）的 payload 保留原始字节。
type Packet struct {
	Type  Type
	Flags uint8
	Seq   uint16
	HMAC  [8]byte // hmac_trunc，未认证时全 0

	Payload []byte // 原始 payload（Decode 时为独立副本）

	Move   Move
	Scroll Scroll
	Button Button
	Key    Key
	Echo   Echo
	Text   string // Type==TypeText 时有效
}

// Decode 按 PROTOCOL.md §2.2 顺序校验并解析报文。
// FlagAuth 置位时不在此校验 HMAC（本层不持有 key），由调用方调 VerifyHMAC。
func Decode(data []byte) (Packet, error) {
	var p Packet
	if len(data) < HeaderSize {
		return p, fmt.Errorf("%w: 需要 %d 字节头，实得 %d", ErrTruncated, HeaderSize, len(data))
	}
	if data[0] != magic[0] || data[1] != magic[1] {
		return p, fmt.Errorf("%w: % 02x", ErrBadMagic, data[0:2])
	}
	if data[2] != Ver {
		return p, fmt.Errorf("%w: ver=%d", ErrBadVersion, data[2])
	}
	p.Type = Type(data[3])
	p.Flags = data[4]
	p.Seq = binary.BigEndian.Uint16(data[5:7])
	plen := int(binary.BigEndian.Uint16(data[7:9]))
	// 偏移 9..11 为 reserved，接收不校验
	copy(p.HMAC[:], data[11:19])
	if plen > MaxPayload {
		return p, fmt.Errorf("%w: payload_len=%d > %d", ErrLenRange, plen, MaxPayload)
	}
	if plen != len(data)-HeaderSize {
		return p, fmt.Errorf("%w: payload_len=%d，实得 %d 字节", ErrTruncated, plen, len(data)-HeaderSize)
	}
	p.Payload = append([]byte(nil), data[HeaderSize:]...)

	switch p.Type {
	case TypeMove:
		if len(p.Payload) != 4 {
			return p, fmt.Errorf("%w: MOVE 需要 4B，实得 %d", ErrPayloadLen, len(p.Payload))
		}
		p.Move = Move{
			DX: int16(binary.BigEndian.Uint16(p.Payload[0:2])),
			DY: int16(binary.BigEndian.Uint16(p.Payload[2:4])),
		}
	case TypeScroll:
		if len(p.Payload) != 4 {
			return p, fmt.Errorf("%w: SCROLL 需要 4B，实得 %d", ErrPayloadLen, len(p.Payload))
		}
		p.Scroll = Scroll{DyHiRes: int32(binary.BigEndian.Uint32(p.Payload))}
	case TypeButton:
		if len(p.Payload) != 2 {
			return p, fmt.Errorf("%w: BUTTON 需要 2B，实得 %d", ErrPayloadLen, len(p.Payload))
		}
		p.Button = Button{Btn: p.Payload[0], Down: p.Payload[1] != 0}
	case TypeKey:
		if len(p.Payload) != 3 {
			return p, fmt.Errorf("%w: KEY 需要 3B，实得 %d", ErrPayloadLen, len(p.Payload))
		}
		p.Key = Key{
			HIDUsage: binary.BigEndian.Uint16(p.Payload[0:2]),
			Down:     p.Payload[2] != 0,
		}
	case TypeText:
		if !utf8.Valid(p.Payload) {
			return p, ErrBadUTF8
		}
		p.Text = string(p.Payload)
	case TypeEcho:
		if len(p.Payload) != 8 {
			return p, fmt.Errorf("%w: ECHO 需要 8B，实得 %d", ErrPayloadLen, len(p.Payload))
		}
		p.Echo = Echo{TsMs: int64(binary.BigEndian.Uint64(p.Payload))}
	case TypeHello, TypeDiscoverResp, TypePairReq, TypePairOK, TypePairNak, TypeBye, TypeErr:
		// 会话层 payload 本层不解析，保留在 p.Payload
	default:
		return p, fmt.Errorf("%w: type=0x%02X", ErrUnknownType, uint8(p.Type))
	}
	return p, nil
}

// Encode 将报文序列化为线格式；reserved 置 0，hmac 字段原样写入 pkt.HMAC。
func Encode(pkt *Packet) ([]byte, error) {
	if len(pkt.Payload) > MaxPayload {
		return nil, fmt.Errorf("%w: %d > %d", ErrLenRange, len(pkt.Payload), MaxPayload)
	}
	return marshal(pkt, false), nil
}

// Seal 计算并写入 hmac_trunc，置 FlagAuth。
// hmac_trunc = HMAC-SHA256(key=token, 头 19B（hmac 字段全零）|| payload)[:8]。
func Seal(pkt *Packet, token []byte) {
	pkt.Flags |= FlagAuth
	pkt.HMAC = hmacTrunc(pkt, token)
}

// VerifyHMAC 校验已认证报文的 hmac_trunc（常数时间比较）。
// FlagAuth 未置位、或比较失败均返回 false。
func VerifyHMAC(pkt *Packet, token []byte) bool {
	if pkt.Flags&FlagAuth == 0 {
		return false
	}
	sum := hmacTrunc(pkt, token)
	return subtle.ConstantTimeCompare(sum[:], pkt.HMAC[:]) == 1
}

func hmacTrunc(pkt *Packet, token []byte) [8]byte {
	m := hmac.New(sha256.New, token)
	m.Write(marshal(pkt, true))
	var out [8]byte
	copy(out[:], m.Sum(nil))
	return out
}

// marshal 序列化报文；zeroHMAC 时偏移 11..19 以全零参与（HMAC 计算输入）。
func marshal(pkt *Packet, zeroHMAC bool) []byte {
	buf := make([]byte, HeaderSize+len(pkt.Payload))
	buf[0], buf[1] = magic[0], magic[1]
	buf[2] = Ver
	buf[3] = byte(pkt.Type)
	buf[4] = pkt.Flags
	binary.BigEndian.PutUint16(buf[5:7], pkt.Seq)
	binary.BigEndian.PutUint16(buf[7:9], uint16(len(pkt.Payload)))
	// 9..11 reserved 恒 0
	if !zeroHMAC {
		copy(buf[11:19], pkt.HMAC[:])
	}
	copy(buf[HeaderSize:], pkt.Payload)
	return buf
}

// NewRaw 构造会话层报文（HELLO/DISCOVER_RESP/PAIR_*/BYE/ERR），payload 透传。
func NewRaw(t Type, seq uint16, payload []byte) Packet {
	return Packet{Type: t, Seq: seq, Payload: append([]byte(nil), payload...)}
}

// NewMove 构造 MOVE 报文。
func NewMove(seq uint16, dx, dy int16) Packet {
	payload := make([]byte, 4)
	binary.BigEndian.PutUint16(payload[0:2], uint16(dx))
	binary.BigEndian.PutUint16(payload[2:4], uint16(dy))
	return Packet{Type: TypeMove, Seq: seq, Payload: payload}
}

// NewScroll 构造 SCROLL 报文。
func NewScroll(seq uint16, dyHiRes int32) Packet {
	payload := make([]byte, 4)
	binary.BigEndian.PutUint32(payload, uint32(dyHiRes))
	return Packet{Type: TypeScroll, Seq: seq, Payload: payload}
}

// NewButton 构造 BUTTON 报文（btn 1=左 2=中 3=右）。
func NewButton(seq uint16, btn uint8, down bool) Packet {
	var downByte byte
	if down {
		downByte = 1
	}
	return Packet{Type: TypeButton, Seq: seq, Payload: []byte{btn, downByte}}
}

// NewKey 构造 KEY 报文。
func NewKey(seq uint16, hidUsage uint16, down bool) Packet {
	payload := make([]byte, 3)
	binary.BigEndian.PutUint16(payload[0:2], hidUsage)
	if down {
		payload[2] = 1
	}
	return Packet{Type: TypeKey, Seq: seq, Payload: payload}
}

// NewText 构造 TEXT 报文（payload 即 UTF-8 原始字节）。
func NewText(seq uint16, text string) Packet {
	return Packet{Type: TypeText, Seq: seq, Payload: []byte(text)}
}

// NewEcho 构造 ECHO 报文。
func NewEcho(seq uint16, tsMs int64) Packet {
	payload := make([]byte, 8)
	binary.BigEndian.PutUint64(payload, uint64(tsMs))
	return Packet{Type: TypeEcho, Seq: seq, Payload: payload}
}
