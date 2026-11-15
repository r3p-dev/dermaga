# Code Conventions

These conventions define how code is written and maintained in this project. The goal is a codebase
that is simple, readable, consistent and easy to maintain.

**The guiding principle: write code that is easy to understand without requiring additional
explanations.**

## 0. Project rules

1. **No unexplained workarounds.** A workaround has a short comment saying _why_, what breaks without
   it, and a link, plus an entry in the decisions log of `docs/ARCHITECTURE.md`. A workaround without
   an explanation is a bug.
2. **Find the cause, not the symptom.** When something breaks, find out why before changing code, and
   explain the cause in the report.
3. **Explicit state machines.** Deployment status (`DEP-9`), app runtime status (`RUN-1`), setup mode
   (`SETUP-1`) and the update flow (`UPD-2`, `UPD-3`) each change in exactly one place, with every
   transition tested.
4. **Outside services can fail** (`NFR-4`). GitHub and Let's Encrypt each degrade one feature with a
   clear message. Tests swap each one for a fake; a test never
   calls a real outside service.
5. **The host is a boundary.** Every host command and file write goes through `internal/host`, so tests
   use a fake host. No shell, ever (`SEC-16`).
6. **Small, focused changes.** One concern at a time. No drive-by refactors.
7. **No deprecated code**, ever (§11).

## 1. General principles

- Prefer simplicity over complexity, and readability over cleverness.
- Write code that explains itself through its structure and naming.
- Avoid unnecessary abstractions, indirection and premature optimization. An interface exists only
  where a test needs a fake (the host, outside services, the clock).
- Use the standard library and existing language features instead of reinventing them.
- Keep each package and function focused on a single responsibility.
- Do not add code for hypothetical future requirements.

## 2. Naming

- Use the glossary of `docs/PRD/summary.md` §5 in code, UI and docs: GitHub App, app, release,
  deployment, live release, route, hook domain.
- Go: package names short, lowercase, singular, no `util`/`common`/`helpers`; exported names don't
  repeat the package (`deploy.Queue`, not `deploy.DeployQueue`); errors `errSomething` /
  `ErrSomething`; receivers short and consistent.
- The project name and every derived prefix (`dg-`, `app-`) come from `internal/name`. Never hard-code
  them elsewhere.
- UI: components `PascalCase.svelte`, routes and other files `kebab-case`.

## 3. Files and directories

- `cmd/dermaga` (entry point), `internal/<package>` (everything else), `ui/` (the SvelteKit app),
  `install/` (installer, its tests, the user unit), `test/vm/` (VM scenarios), `docs/`.
- One package per concern; tests next to the code (`x_test.go`), golden files in `testdata/`.
- Generated files (tygo output, golden files) are committed and checked for freshness by `make check`.

## 4. Go

- The Go version and tools are pinned in `go.mod` (`go` line and `tool` directives).
- **Host commands**: `exec.CommandContext` with an argument slice and a timeout, through
  `internal/host`. Values are never formatted into a shell string, also not inside a container (pass
  them as arguments or environment).
- **Validation at the boundary**: names through the shared validator, every app spec through its
  `Validate` before anything is rendered. The server is the authority; the UI validates only for
  convenience.
- **Generated files**: typed structs → serializer (JSON for Traefik, one strict INI writer for
  Quadlet/systemd). Atomic writes: temp file in the same directory, `fsync`, `rename`,
  `fsync` the directory.
- **Secrets**: sealed with the secrets box and their column name; never logged; masked in build logs.
- **API**: every request and response is a Go struct; the UI's types are generated with tygo. Never
  hand-write a type the UI reads from the server.
- **Concurrency**: a per-app mutex for mutating actions; contexts carry deadlines; the daemon owns
  every deadline and never blocks on `systemctl start`.

## 5. UI (`ui/`)

- Svelte 5 runes only; no legacy component syntax.
- No heavy component framework; small local components in `ui/src/lib/components`.
- All server calls go through one API client using the generated types.
- Show only what the server returned (`UI-2`); loading, empty and error states for every view
  (`UI-3`).
- Live streams use SSE.

### 5.1 Styling

