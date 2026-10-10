package proto

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// vectorsPath 定位仓库根的 protocol/testvectors.json（对拍单源，不得复制副本）。
func vectorsPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位源文件路径")
	}
	// 本包位于 <repo>/daemon/internal/proto，向上三级即仓库根
	root := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
	return filepath.Join(root, "protocol", "testvectors.json")
}

type vectorFile struct {
	TokenHex string         `json:"token_hex"`
	Vectors  []goldenVector `json:"vectors"`
}

type goldenVector struct {
	ID            int            `json:"id"`
	Desc          string         `json:"desc"`
	Hex           string         `json:"hex"`
	Valid         bool           `json:"valid"`
	Type          string         `json:"type"`
	Seq           *uint16        `json:"seq"`
	Authenticated bool           `json:"authenticated"`
	Semantics     map[string]any `json:"payload_semantics"`
	ExpectErr     string         `json:"expect_error"`
}

func loadVectors(t *testing.T) (vectorFile, []byte) {
	t.Helper()
	data, err := os.ReadFile(vectorsPath(t))
	if err != nil {
		t.Fatalf("读取 testvectors.json: %v", err)
	}
	var vf vectorFile
	if err := json.Unmarshal(data, &vf); err != nil {
		t.Fatalf("解析 testvectors.json: %v", err)
	}
	if len(vf.Vectors) == 0 {
		t.Fatal("testvectors.json 无向量")
	}
	return vf, data
}

var typeByName = map[string]Type{
	"HELLO": TypeHello, "DISCOVER_RESP": TypeDiscoverResp,
	"PAIR_REQ": TypePairReq, "PAIR_OK": TypePairOK, "PAIR_NAK": TypePairNak,
	"MOVE": TypeMove, "SCROLL": TypeScroll, "BUTTON": TypeButton,
	"KEY": TypeKey, "TEXT": TypeText, "ECHO": TypeEcho, "BYE": TypeBye, "ERR": TypeErr,
	"NOTICE": TypeNotice,
}

var expectErrByName = map[string]error{
	"bad_magic":        ErrBadMagic,
	"len_out_of_range": ErrLenRange,
	"truncated":        ErrTruncated,
}

func num(t *testing.T, m map[string]any, key string) float64 {
	t.Helper()
	v, ok := m[key].(float64)
	if !ok {
		t.Fatalf("向量语义字段 %q 缺失或非数字: %v", key, m[key])
	}
	return v
}

