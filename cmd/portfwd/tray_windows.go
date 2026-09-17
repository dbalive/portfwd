//go:build windows

package main

import (
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	wmTrayCallback = 0x8001
	wmAppQuit      = 0x8002
	wmRButtonUp    = 0x0205
	wmLButtonUp    = 0x0202
	wmCommand      = 0x0111
	wmDestroy      = 0x0002
	wmNull         = 0x0000
	nimAdd         = 0
	nimModify      = 1
	nimDelete      = 2
	nifMessage     = 0x00000001
	nifIcon        = 0x00000002
	nifTip         = 0x00000004
	mfString       = 0x00000000
	mfSeparator    = 0x00000800
	mfGrayed       = 0x00000001
	mfPopup        = 0x00000010
	tpmRightButton = 0x0002
	wsExNoActivate = 0x08000000
	wsExToolWindow = 0x00000080
	wsPopup        = 0x80000000
)

type notifyIconData struct {
	Size            uint32
	Wnd             windows.HWND
	ID              uint32
	Flags           uint32
	CallbackMessage uint32
	Icon            windows.Handle
	Tip             [128]uint16
}

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   windows.Handle
	Icon       windows.Handle
	Cursor     windows.Handle
	Background windows.Handle
	MenuName   *uint16
	ClassName  *uint16
	IconSm     windows.Handle
}

type point struct {
	X, Y int32
}

type msg struct {
	HWnd    windows.HWND
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
	Private uint32
}

var (
	modShell32                 = windows.NewLazySystemDLL("shell32.dll")
	procShellNotifyIconW       = modShell32.NewProc("Shell_NotifyIconW")
	procRegisterClassExW       = modUser32.NewProc("RegisterClassExW")
	procCreateWindowExW        = modUser32.NewProc("CreateWindowExW")
	procDestroyWindow          = modUser32.NewProc("DestroyWindow")
	procDefWindowProcW         = modUser32.NewProc("DefWindowProcW")
	procGetMessageW            = modUser32.NewProc("GetMessageW")
	procTranslateMessage       = modUser32.NewProc("TranslateMessage")
	procDispatchMessageW       = modUser32.NewProc("DispatchMessageW")
	procPostQuitMessage        = modUser32.NewProc("PostQuitMessage")
	procPostMessageW           = modUser32.NewProc("PostMessageW")
	procCreatePopupMenu        = modUser32.NewProc("CreatePopupMenu")
	procAppendMenuW            = modUser32.NewProc("AppendMenuW")
	procTrackPopupMenu         = modUser32.NewProc("TrackPopupMenu")
	procDestroyMenu            = modUser32.NewProc("DestroyMenu")
	procGetCursorPos           = modUser32.NewProc("GetCursorPos")
	procGetModuleHandleW       = modKernel32.NewProc("GetModuleHandleW")
	procRegisterWindowMessageW = modUser32.NewProc("RegisterWindowMessageW")
	trayWndProcCallback        = syscall.NewCallback(trayWndProc)
	trayTaskbarCreated         uint32
	tray                       struct {
		hub      *hub
		show     func()
		hwnd     windows.HWND
		icon     windows.Handle
		lastShow time.Time
		jumps    []Jump
	}
)

func postQuitTray() {
	hwnd := tray.hwnd
	if hwnd == 0 {
		return
	}
	procPostMessageW.Call(uintptr(hwnd), wmAppQuit, 0, 0)
}

func runTray(h *hub, show func()) {
	tray.hub = h
	tray.show = show
	quitUILoop = postQuitTray
	if name, err := windows.UTF16PtrFromString("TaskbarCreated"); err == nil {
		r, _, _ := procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(name)))
		trayTaskbarCreated = uint32(r)
	}
	className, _ := windows.UTF16PtrFromString("PortFwdTray")
	title, _ := windows.UTF16PtrFromString("端口转发")
	inst, _, _ := procGetModuleHandleW.Call(0)
	wc := wndClassEx{
		WndProc:   trayWndProcCallback,
		Instance:  windows.Handle(inst),
		ClassName: className,
	}
	wc.Size = uint32(unsafe.Sizeof(wc))
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	hwnd, _, _ := procCreateWindowExW.Call(wsExNoActivate|wsExToolWindow, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(title)), wsPopup, 0, 0, 0, 0, 0, 0, inst, 0)
	if hwnd == 0 {
		return
	}
	tray.hwnd = windows.HWND(hwnd)
	tray.icon = loadTrayIcon()
	addTrayIcon()
	var m msg
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	delTrayIcon()
}

func loadTrayIcon() windows.Handle {
	path := writeAppIconICO()
	if path == "" {
		return 0
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0
	}
	cx := sysMetric(smCxSmIcon)
	cy := sysMetric(smCySmIcon)
	if cx == 0 {
		cx, cy = 16, 16
	}
	return windows.Handle(loadIco(p, cx, cy))
}

