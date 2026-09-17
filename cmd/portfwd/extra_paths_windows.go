//go:build windows

package main

import (
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

func extraUninstallPaths(nameContains, exeName string) []string {
	var out []string
	out = append(out, appPathExe(exeName)...)
	out = append(out, scanUninstall(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`, nameContains, exeName)...)
	out = append(out, scanUninstall(registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`, nameContains, exeName)...)
	out = append(out, scanUninstall(registry.CURRENT_USER, `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`, nameContains, exeName)...)
	out = append(out, scanVendor(registry.LOCAL_MACHINE, `SOFTWARE\NetSarang`, exeName)...)
	out = append(out, scanVendor(registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\NetSarang`, exeName)...)
	out = append(out, scanVendor(registry.CURRENT_USER, `SOFTWARE\NetSarang`, exeName)...)
	return out
}

func appPathExe(exeName string) []string {
	var out []string
	for _, root := range []registry.Key{registry.LOCAL_MACHINE, registry.CURRENT_USER} {
		for _, path := range []string{
			`SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\` + exeName,
			`SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\App Paths\` + exeName,
		} {
			k, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			val, _, err := k.GetStringValue("")
			_ = k.Close()
			if err == nil {
				out = append(out, strings.Trim(val, `"`))
			}
		}
	}
	return out
}

func scanUninstall(root registry.Key, path, nameContains, exeName string) []string {
	k, err := registry.OpenKey(root, path, registry.ENUMERATE_SUB_KEYS|registry.QUERY_VALUE)
	if err != nil {
		return nil
	}
	defer k.Close()
	names, _ := k.ReadSubKeyNames(0)
	needle := strings.ToLower(nameContains)
	var out []string
	for _, n := range names {
		sk, err := registry.OpenKey(k, n, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		disp, _, _ := sk.GetStringValue("DisplayName")
		if !strings.Contains(strings.ToLower(disp), needle) {
			_ = sk.Close()
			continue
		}
		loc, _, _ := sk.GetStringValue("InstallLocation")
		icon, _, _ := sk.GetStringValue("DisplayIcon")
		_ = sk.Close()
		loc = strings.TrimSpace(strings.Trim(loc, `"`))
		if loc != "" {
			out = append(out, filepath.Join(loc, exeName))
			if p := findNamedUnder(loc, exeName, 3); p != "" {
				out = append(out, p)
			}
		}
		if icon != "" {
			icon = strings.TrimSpace(strings.Trim(strings.Split(icon, ",")[0], `"`))
			if icon != "" {
				out = append(out, icon)
			}
		}
	}
	return out
}

func scanVendor(root registry.Key, path, exeName string) []string {
	k, err := registry.OpenKey(root, path, registry.ENUMERATE_SUB_KEYS|registry.QUERY_VALUE)
	if err != nil {
		return nil
	}
	defer k.Close()
	var out []string
	collectKeyPaths(k, exeName, &out)
	names, _ := k.ReadSubKeyNames(0)
	for _, n := range names {
		sk, err := registry.OpenKey(k, n, registry.ENUMERATE_SUB_KEYS|registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		collectKeyPaths(sk, exeName, &out)
		subs, _ := sk.ReadSubKeyNames(0)
		for _, sn := range subs {
			ck, err := registry.OpenKey(sk, sn, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			collectKeyPaths(ck, exeName, &out)
			_ = ck.Close()
		}
		_ = sk.Close()
	}
	return out
}

func collectKeyPaths(k registry.Key, exeName string, out *[]string) {
	for _, name := range []string{"", "Path", "InstallDir", "InstallPath", "AppPath", "InstallLocation"} {
		val, _, err := k.GetStringValue(name)
		if err != nil || strings.TrimSpace(val) == "" {
			continue
		}
		val = strings.Trim(val, `"`)
		*out = append(*out, val)
		if strings.HasSuffix(strings.ToLower(val), ".exe") {
			continue
		}
		*out = append(*out, filepath.Join(val, exeName))
	}
}