// TestGoldenVectors 对 protocol/testvectors.json 全量对拍（正/负用例）。
func TestGoldenVectors(t *testing.T) {
	vf, _ := loadVectors(t)
	token, err := hex.DecodeString(vf.TokenHex)
	if err != nil {
		t.Fatalf("token_hex 解码: %v", err)
	}

	for _, v := range vf.Vectors {
		v := v
		t.Run(fmt.Sprintf("vector_%d", v.ID), func(t *testing.T) {
			raw, err := hex.DecodeString(v.Hex)
			if err != nil {
				t.Fatalf("向量 hex 解码: %v", err)
			}

			if !v.Valid {
				_, err := Decode(raw)
				if err == nil {
					t.Fatalf("期望失败，实际解码成功")
				}
				want, ok := expectErrByName[v.ExpectErr]
				if !ok {
					t.Fatalf("未知 expect_error: %q", v.ExpectErr)
				}
				if !errors.Is(err, want) {
					t.Fatalf("期望 %v，实得 %v", want, err)
				}
				return
			}

			pkt, err := Decode(raw)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			wantType, ok := typeByName[v.Type]
			if !ok {
				t.Fatalf("未知向量类型名: %q", v.Type)
			}
			if pkt.Type != wantType {
				t.Errorf("type = %v, want %v", pkt.Type, wantType)
			}
			if v.Seq != nil && pkt.Seq != *v.Seq {
				t.Errorf("seq = %d, want %d", pkt.Seq, *v.Seq)
			}
			if got := pkt.Flags&FlagAuth != 0; got != v.Authenticated {
				t.Errorf("FlagAuth = %v, want %v", got, v.Authenticated)
			}
			if !v.Authenticated {
				for i, b := range pkt.HMAC {
					if b != 0 {
						t.Errorf("未认证向量 hmac 字段非零: [%d]=%#x", i, b)
						break
					}
				}
			}

			// 语义字段断言
			switch pkt.Type {
			case TypeMove:
				wantDX := int16(num(t, v.Semantics, "dx"))
				wantDY := int16(num(t, v.Semantics, "dy"))
				if pkt.Move.DX != wantDX || pkt.Move.DY != wantDY {
					t.Errorf("Move = %+v, want {DX:%d DY:%d}", pkt.Move, wantDX, wantDY)
				}
			case TypeScroll:
				want := int32(num(t, v.Semantics, "dy_hi_res"))
				if pkt.Scroll.DyHiRes != want {
					t.Errorf("Scroll = %+v, want {DyHiRes:%d}", pkt.Scroll, want)
				}
			case TypeButton:
				wantBtn := uint8(num(t, v.Semantics, "btn"))
				wantDown := v.Semantics["down"].(bool)
				if pkt.Button.Btn != wantBtn || pkt.Button.Down != wantDown {
					t.Errorf("Button = %+v, want {Btn:%d Down:%v}", pkt.Button, wantBtn, wantDown)
				}
			case TypeKey:
				usageStr, ok := v.Semantics["hid_usage"].(string)
				if !ok {
					t.Fatalf("hid_usage 缺失")
				}
				var wantUsage uint16
				if _, err := fmt.Sscanf(usageStr, "0x%04X", &wantUsage); err != nil {
					t.Fatalf("hid_usage 解析 %q: %v", usageStr, err)
				}
				wantDown := v.Semantics["down"].(bool)
				if pkt.Key.HIDUsage != wantUsage || pkt.Key.Down != wantDown {
					t.Errorf("Key = %+v, want {HIDUsage:%#04x Down:%v}", pkt.Key, wantUsage, wantDown)
				}
			case TypeText:
				wantText, ok := v.Semantics["text"].(string)
				if !ok {
					t.Fatalf("text 缺失")
				}
				if pkt.Text != wantText {
					t.Errorf("Text = %q, want %q", pkt.Text, wantText)
				}
				if string(pkt.Payload) != wantText {
					t.Errorf("Payload = %q, want %q", pkt.Payload, wantText)
				}
			case TypeEcho:
				want := int64(num(t, v.Semantics, "ts_ms"))
				if pkt.Echo.TsMs != want {
					t.Errorf("Echo = %+v, want {TsMs:%d}", pkt.Echo, want)
				}
			case TypeNotice:
				wantCode := uint8(num(t, v.Semantics, "code"))
				wantArg := uint8(num(t, v.Semantics, "arg"))
				if pkt.Notice.Code != wantCode || pkt.Notice.Arg != wantArg {
					t.Errorf("Notice = %+v, want {Code:%d Arg:%d}", pkt.Notice, wantCode, wantArg)
				}
			}

			// 重编码必须字节级一致
			re, err := Encode(&pkt)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			if string(re) != string(raw) {
				t.Errorf("重编码不一致\n got  %x\n want %x", re, raw)
			}

			// 向量 2：HMAC 对拍（校验 / 篡改 / 重签）
			if v.ID == 2 {
				if !VerifyHMAC(&pkt, token) {
					t.Errorf("VerifyHMAC(合法 token) = false, want true")
				}
				tampered := append([]byte(nil), raw...)
				tampered[len(tampered)-1] ^= 0x01 // 篡改 payload 末字节
				tp, err := Decode(tampered)
				if err != nil {
					t.Fatalf("Decode(篡改包): %v", err)
				}
				if VerifyHMAC(&tp, token) {
					t.Errorf("篡改后 VerifyHMAC = true, want false")
				}
				// Seal 重签：hmac 字段必须与向量一致，整包字节级还原
				fresh := NewMove(*v.Seq, pkt.Move.DX, pkt.Move.DY)
				Seal(&fresh, token)
				if fresh.HMAC != pkt.HMAC {
					t.Errorf("Seal hmac = %x, want %x", fresh.HMAC[:], pkt.HMAC[:])
				}
				resealed, err := Encode(&fresh)
				if err != nil {
					t.Fatalf("Encode(Seal 后): %v", err)
				}
				if string(resealed) != string(raw) {
					t.Errorf("Seal 重签整包不一致\n got  %x\n want %x", resealed, raw)
				}
			}
		})
	}
}

// build 构造一段任意报文字节（不校验），供负例使用。
func build(t Type, flags byte, seq uint16, plen uint16, payload []byte) []byte {
	b := make([]byte, HeaderSize+len(payload))
	b[0], b[1] = 'P', 'L'
	b[2] = Ver
	b[3] = byte(t)
	b[4] = flags
	b[5] = byte(seq >> 8)
	b[6] = byte(seq)
	b[7] = byte(plen >> 8)
	b[8] = byte(plen)
	copy(b[HeaderSize:], payload)
	return b
}

