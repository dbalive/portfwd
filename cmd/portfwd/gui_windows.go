//go:build windows

package main

import (
	"runtime"
	"syscall"

	"golang.org/x/sys/windows"
)

func init() {
	runtime.LockOSThread()
}

func runGUI() {
	runtime.LockOSThread()
	inst, ok := acquireSingleInstance()
	if !ok {
		notifyExistingInstance()
		return
	}
	defer inst.Release()
	addr, h, _, closer, err := startServer()
	if err != nil {
		messageBox(0, "无法启动界面: "+err.Error(), "端口转发", 0x30)
		return
	}
	defer closer()
	ctrl := newAppWindowCtrl("http://" + addr)
	ctrl.setOnClosed(h.requestShutdown)
	inst.SetShow(ctrl.show)
	if err := ctrl.start(); err != nil {
		messageBox(0, err.Error()+"\n\n也可在浏览器打开 http://"+addr, "端口转发", 0x30)
	}
	go ctrl.watchWindow()
	unhook := installAppWindowHooks(ctrl)
	runTray(h, ctrl.show)
	unhook()
	ctrl.kill()
}

func messageBox(owner windows.HWND, text, caption string, flags uint32) int32 {
	t, _ := windows.UTF16PtrFromString(text)
	c, _ := windows.UTF16PtrFromString(caption)
	r, err := windows.MessageBox(owner, t, c, flags)
	if err != nil && err != syscall.Errno(0) {
		return 0
	}
	return r
}
