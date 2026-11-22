package update

import (
	"path"
	"strings"
)

// Install describes where the running tlm lives, to tell how it
// was installed.
type Install struct {
	Exe    string // The executable's path, symlinks resolved; "" if unknown.
	GOOS   string
	Home   string // The user's home directory.
	GOPATH string // $GOPATH, if set.
	GOBIN  string // $GOBIN, if set.
	// Has reports whether a file exists; it tells the package managers apart.
	Has func(string) bool
}

const (
	releases  = "download it from https://github.com/cirope/tlm-cli/releases"
	installer = "curl -fsSL https://packages.hullop.com/tlm/install.sh | sh"
)

// UpgradeHint is the command that upgrades tlm the way it was
// installed: Homebrew, a Linux package, go install or the install script.
func UpgradeHint(in Install) string {
	if in.Exe == "" || in.GOOS == "windows" {
		return releases
	}
	dir := path.Dir(in.Exe)
	switch {
	case strings.Contains(in.Exe, "/Caskroom/") || strings.Contains(in.Exe, "/Cellar/") ||
		strings.Contains(in.Exe, "/.linuxbrew/"):
		return "brew upgrade tlm"
	case dir == "/usr/bin":
		switch {
		case in.Has("/usr/bin/apt-get"):
			return "sudo apt update && sudo apt upgrade tlm"
		case in.Has("/usr/bin/dnf"):
			return "sudo dnf upgrade tlm"
		case in.Has("/usr/bin/zypper"):
			return "sudo zypper update tlm"
		case in.Has("/sbin/apk"):
			return installer
		}
		return releases
	case isGoBin(dir, in):
		return "go install github.com/cirope/tlm-cli/cmd/tlm@latest"
	case dir == "/usr/local/bin":
		return installer
	}
	return releases
}

func isGoBin(dir string, in Install) bool {
	if in.GOBIN != "" {
		return dir == in.GOBIN
	}
	if in.GOPATH != "" {
		return dir == path.Join(in.GOPATH, "bin")
	}
	return in.Home != "" && dir == path.Join(in.Home, "go/bin")
}
