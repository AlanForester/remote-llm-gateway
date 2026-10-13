//go:build unix

package secrets

import "syscall"

func reexec(exe string, args []string, env []string) error {
	return syscall.Exec(exe, args, env)
}
