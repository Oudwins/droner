package desktop

import (
	"errors"
	"os/exec"
	"runtime"
)

var ExecCommand = exec.Command
var RuntimeGOOS = runtime.GOOS

func OpenURL(url string) error {
	if url == "" {
		return errors.New("url is empty")
	}

	var cmd *exec.Cmd
	switch RuntimeGOOS {
	case "darwin":
		cmd = ExecCommand("open", url)
	case "linux":
		cmd = ExecCommand("xdg-open", url)
	default:
		return errors.New("unsupported platform")
	}

	return cmd.Start()
}
