# Chrome Optimizer

Chrome Optimizer is a macOS/Apple Silicon Go application for inspecting Chrome tabs via the Chrome DevTools Protocol (CDP). **Phase 1 discovers tabs; Phases 2–3 add per-tab CDP metrics, sorting, and a basic live terminal display.** Explicit, experimental CDP freeze/resume is available behind safety checks; OS throttling and the desktop GUI are not implemented.

The canonical development roadmap is [Issues #1–#5](https://github.com/esobczak1970/chrome-optimizer/issues). A future native desktop UI will use **Fyne**, consistent with [Schema Artisan](https://github.com/esobczak1970/schemaartisan); the CLI will remain fully independent of Fyne.

## Quick start on macOS

The normal `make run` command **only attaches** to an existing CDP endpoint.
If Chrome was started normally, port 9222 is usually closed and `make run`
returns `connect: connection refused`. This is an expected setup error, **not**
a Go compilation error.

Use the one-command development workflow instead:

```bash
git pull origin main
make run-debug
```

`make run-debug` safely starts or reuses an **isolated** Chrome debugging
profile, verifies that CDP is listening at `127.0.0.1:9222`, and then runs
the CLI. It does **not** restart, reconfigure, or expose your normal Chrome
profile. Open some pages in the new Chrome window to see them in the listing.
**Tabs already open in your usual Chrome profile will not appear.**

Alternatively, start the isolated profile separately and connect later:

```bash
make chrome-start
curl --noproxy '*' -fsS http://127.0.0.1:9222/json/version
make run
```

Override the debug port or profile when necessary:

```bash
make run-debug CHROME_DEBUG_PORT=9333
make chrome-start CHROME_DEBUG_PROFILE="$HOME/Library/Application Support/ChromeOptimizerDebug2"
```

Use `make test-shell` for mock-based verification of the startup safeguards.
A Chrome process listening on the selected port must identify itself as a CDP
endpoint; otherwise the launcher refuses to start a competing process.

## Requirements

- macOS Apple Silicon, Go **1.27.2**
- Google Chrome installed, running in a **separate debugging profile**
- CDP endpoint bound to **loopback** (default `http://127.0.0.1:9222`)

Chrome 136+ ignores remote-debugging switches when they target the default Chrome user data directory. **Do not attach a debugging port to your normal browsing profile.** Launch a separate debug profile using:

```bash
"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \
  --remote-debugging-address=127.0.0.1 \
  --remote-debugging-port=9222 \
  --user-data-dir="$HOME/Library/Application Support/ChromeOptimizerDebug"
```

The debugging endpoint can control all tabs in that **debugging profile**. Never expose the port to another machine. Chrome may redirect when existing instances are open; verify that `curl http://127.0.0.1:9222/json/version` succeeds.

## Build and run

```bash
make help
make doctor
make deps
make check
make run-debug  # start/reuse isolated Chrome debugging profile
make run        # attach only if Chrome CDP is already listening
# Or:
go run . --config=config.yaml --sort=domain
```

Config values live in `config.yaml` and use strict YAML (unknown keys and multiple YAML documents are rejected). Overrides: `--config`, `--endpoint`, `--sort=domain|title|id|memory|cpu|activity`. All HTTP discovery requests are local read-only GETs. CDP `Target.getTargets` is issued after attaching to an **existing** page; when no page exists, the application returns an empty list rather than opening a new tab.

## Tab metrics, sorting, and watch mode

The default `make run` collects `Performance.getMetrics` from currently
open page targets, using at most four simultaneous target sessions and a
shared Chrome browser connection. A tab that disappears or refuses metrics
appears with `-` readings and its error status, not invented zeros. No
tab is navigated, frozen, or closed: all attachments use
`chromedp.WithDetachOnCancel()`.

```bash
make run
make run ARGS="--sort=memory --desc"
make run ARGS="--sort=cpu --desc"
make run ARGS="--sort=domain"
make run ARGS="--watch --sort=activity --desc"
make run ARGS="--watch --interval=10s --sort=memory --desc"
```

Sort keys: `domain`, `title`, `id`, `memory` (JavaScript heap),
`cpu` (cumulative `TaskDuration`), and `activity` (last **observed
change** in task duration). Use `--desc` for descending; missing values
always sort last. The YAML configuration has `poll_interval: 5s` by default,
with permitted values from 1s to 1m.

**Readings mean precisely:**

- **JS HEAP MiB:** `JSHeapUsedSize` converted from bytes. This is not
  Chrome's resident memory, compressed memory, GPU allocation, or overall
  per-tab RAM.
- **TASK TIME s:** `TaskDuration`, a cumulative execution duration supplied
  by CDP. It is not a CPU usage percentage.
- **LAST TASK ACTIVITY:** A timestamp **first available after at least two
  samples within one running `--watch` invocation** when TaskDuration
  increases. Background scripts can increment it too. This is **not**
  actual last user access, browser focus history, or a historical last-used
  timestamp. `-` means no observed evidence yet.

The implementation does not persist URLs, titles, metrics, or browsing history
to disk. Closing or restarting the CLI resets its in-memory observations.

**Live testing:** On a real macOS Chrome debugging session, open at least
two ordinary web pages, run the metrics/sorting commands above, verify that
the Target IDs persist across consecutive invocations, and confirm that
no tab closes when Chrome Optimizer exits. Check `make check` and
`make test-race` locally before treating the phase as validated.

## Search and compact terminal view

For narrow macOS Terminal windows, use `--compact` to display a shorter ID,
bounded domain/title columns, resource metrics, activity time and status. It
intentionally omits full URLs and query strings from terminal output.

```bash
make run ARGS="--compact --sort=memory --desc"
make run ARGS="--filter=news --compact"
make run ARGS="--watch --filter=example.com --compact --sort=activity --desc"
```

`--filter` performs case-insensitive substring matching against the
TargetID, domain, title and URL. It is in-memory and affects display only:
monitoring still observes all discovered tabs. If nothing matches, the CLI
shows an explicit no-match message rather than suggesting Chrome has no tabs.
The regular output remains available when a full TargetID or URL is needed.

The filtering helper is shared by the CLI and future Fyne view model; no
Fyne runtime is required for headless Chrome monitoring.

## Explicit background-tab freeze and resume (experimental)

**Normal `make run` and `--watch` are read-only.** Freezing requires a
complete TargetID **and** a second confirmation flag. Chrome Optimizer
refuses to freeze the foreground page, internal Chrome pages or a page
whose visibility cannot be verified. Keep at least one separate helper
page open in the isolated debugging profile.

```bash
make run
# Identify the full ID of an ordinary HTTP(S) page that is in the background:
make run ARGS="--freeze=FULL_TARGET_ID --confirm-freeze"
# Later request resumption by that exact ID:
make run ARGS="--resume=FULL_TARGET_ID"
```

This uses CDP `Page.setWebLifecycleState` (experimental) on the selected
page; **it is not a process suspension, memory release, or CPU throttle**.
Acknowledgment of the protocol command does not establish that Chrome
actually persisted the state. Foreground detection has a small race if
a user switches tabs during the request, and pages with active media or
unsaved state should not be frozen. A frozen tab may not respond to
chromedp session initialization during resumption, so Chrome's own tab
selection/reload or a debug-profile restart may be necessary. Do not
use freeze on irreplaceable page state.

Read the full [lifecycle safety and recovery guide](docs/CHROME_LIFECYCLE_SAFETY.md).
Chrome process-level SIGSTOP/SIGCONT remains **intentionally unavailable**
until exclusive renderer mapping and independent recovery are established.

## Metric and safety limitations

CDP `TargetInfo` does **not** provide last-accessed time, so the program does not claim it does. Watch mode records observed task execution (not necessarily user interaction). `JSHeapUsedSize` will be reported as **JavaScript heap**, not process resident memory; `TaskDuration` is cumulative task time, not a CPU percent. A tab may share a Chrome renderer process with other tabs, so operating-system signals must never be sent without exclusive renderer proof and an independent recovery mechanism.

The CLI never sends `SIGSTOP` or `SIGCONT`, navigates, discards, or closes tabs. CDP freeze/resume is available only through the explicit commands documented above.

## Validation

The unit tests cover strict YAML, URL/domain parsing, local endpoint protections, read-only empty discovery, unsafe browser WebSocket responses, and terminal sorting. A live CDP smoke test needs a local Chrome instance on macOS; Linux CI alone cannot certify macOS runtime behavior.
