package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	"padlink/daemon/internal/proto"
)

// vectors 子命令：以 internal/proto 实现生成 protocol/testvectors.json。
// 正向向量的 hex 一律由编码器现算（手写 hex 不进库）；负向向量是故意畸形字节，
// 编码器产不出，以字面量承载。改协议先改本文件，再 `padlinktoy vectors -w` 重生成，
// 双端对拍（Go proto_test / tools/prototest）随之校验。
const vectorsTokenHex = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"

type vectorOut struct {
	ID          int    `json:"id"`
	Desc        string `json:"desc"`
	Hex         string `json:"hex"`
	Valid       bool   `json:"valid"`
	Type        string `json:"type,omitempty"`
	Seq         uint16 `json:"seq,omitempty"`
	Auth        bool   `json:"authenticated"`
	Semantics   any    `json:"payload_semantics,omitempty"`
	ExpectError string `json:"expect_error,omitempty"`
}

// 语义结构体：键序即输出序（encoding/json 的 map 字典序不可控）；
// 字段名与两端断言（proto_test / prototest）耦合，改名须两端同改。
type semMove struct {
	DX int16 `json:"dx"`
	DY int16 `json:"dy"`
}

type semScroll struct {
	DyHiRes int32 `json:"dy_hi_res"`
}

type semButton struct {
	Btn     uint8  `json:"btn"`
	BtnName string `json:"btn_name"`
	Down    bool   `json:"down"`
}

type semKey struct {
	HidUsage     string `json:"hid_usage"`
	Key          string `json:"key"`
	LinuxKeyCode int    `json:"linux_key_code"`
	Down         bool   `json:"down"`
}

type semText struct {
	Text string `json:"text"`
}

type semEcho struct {
	TsMs int64 `json:"ts_ms"`
}

type semNotice struct {
	Code uint8 `json:"code"`
	Arg  uint8 `json:"arg"`
}

type semHello struct {
	Ver        int    `json:"ver"`
	PayloadHex string `json:"payload_hex"`
}

type semPairReq struct {
	Form       string `json:"form"`
	Name       string `json:"name,omitempty"`
	Dev        string `json:"dev,omitempty"`
	Code       string `json:"code,omitempty"`
	PayloadHex string `json:"payload_hex,omitempty"`
}

type semPairOK struct {
	TokenHex   string `json:"token_hex"`
	PayloadHex string `json:"payload_hex"`
}

type semPairNak struct {
	Reason     int    `json:"reason"`
	ReasonName string `json:"reason_name"`
	PayloadHex string `json:"payload_hex"`
}

type semErr struct {
	Code       int    `json:"code"`
	PayloadHex string `json:"payload_hex"`
}

type semDiscoverResp struct {
	PayloadHex string `json:"payload_hex"`
}

// orderedTypes 保持 types 表的数字序输出（encoding/json 对 map 按字典序会打乱 1..14）。
type orderedTypes struct{}

func (orderedTypes) MarshalJSON() ([]byte, error) {
	names := map[proto.Type]string{
		proto.TypeHello: "HELLO", proto.TypeDiscoverResp: "DISCOVER_RESP",
		proto.TypePairReq: "PAIR_REQ", proto.TypePairOK: "PAIR_OK", proto.TypePairNak: "PAIR_NAK",
		proto.TypeMove: "MOVE", proto.TypeScroll: "SCROLL", proto.TypeButton: "BUTTON",
		proto.TypeKey: "KEY", proto.TypeText: "TEXT", proto.TypeEcho: "ECHO",
		proto.TypeBye: "BYE", proto.TypeErr: "ERR", proto.TypeNotice: "NOTICE",
	}
	out := []byte{'{'}
	for t := 1; t <= 14; t++ {
		name, ok := names[proto.Type(t)]
		if !ok {
			return nil, fmt.Errorf("types 表缺 type %d", t)
		}
		if t > 1 {
			out = append(out, ',')
		}
		out = append(out, fmt.Sprintf("%q:%q", strconv.Itoa(t), name)...)
	}
	return append(out, '}'), nil
}

type vectorsFileOut struct {
	Spec         string            `json:"spec"`
	Consumer     string            `json:"consumer"`
	HeaderLayout string            `json:"header_layout"`
	HeaderSize   int               `json:"header_size_bytes"`
	NotePRD      string            `json:"note_prd_discrepancy"`
	ByteOrder    string            `json:"byte_order"`
	Flags        map[string]string `json:"flags"`
	TokenHex     string            `json:"token_hex"`
	TokenNote    string            `json:"token_note"`
	Types        orderedTypes      `json:"types"`
	Vectors      []vectorOut       `json:"vectors"`
}

