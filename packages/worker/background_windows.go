package worker

import (
	"os/exec"
	"syscall"
)

const (
	belowNormalPriorityClass = 0x00004000
	createNoWindow           = 0x08000000
)

// background runs the child below normal priority and without a console
// window. On the owner's PC the parse should give way to whatever they are
// doing - a game, most likely - and not flash a window up while it runs.
func background(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: belowNormalPriorityClass | createNoWindow}
}
