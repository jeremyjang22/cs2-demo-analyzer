//go:build !windows

package worker

import "os/exec"

// background does nothing off Windows: in the cloud the worker has its
// machine to itself, and there is no one to give way to.
func background(*exec.Cmd) {}
