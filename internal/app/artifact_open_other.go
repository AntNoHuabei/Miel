//go:build !windows

package app

import (
	"os/exec"
	"runtime"
)

func openArtifactFile(path string) error {
	command := "xdg-open"
	if runtime.GOOS == "darwin" {
		command = "open"
	}
	cmd := exec.Command(command, path)
	hideProcessWindow(cmd)
	return cmd.Start()
}
