# Changelog

What changed in each release of `tlm`, for the people who use it.

## Unreleased

- `issues delete <id>` deletes an issue and all its events, for good
  (admins), for when an event brought something that must not be kept.
  It shows the issue and asks first, or takes `--yes`.

## 0.3.1

- `tlm api --help` links to Tlm's API docs.

## 0.3.0

- A repository names its project in `.tlm.toml` (`tlm use --here
  acme/shop` writes it), found from the current directory up. It wins
  over `tlm use`'s machine-wide default; `--org`, `--project`, `TLM_ORG`
  and `TLM_PROJECT` still win over it.
- `projects create --origin <host>` allows the app's domains as the
  project is created (repeatable).
- `upload-tokens create --gh-secret <name>` stores the new token as a
  GitHub Actions secret through `gh secret set` (`--gh-env`,
  `--gh-repo`), so it never shows or goes through a shell.
- `upload --sources` maps scripts that have no source map line for line,
  from their own code, for apps that serve JavaScript as written
  (importmap). Minified ones are skipped with a warning.

## 0.2.0

- `tlm guide` prints Tlm's step-by-step guide for AI agents to add Tlm
  to an app, without signing in. `tlm --help`, `login` and
  `projects create` point to it.
- `upload` sends to the DSN without its key, path included, as `test`
  does: a Tlm served under a path gets its source maps there. It needs a
  DSN (`--dsn` or `TLM_DSN`), except with `--dry-run`, instead of
  falling back to tlm.hullop.com.
- `test` sends its event as platform `other`, so it no longer shows a
  project as Node.
- `issues list --platform` mentions `ruby`.

## 0.1.0

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