- Tailwind classes in the markup; no custom CSS files beyond the theme entry point.
- Colors, spacing and type only from the theme's tokens; new tokens need my approval.
- Dark and light themes; visible focus; AA contrast (`NFR-7`).
- Visual design (layout, components, copy, the logo): ask me (`AGENTS.md` §1).

### 5.2 Desktop and narrow screens

- The panel is designed for a laptop and stays usable on a narrow screen (`UI-8`): tables become
  stacked rows, primary actions stay reachable without horizontal scrolling.

## 6. Installer (`install/install.sh`)

- Bash, `set -Eeuo pipefail`, idempotent, Debian 13 only.
- Prompts read from `/dev/tty`; every prompt has a default; never ask what has a safe default.
- Validate a system file before reloading it (`sshd -t`); never restart nftables; keep the old access
  path until the new one is proven (`LOCK-1`–`LOCK-5`).
- Small functions with one job each, named for what they do.

## 7. Tests

- Every acceptance criterion that can be automated has a test, named by requirement ID where one
  applies.
- **Golden tests** for every generator; `go test ./... -update` refreshes them; review the diff.
- **Fake-host tests** for the deploy state machine, reconciliation and anything that runs commands.
- **API tests** for every route: happy path, authentication, and the PRD rule it implements.
- **Injection tests**: newline, `%`, `$`, backtick and quotes in every user-controlled field are
  rejected or escaped.
- **Browser tests** for the main panel journeys (tool chosen in M1, listed in `docs/DEPENDENCIES.md`).
- Tests that need Podman or systemd run on the VM (`test/vm/`), and in CI where possible.

## 8. Comments and documentation

Comments are optional. Write one only when it gives information the code can't.

- Do not repeat what the code says. Prefer better names over explanatory comments.
- Explain **why**, not what. Keep comments short. Remove outdated ones.
- **No file headers.** Go package comments (`// Package x …`, one short paragraph on the package's
  job) are the exception.
- Comments never carry status or plans ("added in M4", "TODO later").
- Comments **are** appropriate for non-obvious rules, important decisions (cite `D<n>` or the
  requirement ID), unexpected host or tool behavior, workarounds (with a link) and security
  considerations.

## 9. Error handling

- Handle errors at the boundary that has enough context to decide; never swallow them silently.
- Wrap with context (`fmt.Errorf("starting the candidate: %w", err)`): lowercase, no trailing period.
- Do not use panics for ordinary control flow.
- User-facing errors say what happened and what to do next (`NFR-6`). Never put a secret or token in
  an error.
- A failed side action that must not break the main one (cleanup after a deploy) is logged with context and
  the action continues.

## 10. Formatting and linting

- Go: `gofmt`, `go vet`, staticcheck (which reports deprecated APIs), govulncheck.
- UI: Prettier (`format:check`), `svelte-check`, and a lint rule that reports deprecated APIs.
- All part of `make check`. Do not hand-format against the tools.

## 11. No deprecated code

- Check the current official documentation before using an API. If it is marked deprecated (in the
  docs, in its types, or by a runtime warning), use its replacement.
- Deprecation warnings in the build, the tests or the browser console are failures to fix, not noise.

## 12. Dependencies and tooling

- Prefer the standard library and platform capabilities when they are sufficient.
- No dependency for trivial functionality. Every new one is justified in `docs/DEPENDENCIES.md`,
  stable and actively maintained.
- Keep the toolchain small: a Go toolchain and Bun are all `make check` needs.

## 13. Language

UI, server command output, user-facing errors, code, comments, docs and commit messages are English. Plain words,
short sentences; never blame the user.

## 14. Instructions for AI agents

When creating or modifying code:

- Read the existing implementation and these conventions before changing anything.
- Prefer modifying existing code over introducing new abstractions.
- Use descriptive, consistent naming from the glossary.
- Do not add comments or file headers that the code doesn't need.
- Do not over-engineer straightforward requirements, and avoid unrelated changes.
- Preserve existing behavior unless explicitly instructed otherwise.
- When several solutions are valid, choose the simplest one consistent with the existing codebase.
- Do not introduce patterns, abstractions or dependencies just because they are common in other
  projects.
