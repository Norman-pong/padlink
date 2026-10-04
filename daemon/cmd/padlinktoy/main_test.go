package main

import (
	"bytes"
	"testing"
)

func TestResolveToken(t *testing.T) {
	t.Run("hex 参数", func(t *testing.T) {
		tok, err := resolveToken("aabb")
		if err != nil {
			t.Fatalf("resolveToken: %v", err)
		}
		if !bytes.Equal(tok, []byte{0xAA, 0xBB}) {
			t.Errorf("tok = %v, want aabb", tok)
		}
	})
	t.Run("非法 hex 报错", func(t *testing.T) {
		if _, err := resolveToken("zzzz"); err == nil {
			t.Fatal("未报错")
		}
	})
	t.Run("环境变量回退", func(t *testing.T) {
		t.Setenv("PADLINK_TOKEN", "ccdd")
		tok, err := resolveToken("")
		if err != nil {
			t.Fatalf("resolveToken: %v", err)
		}
		if !bytes.Equal(tok, []byte{0xCC, 0xDD}) {
			t.Errorf("tok = %v, want ccdd", tok)
		}
	})
	t.Run("参数优先于环境变量", func(t *testing.T) {
		t.Setenv("PADLINK_TOKEN", "eeff")
		tok, err := resolveToken("aabb")
		if err != nil {
			t.Fatalf("resolveToken: %v", err)
		}
		if !bytes.Equal(tok, []byte{0xAA, 0xBB}) {
			t.Errorf("tok = %v, want aabb", tok)
		}
	})
	t.Run("皆空为 nil", func(t *testing.T) {
		t.Setenv("PADLINK_TOKEN", "")
		tok, err := resolveToken("")
		if err != nil || tok != nil {
			t.Errorf("got (%v, %v), want (nil, nil)", tok, err)
		}
	})
}

func TestParseTwoInts(t *testing.T) {
	a, b, err := parseTwoInts("200 120")
	if err != nil || a != 200 || b != 120 {
		t.Errorf("got (%d, %d, %v), want (200, 120, nil)", a, b, err)
	}
	for _, s := range []string{"200", "200 120 30", "a b", "200,x"} {
		if _, _, err := parseTwoInts(s); err == nil {
			t.Errorf("parseTwoInts(%q) 未报错", s)
		}
	}
}
