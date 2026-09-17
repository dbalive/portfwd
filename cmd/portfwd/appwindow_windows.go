//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	iconBig                  = 1
	iconSmall                = 0
	wmSetIcon                = 0x0080
	swRestore                = 9
	swShow                   = 5
	swHide                   = 0
	swpNoMove                = 0x0002
	swpNoSize                = 0x0001
	swpShow                  = 0x0040
	hwndTop                  = 0
	imageIcon                = 1
	lrLoadFile               = 0x0010
	smCxIcon                 = 11
	smCyIcon                 = 12
	smCxSmIcon               = 49
	smCySmIcon               = 50
	enumByPID                = 1
	enumByTitle              = 2
	enumByPIDList            = 3
	enumByPIDTop             = 4
	enumByStale              = 5
	gwOwner                  = 4
	gaRoot                   = 2
	wsExAppWindow            = 0x00040000
	clsctxInproc             = 1
	swpNoZOrder              = 0x0004
	swpFrameChanged          = 0x0020
	swpNoActivate            = 0x0010
	eventSystemMinimizeStart = 0x0016
	wineventOutOfContext     = 0x0000
	wineventSkipOwnProcess   = 0x0002
	dwmwaCloak               = 13
	dwmwaCaptionButtonBounds = 5
	smCxSize                 = 30
	smCyCaption              = 4
)

var (
	modUser32                 = windows.NewLazySystemDLL("user32.dll")
	procEnumWindows           = modUser32.NewProc("EnumWindows")
	procGetWindowThreadPID    = modUser32.NewProc("GetWindowThreadProcessId")
	procIsWindowVisible       = modUser32.NewProc("IsWindowVisible")
	procIsIconic              = modUser32.NewProc("IsIconic")
	procShowWindow            = modUser32.NewProc("ShowWindow")
	procSetForegroundWindow   = modUser32.NewProc("SetForegroundWindow")
	procSetWindowPos          = modUser32.NewProc("SetWindowPos")
	procGetForegroundWindow   = modUser32.NewProc("GetForegroundWindow")
	procAttachThreadInput     = modUser32.NewProc("AttachThreadInput")
	procGetClassNameW         = modUser32.NewProc("GetClassNameW")
	procGetWindowTextW        = modUser32.NewProc("GetWindowTextW")
	procIsWindow              = modUser32.NewProc("IsWindow")
	procGetSystemMetrics      = modUser32.NewProc("GetSystemMetrics")
	procGetWindow             = modUser32.NewProc("GetWindow")
	procGetAncestor           = modUser32.NewProc("GetAncestor")
	procGetWindowLongPtr      = modUser32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtr      = modUser32.NewProc("SetWindowLongPtrW")
	procGetWindowRect         = modUser32.NewProc("GetWindowRect")
	procSetWinEventHook       = modUser32.NewProc("SetWinEventHook")
	procUnhookWinEvent        = modUser32.NewProc("UnhookWinEvent")
	modDwmapi                 = windows.NewLazySystemDLL("dwmapi.dll")
	procDwmSetWindowAttribute = modDwmapi.NewProc("DwmSetWindowAttribute")
	procDwmGetWindowAttribute = modDwmapi.NewProc("DwmGetWindowAttribute")
	modOle32                  = windows.NewLazySystemDLL("ole32.dll")
	procCoInitializeEx        = modOle32.NewProc("CoInitializeEx")
	procCoUninitialize        = modOle32.NewProc("CoUninitialize")
	procCoCreateInstance      = modOle32.NewProc("CoCreateInstance")
	modKernel32               = windows.NewLazySystemDLL("kernel32.dll")
	procGetCurrentThreadId    = modKernel32.NewProc("GetCurrentThreadId")
	enumWindowsCallback       = syscall.NewCallback(enumAppWindowProc)
	winEventCallback          = syscall.NewCallback(appWinEventProc)
	hookedAppWindow           *appWindowCtrl
)

var enumSearch struct {
	mu            sync.Mutex
	mode          int
	want          map[uint32]struct{}
	includeHidden bool
	url           string
	found         windows.HWND
	list          []windows.HWND
}

