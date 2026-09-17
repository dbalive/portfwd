package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func shouldLaunchAppWindow(pid uint32, starting, haveWindow bool) bool {
	return pid == 0 && !starting && !haveWindow
}

func shouldRescanAppWindow(hwndValid bool) bool {
	return !hwndValid
}

func shouldQuitOnWindowExit(killed bool) bool {
	return !killed
}

func shouldQuitAfterLostWindow(killed, processAlive, haveReplacement, everShown bool, sinceStart, goneFor time.Duration) bool {
	if killed || haveReplacement {
		return false
	}
	if !processAlive {
		if !everShown {
			return goneFor >= 2*time.Second
		}
		return goneFor >= 600*time.Millisecond
	}
	if !everShown || sinceStart < 2500*time.Millisecond {
		return false
	}
	return goneFor >= 600*time.Millisecond
}

func shouldHideMinimized(iconic bool) bool {
	return iconic
}

func shouldConfirmMinimize(iconic bool, waited time.Duration) bool {
	return iconic && waited >= 80*time.Millisecond
}

func shouldQuitHiddenUI(visible, iconic, hiddenToTray, killed, everShown bool) bool {
	if killed || iconic || !everShown {
		return false
	}
	return !visible && !hiddenToTray
}

func shouldConfirmHiddenClose(everShown bool, sinceStart, hiddenFor time.Duration) bool {
	return everShown && sinceStart >= 3*time.Second && hiddenFor >= 800*time.Millisecond
}

func captionButtonHit(winLeft, winTop int32, boundsLeft, boundsTop, boundsRight, boundsBottom, x, y int32) string {
	left := winLeft + boundsLeft
	top := winTop + boundsTop
	right := winLeft + boundsRight
	bottom := winTop + boundsBottom
	if x < left || x >= right || y < top || y >= bottom {
		return ""
	}
	w := right - left
	if w < 3 {
		return ""
	}
	third := w / 3
	rel := x - left
	switch {
	case rel < third:
		return "min"
	case rel < 2*third:
		return "max"
	default:
		return "close"
	}
}

func fallbackCaptionHit(winLeft, winTop, winRight, x, y, btnW, capH int32) string {
	if btnW < 8 || capH < 8 {
		return ""
	}
	if y < winTop || y >= winTop+capH || x >= winRight || x < winRight-3*btnW {
		return ""
	}
	if x >= winRight-btnW {
		return "close"
	}
	if x >= winRight-2*btnW {
		return "max"
	}
	return "min"
}

func shouldTrayOnCaption(hit string) bool {
	return hit != "close"
}

func shouldQuitOnCaption(hit string) bool {
	return hit == "close"
}

func isStaleAppErrorTitle(title string) bool {
	t := strings.TrimSpace(title)
	if t == "127.0.0.1" {
		return true
	}
	return strings.HasPrefix(t, "127.0.0.1:")
}

func isPortFwdUITitle(title, url string) bool {
	title = strings.TrimSpace(title)
	if title == "" {
		return false
	}
	if strings.Contains(title, "动态端口转发") {
		return true
	}
	if u := strings.TrimSpace(url); u != "" && strings.Contains(title, u) {
		return true
	}
	return false
}

func waitLocalUI(url string, d time.Duration) bool {
	url = strings.TrimSpace(url)
	if url == "" || d <= 0 {
		return false
	}
	client := &http.Client{Timeout: 200 * time.Millisecond}
	deadline := time.Now().Add(d)
	for {
		resp, err := client.Get(url)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode < 500 {
				return true
			}
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
}

const uiListenPort = 18765

func uiListenCandidates() []string {
	out := make([]string, 0, 11)
	for p := uiListenPort; p <= uiListenPort+8; p++ {
		out = append(out, fmt.Sprintf("127.0.0.1:%d", p))
	}
	out = append(out, "127.0.0.1:0")
	return out
}

func listenLocalUI() (net.Listener, error) {
	var last error
	for _, addr := range uiListenCandidates() {
		ln, err := net.Listen("tcp", addr)
		if err == nil {
			return ln, nil
		}
		last = err
	}
	if last == nil {
		last = fmt.Errorf("无法监听界面端口")
	}
	return nil, last
}

func appProfileDir() string {
	return filepath.Join(os.TempDir(), "portfwd-ui")
}

func prepareAppProfile(dir string) {
	if strings.TrimSpace(dir) == "" {
		return
	}
	_ = os.MkdirAll(dir, 0o700)
	for _, name := range []string{"SingletonLock", "SingletonSocket", "SingletonCookie"} {
		_ = os.Remove(filepath.Join(dir, name))
		_ = os.Remove(filepath.Join(dir, "Default", name))
	}
}

func appWindowArgs(url, profile string) []string {
	return []string{
		"--app=" + url,
		"--window-size=1000,700",
		"--window-position=80,60",
		"--user-data-dir=" + profile,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-session-crashed-bubble",
		"--hide-crash-restore-bubble",
		"--disable-component-update",
		"--disable-background-networking",
		"--disable-sync",
		"--disable-features=CalculateNativeWinOcclusion,TranslateUI,MediaRouter",
	}
}

func pngToICO(png []byte) ([]byte, error) {
	if len(png) < 24 || png[0] != 0x89 || string(png[1:4]) != "PNG" {
		return nil, fmt.Errorf("not a PNG")
	}
	w := binary.BigEndian.Uint32(png[16:20])
	h := binary.BigEndian.Uint32(png[20:24])
	iw, ih := byte(w), byte(h)
	if w >= 256 {
		iw = 0
	}
	if h >= 256 {
		ih = 0
	}
	out := make([]byte, 22+len(png))
	binary.LittleEndian.PutUint16(out[2:4], 1)
	binary.LittleEndian.PutUint16(out[4:6], 1)
	out[6] = iw
	out[7] = ih
	out[10] = 1
	out[12] = 32
	binary.LittleEndian.PutUint32(out[14:18], uint32(len(png)))
	binary.LittleEndian.PutUint32(out[18:22], 22)
	copy(out[22:], png)
	return out, nil
}

func appIconICO() ([]byte, error) {
	b, err := webFS.ReadFile("web/icon.ico")
	if err != nil {
		return nil, err
	}
	if _, err := icoFrameSizes(b); err != nil {
		return nil, err
	}
	return b, nil
}

func icoFrameSizes(ico []byte) ([]int, error) {
	if len(ico) < 6 {
		return nil, fmt.Errorf("ico too small")
	}
	if binary.LittleEndian.Uint16(ico[2:4]) != 1 {
		return nil, fmt.Errorf("not an icon")
	}
	n := int(binary.LittleEndian.Uint16(ico[4:6]))
	if n < 1 || len(ico) < 6+16*n {
		return nil, fmt.Errorf("bad ico count %d", n)
	}
	out := make([]int, n)
	for i := 0; i < n; i++ {
		w := int(ico[6+16*i])
		if w == 0 {
			w = 256
		}
		out[i] = w
	}
	return out, nil
}
