//go:build !windows

package main

import "os"

func replaceFile(tmp, dest string) error {
	return os.Rename(tmp, dest)
}
