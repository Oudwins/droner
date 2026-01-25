package desktop

import (
	"errors"
	"os/exec"
	"runtime"
)

func OpenURL(url string) error {
	if url == "" {
		return errors.New("url is empty")
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		return errors.New("unsupported platform")
	}

	return cmd.Start()
}
