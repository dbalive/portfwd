//go:build windows

package main

import (
	"sync"

	"golang.org/x/sys/windows"
)

type singleInstance struct {
	mu    windows.Handle
	ev    windows.Handle
	show  func()
	showM sync.Mutex
}

func acquireSingleInstance() (*singleInstance, bool) {
	mName, err := windows.UTF16PtrFromString(portfwdMutexName)
	if err != nil {
		return &singleInstance{}, true
	}
	mutex, err := windows.CreateMutex(nil, false, mName)
	if err == windows.ERROR_ALREADY_EXISTS {
		if mutex != 0 {
			_ = windows.CloseHandle(mutex)
		}
		return nil, false
	}
	if err != nil {
		return &singleInstance{}, true
	}
	eName, err := windows.UTF16PtrFromString(portfwdShowEvent)
	if err != nil {
		_ = windows.CloseHandle(mutex)
		return &singleInstance{}, true
	}
	ev, err := windows.CreateEvent(nil, 0, 0, eName)
	if err != nil {
		_ = windows.CloseHandle(mutex)
		return &singleInstance{}, true
	}
	inst := &singleInstance{mu: mutex, ev: ev}
	go inst.listenShow()
	return inst, true
}

func (s *singleInstance) SetShow(fn func()) {
	if s == nil {
		return
	}
	s.showM.Lock()
	s.show = fn
	s.showM.Unlock()
}

func (s *singleInstance) listenShow() {
	for {
		ev, err := windows.WaitForSingleObject(s.ev, windows.INFINITE)
		if err != nil || ev != windows.WAIT_OBJECT_0 {
			return
		}
		s.showM.Lock()
		fn := s.show
		s.showM.Unlock()
		if fn != nil {
			fn()
		}
	}
}

func (s *singleInstance) Release() {
	if s == nil {
		return
	}
	if s.ev != 0 {
		_ = windows.CloseHandle(s.ev)
		s.ev = 0
	}
	if s.mu != 0 {
		_ = windows.CloseHandle(s.mu)
		s.mu = 0
	}
}

func notifyExistingInstance() {
	eName, err := windows.UTF16PtrFromString(portfwdShowEvent)
	if err == nil {
		ev, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE|windows.SYNCHRONIZE, false, eName)
		if err == nil {
			_ = windows.SetEvent(ev)
			_ = windows.CloseHandle(ev)
		}
	}
	if w := findAppWindowByTitle(""); w != 0 {
		forceForeground(w)
	}
}
