//go:build !darwin && !linux

package archive

import "os/exec"

func configureNativeProcess(cmd *exec.Cmd) {}

func finishNativeProcess(cmd *exec.Cmd) {}
