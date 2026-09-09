//go:build !windows

package capture

import (
	"fmt"
	"image"
)

// Screen 非 Windows 平台暂未实现(占位;后续按平台补 Linux/macOS)。
func Screen() (image.Image, error) {
	return nil, fmt.Errorf("当前平台暂不支持截屏")
}