func addTrayIcon() {
	nid := notifyIconData{
		Wnd:             tray.hwnd,
		ID:              1,
		Flags:           nifMessage | nifIcon | nifTip,
		CallbackMessage: wmTrayCallback,
		Icon:            tray.icon,
	}
	nid.Size = uint32(unsafe.Sizeof(nid))
	copy(nid.Tip[:], windows.StringToUTF16("端口转发"))
	if r, _, _ := procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&nid))); r != 0 {
		return
	}
	procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nid)))
}

func delTrayIcon() {
	hwnd := tray.hwnd
	if !shouldDeleteTrayIcon(uintptr(hwnd)) {
		return
	}
	nid := notifyIconData{Wnd: hwnd, ID: 1}
	nid.Size = uint32(unsafe.Sizeof(nid))
	procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
}

func trayWndProc(hwnd windows.HWND, msg uint32, wParam, lParam uintptr) uintptr {
	if trayTaskbarCreated != 0 && msg == trayTaskbarCreated {
		addTrayIcon()
		return 0
	}
	switch msg {
	case wmTrayCallback:
		switch lParam {
		case wmLButtonUp:
			now := time.Now()
			if !acceptTrayShow(now, tray.lastShow, trayClickGap) {
				return 0
			}
			tray.lastShow = now
			if tray.show != nil {
				tray.show()
			}
		case wmRButtonUp:
			showTrayMenu(hwnd)
		}
		return 0
	case wmCommand:
		handleTrayCommand(uint16(wParam))
		return 0
	case wmAppQuit:
		delTrayIcon()
		procDestroyWindow.Call(uintptr(hwnd))
		return 0
	case wmDestroy:
		delTrayIcon()
		tray.hwnd = 0
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), wParam, lParam)
	return r
}

func showTrayMenu(hwnd windows.HWND) {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	connected, connecting := false, false
	if tray.hub != nil {
		connected, connecting = tray.hub.connectionFlags()
	}
	jumps := []Jump{}
	if tray.hub != nil {
		jumps = trayJumpChoices(tray.hub.savedJumps())
	}
	tray.jumps = append([]Jump(nil), jumps...)
	for _, item := range trayItems(connected, connecting) {
		if item.ID == trayIDSettings {
			procAppendMenuW.Call(menu, mfSeparator, 0, 0)
		}
		text, err := windows.UTF16PtrFromString(item.Label)
		if err != nil {
			continue
		}
		if item.ID == trayIDConnect && shouldAttachTrayJumpSubmenu(len(jumps)) {
			sub := jumpSubmenu(jumps)
			if sub != 0 {
				flags := uintptr(mfPopup | mfString)
				if !item.Enabled {
					flags |= mfGrayed
				}
				procAppendMenuW.Call(menu, flags, sub, uintptr(unsafe.Pointer(text)))
				continue
			}
		}
		flags := uintptr(mfString)
		if !item.Enabled {
			flags |= mfGrayed
		}
		procAppendMenuW.Call(menu, flags, uintptr(item.ID), uintptr(unsafe.Pointer(text)))
	}
	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procSetForegroundWindow.Call(uintptr(hwnd))
	procTrackPopupMenu.Call(menu, tpmRightButton, uintptr(pt.X), uintptr(pt.Y), 0, uintptr(hwnd), 0)
	procPostMessageW.Call(uintptr(hwnd), wmNull, 0, 0)
	procDestroyMenu.Call(menu)
}

func handleTrayCommand(id uint16) {
	if id >= trayIDJumpBase {
		idx := int(id - trayIDJumpBase)
		if idx >= 0 && idx < len(tray.jumps) && tray.hub != nil {
			if err := tray.hub.connectFromJump(tray.jumps[idx]); err == errNeedSettings && tray.show != nil {
				tray.show()
			}
		}
		return
	}
	switch id {
	case trayIDConnect:
		handleTrayConnect()
	case trayIDDisconnect:
		tray.hub.disconnect()
	case trayIDBrowser:
		if err := tray.hub.openBrowser(); err != nil {
			messageBox(0, err.Error(), "端口转发", 0x30)
		}
	case trayIDSSH:
		if tray.hub == nil {
			break
		}
		if err := tray.hub.writeSSHFromSaved(); err != nil {
			messageBox(0, err.Error(), "端口转发", 0x30)
			if sshConfigNeedsUI(err) && tray.show != nil {
				tray.show()
			}
		}
	case trayIDSettings:
		if tray.show != nil {
			tray.show()
		}
	case trayIDExit:
		if tray.hub != nil {
			tray.hub.requestShutdown()
		}
	}
}

func handleTrayConnect() {
	if tray.hub == nil {
		return
	}
	if err := tray.hub.connectFromSaved(); err == errNeedSettings && tray.show != nil {
		tray.show()
	}
}

func jumpSubmenu(jumps []Jump) uintptr {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return 0
	}
	for i, j := range jumps {
		text, err := windows.UTF16PtrFromString(trayJumpMenuLabel(j))
		if err != nil {
			continue
		}
		procAppendMenuW.Call(menu, mfString, uintptr(trayIDJumpBase+i), uintptr(unsafe.Pointer(text)))
	}
	return menu
}
