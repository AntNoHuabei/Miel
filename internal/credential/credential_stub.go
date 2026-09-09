//go:build !windows

package credential

import "errors"

// Set/Get/Delete 的非 Windows 占位:返回“不支持”,由调用方回退到数据库存储。

func Set(target, value string) error {
	return errors.New("当前平台不支持系统凭据管理")
}

func Get(target string) (string, error) {
	return "", errors.New("当前平台不支持系统凭据管理")
}

func Delete(target string) error {
	return errors.New("当前平台不支持系统凭据管理")
}
