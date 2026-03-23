package tui

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

const maxClipboardImageBytes = 5 << 20

type clipboardImage struct {
	Bytes    []byte
	Mime     string
	Filename string
}

type clipboardImageReader func() (clipboardImage, bool, error)

var supportedClipboardImageMIMEs = []string{"image/png", "image/jpeg", "image/webp", "image/gif"}

var clipboardImageExtensions = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

func defaultReadClipboardImage() (clipboardImage, bool, error) {
	switch runtime.GOOS {
	case "darwin":
		return readClipboardImageDarwin()
	case "linux":
		return readClipboardImageLinux()
	default:
		return clipboardImage{}, false, nil
	}
}

func readClipboardImageDarwin() (clipboardImage, bool, error) {
	if _, err := exec.LookPath("pngpaste"); err != nil {
		return clipboardImage{}, false, nil
	}
	output, err := exec.Command("pngpaste", "-").Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return clipboardImage{}, false, nil
		}
		return clipboardImage{}, false, err
	}
	if len(output) == 0 {
		return clipboardImage{}, false, nil
	}
	return clipboardImage{Bytes: output, Mime: "image/png"}, true, nil
}

func readClipboardImageLinux() (clipboardImage, bool, error) {
	if image, ok, err := readClipboardImageWayland(); ok || err != nil {
		return image, ok, err
	}
	return readClipboardImageX11()
}

func readClipboardImageWayland() (clipboardImage, bool, error) {
	if _, err := exec.LookPath("wl-paste"); err != nil {
		return clipboardImage{}, false, nil
	}
	types, err := exec.Command("wl-paste", "--list-types").Output()
	if err != nil {
		return clipboardImage{}, false, nil
	}
	return readClipboardImageFromTypes(types, func(mime string) ([]byte, error) {
		return exec.Command("wl-paste", "--no-newline", "--type", mime).Output()
	})
}

func readClipboardImageX11() (clipboardImage, bool, error) {
	if _, err := exec.LookPath("xclip"); err != nil {
		return clipboardImage{}, false, nil
	}
	types, err := exec.Command("xclip", "-selection", "clipboard", "-t", "TARGETS", "-o").Output()
	if err != nil {
		return clipboardImage{}, false, nil
	}
	return readClipboardImageFromTypes(types, func(mime string) ([]byte, error) {
		return exec.Command("xclip", "-selection", "clipboard", "-t", mime, "-o").Output()
	})
}

func readClipboardImageFromTypes(rawTypes []byte, readData func(mime string) ([]byte, error)) (clipboardImage, bool, error) {
	available := make(map[string]struct{})
	for _, line := range strings.Split(strings.ToLower(string(rawTypes)), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			available[trimmed] = struct{}{}
		}
	}
	for _, mime := range supportedClipboardImageMIMEs {
		if _, ok := available[mime]; !ok {
			continue
		}
		bytes, err := readData(mime)
		if err != nil {
			return clipboardImage{}, false, fmt.Errorf("read clipboard image %s: %w", mime, err)
		}
		if len(bytes) == 0 {
			continue
		}
		return clipboardImage{Bytes: bytes, Mime: mime}, true, nil
	}
	return clipboardImage{}, false, nil
}

func isSupportedClipboardImageMime(mime string) bool {
	trimmed := strings.TrimSpace(strings.ToLower(mime))
	for _, supported := range supportedClipboardImageMIMEs {
		if trimmed == supported {
			return true
		}
	}
	return false
}

func normalizeClipboardImage(image clipboardImage, index int) (clipboardImage, error) {
	if len(image.Bytes) == 0 {
		return clipboardImage{}, errors.New("clipboard does not contain image data")
	}
	if len(image.Bytes) > maxClipboardImageBytes {
		return clipboardImage{}, fmt.Errorf("clipboard image is too large (max 5 MiB)")
	}
	image.Mime = strings.TrimSpace(strings.ToLower(image.Mime))
	if !isSupportedClipboardImageMime(image.Mime) {
		return clipboardImage{}, fmt.Errorf("clipboard image type %q is not supported", image.Mime)
	}
	image.Bytes = bytes.Clone(image.Bytes)
	image.Filename = strings.TrimSpace(image.Filename)
	if image.Filename == "" {
		image.Filename = fmt.Sprintf("pasted-image-%d%s", index, clipboardImageExtensions[image.Mime])
	}
	return image, nil
}
