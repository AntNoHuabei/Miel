//go:build !windows

package app

func applyNativeWindowTheme(_ uintptr, _ string) error {
	return nil
}