func closeStaleAppWindows(url string) {
	enumSearch.mu.Lock()
	enumSearch.mode = enumByStale
	enumSearch.url = url
	enumSearch.list = nil
	procEnumWindows.Call(enumWindowsCallback, 0)
	wins := append([]windows.HWND(nil), enumSearch.list...)
	enumSearch.mu.Unlock()
	seen := map[uint32]bool{}
	for _, hwnd := range wins {
		var pid uint32
		procGetWindowThreadPID.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&pid)))
		if pid == 0 || seen[pid] {
			continue
		}
		seen[pid] = true
		for _, p := range processTree(pid) {
			if seen[p] && p != pid {
				continue
			}
			seen[p] = true
			if proc, err := os.FindProcess(int(p)); err == nil {
				_ = proc.Kill()
			}
		}
	}
	if len(wins) > 0 {
		time.Sleep(120 * time.Millisecond)
	}
}

func decorateAppWindow(rootPID uint32) {
	if rootPID == 0 {
		return
	}
	iconPath := writeAppIconICO()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		pids := processTree(rootPID)
		hwnd := findChromeWindow(pids, false)
		if hwnd != 0 {
			if iconPath != "" {
				applyWindowIcon(hwnd, iconPath)
			}
			forceForeground(hwnd)
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func writeAppIconICO() string {
	ico, err := appIconICO()
	if err != nil {
		return ""
	}
	dir := filepath.Join(os.TempDir(), "portfwd-ui")
	_ = os.MkdirAll(dir, 0o700)
	path := filepath.Join(dir, "app.ico")
	if os.WriteFile(path, ico, 0o600) != nil {
		return ""
	}
	return path
}

func applyWindowIcon(hwnd windows.HWND, icoPath string) {
	p, err := windows.UTF16PtrFromString(icoPath)
	if err != nil {
		return
	}
	smallW, smallH := sysMetric(smCxSmIcon), sysMetric(smCySmIcon)
	if smallW == 0 {
		smallW, smallH = 16, 16
	}
	bigW, bigH := sysMetric(smCxIcon), sysMetric(smCyIcon)
	if bigW == 0 {
		bigW, bigH = 32, 32
	}
	if h := loadIco(p, smallW, smallH); h != 0 {
		modUser32.NewProc("SendMessageW").Call(uintptr(hwnd), wmSetIcon, iconSmall, h)
	}
	if h := loadIco(p, bigW, bigH); h != 0 {
		modUser32.NewProc("SendMessageW").Call(uintptr(hwnd), wmSetIcon, iconBig, h)
	}
}

func sysMetric(id int) int {
	v, _, _ := procGetSystemMetrics.Call(uintptr(id))
	return int(v)
}

func loadIco(path *uint16, cx, cy int) uintptr {
	h, _, _ := modUser32.NewProc("LoadImageW").Call(
		0,
		uintptr(unsafe.Pointer(path)),
		imageIcon,
		uintptr(cx),
		uintptr(cy),
		lrLoadFile,
	)
	return h
}

func forceForeground(hwnd windows.HWND) {
	procShowWindow.Call(uintptr(hwnd), swRestore)
	procShowWindow.Call(uintptr(hwnd), swShow)
	procSetWindowPos.Call(uintptr(hwnd), hwndTop, 0, 0, 0, 0, swpNoMove|swpNoSize|swpShow)
	fg, _, _ := procGetForegroundWindow.Call()
	if windows.HWND(fg) == hwnd {
		return
	}
	cur, _, _ := procGetCurrentThreadId.Call()
	var fgPID uint32
	fgTID, _, _ := procGetWindowThreadPID.Call(fg, uintptr(unsafe.Pointer(&fgPID)))
	var myPID uint32
	myTID, _, _ := procGetWindowThreadPID.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&myPID)))
	if fgTID != 0 {
		procAttachThreadInput.Call(cur, fgTID, 1)
	}
	if myTID != 0 && myTID != fgTID {
		procAttachThreadInput.Call(cur, myTID, 1)
	}
	procSetForegroundWindow.Call(uintptr(hwnd))
	if fgTID != 0 {
		procAttachThreadInput.Call(cur, fgTID, 0)
	}
	if myTID != 0 && myTID != fgTID {
		procAttachThreadInput.Call(cur, myTID, 0)
	}
}

