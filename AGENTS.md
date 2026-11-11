# Agent development guidance

Read the repository README and existing Go module/toolchain requirements before modifying code. Keep runtime safety, tests, security boundaries and existing functionality intact.

## Best-effort Go testing when the pinned toolchain is unavailable

- The exact version in `.go-version` (and the module's `go.mod` directive)
  remains authoritative. In a full development, Codex, or CI environment where
  that version is available, use it and the repository's normal Make/test
  checks. Only pinned-toolchain validation can satisfy release or merge gates
  that require it.
- In a constrained agent or other environment **without** the pinned Go
  version, do not abandon testing. Use the newest Go version actually installed
  to run the most relevant executable checks it supports. Record its full `go
  version` output. Prefer existing focused package tests, then `go test -race`,
  `go vet`, and formatting/static checks when supported.
- When an older Go cannot load the module because its `go.mod` requires a newer
  release, use a temporary, isolated module to test **unaltered relevant source
  and tests** with minimal compatible dependencies if practical. Explicitly
  identify the isolation and coverage limits; do not pretend that a
  scratch-module result proves the full repository builds.
- Never edit `.go-version`, `go.mod`, repository checks, tests, or security
  rules just to make fallback validation pass. Do not replace required
  databases, integrations, or test fixtures with weaker substitutes. Preserve
  failures and identify tests that could not run.
- Report results separately: **fallback Go version and exact tests
  passed/failed** versus **pinned Go version and checks still unverified**.
  Fallback evidence is useful for progress but does not replace the pinned
  `make check`, CI, full integration suite, or release gate. Follow up with
  pinned verification when a complete environment is available.
