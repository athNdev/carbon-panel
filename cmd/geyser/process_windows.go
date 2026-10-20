//go:build windows

package main

import (
	"os"
	"os/exec"
)

func chownRecursive(path string, uid, gid int) error {
	return nil
}

func chownPath(path string, uid, gid int) error {
	return nil
}

func runJava(javaPath string, args []string, puid, pgid int) error {
	cmd := exec.Command(javaPath, args[1:]...)
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}