func findChromeWindow(pids []uint32, includeHidden bool) windows.HWND {
	want := map[uint32]struct{}{}
	for _, p := range pids {
		want[p] = struct{}{}
	}
	enumSearch.mu.Lock()
	defer enumSearch.mu.Unlock()
	enumSearch.mode = enumByPID
	enumSearch.want = want
	enumSearch.includeHidden = includeHidden
	enumSearch.found = 0
	procEnumWindows.Call(enumWindowsCallback, 0)
	return enumSearch.found
}

func enumAppWindowProc(hwnd, _ uintptr) uintptr {
	h := windows.HWND(hwnd)
	switch enumSearch.mode {
	case enumByPID:
		var pid uint32
		procGetWindowThreadPID.Call(uintptr(h), uintptr(unsafe.Pointer(&pid)))
		if _, ok := enumSearch.want[pid]; !ok {
			return 1
		}
		if !isChromeWidget(h) {
			return 1
		}
		if !enumSearch.includeHidden {
			vis, _, _ := procIsWindowVisible.Call(uintptr(h))
			iconic, _, _ := procIsIconic.Call(uintptr(h))
			if vis == 0 && iconic == 0 {
				return 1
			}
		}
		enumSearch.found = h
		return 0
	case enumByTitle:
		if !isChromeWidget(h) {
			return 1
		}
		if !isPortFwdUITitle(windowTitle(h), enumSearch.url) {
			return 1
		}
		enumSearch.found = h
		return 0
	case enumByPIDList, enumByPIDTop:
		var pid uint32
		procGetWindowThreadPID.Call(uintptr(h), uintptr(unsafe.Pointer(&pid)))
		if _, ok := enumSearch.want[pid]; !ok {
			return 1
		}
		if enumSearch.mode == enumByPIDList && !isChromeWidget(h) {
			return 1
		}
		if !enumSearch.includeHidden {
			vis, _, _ := procIsWindowVisible.Call(uintptr(h))
			iconic, _, _ := procIsIconic.Call(uintptr(h))
			if vis == 0 && iconic == 0 {
				return 1
			}
		}
		enumSearch.list = append(enumSearch.list, h)
		return 1
	case enumByStale:
		if !isChromeWidget(h) {
			return 1
		}
		title := windowTitle(h)
		if isPortFwdUITitle(title, enumSearch.url) {
			enumSearch.list = append(enumSearch.list, h)
		}
		return 1
	}
	return 1
}

func listProcessWindows(pids []uint32, includeHidden bool) []windows.HWND {
	want := map[uint32]struct{}{}
	for _, p := range pids {
		want[p] = struct{}{}
	}
	enumSearch.mu.Lock()
	defer enumSearch.mu.Unlock()
	enumSearch.mode = enumByPIDTop
	enumSearch.want = want
	enumSearch.includeHidden = includeHidden
	enumSearch.list = nil
	procEnumWindows.Call(enumWindowsCallback, 0)
	return append([]windows.HWND(nil), enumSearch.list...)
}

func listChromeWindows(pids []uint32, includeHidden bool) []windows.HWND {
	want := map[uint32]struct{}{}
	for _, p := range pids {
		want[p] = struct{}{}
	}
	enumSearch.mu.Lock()
	defer enumSearch.mu.Unlock()
	enumSearch.mode = enumByPIDList
	enumSearch.want = want
	enumSearch.includeHidden = includeHidden
	enumSearch.list = nil
	procEnumWindows.Call(enumWindowsCallback, 0)
	return append([]windows.HWND(nil), enumSearch.list...)
}

func ownerWindow(hwnd windows.HWND) windows.HWND {
	r, _, _ := procGetWindow.Call(uintptr(hwnd), gwOwner)
	return windows.HWND(r)
}

func rootWindow(hwnd windows.HWND) windows.HWND {
	r, _, _ := procGetAncestor.Call(uintptr(hwnd), gaRoot)
	return windows.HWND(r)
}

