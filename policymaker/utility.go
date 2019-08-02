package policymaker

import (
	"io"
	"os"
	"os/exec"
	"runtime"

	"github.com/armon/circbuf"
)

func execCmd(command string) string {
	const maxBufSize = 16 * 1024
	// Execute the command using a shell
	var shell, flag string
	if runtime.GOOS == "windows" {
		shell = "cmd"
		flag = "/C"
	} else {
		shell = "/bin/sh"
		flag = "-c"
	}
	cmd := exec.Command(shell, flag, command)
	stdout, _ := circbuf.NewBuffer(maxBufSize)
	stderr, _ := circbuf.NewBuffer(maxBufSize)
	cmd.Stderr = io.Writer(stderr)
	cmd.Stdout = io.Writer(stdout)
	cmd.Run()
	return stdout.String()
}

// exists returns whether the given file or directory exists
func exists(path string) bool {
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	return true
}
