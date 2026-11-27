# Tlm CLI

Manage [Tlm](https://tlm.hullop.com) from the terminal: sign in, set
projects up, read and settle issues, see how fast your pages are, and
upload your build's source maps. One static binary, for people, their
coding agents and CI.

```sh
tlm login
tlm issues list
tlm issues show 2C7ZffP787kc --markdown > issue.md
```

## Install

On Linux or macOS, the quickest way:

```sh
curl -fsSL https://packages.hullop.com/tlm/install.sh | sh
```

On Debian, Ubuntu, Fedora, RHEL, CentOS Stream and openSUSE it adds
Hullop's package repository, so your usual system upgrade brings each new
release. On macOS it uses Homebrew when you have it. Elsewhere it installs
the release binary, checked against the release's checksums.

```sh
brew install cirope/tap/tlm                          # macOS, or Linux with Homebrew
go install github.com/cirope/tlm-cli/cmd/tlm@latest  # with Go
```

On Debian, Ubuntu and derivatives, by hand (`sudo apt upgrade` brings each
new release):

```sh
curl -fsSL https://packages.hullop.com/hullop.gpg | sudo tee /usr/share/keyrings/hullop.gpg >/dev/null
echo "deb [signed-by=/usr/share/keyrings/hullop.gpg] https://packages.hullop.com/apt stable main" | sudo tee /etc/apt/sources.list.d/hullop.list
sudo apt update && sudo apt install tlm
```

On Fedora, RHEL and CentOS Stream (`sudo dnf upgrade` brings new releases):

```sh
sudo curl -fsSLo /etc/yum.repos.d/hullop.repo https://packages.hullop.com/rpm/hullop.repo
sudo dnf install tlm
```

On openSUSE, `sudo zypper addrepo https://packages.hullop.com/rpm/hullop.repo`
and `sudo zypper install tlm`. Packages and repositories are signed; see
[cirope/packages](https://github.com/cirope/packages).

Every release also has archives and `.deb`, `.rpm` and `.apk` packages for
macOS, Linux and Windows on amd64 and arm64, with a `checksums.txt` and a
signed provenance attestation:
[releases](https://github.com/cirope/tlm-cli/releases). On Windows, unzip
`tlm_windows_amd64.zip` and put `tlm.exe` on your `PATH`.

At a terminal, `tlm` says when a new version is out, at most once a day,
with the command to upgrade (`TLM_NO_UPDATE_NOTIFIER=1` turns it off).
`tlm version` checks now, anywhere (`--json` too). It never updates
itself.

## Signing in

```sh
tlm login
```

It prints a one-time code and opens Tlm in your browser (`--no-browser`
skips that, e.g. over SSH). Check that the code matches, approve, and the
terminal is signed in. Approving asks for a recent sign-in with your
passkey, or through your organization's identity provider where it uses
single sign-on. For a local Tlm, add `--url http://localhost:4000`; later
commands use the Tlm you last signed in to, until you pass another
`--url`.

```sh
tlm whoami    # who you are, the token's access, your organizations
tlm logout    # revokes this machine's token and forgets it
```

The token is kept in the system keychain (macOS Keychain, Windows
Credential Manager, the Secret Service on Linux), or, where there's none,
in a file only you can read. A token nobody uses for 90 days stops
working; an organization that turned single sign-on on may ask you to
sign in again (`tlm login`).

In CI and for agents, skip `login`: create a token in Tlm's account
settings, under "Access tokens" (it can be limited to one organization and
to read-only), and set it as `TLM_TOKEN`, with `TLM_URL` for a Tlm other
than tlm.hullop.com.

## Projects and setup

```sh
tlm projects list
tlm projects create "Web shop"       # admins; prints the new project's DSN
tlm projects create "Web shop" --origin shop.example.com   # and allows its domain
tlm projects show web
tlm dsn                              # only the DSN: TLM_DSN=$(tlm dsn)

tlm upload-tokens create "GitHub Actions"   # admins; prints the secret once
tlm upload-tokens create "GitHub Actions" --gh-secret TLM_UPLOAD_TOKEN   # or stores it in GitHub
tlm upload-tokens list
tlm upload-tokens revoke <id>

tlm origins list                      # the project's allowed domains (admins)
tlm origins allow shop.example.com    # a bare host means https://
tlm origins allow '*.example.com'     # every subdomain, not example.com itself
tlm origins block http://localhost:3000
tlm origins forget https://old.example.com

tlm projects edit web --name Storefront   # admins; the slug stays
tlm projects mute web                     # its issue emails off, for you (unmute: back on)
```

`--gh-secret` hands the secret to the GitHub CLI (`gh secret set`, of the
repository in the current directory, or `--gh-repo owner/name`, and
`--gh-env` for an environment's secret), so it never shows or goes
through a shell. If `gh` fails, the new token is revoked.

Events from an origin nobody answered yet are kept, and the project's admins
are asked, by email and on its page, to allow or block it; a blocked
origin's events are refused (403). A wildcard matches every subdomain, with
the same scheme and port, but not the domain itself: allow it too.

Deleting a project and rotating its DSN stay in the web app.

## Organizations and people

```sh
tlm orgs list
tlm orgs show acme
tlm orgs edit acme --name "Acme Labs"        # admins; the slug stays

tlm members list                              # admins
tlm members role bob@example.com admin
tlm members remove bob@example.com            # asks first, or --yes

tlm audit                                     # who did what (admins)
```

People are added in the web app.

## Which project

The organization and project come from `--org` and `--project` (or
`TLM_ORG` and `TLM_PROJECT`), else the repository's `.tlm.toml`, else the
default you set with `tlm use acme/shop` (per Tlm; `tlm use --clear`
forgets it). When you're in only one organization, or it has only one
project, you don't need any of them; when it's ambiguous, the error lists
the choices.

```sh
tlm use --here acme/shop   # writes .tlm.toml here: commit it
tlm use                    # what commands use, and where it comes from
tlm use --here --clear     # removes it
```

`.tlm.toml` is found from the current directory up, so it works anywhere
in the repository:

```toml
org = "acme"
project = "shop"
```

## Issues

```sh
tlm issues list                                  # unresolved
tlm issues list --range week is not a function   # seen this week, words search
tlm issues list --env production --release "$GIT_SHA"   # what a deploy introduced
tlm issues show 2C7ZffP787kc                     # or paste the issue's URL
tlm issues show 2C7ZffP787kc --markdown          # everything, written for an AI
tlm issues events 2C7ZffP787kc
tlm issues event 2C7ZffP787kc <uuid>             # one event in full (--markdown too)
tlm issues resolve 2C7ZffP787kc 9Hk2LmQp4RsT     # also: ignore, reopen
tlm issues assign 2C7ZffP787kc me                # an email, me, or none
tlm issues delete 2C7ZffP787kc                   # admins: it and all its events, for good
```

Filters for `issues list`: `--status unresolved|resolved|ignored`,
`--range day|week|month|all`, `--env`, `--platform elixir|javascript|node|ruby|other`,
`--release` (the release that introduced the issue) and `--search` (text
in the title, message or culprit; or give it as words). An issue id or URL
alone works without `--org` and `--project`.

`issues delete` is for when an event brought something that must not be
kept (an issue keeps its first event's message as its title). It can't be
undone: it shows the issue and its event count and asks first, or takes
`--yes` (without a terminal it needs `--yes`). The issue goes at once, its
events are purged in the background, and the next event like them opens a
new issue.

## Speed and signs of life

```sh
tlm speed                       # Web Vitals at p75, the slowest pages and requests
tlm speed --period month --device phone
tlm speed pages
tlm speed page /checkout
tlm speed request GET /api/cart
tlm signals                     # when each environment's servers and browsers were last heard from
```

The vitals are LCP, INP and CLS for page loads, and for in-app navigations
NAV (how long the move took) and NAV CLS (layout shifts in the 500 ms after
the click; CLS leaves those out). CLS and NAV CLS are scores; the rest are
times. A vital shows "—" until enough page views have it.

## Using it with an agent

To add Tlm to an app, hand the agent Tlm's guide: install and sign in,
pick the project, add Tlm in the app's language (package, settings, speed,
source maps in CI), then check it worked. It needs no sign-in and holds
nothing secret; `--lang es` asks for it in Spanish.

`tlm guide` covers every stack. `--stack ruby` (or `elixir`, `node`,
`javascript`, `other`; several with commas) shows the general steps and
that stack only. Without the flag, `tlm guide` reads the stack from the
current directory: `Gemfile`, `mix.exs`, or `package.json` (Node and the
browser). With several of those or none, it shows the whole guide.

```sh
tlm guide > TLM.md
claude "Add Tlm to this app: follow TLM.md"
```

Once it reports:

```sh
tlm issues list --env production --json
tlm issues show 2C7ZffP787kc --markdown > issue.md
claude "Fix the error in issue.md. When the fix is merged, run: tlm issues resolve 2C7ZffP787kc"
```

- Give the agent a read-only token, or one limited to one organization,
  through `TLM_TOKEN`.
- `--json` on any command prints Tlm's JSON as is. Lists show one page
  (`--limit` up to 100, then `--cursor`), or every page with `--all`.
  Errors under `--json` are `{"error": {"code", "message"}}` on stderr.
- Results go to stdout; hints, progress and warnings to stderr.
- `tlm issues show <id> --markdown` gives the error, the stack trace with
  the code around your app's lines, the request, breadcrumbs and tags,
  written for an AI asked to fix it.
- Tlm's messages and the Markdown follow `--lang en` or `--lang es` (or
  `TLM_LANG`), else the install's language. The CLI's own text is
  English.
- `tlm api <path>` makes a raw, authenticated request under `/api/v1`, for
  anything without a command of its own: see
  [Tlm's API docs](https://tlm.hullop.com/docs/api).
- `tlm open issue <id>` prints (and, at a terminal, opens) the issue's
  page in the web app; also `open setup`, `open speed`, `open settings`.

## Source maps

Upload **before you deploy**: events from a release whose maps aren't
uploaded yet stay minified until they are.

```sh
TLM_UPLOAD_TOKEN=tlmu_… TLM_DSN=https://tlm_…@tlm.hullop.com \
  tlm upload ./dist --release "$GIT_SHA"
```

- An **upload token** (`tlm upload-tokens create`, it starts with `tlmu_`)
  is read only from `TLM_UPLOAD_TOKEN`, never from a flag or a file. Keep
  it in your CI's secrets.
- The **DSN** (`--dsn` or `TLM_DSN`) says where Tlm is.
- The **release** is the same string your app reports.

| Option | Default | |
|---|---|---|
| `--release` | `TLM_RELEASE` | Required. The release your app reports |
| `--dsn` | `TLM_DSN` | Required (except with `--dry-run`). Your project's DSN: where Tlm is, path included |
| `--url-prefix` | `/` | The URL path the directory is served at, e.g. `/static/` |
| `--delete` | off | Delete the `.map` files (and the comments pointing at them) after a successful upload |
| `--dry-run` | off | List what would be uploaded, send nothing |
| `--sources` | off | Also map each script that has no map line for line, from its own code: for apps that serve JavaScript as written (importmap) |

It finds every `.map` file under the directory and pairs it with its bundle
(by the bundle's `sourceMappingURL` comment, the map's `file` field, or the
name). Index maps (Turbopack) are flattened. Maps without `sourcesContent`
get a warning; maps over 20 MB are skipped.

Rails with importmap (no bundler) serves its JavaScript as written, so
there are no maps to upload: `--sources` makes them.

```sh
bin/rails assets:precompile
tlm upload public/assets --url-prefix /assets --release "$GIT_SHA" --sources
```

With Vite, set the release at build time and keep the maps out of the
bundle, then upload and delete them:

```ts
// vite.config.ts
export default defineConfig({
  define: { __TLM_RELEASE__: JSON.stringify(process.env.GIT_SHA) },
  build: { sourcemap: "hidden" },
});
```

```sh
vite build && tlm upload ./dist --release "$GIT_SHA" --delete
```

```sh
tlm test --dsn "$TLM_DSN"   # sends one test event, the way a server does
```

## Global flags and environment

| Flag | Environment | |
|---|---|---|
| `--url` | `TLM_URL` | Which Tlm. Default: the one last signed in to, else tlm.hullop.com |
| `--org`, `--project` | `TLM_ORG`, `TLM_PROJECT` | Which project |
| | `TLM_TOKEN` | A personal token; always wins over the stored one |
| `--json` | | Tlm's JSON as is |
| `--lang en\|es` | `TLM_LANG` | Language of Tlm's messages |
| `-q`, `--quiet` | | Only errors and the essential result |
| `-v`, `--verbose` | `TLM_DEBUG=1` | Requests and timings on stderr; never tokens |
| `--no-color` | `NO_COLOR` | |

`tlm completion bash|zsh|fish|powershell` prints shell completions.

## Exit codes

`0` done; `1` Tlm refused or failed (not found, not allowed, validation,
network); `2` a usage mistake; `3` not signed in, a rejected or revoked
token, a read-only token asked to change something, or an organization
asking for a new sign-in. `upload` and `test` use only `0`, `1` and `2`.

## Development

```sh
make check          # gofmt, tidy, vet, staticcheck, golangci-lint, tests, govulncheck, cross-builds
make build          # bin/tlm
make fixtures-check # the API fixtures match Tlm's (../tlm)
make fixtures-sync  # copy them over
make update         # latest dependencies and tools, then make check
make release        # tag the next patch version (BUMP=minor|major, or V=1.2.3); CI publishes
```

The tests run against a fake Tlm built from fixtures that Tlm renders
from its real controllers (`test/cli_fixtures` in its repository).
`go test -tags live ./cmd/tlm` drives the binary against a local Tlm (see
`cmd/tlm/live_test.go`).
