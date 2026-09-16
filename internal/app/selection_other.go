//go:build !windows

package app

import "errors"

func ReadSelectedText() (string, error) {
	return "", errors.New("划词快捷收藏目前仅支持 Windows")
}
