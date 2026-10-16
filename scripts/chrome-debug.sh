#!/bin/bash
# Start only an explicitly isolated Chrome debugging profile. Never restart or
# reconfigure the user's regular Chrome browser/profile.
set -euo pipefail

fail() {
  printf 'chrome-optimizer: %s\n' "$*" >&2
  exit 1
}

[[ "$(uname -s)" == "Darwin" ]] || fail 'chrome-start is supported on macOS only.'

port="${CHROME_DEBUG_PORT:-9222}"
[[ "$port" =~ ^[1-9][0-9]{0,4}$ ]] || fail 'CHROME_DEBUG_PORT must be a number from 1 to 65535.'
(( port <= 65535 )) || fail 'CHROME_DEBUG_PORT must be at most 65535.'

app="${CHROME_APP:-/Applications/Google Chrome.app}"
[[ -d "$app" ]] || fail "Chrome application not found: $app"

profile="${CHROME_DEBUG_PROFILE:-$HOME/Library/Application Support/ChromeOptimizerDebug}"
[[ "$profile" == /* ]] || fail 'CHROME_DEBUG_PROFILE must be an absolute path.'
mkdir -p "$profile" || fail "Cannot create debugging profile: $profile"
profile="$(cd "$profile" && pwd -P)"

# Chrome 136+ ignores the debugging port when pointed at its default profile.
# Also reject nested paths to avoid inadvertently touching an ordinary profile.
normal="$HOME/Library/Application Support/Google/Chrome"
if [[ -d "$normal" ]]; then
  normal="$(cd "$normal" && pwd -P)"
fi
case "$profile" in
  "$normal"|"$normal"/*)
    fail 'Refusing to enable debugging for the normal Chrome profile. Use a separate CHROME_DEBUG_PROFILE.'
    ;;
esac

endpoint="http://127.0.0.1:$port"
ready() {
  local response
  response="$(curl --noproxy '*' -fsS --connect-timeout 1 --max-time 2 "$endpoint/json/version" 2>/dev/null)" &&
    [[ "$response" == *'"webSocketDebuggerUrl"'* ]]
}

if ready; then
  printf 'Chrome CDP is already available at %s (using existing session).\n' "$endpoint"
  exit 0
fi

# Do not launch a competing browser if another service already owns the port.
if lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; then
  fail "Port $port is already occupied, but the Chrome CDP endpoint did not respond correctly."
fi

printf 'Starting isolated Chrome debugging profile: %s\n' "$profile"
open -na "$app" --args \
  --remote-debugging-address=127.0.0.1 \
  "--remote-debugging-port=$port" \
  "--user-data-dir=$profile" \
  --no-first-run \
  --no-default-browser-check \
  about:blank || fail 'Could not launch Chrome.'

for (( attempt = 0; attempt < 20; attempt++ )); do
  if ready; then
    printf 'Chrome CDP ready at %s. Tabs in your regular Chrome profile are separate.\n' "$endpoint"
    exit 0
  fi
  sleep 0.5
done

fail "Chrome did not start its CDP endpoint at $endpoint. Check the Chrome window, profile lock, or port conflict."
