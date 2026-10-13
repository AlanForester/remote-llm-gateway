//go:build windows

package backend

import "os/exec"

func hardenCommand(cmd *exec.Cmd) {}