var (
	clsidTaskbarList = windows.GUID{Data1: 0x56FDF344, Data2: 0xFD6D, Data3: 0x11D0, Data4: [8]byte{0x95, 0x8A, 0x00, 0x60, 0x97, 0xC9, 0xA0, 0x90}}
	iidITaskbarList  = windows.GUID{Data1: 0x56FDF342, Data2: 0xFD6D, Data3: 0x11D0, Data4: [8]byte{0x95, 0x8A, 0x00, 0x60, 0x97, 0xC9, 0xA0, 0x90}}
)

type iTaskbarList struct{ lpVtbl *iTaskbarListVtbl }
type iTaskbarListVtbl struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr
	HrInit         uintptr
	AddTab         uintptr
	DeleteTab      uintptr
	ActivateTab    uintptr
	SetActiveAlt   uintptr
}

func withTaskbarList(fn func(*iTaskbarList)) {
	procCoInitializeEx.Call(0, 2)
	defer procCoUninitialize.Call()
	var tlb *iTaskbarList
	hr, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidTaskbarList)),
		0,
		clsctxInproc,
		uintptr(unsafe.Pointer(&iidITaskbarList)),
		uintptr(unsafe.Pointer(&tlb)),
	)
	if hr != 0 || tlb == nil {
		return
	}
	defer syscall.SyscallN(tlb.lpVtbl.Release, uintptr(unsafe.Pointer(tlb)))
	syscall.SyscallN(tlb.lpVtbl.HrInit, uintptr(unsafe.Pointer(tlb)))
	fn(tlb)
}

func deleteTaskbarTab(hwnd windows.HWND) {
	if hwnd == 0 {
		return
	}
	withTaskbarList(func(tlb *iTaskbarList) {
		syscall.SyscallN(tlb.lpVtbl.DeleteTab, uintptr(unsafe.Pointer(tlb)), uintptr(hwnd))
	})
}

func addTaskbarTab(hwnd windows.HWND) {
	if hwnd == 0 {
		return
	}
	withTaskbarList(func(tlb *iTaskbarList) {
		syscall.SyscallN(tlb.lpVtbl.AddTab, uintptr(unsafe.Pointer(tlb)), uintptr(hwnd))
	})
}

func markToolWindow(hwnd windows.HWND, tool bool) {
	if hwnd == 0 {
		return
	}
	idx := ^uintptr(19) // GWL_EXSTYLE = -20
	ex, _, _ := procGetWindowLongPtr.Call(uintptr(hwnd), idx)
	if tool {
		ex = (ex | wsExToolWindow) &^ wsExAppWindow
	} else {
		ex = (ex | wsExAppWindow) &^ wsExToolWindow
	}
	procSetWindowLongPtr.Call(uintptr(hwnd), idx, ex)
	procSetWindowPos.Call(uintptr(hwnd), 0, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoZOrder|swpNoActivate|swpFrameChanged)
}

type winRect struct {
	Left, Top, Right, Bottom int32
}

func setCloak(hwnd windows.HWND, on bool) {
	if hwnd == 0 {
		return
	}
	var v int32
	if on {
		v = 1
	}
	procDwmSetWindowAttribute.Call(uintptr(hwnd), dwmwaCloak, uintptr(unsafe.Pointer(&v)), 4)
}

func captionButtonNow(hwnd windows.HWND) string {
	if hwnd == 0 {
		return ""
	}
	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	var wr winRect
	procGetWindowRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&wr)))
	var bounds winRect
	hr, _, _ := procDwmGetWindowAttribute.Call(uintptr(hwnd), dwmwaCaptionButtonBounds, uintptr(unsafe.Pointer(&bounds)), 16)
	if hr == 0 && bounds.Right > bounds.Left {
		return captionButtonHit(wr.Left, wr.Top, bounds.Left, bounds.Top, bounds.Right, bounds.Bottom, pt.X, pt.Y)
	}
	btnW := int32(sysMetric(smCxSize))
	capH := int32(sysMetric(smCyCaption))
	if btnW < 24 {
		btnW = 46
	}
	if capH < 16 {
		capH = 32
	}
	return fallbackCaptionHit(wr.Left, wr.Top, wr.Right, pt.X, pt.Y, btnW, capH)
}

