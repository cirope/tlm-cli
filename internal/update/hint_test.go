package update

import "testing"

func TestUpgradeHint(t *testing.T) {
	has := func(paths ...string) func(string) bool {
		return func(p string) bool {
			for _, q := range paths {
				if p == q {
					return true
				}
			}
			return false
		}
	}
	none := has()
	const home = "/home/ana"
	installer := "curl -fsSL https://packages.hullop.com/tlm/install.sh | sh"
	tests := []struct {
		name, exe, goos, gopath string
		has                     func(string) bool
		want                    string
	}{
		{"homebrew cask", "/opt/homebrew/Caskroom/tlm/0.1.1/tlm", "darwin", "", none, "brew upgrade tlm"},
		{"homebrew on intel", "/usr/local/Caskroom/tlm/0.1.1/tlm", "darwin", "", none, "brew upgrade tlm"},
		{"linuxbrew", "/home/linuxbrew/.linuxbrew/Caskroom/tlm/0.1.1/tlm", "linux", "", none, "brew upgrade tlm"},
		{"apt", "/usr/bin/tlm", "linux", "", has("/usr/bin/apt-get"), "sudo apt update && sudo apt upgrade tlm"},
		{"dnf", "/usr/bin/tlm", "linux", "", has("/usr/bin/dnf"), "sudo dnf upgrade tlm"},
		{"zypper", "/usr/bin/tlm", "linux", "", has("/usr/bin/zypper"), "sudo zypper update tlm"},
		{"alpine", "/usr/bin/tlm", "linux", "", has("/sbin/apk"), installer},
		{"unknown package manager", "/usr/bin/tlm", "linux", "", none, releases},
		{"go install default", home + "/go/bin/tlm", "linux", "", none, "go install github.com/cirope/tlm-cli/cmd/tlm@latest"},
		{"go install GOPATH", "/opt/go/bin/tlm", "linux", "/opt/go", none, "go install github.com/cirope/tlm-cli/cmd/tlm@latest"},
		{"installer, system", "/usr/local/bin/tlm", "darwin", "", none, installer},
		{"by hand, elsewhere", "/opt/tools/tlm", "linux", "", none, releases},
		{"windows", `C:\tools\tlm.exe`, "windows", "", none, "download it from https://github.com/cirope/tlm-cli/releases"},
		{"unknown path", "", "linux", "", none, "download it from https://github.com/cirope/tlm-cli/releases"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := UpgradeHint(Install{Exe: tt.exe, GOOS: tt.goos, Home: home, GOPATH: tt.gopath, Has: tt.has})
			if got != tt.want {
				t.Errorf("UpgradeHint(%q) = %q, want %q", tt.exe, got, tt.want)
			}
		})
	}
}
