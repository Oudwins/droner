//go:build !windows

package backends

import (
	"os/exec"
	"syscall"
)

func detachCmd(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
