package main

import (
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestShouldQuitOnWindowExit(t *testing.T) {
	if !shouldQuitOnWindowExit(false) {
		t.Fatal("closing the UI window should quit")
	}
	if shouldQuitOnWindowExit(true) {
		t.Fatal("our own kill should not quit twice")
	}
	if shouldQuitAfterLostWindow(false, true, false, false, 100*time.Millisecond, 100*time.Millisecond) {
		t.Fatal("Edge recreates the first hwnd; that is not quit")
	}
	if shouldQuitAfterLostWindow(false, true, true, true, time.Second, time.Second) {
		t.Fatal("a replacement window must keep the UI")
	}
	if shouldQuitAfterLostWindow(false, false, false, false, 100*time.Millisecond, 100*time.Millisecond) {
		t.Fatal("Edge may hand off to an existing process; do not quit instantly")
	}
	if !shouldQuitAfterLostWindow(false, false, false, true, time.Second, time.Second) {
		t.Fatal("the browser process exiting after a real window should quit")
	}
}

func TestCloseButtonQuitsInsteadOfTray(t *testing.T) {
	if shouldQuitHiddenUI(false, false, false, false, false) {
		t.Fatal("the first hidden Chrome window is startup, not the close button")
	}
	if !shouldQuitHiddenUI(false, false, false, false, true) {
		t.Fatal("X hides the window without minimizing; that must quit")
	}
	if shouldQuitHiddenUI(false, false, true, false, true) {
		t.Fatal("after minimize-to-tray, keep running")
	}
	if shouldQuitHiddenUI(false, true, false, false, true) {
		t.Fatal("minimize button should go to tray, not quit")
	}
	if shouldQuitHiddenUI(true, false, false, false, true) {
		t.Fatal("visible window must stay")
	}
	if !shouldHideMinimized(true) || shouldHideMinimized(false) {
		t.Fatal("only the minimize button hides to tray")
	}
	if shouldConfirmMinimize(true, 0) {
		t.Fatal("one poll is enough to see a real minimize")
	}
	if !shouldConfirmMinimize(true, 80*time.Millisecond) {
		t.Fatal("a held minimize should go to tray")
	}
	if shouldConfirmHiddenClose(true, time.Second, 200*time.Millisecond) {
		t.Fatal("Edge hides the first window while loading; that must not stop the server")
	}
	if !shouldConfirmHiddenClose(true, 3*time.Second, 800*time.Millisecond) {
		t.Fatal("after the UI was up, a real X should quit")
	}
	if !shouldQuitOnCaption("close") || shouldTrayOnCaption("close") {
		t.Fatal("the window X must quit, not tray")
	}
	if !shouldTrayOnCaption("min") || shouldQuitOnCaption("min") {
		t.Fatal("the minimize button should tray")
	}
	if captionButtonHit(0, 0, 100, 0, 235, 32, 220, 10) != "close" {
		t.Fatal("right third of the caption cluster is close")
	}
	if captionButtonHit(0, 0, 100, 0, 235, 32, 110, 10) != "min" {
		t.Fatal("left third of the caption cluster is minimize")
	}
	if fallbackCaptionHit(0, 0, 300, 290, 8, 46, 32) != "close" {
		t.Fatal("fallback close is the rightmost caption button")
	}
	if fallbackCaptionHit(0, 0, 300, 180, 8, 46, 32) != "min" {
		t.Fatal("fallback minimize is the leftmost caption button")
	}
}

func TestWaitLocalUIUntilReady(t *testing.T) {
	ready := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-ready:
		default:
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	go func() {
		time.Sleep(40 * time.Millisecond)
		close(ready)
	}()
	if !waitLocalUI(srv.URL, time.Second) {
		t.Fatal("must wait until the local UI answers")
	}
	if waitLocalUI("", time.Second) || waitLocalUI("http://127.0.0.1:1", 30*time.Millisecond) {
		t.Fatal("empty or dead url must not look ready")
	}
}

func TestAppWindowArgsForceVisibleProfile(t *testing.T) {
	got := appWindowArgs("http://127.0.0.1:9", `C:\tmp\portfwd-ui`)
	joined := strings.Join(got, "\n")
	if !containsAll(got, "--app=http://127.0.0.1:9") {
		t.Fatalf("missing app url: %s", joined)
	}
	if !containsAll(got, "--window-size=1000,700") {
		t.Fatalf("need a compact default window: %s", joined)
	}
	if !containsAll(got, "--window-position=80,60") {
		t.Fatalf("need explicit position so the window is not restored minimized: %s", joined)
	}
	if !containsAll(got, "--no-first-run") || !containsAll(got, "--no-default-browser-check") {
		t.Fatalf("need first-run flags: %s", joined)
	}
	if !containsAll(got, `--user-data-dir=C:\tmp\portfwd-ui`) {
		t.Fatalf("profile: %s", joined)
	}
	if !containsAll(got, "--disable-session-crashed-bubble") || !containsAll(got, "--hide-crash-restore-bubble") {
		t.Fatalf("must not restore a dead session: %s", joined)
	}
	if !containsAll(got, "--disable-component-update") || !containsAll(got, "--disable-background-networking") {
		t.Fatalf("first launch must skip Edge updater work: %s", joined)
	}
}

func TestUIListenPrefersStablePort(t *testing.T) {
	got := uiListenCandidates()
	if len(got) < 2 || got[0] != "127.0.0.1:18765" || got[len(got)-1] != "127.0.0.1:0" {
		t.Fatalf("%v", got)
	}
}

func TestStaleAppErrorTitle(t *testing.T) {
	if !isStaleAppErrorTitle("127.0.0.1") || !isStaleAppErrorTitle("127.0.0.1:18765") {
		t.Fatal("Edge refused-connection page")
	}
	if isStaleAppErrorTitle("Microsoft Edge") || isStaleAppErrorTitle("") {
		t.Fatal("unrelated title")
	}
}

func TestPrepareAppProfileClearsSessionLocks(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SingletonLock"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "Sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	prepareAppProfile(dir)
	if _, err := os.Stat(filepath.Join(dir, "SingletonLock")); !os.IsNotExist(err) {
		t.Fatal("lock must be cleared so a new Edge can start")
	}
	if _, err := os.Stat(filepath.Join(dir, "Sessions")); err != nil {
		t.Fatal("keep the warm profile so the next launch is faster")
	}
}

func TestPngToICOHasIconHeader(t *testing.T) {
	png, err := webFS.ReadFile("web/icon.png")
	if err != nil {
		t.Fatal(err)
	}
	ico, err := pngToICO(png)
	if err != nil {
		t.Fatal(err)
	}
	if len(ico) < 22+len(png) {
		t.Fatalf("ico too small: %d", len(ico))
	}
	if binary.LittleEndian.Uint16(ico[2:4]) != 1 {
		t.Fatalf("type should be icon, got %d", binary.LittleEndian.Uint16(ico[2:4]))
	}
	if binary.LittleEndian.Uint16(ico[4:6]) != 1 {
		t.Fatalf("one image, got %d", binary.LittleEndian.Uint16(ico[4:6]))
	}
	off := binary.LittleEndian.Uint32(ico[18:22])
	if int(off) != 22 {
		t.Fatalf("png offset %d", off)
	}
}

func TestAppIconICOHasOpticalSizes(t *testing.T) {
	ico, err := appIconICO()
	if err != nil {
		t.Fatal(err)
	}
	sizes, err := icoFrameSizes(ico)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int]bool{}
	for _, w := range sizes {
		seen[w] = true
	}
	for _, need := range []int{16, 24, 32, 48, 256} {
		if !seen[need] {
			t.Fatalf("icon.ico missing %dpx frame, got %v", need, sizes)
		}
	}
	if len(sizes) < 6 {
		t.Fatalf("want separate optical sizes, got %v", sizes)
	}
	frameW := int(ico[6])
	if frameW == 0 {
		frameW = 256
	}
	if frameW != 16 {
		t.Fatalf("first ico frame should be the 16px optical cut, got %d", frameW)
	}
	pngLen := binary.LittleEndian.Uint32(ico[14:18])
	off := binary.LittleEndian.Uint32(ico[18:22])
	if int(off)+int(pngLen) > len(ico) {
		t.Fatalf("16px frame overflow")
	}
	blob := ico[off : int(off)+int(pngLen)]
	var native uint32
	if len(blob) >= 24 && blob[0] == 0x89 && string(blob[1:4]) == "PNG" {
		native = binary.BigEndian.Uint32(blob[16:20])
	} else if len(blob) >= 8 {
		native = binary.LittleEndian.Uint32(blob[4:8])
	}
	if native != 16 {
		t.Fatalf("16px frame was not drawn at native 16px, got %d", native)
	}
}