func appWinEventProc(_, event, hwnd, _, _, _, _ uintptr) uintptr {
	c := hookedAppWindow
	if c == nil || hwnd == 0 {
		return 0
	}
	h := windows.HWND(hwnd)
	if !c.ownsHWND(h) {
		return 0
	}
	if event != eventSystemMinimizeStart {
		return 0
	}
	root := rootWindow(h)
	if root == 0 {
		root = h
	}
	c.hideToTray(root)
	c.setHiddenToTray(true)
	return 0
}

func installAppWindowHooks(ctrl *appWindowCtrl) func() {
	hookedAppWindow = ctrl
	h, _, _ := procSetWinEventHook.Call(
		eventSystemMinimizeStart,
		eventSystemMinimizeStart,
		0,
		winEventCallback,
		0,
		0,
		wineventOutOfContext|wineventSkipOwnProcess,
	)
	return func() {
		if h != 0 {
			procUnhookWinEvent.Call(h)
		}
		if hookedAppWindow == ctrl {
			hookedAppWindow = nil
		}
	}
}

func hideOneWindow(hwnd windows.HWND) {
	if hwnd == 0 {
		return
	}
	setCloak(hwnd, true)
	markToolWindow(hwnd, true)
	deleteTaskbarTab(hwnd)
	procShowWindow.Call(uintptr(hwnd), swHide)
	deleteTaskbarTab(hwnd)
}

func showOneWindow(hwnd windows.HWND) {
	if hwnd == 0 {
		return
	}
	setCloak(hwnd, false)
	markToolWindow(hwnd, false)
	addTaskbarTab(hwnd)
}

func (c *appWindowCtrl) hideToTray(hwnd windows.HWND) {
	pid := c.pid()
	tree := processTree(pid)
	wins := listProcessWindows(tree, true)
	wins = append(wins, listChromeWindows(tree, true)...)
	if hwnd != 0 {
		wins = append([]windows.HWND{hwnd}, wins...)
	}
	seen := map[windows.HWND]bool{}
	for _, w := range wins {
		if w == 0 || seen[w] {
			continue
		}
		seen[w] = true
		if r := rootWindow(w); r != 0 && !seen[r] {
			seen[r] = true
			hideOneWindow(r)
		}
		if o := ownerWindow(w); o != 0 && !seen[o] {
			seen[o] = true
			hideOneWindow(o)
		}
		hideOneWindow(w)
	}
}

func (c *appWindowCtrl) restoreFromTray(hwnd windows.HWND) {
	pid := c.pid()
	tree := processTree(pid)
	wins := listProcessWindows(tree, true)
	wins = append(wins, listChromeWindows(tree, true)...)
	if hwnd != 0 {
		wins = append([]windows.HWND{hwnd}, wins...)
	}
	seen := map[windows.HWND]bool{}
	for _, w := range wins {
		if w == 0 || seen[w] {
			continue
		}
		seen[w] = true
		if r := rootWindow(w); r != 0 && !seen[r] {
			seen[r] = true
			showOneWindow(r)
		}
		if o := ownerWindow(w); o != 0 && !seen[o] {
			seen[o] = true
			showOneWindow(o)
		}
		showOneWindow(w)
	}
	showOneWindow(hwnd)
	forceForeground(hwnd)
}

func isChromeWidget(hwnd windows.HWND) bool {
	var buf [64]uint16
	n, _, _ := procGetClassNameW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), 64)
	if n == 0 {
		return false
	}
	name := windows.UTF16ToString(buf[:n])
	return name == "Chrome_WidgetWin_1" || name == "Chrome_WidgetWin_0"
}

func processTree(root uint32) []uint32 {
	out := []uint32{root}
	seen := map[uint32]bool{root: true}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return out
	}
	defer windows.CloseHandle(snap)
	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	if err := windows.Process32First(snap, &pe); err != nil {
		return out
	}
	var all []windows.ProcessEntry32
	for {
		cp := pe
		all = append(all, cp)
		if err := windows.Process32Next(snap, &pe); err != nil {
			break
		}
	}
	changed := true
	for changed {
		changed = false
		for _, p := range all {
			if seen[p.ProcessID] || !seen[p.ParentProcessID] {
				continue
			}
			seen[p.ProcessID] = true
			out = append(out, p.ProcessID)
			changed = true
		}
	}
	return out
}

