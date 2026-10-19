package session

import (
	"net"
	"reflect"
	"testing"
	"time"

	"padlink/daemon/internal/inject"
	"padlink/daemon/internal/proto"
)

func TestMotionChannelsProduceSameFrames(t *testing.T) {
	for _, channel := range []string{"TCP", "UDP"} {
		t.Run(channel, func(t *testing.T) {
			ts := newTestServer(t, nil)
			token := prepair(t, ts)
			var send func(proto.Packet)
			if channel == "TCP" {
				c := dialTCP(t, ts.srv.Port())
				c.hello(1, 1)
				authEcho(t, c, token)
				send = c.send
			} else {
				uc, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: ts.srv.Port()})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { uc.Close() })
				send = func(pkt proto.Packet) {
					t.Helper()
					buf, err := proto.Encode(&pkt)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := uc.Write(buf); err != nil {
						t.Fatal(err)
					}
				}
			}
			for _, pkt := range []proto.Packet{
				proto.NewMove(2, -12, 34),
				proto.NewScroll(3, -240),
				proto.NewScroll(4, 60),
			} {
				proto.Seal(&pkt, token)
				send(pkt)
			}
			waitFor(t, "三帧注入", func() bool { return ts.cap.count(evSync, 0, 0) == 3 })
			want := []ev{
				{evRel, inject.RelX, -12}, {evRel, inject.RelY, 34}, {kind: evSync},
				{evRel, inject.RelWheelHiRes, -240}, {evRel, inject.RelWheel, -2}, {kind: evSync},
				{evRel, inject.RelWheelHiRes, 60}, {kind: evSync},
			}
			ts.cap.mu.Lock()
			got := append([]ev(nil), ts.cap.events...)
			ts.cap.mu.Unlock()
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("注入事件 = %v，期望 %v", got, want)
			}
		})
	}
}

func TestUDPRejectsReliableInputs(t *testing.T) {
	ts := newTestServerInj(t, nil, inject.Config{}, func(clientID, text string) error {
		t.Error("UDP TEXT 不应进入文本注入")
		return nil
	})
	token := prepair(t, ts)
	uc, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: ts.srv.Port()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { uc.Close() })
	before := ts.srv.StatsSnapshot().Dropped
	for _, pkt := range []proto.Packet{
		proto.NewButton(1, 1, true),
		proto.NewKey(2, inject.HIDA, true),
		proto.NewText(3, "不应注入"),
	} {
		proto.Seal(&pkt, token)
		buf, err := proto.Encode(&pkt)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := uc.Write(buf); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, "UDP 专属通道检查", func() bool { return ts.srv.StatsSnapshot().Dropped == before+3 })
	ts.cap.mu.Lock()
	defer ts.cap.mu.Unlock()
	if len(ts.cap.events) != 0 {
		t.Fatalf("UDP 不应注入可靠事件：%v", ts.cap.events)
	}
}

func TestTextAndKeysShareInputQueue(t *testing.T) {
	entered := make(chan string, 1)
	release := make(chan struct{})
	ts := newTestServerInj(t, nil, inject.Config{RepeatDelay: time.Hour}, func(clientID, text string) error {
		entered <- text
		<-release
		return nil
	})
	// 即使断言失败，也须先放行 TEXT，避免服务清理等待注入队列。
	defer close(release)
	token := prepair(t, ts)
	c := dialTCP(t, ts.srv.Port())
	c.hello(1, 1)
	authEcho(t, c, token)
	text := proto.NewText(2, "串行文本")
	proto.Seal(&text, token)
	c.send(text)
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("TEXT 未进入注入队列")
	}
	for _, pkt := range []proto.Packet{
		proto.NewKey(3, inject.HIDA, true),
		proto.NewKey(4, inject.HIDA, false),
		proto.NewEcho(5, 777),
	} {
		proto.Seal(&pkt, token)
		c.send(pkt)
	}
	// ECHO 回包确认 TCP 已处理完前面的 KEY，不依赖延时猜测。
	if pkt := c.mustRecv(time.Second); pkt.Type != proto.TypeEcho {
		t.Fatalf("期望 ECHO，实得 %v", pkt.Type)
	}
	if got := ts.cap.keySeq(inject.HIDToKey[inject.HIDA]); len(got) != 0 {
		t.Fatalf("TEXT 完成前不应注入 KEY：%v", got)
	}
	release <- struct{}{}
	waitFor(t, "TEXT 后按序注入 KEY", func() bool {
		return reflect.DeepEqual(ts.cap.keySeq(inject.HIDToKey[inject.HIDA]), []int32{1, 0})
	})
}
