//go:build !windows

package main

func copyToClipboard(string) error { return nil }