type appWindowCtrl struct {
	mu           sync.Mutex
	url          string
	cmd          *exec.Cmd
	hwnd         windows.HWND
	stop         chan struct{}
	starting     bool
	killed       bool
	hiddenToTray bool
	everShown    bool
	startedAt    time.Time
	onClosed     func()
}

func newAppWindowCtrl(url string) *appWindowCtrl {
	return &appWindowCtrl{url: url, stop: make(chan struct{})}
}

func (c *appWindowCtrl) start() error {
	cmd, err := startAppWindow(c.url)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.cmd = cmd
	c.startedAt = time.Now()
	c.mu.Unlock()
	go func() {
		_ = cmd.Wait()
		c.mu.Lock()
		if c.cmd == cmd {
			c.cmd = nil
		}
		c.mu.Unlock()
	}()
	return nil
}

func (c *appWindowCtrl) setOnClosed(fn func()) {
	c.mu.Lock()
	c.onClosed = fn
	c.mu.Unlock()
}

func (c *appWindowCtrl) isKilled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.killed
}

func (c *appWindowCtrl) fireClosed() {
	if !c.hasEverShown() {
		return
	}
	c.mu.Lock()
	fn := c.onClosed
	c.mu.Unlock()
	if fn != nil {
		fn()
	}
}

func (c *appWindowCtrl) setHiddenToTray(on bool) {
	c.mu.Lock()
	c.hiddenToTray = on
	c.mu.Unlock()
}

func (c *appWindowCtrl) isHiddenToTray() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hiddenToTray
}

func (c *appWindowCtrl) markEverShown() {
	c.mu.Lock()
	c.everShown = true
	c.mu.Unlock()
}

func (c *appWindowCtrl) hasEverShown() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.everShown
}

func (c *appWindowCtrl) sinceStart() time.Duration {
	c.mu.Lock()
	started := c.startedAt
	c.mu.Unlock()
	if started.IsZero() {
		return 0
	}
	return time.Since(started)
}

func (c *appWindowCtrl) pid() uint32 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cmd == nil || c.cmd.Process == nil {
		return 0
	}
	return uint32(c.cmd.Process.Pid)
}

func isWindow(hwnd windows.HWND) bool {
	if hwnd == 0 {
		return false
	}
	ok, _, _ := procIsWindow.Call(uintptr(hwnd))
	return ok != 0
}

func windowTitle(hwnd windows.HWND) string {
	var buf [256]uint16
	n, _, _ := procGetWindowTextW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), 256)
	if n == 0 {
		return ""
	}
	return windows.UTF16ToString(buf[:n])
}

func findAppWindowByTitle(url string) windows.HWND {
	enumSearch.mu.Lock()
	defer enumSearch.mu.Unlock()
	enumSearch.mode = enumByTitle
	enumSearch.url = url
	enumSearch.found = 0
	procEnumWindows.Call(enumWindowsCallback, 0)
	return enumSearch.found
}

func (c *appWindowCtrl) existingHWND() windows.HWND {
	c.mu.Lock()
	hwnd := c.hwnd
	url := c.url
	var pid uint32
	if c.cmd != nil && c.cmd.Process != nil {
		pid = uint32(c.cmd.Process.Pid)
	}
	c.mu.Unlock()
	if isWindow(hwnd) {
		return hwnd
	}
	if w := findAppWindowByTitle(url); w != 0 {
		c.setHWND(w)
		return w
	}
	if pid != 0 {
		if w := findChromeWindow(processTree(pid), true); w != 0 {
			c.setHWND(w)
			return w
		}
	}
	return 0
}

func (c *appWindowCtrl) setHWND(hwnd windows.HWND) {
	c.mu.Lock()
	c.hwnd = hwnd
	c.mu.Unlock()
}

