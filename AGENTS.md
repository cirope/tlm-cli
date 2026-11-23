# Working in this repository

`tlm` is the command-line client for Tlm ("Hullop Telemetry", `../tlm`),
in Go. This file is the short list of rules to follow while working on
it.

## The product, in one paragraph

A developer, their coding agent or CI manages Tlm from the terminal.
`tlm login` signs in by browser approval. `projects`, `upload-tokens` and
`origins` set a project up. `issues` reads what broke and settles it,
`speed` and `signals` show how fast pages are and who reports. `upload`
sends source maps, `test` one test event. `orgs`, `members` and `audit`
cover the organization. Output is human-readable by default, `--json`
for agents and `--markdown` for an AI, with stable exit codes.

## Rules

### Process

- **Tests first.** Write the failing test and watch it fail for the
  right reason, then write the code.
- **Run the whole gate** (`make check`) before saying something is done.
  Report what ran and what it printed.
- **Try it for real.** The final check of a command is the built binary
  against a local Tlm (`http://localhost:4000`). Say plainly what was and
  wasn't tried.
- **Ask before deciding the product.** Ask questions with options and a
  recommended default; don't invent new product behaviour.

### Franco's working rules

- **Never commit or run git-modifying commands** (`add`, `commit`,
  `branch`, `tag`, `push`) unless asked. End every change with a
  suggested commit subject of 50 characters or less, and the `git add`
  command for Franco to run.
- **Never name or point at other codebases**: no other product's name,
  path or domain in code, comments, docs, tests or fixtures.
- **Latest dependencies.** Add each at its latest version.
- **CHANGELOG under `## Unreleased`.** Versions are bumped only when
  Franco cuts a release (`make release`). Give him the commands; never
  publish yourself.
- **Always say whether to release.** At the end of every change, say
  whether it's worth releasing now and give the exact command
  (`make release`, `BUMP=minor`/`major` or `V=x.y.z`), with why.

### Work tracking

- The work is tracked with the `trk` CLI (already signed in), on the board
  **"Telemetría"**. It's a tool we use, not a code reference: never name it
  in code, tests or docs
- Everything written there is in neutral Spanish (tú, no regionalisms) and
  without Spanglish: "aplicación", never "app"; "versión", not "release";
  "tarea", not "job"; "credencial", not "token". Only names stay as they
  are (products, code, commands, environment variables), plus "framework",
  "source maps" and "stack trace"
- Every card is created with the agent off, so its agent never duplicates
  our work: `trk card new --board Telemetría "<título>" --take --no-pull`
  (take turns the agent off and moves it to In progress); for work not
  started yet, then `trk card move <id> todo`
- Built but not published stays In progress; once it's out, move each card
  where it belongs (`trk card move`, `trk card done --yes`)
- Infrastructure lives in `../infra`; Franco handles it

### Go

- The standard library first.
- Tools run through the `tool` directive (`tools/go.mod`).
- Everything goes under `internal/`; `cmd/tlm/main.go` only wires and
  exits.
- `context.Context` comes first, and every request has a timeout.
- Wrap errors and inspect them with `errors.Is`/`As`.
- No package-level state, and no `os.Exit` outside `main`.
- The gate is `gofmt`, `go vet`, `staticcheck` and `golangci-lint`.

### Safety

- **A token never appears** in output, logs, errors, `--verbose`, test
  fixtures or commits. The token type redacts itself.
- **`TLM_UPLOAD_TOKEN` is read only from the environment.**
- **Text from the server is untrusted.** Neutralize control characters
  before printing it to a terminal (`render.Safe`).
- Tests never touch the developer's real config, keychain or network.
- Never point tests or experiments at `tlm.hullop.com`. Use a local Tlm.

### Output

- Results go to stdout; everything else goes to stderr.
- `--json` is Tlm's JSON as is, a public contract.
- Exit codes: 0 done, 1 Tlm refused or failed, 2 usage, 3 the token
  can't be used (not signed in, revoked, read-only, `sign_in_again`).
  `upload` and `test` use only 0, 1 and 2. They are tested.
- Every command's `--help` has an example.

### The server

- `../tlm/docs/api.md` (the management API) and `../tlm/docs/protocol.md`
  (events and source map uploads) are the binding contract.
- The fake Tlm in `internal/api/apitest` serves `../tlm`'s
  `test/cli_fixtures`, rendered by its real controllers. Copy them with
  `make fixtures-sync`; `make fixtures-check` says when they drifted.
- A missing endpoint is built in `../tlm`, by its `AGENTS.md`. Don't work
  around it from the CLI: agree its shape, have `docs/api.md` and the
  fixtures updated, then write the command.
