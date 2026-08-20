//go:build windows

package client

import (
	"fmt"
	"syscall"
	"unsafe"
)

type fixedFileInfo struct {
	Signature, StructVersion, FileVersionMS, FileVersionLS  uint32
	ProductVersionMS, ProductVersionLS                      uint32
	FileFlagsMask, FileFlags, FileOS, FileType, FileSubtype uint32
	FileDateMS, FileDateLS                                  uint32
}

func executableVersion(path string) (string, int, error) {
	versionDLL := syscall.NewLazyDLL("version.dll")
	sizeProc := versionDLL.NewProc("GetFileVersionInfoSizeW")
	infoProc := versionDLL.NewProc("GetFileVersionInfoW")
	queryProc := versionDLL.NewProc("VerQueryValueW")
	pathPtr, _ := syscall.UTF16PtrFromString(path)
	size, _, _ := sizeProc.Call(uintptr(unsafe.Pointer(pathPtr)), 0)
	if size == 0 {
		return "", 0, fmt.Errorf("version resource is missing")
	}
	buf := make([]byte, size)
	ok, _, callErr := infoProc.Call(uintptr(unsafe.Pointer(pathPtr)), 0, size, uintptr(unsafe.Pointer(&buf[0])))
	if ok == 0 {
		return "", 0, callErr
	}
	root, _ := syscall.UTF16PtrFromString("\\")
	var value unsafe.Pointer
	var valueLen uint32
	ok, _, callErr = queryProc.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(root)), uintptr(unsafe.Pointer(&value)), uintptr(unsafe.Pointer(&valueLen)))
	if ok == 0 || valueLen < uint32(unsafe.Sizeof(fixedFileInfo{})) {
		return "", 0, callErr
	}
	v := (*fixedFileInfo)(value)
	// WoW 3.3.5a commonly keeps ProductVersion at 3.3.0.0. FileVersion is
	// the authoritative resource and contains 3.3.5.12340.
	major, minor := v.FileVersionMS>>16, v.FileVersionMS&0xffff
	patch, build := v.FileVersionLS>>16, v.FileVersionLS&0xffff
	return fmt.Sprintf("%d.%d.%d.%d", major, minor, patch, build), int(build), nil
}
