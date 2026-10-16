#!/bin/bash
# Dependency-free tests for the macOS launch shell workflow using command mocks.
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/bin" "$tmp/home" "$tmp/Google Chrome.app"

cat > "$tmp/bin/uname" <<'MOCK'
#!/bin/bash
echo Darwin
MOCK
cat > "$tmp/bin/curl" <<'MOCK'
#!/bin/bash
if [[ -f "$MOCK_RUNNING" ]]; then
  printf '{"webSocketDebuggerUrl":"ws://127.0.0.1:9222/devtools/browser/test"}\n'
  exit 0
fi
exit 7
MOCK
cat > "$tmp/bin/lsof" <<'MOCK'
#!/bin/bash
[[ -f "$MOCK_OCCUPIED" ]]
MOCK
cat > "$tmp/bin/open" <<'MOCK'
#!/bin/bash
printf '%s\n' "$@" > "$MOCK_ARGS"
touch "$MOCK_RUNNING"
MOCK
cat > "$tmp/bin/sleep" <<'MOCK'
#!/bin/bash
exit 0
MOCK
chmod +x "$tmp/bin"/*
export PATH="$tmp/bin:$PATH"
export HOME="$tmp/home"
export CHROME_APP="$tmp/Google Chrome.app"
export CHROME_DEBUG_PROFILE="$tmp/home/Library/Application Support/ChromeOptimizerDebug"
export MOCK_RUNNING="$tmp/running" MOCK_OCCUPIED="$tmp/occupied" MOCK_ARGS="$tmp/open-args"

bash "$script_dir/chrome-debug.sh" > "$tmp/output"
grep -q 'Chrome CDP ready' "$tmp/output"
grep -Fxq -- '--remote-debugging-address=127.0.0.1' "$MOCK_ARGS"
grep -Fxq -- '--remote-debugging-port=9222' "$MOCK_ARGS"
grep -Fxq -- "--user-data-dir=$CHROME_DEBUG_PROFILE" "$MOCK_ARGS"

rm "$MOCK_ARGS"
bash "$script_dir/chrome-debug.sh" > "$tmp/output"
grep -q 'already available' "$tmp/output"
[[ ! -e "$MOCK_ARGS" ]] || { echo 'unexpected relaunch' >&2; exit 1; }

rm "$MOCK_RUNNING"
touch "$MOCK_OCCUPIED"
if bash "$script_dir/chrome-debug.sh" > "$tmp/output" 2>&1; then
  echo 'expected failure when CDP port belongs to another service' >&2
  exit 1
fi
grep -q 'already occupied' "$tmp/output"
[[ ! -e "$MOCK_ARGS" ]] || { echo 'unexpected launch on occupied port' >&2; exit 1; }

rm "$MOCK_OCCUPIED"
export CHROME_DEBUG_PROFILE="$HOME/Library/Application Support/Google/Chrome"
if bash "$script_dir/chrome-debug.sh" > "$tmp/output" 2>&1; then
  echo 'expected refusal of default Chrome profile' >&2
  exit 1
fi
grep -q 'Refusing' "$tmp/output"

export CHROME_DEBUG_PROFILE="$tmp/debug"
export CHROME_DEBUG_PORT='65536'
if bash "$script_dir/chrome-debug.sh" > "$tmp/output" 2>&1; then
  echo 'expected refusal of invalid port' >&2
  exit 1
fi
grep -q '65535' "$tmp/output"

printf 'chrome-debug.sh tests passed\n'
