//go:build windows

package client

import (
	"errors"
	"syscall"
	"unsafe"
)

const credentialTypeGeneric = 1
const credentialPersistLocalMachine = 2

type windowsCredential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWrittenLow     uint32
	LastWrittenHigh    uint32
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

var (
	advapi32        = syscall.NewLazyDLL("advapi32.dll")
	credWriteWProc  = advapi32.NewProc("CredWriteW")
	credReadWProc   = advapi32.NewProc("CredReadW")
	credDeleteWProc = advapi32.NewProc("CredDeleteW")
	credFreeProc    = advapi32.NewProc("CredFree")
)

func credentialTarget(id string) string { return "SWPLauncher/Account/" + id }

func StoreAccountCredential(id, username, password string) error {
	target, _ := syscall.UTF16PtrFromString(credentialTarget(id))
	user, _ := syscall.UTF16PtrFromString(username)
	blob := []byte(password)
	credential := windowsCredential{Type: credentialTypeGeneric, TargetName: target, Persist: credentialPersistLocalMachine, UserName: user, CredentialBlobSize: uint32(len(blob))}
	if len(blob) > 0 {
		credential.CredentialBlob = &blob[0]
	}
	ok, _, callErr := credWriteWProc.Call(uintptr(unsafe.Pointer(&credential)), 0)
	if ok == 0 {
		return callErr
	}
	return nil
}

func ReadAccountCredential(id string) (string, string, error) {
	target, _ := syscall.UTF16PtrFromString(credentialTarget(id))
	var pointer *windowsCredential
	ok, _, callErr := credReadWProc.Call(uintptr(unsafe.Pointer(target)), credentialTypeGeneric, 0, uintptr(unsafe.Pointer(&pointer)))
	if ok == 0 || pointer == nil {
		return "", "", callErr
	}
	defer credFreeProc.Call(uintptr(unsafe.Pointer(pointer)))
	usernameUnits := unsafe.Slice(pointer.UserName, 1024)
	usernameLength := 0
	for usernameLength < len(usernameUnits) && usernameUnits[usernameLength] != 0 {
		usernameLength++
	}
	username := syscall.UTF16ToString(usernameUnits[:usernameLength])
	password := ""
	if pointer.CredentialBlob != nil && pointer.CredentialBlobSize > 0 {
		password = string(unsafe.Slice(pointer.CredentialBlob, pointer.CredentialBlobSize))
	}
	return username, password, nil
}

func DeleteAccountCredential(id string) error {
	target, _ := syscall.UTF16PtrFromString(credentialTarget(id))
	ok, _, callErr := credDeleteWProc.Call(uintptr(unsafe.Pointer(target)), credentialTypeGeneric, 0)
	if ok == 0 && !errors.Is(callErr, syscall.ERROR_NOT_FOUND) {
		return callErr
	}
	return nil
}