func TestDecodeNegative(t *testing.T) {
	t.Run("短于头长度", func(t *testing.T) {
		if _, err := Decode(make([]byte, HeaderSize-1)); !errors.Is(err, ErrTruncated) {
			t.Errorf("got %v, want ErrTruncated", err)
		}
		if _, err := Decode(nil); !errors.Is(err, ErrTruncated) {
			t.Errorf("got %v, want ErrTruncated", err)
		}
	})
	t.Run("坏版本号", func(t *testing.T) {
		raw := build(TypeMove, 0, 1, 4, []byte{0, 1, 0, 2})
		raw[2] = 2
		if _, err := Decode(raw); !errors.Is(err, ErrBadVersion) {
			t.Errorf("got %v, want ErrBadVersion", err)
		}
	})
	t.Run("未知类型 0xF0", func(t *testing.T) {
		if _, err := Decode(build(Type(0xF0), 0, 1, 0, nil)); !errors.Is(err, ErrUnknownType) {
			t.Errorf("got %v, want ErrUnknownType", err)
		}
	})
	t.Run("声明长度大于剩余字节", func(t *testing.T) {
		raw := build(TypeMove, 0, 1, 8, []byte{0, 0, 0, 0}) // plen=8 实际 4
		if _, err := Decode(raw); !errors.Is(err, ErrTruncated) {
			t.Errorf("got %v, want ErrTruncated", err)
		}
	})
	t.Run("剩余字节多于声明长度", func(t *testing.T) {
		raw := build(TypeMove, 0, 1, 4, []byte{0, 0, 0, 0, 0xFF})
		if _, err := Decode(raw); !errors.Is(err, ErrTruncated) {
			t.Errorf("got %v, want ErrTruncated", err)
		}
	})
	t.Run("各类型 payload 长度不符", func(t *testing.T) {
		cases := []struct {
			typ     Type
			payload []byte
		}{
			{TypeMove, []byte{0, 0, 0}},         // 3B
			{TypeScroll, []byte{0, 0, 0, 0, 0}}, // 5B
			{TypeButton, []byte{1}},             // 1B
			{TypeKey, []byte{0, 4}},             // 2B
			{TypeEcho, make([]byte, 7)},         // 7B
		}
		for _, c := range cases {
			_, err := Decode(build(c.typ, 0, 1, uint16(len(c.payload)), c.payload))
			if !errors.Is(err, ErrPayloadLen) {
				t.Errorf("type %v: got %v, want ErrPayloadLen", c.typ, err)
			}
		}
	})
	t.Run("TEXT 非法 UTF-8", func(t *testing.T) {
		bad := []byte{'a', 0xFF, 'b'}
		_, err := Decode(build(TypeText, 0, 1, uint16(len(bad)), bad))
		if !errors.Is(err, ErrBadUTF8) {
			t.Errorf("got %v, want ErrBadUTF8", err)
		}
	})
	t.Run("Encode 超限 payload", func(t *testing.T) {
		p := Packet{Type: TypeText, Payload: make([]byte, MaxPayload+1)}
		if _, err := Encode(&p); !errors.Is(err, ErrLenRange) {
			t.Errorf("got %v, want ErrLenRange", err)
		}
	})
}

func TestHMACRoundTrip(t *testing.T) {
	token := []byte("0123456789abcdef0123456789abcdef")
	pkt := NewText(42, "你好, padlink")
	Seal(&pkt, token)
	if pkt.Flags&FlagAuth == 0 {
		t.Fatal("Seal 未置 FlagAuth")
	}
	if !VerifyHMAC(&pkt, token) {
		t.Fatal("VerifyHMAC(正确 token) = false")
	}
	if VerifyHMAC(&pkt, []byte("wrong-token-wrong-token-123456")) {
		t.Fatal("VerifyHMAC(错误 token) = true")
	}

	// 未认证包直接判 false
	unsigned := NewEcho(7, 123)
	if VerifyHMAC(&unsigned, token) {
		t.Fatal("未置 FlagAuth 的包 VerifyHMAC = true")
	}

	// FlagAuth 置位但 hmac 全零（无 key 情况的防御）必须失败
	zeroed := NewMove(1, 1, 1)
	Seal(&zeroed, token)
	for i := range zeroed.HMAC {
		zeroed.HMAC[i] = 0
	}
	if VerifyHMAC(&zeroed, token) {
		t.Fatal("hmac 全零 VerifyHMAC = true")
	}

	// 逐字节篡改 payload 都必须失败
	big := NewText(9, string(make([]byte, 256)))
	Seal(&big, token)
	for i := range big.Payload {
		big.Payload[i] ^= 0xFF
		if VerifyHMAC(&big, token) {
			t.Fatalf("篡改 payload[%d] 后 VerifyHMAC = true", i)
		}
		big.Payload[i] ^= 0xFF
	}
	if !VerifyHMAC(&big, token) {
		t.Fatal("还原后 VerifyHMAC = false")
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	// 会话层类型 payload 透传往返
	raw := NewRaw(TypePairOK, 3, []byte{1, 2, 3, 4, 5})
	enc, err := Encode(&raw)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	dec, err := Decode(enc)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if string(dec.Payload) != string(raw.Payload) {
		t.Errorf("payload 往返不一致: %x vs %x", dec.Payload, raw.Payload)
	}
	if dec.Type != TypePairOK || dec.Seq != 3 {
		t.Errorf("头字段往返不一致: type=%v seq=%d", dec.Type, dec.Seq)
	}

	// 空 TEXT 合法
	empty := NewText(1, "")
	enc, _ = Encode(&empty)
	if _, err := Decode(enc); err != nil {
		t.Errorf("空 TEXT Decode: %v", err)
	}

	// TEXT 恰为 MaxPayload 上限
	max := NewText(2, string(make([]byte, MaxPayload)))
	enc, err = Encode(&max)
	if err != nil {
		t.Fatalf("MaxPayload TEXT Encode: %v", err)
	}
	if _, err := Decode(enc); err != nil {
		t.Errorf("MaxPayload TEXT Decode: %v", err)
	}
}
