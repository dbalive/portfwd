//go:build !windows

package main

type singleInstance struct{}

func acquireSingleInstance() (*singleInstance, bool) {
	return &singleInstance{}, true
}

func (s *singleInstance) SetShow(func()) {}

func (s *singleInstance) Release() {}

func notifyExistingInstance() {}
