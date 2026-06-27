//go:build !windows && !linux

package captcha

import (
	"os/exec"
	"syscall"
)

func hideProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func attachProcess(_ *exec.Cmd) error { return nil }

func killProcessTree(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}
