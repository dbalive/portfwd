//go:build !windows

package main

func secureSSHPath(path string, dir bool) error {
	return nil
}
