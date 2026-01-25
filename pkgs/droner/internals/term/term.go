package term

import "os"

func SupportsHyperlinks() bool {
	term := os.Getenv("TERM")
	if term == "" || term == "dumb" {
		return false
	}
	if term == "alacritty" {
		return false
	}
	if os.Getenv("WT_SESSION") != "" {
		return true
	}
	if os.Getenv("VTE_VERSION") != "" {
		return true
	}
	if os.Getenv("KONSOLE_VERSION") != "" {
		return true
	}
	if os.Getenv("KITTY_WINDOW_ID") != "" {
		return true
	}
	if os.Getenv("WEZTERM_EXECUTABLE") != "" {
		return true
	}
	if os.Getenv("DOMTERM") != "" {
		return true
	}
	if os.Getenv("TERM_PROGRAM") != "" {
		return true
	}
	return false
}

func ClickableLink(label string, url string) string {
	if url == "" {
		return label
	}
	if label == "" {
		label = url
	}
	if !SupportsHyperlinks() {
		return label
	}
	return "\x1b]8;;" + url + "\x1b\\" + label + "\x1b]8;;\x1b\\"
}
