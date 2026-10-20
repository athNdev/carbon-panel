//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"syscall"
)

func chownPath(path string, uid, gid int) error {
	return os.Chown(path, uid, gid)
}

func chownRecursive(path string, uid, gid int) error {
	return filepath.Walk(path, func(name string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		return os.Chown(name, uid, gid)
	})
}

func runJava(javaPath string, args []string, puid, pgid int) error {
	if err := syscall.Setgid(pgid); err != nil {
		return err
	}
	if err := syscall.Setuid(puid); err != nil {
		return err
	}
	return syscall.Exec(javaPath, args, os.Environ())
}
