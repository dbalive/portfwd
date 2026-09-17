//go:build !windows

package main

import "os/exec"

func startNewConsole(cmd *exec.Cmd) error {
	return cmd.Start()
}

func startDetachedConsole(title, script string) error {
	cmd := exec.Command("cmd.exe", cmdStartArgv(title, script)...)
	return cmd.Start()
}
