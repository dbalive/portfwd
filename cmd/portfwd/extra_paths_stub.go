//go:build !windows

package main

func extraUninstallPaths(nameContains, exeName string) []string {
	return nil
}