// sealHex 构造并封签编码（authenticated 向量用 demo token 封签）。
func sealHex(pkt proto.Packet, token []byte, authenticated bool) (string, error) {
	if authenticated {
		proto.Seal(&pkt, token)
	}
	buf, err := proto.Encode(&pkt)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func buildVectors() ([]vectorOut, error) {
	token, err := hex.DecodeString(vectorsTokenHex)
	if err != nil {
		return nil, err
	}
	mk := func(id int, desc string, pkt proto.Packet, typ string, seq uint16, auth bool, sem any) (vectorOut, error) {
		h, err := sealHex(pkt, token, auth)
		if err != nil {
			return vectorOut{}, err
		}
		return vectorOut{ID: id, Desc: desc, Hex: h, Valid: true, Type: typ, Seq: seq, Auth: auth, Semantics: sem}, nil
	}
	pairReqJson := `{"name":"口袋键鼠","dev":"aabbccddeeff00112233445566778899aabbccddeeff"}`
	discoverJson := `{"name":"office","os":"darwin","daemon":"0.1.0","ver":1,"paired":1,"port":53021}`
	out := make([]vectorOut, 0, 21)
	v1, err := mk(1, "MOVE dx=-5 dy=3 (cursor relative motion), unauthenticated pre-pairing",
		proto.NewMove(1, -5, 3), "MOVE", 1, false, semMove{DX: -5, DY: 3})
	if err != nil {
		return nil, err
	}
	v2, err := mk(2, "MOVE dx=123 dy=-45 with per-packet HMAC (token = token_hex below)",
		proto.NewMove(2, 123, -45), "MOVE", 2, true, semMove{DX: 123, DY: -45})
	if err != nil {
		return nil, err
	}
	v3, err := mk(3, "SCROLL dy_hi_res=+240 (two wheel notches; unit=1/120 detent, maps to REL_WHEEL_HI_RES)",
		proto.NewScroll(3, 240), "SCROLL", 3, false, semScroll{DyHiRes: 240})
	if err != nil {
		return nil, err
	}
	v4, err := mk(4, "BUTTON btn=1(left) down=1 (tap/long-press press phase)",
		proto.NewButton(4, 1, true), "BUTTON", 4, false, semButton{Btn: 1, BtnName: "left", Down: true})
	if err != nil {
		return nil, err
	}
	v5, err := mk(5, "BUTTON btn=3(right) down=0 (release)",
		proto.NewButton(5, 3, false), "BUTTON", 5, false, semButton{Btn: 3, BtnName: "right", Down: false})
	if err != nil {
		return nil, err
	}
	v6, err := mk(6, "KEY hid=0x0004 (HID keyboard usage A) down=1; daemon maps HID->Linux KEY_A(30)",
		proto.NewKey(6, 0x0004, true), "KEY", 6, false,
		semKey{HidUsage: "0x0004", Key: "A", LinuxKeyCode: 30, Down: true})
	if err != nil {
		return nil, err
	}
	v7, err := mk(7, "TEXT utf8='PadLink 你好' (clipboard-inject path)",
		proto.NewText(7, "PadLink 你好"), "TEXT", 7, false, semText{Text: "PadLink 你好"})
	if err != nil {
		return nil, err
	}
	v8, err := mk(8, "ECHO ts_ms=1760000000123 (Unix ms, RTT measurement)",
		proto.NewEcho(8, 1760000000123), "ECHO", 8, false, semEcho{TsMs: 1760000000123})
	if err != nil {
		return nil, err
	}
	v12, err := mk(12, "NOTICE code=0 (control held by peer device) arg=15 (cooldown seconds left), daemon->phone",
		proto.NewNotice(9, proto.NoticeControlBusy, 15), "NOTICE", 9, false, semNotice{Code: 0, Arg: 15})
	if err != nil {
		return nil, err
	}
	v13, err := mk(13, "HELLO payload 2B ver=1 (version negotiation, unsealed by protocol)",
		proto.NewRaw(proto.TypeHello, 10, []byte{0, byte(proto.Ver)}), "HELLO", 10, false,
		semHello{Ver: proto.Ver, PayloadHex: "0001"})
	if err != nil {
		return nil, err
	}
	v14, err := mk(14, "PAIR_REQ initiate with empty payload (0B form)",
		proto.NewRaw(proto.TypePairReq, 11, nil), "PAIR_REQ", 11, false,
		semPairReq{Form: "initiate"})
	if err != nil {
		return nil, err
	}
	v15, err := mk(15, "PAIR_REQ initiate with JSON {name, dev fingerprint} form",
		proto.NewRaw(proto.TypePairReq, 12, []byte(pairReqJson)), "PAIR_REQ", 12, false,
		semPairReq{Form: "initiate", Name: "口袋键鼠",
			Dev:        "aabbccddeeff00112233445566778899aabbccddeeff",
			PayloadHex: hex.EncodeToString([]byte(pairReqJson))})
	if err != nil {
		return nil, err
	}
	v16, err := mk(16, "PAIR_REQ confirm code '4821' (4B ASCII digits form)",
		proto.NewRaw(proto.TypePairReq, 13, []byte("4821")), "PAIR_REQ", 13, false,
		semPairReq{Form: "confirm_code", Code: "4821", PayloadHex: hex.EncodeToString([]byte("4821"))})
	if err != nil {
		return nil, err
	}
	// PAIR_OK 实际流程用刚签发的新 token 封签；向量统一以 demo token 封签保证可再生（desc 注明）。
	v17, err := mk(17, "PAIR_OK payload = fresh 32B token (demo token here), sealed with that token",
		proto.NewRaw(proto.TypePairOK, 14, token), "PAIR_OK", 14, true,
		semPairOK{TokenHex: vectorsTokenHex, PayloadHex: vectorsTokenHex})
	if err != nil {
		return nil, err
	}
	v18, err := mk(18, "PAIR_NAK reason=1 (code expired), unsealed by protocol",
		proto.NewRaw(proto.TypePairNak, 15, []byte{1}), "PAIR_NAK", 15, false,
		semPairNak{Reason: 1, ReasonName: "expired", PayloadHex: "01"})
	if err != nil {
		return nil, err
	}
	v19, err := mk(19, "ERR code=2 (protocol violation), daemon->phone, unsealed",
		proto.NewRaw(proto.TypeErr, 16, []byte{2}), "ERR", 16, false,
		semErr{Code: 2, PayloadHex: "02"})
	if err != nil {
		return nil, err
	}
	v20, err := mk(20, "BYE empty payload (graceful close)",
		proto.NewRaw(proto.TypeBye, 17, nil), "BYE", 17, false, nil)
	if err != nil {
		return nil, err
	}
	v21, err := mk(21, "DISCOVER_RESP host descriptor JSON, daemon->phone, unsealed",
		proto.NewRaw(proto.TypeDiscoverResp, 18, []byte(discoverJson)), "DISCOVER_RESP", 18, false,
		semDiscoverResp{PayloadHex: hex.EncodeToString([]byte(discoverJson))})
	if err != nil {
		return nil, err
	}
	v22, err := mk(22, "NOTICE code=1 (TEXT not injected) arg=2 (clipboard tool unavailable), daemon->phone",
		proto.NewNotice(19, proto.NoticeTextFailed, proto.TextFailUnavailable), "NOTICE", 19, false,
		semNotice{Code: 1, Arg: 2})
	if err != nil {
		return nil, err
	}
	out = append(out, v1, v2, v3, v4, v5, v6, v7, v8,
		vectorOut{ID: 9, Desc: "INVALID: magic is 'XL' not 'PL' — receiver must drop+count",
			Hex: "584c010600000900040000000000000000000000010001", Valid: false, ExpectError: "bad_magic"},
		vectorOut{ID: 10, Desc: "INVALID: payload_len=2048 exceeds MaxPayload=1400 — drop+count",
			Hex: "504c010b00000a0800000000000000000000000000000000000001", Valid: false, ExpectError: "len_out_of_range"},
		vectorOut{ID: 11, Desc: "INVALID: payload truncated (payload_len=4 but only 2 bytes follow) — drop+count",
			Hex: "504c010600000b0004000000000000000000000007", Valid: false, ExpectError: "truncated"},
		v12, v13, v14, v15, v16, v17, v18, v19, v20, v21, v22)
	return out, nil
}

func renderVectors() ([]byte, error) {
	vs, err := buildVectors()
	if err != nil {
		return nil, err
	}
	doc := vectorsFileOut{
		Spec:         "PadLink wire protocol v1 golden vectors (single source of truth for cross-implementation testing)",
		Consumer:     "HarmonyOS app + Go daemon must both pass all vectors; generate more via daemon/internal/proto",
		HeaderLayout: "magic(2B 'PL') | ver(1B) | type(1B) | flags(1B) | seq(2B) | payload_len(2B) | reserved(2B) | hmac_trunc(8B) | payload(<=1400B)",
		HeaderSize:   19,
		NotePRD:      "PRD v1.0 §5.2 says '16 字节' header, but its field list sums to 19 bytes; the field list is authoritative and these vectors pin 19 bytes. Any change must be decided in protocol/ first.",
		ByteOrder:    "big-endian for seq/payload_len/reserved and all multi-byte payload integers",
		Flags:        map[string]string{"0x01": "FlagAuth: hmac_trunc is HMAC-SHA256(token, header-with-zeroed-hmac || payload)[0:8]"},
		TokenHex:     vectorsTokenHex,
		TokenNote:    "32-byte DEMO token for vectors only; real tokens are random 32B issued at pairing (PRD §4.1)",
		Types:        orderedTypes{},
		Vectors:      vs,
	}
	// SetEscapeHTML(false)：desc/header_layout 含 <、> 与 ->，不得被转成 \u003c 字面量；
	// Encoder 自带结尾换行。
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func vectorsMain(args []string) error {
	fs := flag.NewFlagSet("vectors", flag.ContinueOnError)
	write := fs.Bool("w", false, "写入 protocol/testvectors.json（默认打印到 stdout）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	buf, err := renderVectors()
	if err != nil {
		return err
	}
	if !*write {
		os.Stdout.Write(buf)
		return nil
	}
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return fmt.Errorf("无法定位源文件路径")
	}
	path := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "protocol", "testvectors.json")
	return os.WriteFile(path, buf, 0o644)
}
