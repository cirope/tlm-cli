# Changelog

What changed in each release of `tlm`, for the people who use it.

## Unreleased

The first release of the Tlm CLI: one binary, for macOS, Linux and
Windows.

- `login`, `logout`, `whoami`, signing in by approving a code in the
  browser. The token is kept in the system keychain (or a file only you
  can read where there's none); `TLM_TOKEN` for CI and agents.
- `use acme/shop` sets the organization and project commands use when
  nothing else says.
- `projects list|create|show|edit|mute|unmute`, `dsn`, `upload-tokens`
  and `origins`.
- `issues list|show|events|event|resolve|ignore|reopen|assign`, with
  `--markdown` for an AI. An issue's id or its page's URL is enough.
- `speed` (Web Vitals, the slowest pages and requests, one page, one
  endpoint) and `signals` (when each environment was last heard from).
- `orgs list|show|edit`, `members list|role|remove` and `audit`, the
  organization's audit log.
- `upload` sends a build's source maps with an upload token, and `test`
  sends one test event.
- `open` opens the web app's page for a project or an issue; `api` makes
  a raw, authenticated request; `version` checks for a newer release;
  `completion` for bash, zsh, fish and PowerShell.
- Install with `curl -fsSL https://packages.hullop.com/tlm/install.sh | sh`,
  Homebrew (`cirope/tap/tlm`), `go install`, or Hullop's apt and dnf
  repositories at packages.hullop.com.
