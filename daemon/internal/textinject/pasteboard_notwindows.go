//go:build !windows

package textinject

// winClipboard 非 windows 平台不存在该后端；platformClipboard 的 windows 分支在这些
// 平台不可达，仅为编译完整性声明（值不会被消费）。
var winClipboard = wlClipboard
