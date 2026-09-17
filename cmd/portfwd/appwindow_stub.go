//go:build !windows

package main

func decorateAppWindow(uint32) {}

func closeStaleAppWindows(string) {}
