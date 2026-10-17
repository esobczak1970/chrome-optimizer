# Chrome tab lifecycle safety

Chrome Optimizer provides two **explicit, opt-in** CDP page-lifecycle requests.
It does **not** currently offer process-level `SIGSTOP`/`SIGCONT`,
automatic tab suspension, eviction, or memory-pressure intervention.

## Capability matrix

| Action | Availability | Requirement | Important limitation |
| --- | --- | --- | --- |
| List and sample tabs | Default | Existing loopback CDP session | JS heap is not RSS; task duration is not CPU% |
| Freeze one page via CDP | Explicit opt-in | Exact TargetID, `--confirm-freeze`, ordinary HTTP(S) page confirmed hidden, another open helper page | Experimental API; can affect timers, audio or connections and does not imply RAM is freed |
| Resume one page via CDP | Explicit | Exact TargetID, another open helper page | Experimental API can reject the request; acknowledgment is not a verified state query |
| Suspend a macOS renderer with `SIGSTOP` | **Unavailable** | Would require exclusive TargetID-to-process ownership proof, PID identity checks and independent recovery | Chrome renderer processes can be shared; no reliable proof implemented |
| Set a CPU throttle percentage | **Unavailable** | No safe per-tab OS CPU quota established | `SIGSTOP` would be a full process stop, not proportional throttling |

## Before freezing

Use an **isolated Chrome debug profile** (`make run-debug`) with at least two
page tabs, including one that stays **unfrozen** as a helper/recovery page.

```bash
make run
# Copy the FULL TARGET ID for a background http(s) page from that listing
make run ARGS="--freeze=FULL_TARGET_ID --confirm-freeze"
```

The command checks that the **currently inspected page** has
`document.visibilityState === "hidden"` and the live page protocol is
`http:` or `https:`. It rejects foreground, unknown-visibility, internal,
extension, file and blank pages rather than guessing. Freezing is **not
atomic** with tab selection: changing tabs during the request can change
visibility between the check and command. Do not use it when tab switching
or with tabs whose audio, uploads, forms or connections you cannot interrupt.

No background idle heuristic automatically freezes pages. Freeze does not
guarantee reclamation of JavaScript heap, OS memory, GPU memory, or network
resources. Some pages may reload or have interrupted background tasks.

## Resume and recovery

```bash
make run ARGS="--resume=FULL_TARGET_ID"
```

Resuming does **not** require the page to execute JavaScript to pass our
foreground safety gate, because frozen pages may be unable to run scripts.
However, the current chromedp attachment process can itself need a responsive
target during initialization. This may cause **resume to fail or time out**
on some frozen pages or Chrome versions. In that case, select the frozen
tab directly in Chrome, reload it, or restart the isolated debugging profile.
These manual options may lose unsaved page state.

Keep another open page in the debugging profile so Chrome Optimizer can
establish its browser CDP connection without bootstrapping through the
selected frozen page. A missing helper causes an explicit refusal rather
than creating, navigating, or closing a tab automatically.

The CLI prints that Chrome **acknowledged the request**, not that Chrome
independently verified and persisted a lifecycle state. This is an
experimental CDP protocol command. Future integration testing should
establish the round-trip behavior for the Chrome versions in use before any
GUI promises one-click reversible suspension.

## Unavailable process signals

Chrome's `SystemInfo.getProcessInfo` gives a process list; it does not
prove which renderer is exclusively owned by a TargetID. Site isolation,
cross-origin frames, and process sharing complicate the mapping. Stopping a
shared renderer can suspend unrelated pages. `SIGSTOP` also cannot be
cleaned up by an in-process deferred function after SIGKILL or power loss.

Until exclusive ownership, PID reuse safeguards and **out-of-process**
resume/watchdog recovery are independently validated, **no macOS process
signal path is exposed**, even behind an override flag.

## Manual validation on macOS

1. Run `make check` and `make test-race`.
2. Keep two test pages open in the isolated debug profile; run `make run`.
3. Freeze only a deliberately disposable background HTTP(S) test page and
   confirm another page stays interactive.
4. Verify an attempted freeze of a foreground tab fails.
5. Resume by exact ID; repeat with different tabs, closed pages and Chrome
   restarting. Verify failures are clear and existing tabs remain open.
6. Check `make run` again for stable TargetIDs and no unexpected closures.

The container development environment cannot substitute for those real
Chrome/macOS checks. All of this feature remains **unverified for real
freeze/resume behavior** until the smoke test is run.
