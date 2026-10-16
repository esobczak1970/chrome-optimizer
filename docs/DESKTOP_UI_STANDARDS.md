# Native desktop UI standard

## Decision

Use **Fyne v2** for Chrome Optimizer's optional native Go desktop interface, consistent with the established framework in `esobczak1970/schemaartisan`. Preserve a headless CLI and shared Go domain layer, so macOS process operations and CDP sessions can be exercised/tested without initializing any GUI toolkit.

CIP's Bootstrap 5.3.8 standard belongs to its **HTML/web UI** and is not a native desktop widget toolkit. Reuse its design principles (contrast, system light/dark appearance, keyboard accessibility, single source of truth, useful empty states, regression tests), not CSS dependencies.

## Desktop implementation requirements

- Keep native UI code behind a dedicated command/package and out of core/browser/manager/CLI imports.
- Use the current supported Fyne v2 release at implementation time, with pinned `go.mod` and documented packaging on macOS arm64.
- Respect system theme and keyboard navigation; no color-only status; control tab titles/URLs as untrusted text.
- All CDP operations require bounded contexts and cancellable workers. Never block the Fyne event thread.
- Explicit confirmation and clear safety explanations before changing browser lifecycle state; OS process signals remain inaccessible until proven safe in Issue #4.
- YAML remains the only application configuration; no persisted browsing history without opt-in.
- Validate live on macOS for window resize, dark/light variants, Chrome disconnect, and shutdown. Do not call mock tests equivalent to native integration tests.

## Status

Architecture decision documented during Issue #1. The Fyne GUI itself is scoped to Issue #5; no GUI dependencies are introduced in Phase 1.
