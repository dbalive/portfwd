//go:build !windows

package main

import (
	"fmt"
	"os"
)

func runGUI() {
	fmt.Println("端口转发 GUI 仅支持 Windows")
	os.Exit(1)
}
