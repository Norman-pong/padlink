//go:build !darwin

package textinject

import "errors"

// 非 darwin 平台不使用原生粘贴板后端（pbClipboard 仅 darwin 选用），
// 保留同名符号以共享 textinject.go 的编译。
var (
	pbSnapshot = func() ([]byte, error) { return nil, errors.New("原生粘贴板后端仅 macOS 可用") }
	pbRestore  = func([]byte) error { return errors.New("原生粘贴板后端仅 macOS 可用") }
	pbSetText  = func(string) error { return errors.New("原生粘贴板后端仅 macOS 可用") }
	pbClear    = func() error { return errors.New("原生粘贴板后端仅 macOS 可用") }
)
