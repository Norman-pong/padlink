package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// 黄金向量文件必须与生成器输出逐字节一致：改协议先改 vectors.go 再重生成，
// 手改 testvectors.json 会被本测试拦下。
func TestVectorsFileMatchesGenerator(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位源文件路径")
	}
	path := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "protocol", "testvectors.json")
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 testvectors.json: %v", err)
	}
	got, err := renderVectors()
	if err != nil {
		t.Fatalf("renderVectors: %v", err)
	}
	if string(got) != string(want) {
		t.Fatal("protocol/testvectors.json 与生成器输出不一致：请运行 `go -C daemon run ./cmd/padlinktoy vectors -w` 重生成")
	}
}
