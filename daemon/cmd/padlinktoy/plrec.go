package main

// .plrec 录制格式（token 不入文件）：
// 行 1：PLREC <版本> <raw|sealed>\n（raw 回放时须现场封签，sealed 原样发送）；
// 其后每帧：8B 大端 ms 时间戳 | 2B 大端通道（0=UDP，1=TCP） | 2B 大端帧长 N（≤ 19+1400） | N 字节线协议帧

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"padlink/daemon/internal/proto"
)

const (
	FormatVersion = 1
	RecMagic      = "PLREC"
	// MaxFrameLen 单帧长度上限（19B 头 + 1400B payload）。
	MaxFrameLen = proto.HeaderSize + proto.MaxPayload
)

type Channel uint16

const (
	ChanUDP Channel = 0
	ChanTCP Channel = 1
)

func (c Channel) Valid() bool { return c == ChanUDP || c == ChanTCP }

func (c Channel) String() string {
	if c == ChanUDP {
		return "UDP"
	}
	return "TCP"
}

type Header struct {
	Sealed bool // 帧是否已在录制时封签
}

type Frame struct {
	RelMs uint64 // 相对时间戳（毫秒）
	Chan  Channel
	Data  []byte // 完整线协议帧
}

func headerLine(h Header) string {
	mode := "raw"
	if h.Sealed {
		mode = "sealed"
	}
	return RecMagic + " " + strconv.Itoa(FormatVersion) + " " + mode + "\n"
}

func WritePLREC(w io.Writer, h Header, frames []Frame) error {
	if _, err := io.WriteString(w, headerLine(h)); err != nil {
		return fmt.Errorf("写头行: %w", err)
	}
	rec := make([]byte, 12)
	for i, f := range frames {
		if len(f.Data) == 0 || len(f.Data) > MaxFrameLen {
			return fmt.Errorf("帧 %d 长度 %d 超界（1..%d）", i, len(f.Data), MaxFrameLen)
		}
		if !f.Chan.Valid() {
			return fmt.Errorf("帧 %d 通道非法: %d", i, f.Chan)
		}
		binary.BigEndian.PutUint64(rec[0:8], f.RelMs)
		binary.BigEndian.PutUint16(rec[8:10], uint16(f.Chan))
		binary.BigEndian.PutUint16(rec[10:12], uint16(len(f.Data)))
		if _, err := w.Write(rec); err != nil {
			return fmt.Errorf("写帧 %d 记录头: %w", i, err)
		}
		if _, err := w.Write(f.Data); err != nil {
			return fmt.Errorf("写帧 %d 数据: %w", i, err)
		}
	}
	return nil
}

func ReadPLREC(r io.Reader) (Header, []Frame, error) {
	var h Header
	br := bufio.NewReaderSize(r, 512)
	line, err := br.ReadSlice('\n')
	if err != nil {
		if errors.Is(err, bufio.ErrBufferFull) {
			return h, nil, fmt.Errorf("头行超过 512B，非 %s 格式", RecMagic)
		}
		return h, nil, fmt.Errorf("读头行: %w", err)
	}
	fields := strings.Fields(strings.TrimRight(string(line), "\r\n"))
	if len(fields) != 3 || fields[0] != RecMagic {
		return h, nil, fmt.Errorf("头行非 %s 格式: %q", RecMagic, line)
	}
	ver, err := strconv.Atoi(fields[1])
	if err != nil || ver != FormatVersion {
		return h, nil, fmt.Errorf("格式版本 %q 不受支持（当前 %d）", fields[1], FormatVersion)
	}
	switch fields[2] {
	case "sealed":
		h.Sealed = true
	case "raw":
	default:
		return h, nil, fmt.Errorf("未知封签标记 %q（应为 raw|sealed）", fields[2])
	}

	var frames []Frame
	rec := make([]byte, 12)
	for i := 0; ; i++ {
		if _, err := io.ReadFull(br, rec); err != nil {
			if errors.Is(err, io.EOF) {
				return h, frames, nil
			}
			return h, nil, fmt.Errorf("读帧 %d 记录头: %w", i, err)
		}
		f := Frame{
			RelMs: binary.BigEndian.Uint64(rec[0:8]),
			Chan:  Channel(binary.BigEndian.Uint16(rec[8:10])),
		}
		n := int(binary.BigEndian.Uint16(rec[10:12]))
		if n == 0 || n > MaxFrameLen {
			return h, nil, fmt.Errorf("帧 %d 帧长 %d 超界（1..%d）", i, n, MaxFrameLen)
		}
		if !f.Chan.Valid() {
			return h, nil, fmt.Errorf("帧 %d 通道非法: %d", i, f.Chan)
		}
		f.Data = make([]byte, n)
		if _, err := io.ReadFull(br, f.Data); err != nil {
			return h, nil, fmt.Errorf("读帧 %d 数据: %w", i, err)
		}
		frames = append(frames, f)
	}
}
