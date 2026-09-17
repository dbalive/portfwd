//go:build windows

package main

import (
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002
)

var (
	modClipUser32        = windows.NewLazySystemDLL("user32.dll")
	modClipKernel32      = windows.NewLazySystemDLL("kernel32.dll")
	procOpenClipboard    = modClipUser32.NewProc("OpenClipboard")
	procCloseClipboard   = modClipUser32.NewProc("CloseClipboard")
	procEmptyClipboard   = modClipUser32.NewProc("EmptyClipboard")
	procSetClipboardData = modClipUser32.NewProc("SetClipboardData")
	procGlobalAlloc      = modClipKernel32.NewProc("GlobalAlloc")
	procGlobalLock       = modClipKernel32.NewProc("GlobalLock")
	procGlobalUnlock     = modClipKernel32.NewProc("GlobalUnlock")
	procGlobalFree       = modClipKernel32.NewProc("GlobalFree")
	procRtlMoveMemory    = modClipKernel32.NewProc("RtlMoveMemory")
)

func copyToClipboard(s string) error {
	var last error
	for i := 0; i < 5; i++ {
		if err := setClipboard(s); err == nil {
			return nil
		} else {
			last = err
		}
		time.Sleep(20 * time.Millisecond)
	}
	if last == nil {
		last = errString("复制到剪贴板失败")
	}
	return last
}

func setClipboard(s string) error {
	u16, err := windows.UTF16FromString(s)
	if err != nil {
		return err
	}
	size := uintptr(len(u16) * 2)
	hMem, _, allocErr := procGlobalAlloc.Call(gmemMoveable, size)
	if hMem == 0 {
		return allocErr
	}
	ptr, _, lockErr := procGlobalLock.Call(hMem)
	if ptr == 0 {
		procGlobalFree.Call(hMem)
		return lockErr
	}
	procRtlMoveMemory.Call(ptr, uintptr(unsafe.Pointer(&u16[0])), size)
	procGlobalUnlock.Call(hMem)
	r, _, openErr := procOpenClipboard.Call(0)
	if r == 0 {
		procGlobalFree.Call(hMem)
		return openErr
	}
	defer procCloseClipboard.Call()
	if emptied, _, emptyErr := procEmptyClipboard.Call(); emptied == 0 {
		procGlobalFree.Call(hMem)
		return emptyErr
	}
	ok, _, setErr := procSetClipboardData.Call(cfUnicodeText, hMem)
	if ok == 0 {
		procGlobalFree.Call(hMem)
		return setErr
	}
	return nil
}
