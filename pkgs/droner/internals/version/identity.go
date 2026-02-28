package version

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
)

var (
	identityOnce sync.Once
	identityVal  string
)

// Identity returns a best-effort build identity string that changes on rebuilds.
//
// Format:
//   - <rev12><-dirty?>+<exeHash12>
//   - <exeHash12> (if vcs metadata missing)
//   - <rev12><-dirty?> (if hashing fails)
//   - unknown
func Identity() string {
	identityOnce.Do(func() {
		identityVal = computeIdentity()
	})
	return identityVal
}

func computeIdentity() string {
	rev, dirty := vcsInfo()
	hash := executableHash()

	if hash != "" && rev != "" {
		if dirty {
			return rev + "-dirty+" + hash
		}
		return rev + "+" + hash
	}
	if hash != "" {
		return hash
	}
	if rev != "" {
		if dirty {
			return rev + "-dirty"
		}
		return rev
	}
	return "unknown"
}

func vcsInfo() (rev12 string, dirty bool) {
	info, ok := debug.ReadBuildInfo()
	if !ok || info == nil {
		return "", false
	}

	var revision string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = strings.TrimSpace(s.Value)
		case "vcs.modified":
			v := strings.TrimSpace(strings.ToLower(s.Value))
			dirty = v == "true" || v == "1" || v == "yes"
		}
	}

	if revision == "" {
		return "", dirty
	}
	if len(revision) > 12 {
		revision = revision[:12]
	}
	return revision, dirty
}

func executableHash() string {
	exe, err := os.Executable()
	if err != nil || strings.TrimSpace(exe) == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil && strings.TrimSpace(resolved) != "" {
		exe = resolved
	}

	f, err := os.Open(exe)
	if err != nil {
		return ""
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	sum := h.Sum(nil)
	hexSum := hex.EncodeToString(sum)
	if len(hexSum) > 12 {
		hexSum = hexSum[:12]
	}
	return hexSum
}