func (c *appWindowCtrl) isTracked(hwnd windows.HWND) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return hwnd != 0 && hwnd == c.hwnd
}

func (c *appWindowCtrl) ownsHWND(hwnd windows.HWND) bool {
	if hwnd == 0 {
		return false
	}
	if c.isTracked(hwnd) {
		return true
	}
	root := rootWindow(hwnd)
	if root != 0 && c.isTracked(root) {
		return true
	}
	var pid uint32
	procGetWindowThreadPID.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&pid)))
	if our := c.pid(); our != 0 && pid == our {
		return true
	}
	c.mu.Lock()
	url := c.url
	c.mu.Unlock()
	return isPortFwdUITitle(windowTitle(hwnd), url) || isPortFwdUITitle(windowTitle(root), url)
}

func (c *appWindowCtrl) watchWindow() {
	seen := false
	var iconicSince time.Time
	var goneSince time.Time
	var hiddenSince time.Time
	for {
		select {
		case <-c.stop:
			return
		case <-time.After(80 * time.Millisecond):
		}
		c.mu.Lock()
		hwnd := c.hwnd
		c.mu.Unlock()
		if shouldRescanAppWindow(!isWindow(hwnd)) {
			hwnd = c.existingHWND()
		}
		if hwnd != 0 {
			seen = true
			goneSince = time.Time{}
			iconic, _, _ := procIsIconic.Call(uintptr(hwnd))
			vis, _, _ := procIsWindowVisible.Call(uintptr(hwnd))
			visible := vis != 0
			if visible && iconic == 0 {
				c.markEverShown()
				hiddenSince = time.Time{}
			}
			if shouldHideMinimized(iconic != 0) {
				hiddenSince = time.Time{}
				if iconicSince.IsZero() {
					iconicSince = time.Now()
				}
				if shouldConfirmMinimize(true, time.Since(iconicSince)) {
					c.hideToTray(hwnd)
					c.setHiddenToTray(true)
				}
				continue
			}
			iconicSince = time.Time{}
			if shouldQuitHiddenUI(visible, iconic != 0, c.isHiddenToTray(), c.isKilled(), c.hasEverShown()) {
				if hiddenSince.IsZero() {
					hiddenSince = time.Now()
				}
				if shouldConfirmHiddenClose(c.hasEverShown(), c.sinceStart(), time.Since(hiddenSince)) {
					c.fireClosed()
					return
				}
				continue
			}
			hiddenSince = time.Time{}
			if visible {
				c.setHiddenToTray(false)
			}
			continue
		}
		iconicSince = time.Time{}
		if seen && goneSince.IsZero() {
			goneSince = time.Now()
		}
		replacement := windows.HWND(0)
		if seen {
			replacement = c.existingHWND()
		}
		goneFor := time.Duration(0)
		if !goneSince.IsZero() {
			goneFor = time.Since(goneSince)
		}
		if seen && shouldQuitAfterLostWindow(c.isKilled(), c.pid() != 0, replacement != 0, c.hasEverShown(), c.sinceStart(), goneFor) {
			c.fireClosed()
			return
		}
	}
}

func (c *appWindowCtrl) show() {
	c.mu.Lock()
	var pid uint32
	if c.cmd != nil && c.cmd.Process != nil {
		pid = uint32(c.cmd.Process.Pid)
	}
	starting := c.starting
	c.mu.Unlock()
	hwnd := c.existingHWND()
	if !shouldLaunchAppWindow(pid, starting, hwnd != 0) {
		if hwnd != 0 {
			c.setHiddenToTray(false)
			c.restoreFromTray(hwnd)
		}
		return
	}
	c.mu.Lock()
	if c.starting {
		c.mu.Unlock()
		return
	}
	c.starting = true
	c.mu.Unlock()
	_ = c.start()
	go func() {
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if c.existingHWND() != 0 {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		c.mu.Lock()
		c.starting = false
		c.mu.Unlock()
	}()
}

func (c *appWindowCtrl) kill() {
	select {
	case <-c.stop:
	default:
		close(c.stop)
	}
	c.mu.Lock()
	c.killed = true
	cmd := c.cmd
	c.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
