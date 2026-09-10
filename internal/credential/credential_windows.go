// Package credential 提供 Windows Credential Manager 凭据管理。
package credential

import (
	"errors"
	"syscall"
	"unsafe"
)

var (
	advapi32       = syscall.NewLazyDLL("advapi32.dll")
	procCredWrite  = advapi32.NewProc("CredWriteW")
	procCredRead   = advapi32.NewProc("CredReadW")
	procCredFree   = advapi32.NewProc("CredFree")
	procCredDelete = advapi32.NewProc("CredDeleteW")
)

const (
	credTypeGeneric      = 1
	credPersistLocalUser = 2 // 持久化在当前用户本机
)

type filetime struct {
	Low  uint32
	High uint32
}

type credential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

// Set 写入(或覆盖)一条 Windows 凭据。
func Set(target, value string) error {
	targetPtr, err := syscall.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	blob := []byte(value)
	if len(blob) == 0 {
		return errors.New("secret 为空")
	}
	cred := credential{
		Type:               credTypeGeneric,
		TargetName:         targetPtr,
		CredentialBlobSize: uint32(len(blob)),
		CredentialBlob:     &blob[0],
		Persist:            credPersistLocalUser,
	}
	r, _, callErr := procCredWrite.Call(
		uintptr(unsafe.Pointer(&cred)), 0, 0)
	if r == 0 {
		if callErr != nil {
			return callErr
		}
		return errors.New("CredWriteW 失败")
	}
	return nil
}

// Get 读取一条 Windows 凭据;不存在或失败返回错误。
func Get(target string) (string, error) {
	targetPtr, err := syscall.UTF16PtrFromString(target)
	if err != nil {
		return "", err
	}
	var pcred *credential
	r, _, callErr := procCredRead.Call(
		uintptr(unsafe.Pointer(targetPtr)), credTypeGeneric, 0,
		uintptr(unsafe.Pointer(&pcred)))
	if r == 0 {
		if callErr != nil {
			return "", callErr
		}
		return "", errors.New("凭据不存在")
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(pcred)))
	if pcred == nil || pcred.CredentialBlobSize == 0 {
		return "", errors.New("凭据内容为空")
	}
	buf := make([]byte, pcred.CredentialBlobSize)
	for i := 0; i < len(buf); i++ {
		buf[i] = *(*byte)(unsafe.Pointer(uintptr(unsafe.Pointer(pcred.CredentialBlob)) + uintptr(i)))
	}
	return string(buf), nil
}

// Delete 删除一条 Windows 凭据;不存在视为成功。
func Delete(target string) error {
	targetPtr, err := syscall.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	r, _, callErr := procCredDelete.Call(
		uintptr(unsafe.Pointer(targetPtr)), credTypeGeneric, 0)
	if r == 0 && callErr != nil && callErr != syscall.Errno(1168) { // 1168 = ERROR_NOT_FOUND
		return callErr
	}
	return nil
}
