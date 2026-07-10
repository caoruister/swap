#!/bin/bash
set -euo pipefail
# dev.sh — start swap TUI with auto-reload on file changes
# Uses fswatch on macOS for file watching (brew install fswatch)
# Falls back to simple polling with find if fswatch is unavailable

echo $$ > /tmp/swap_dev.pid

SWAP_PID=""

cleanup() {
    echo "Cleaning up..."
    trap - SIGINT SIGTERM SIGQUIT
    if [[ -n "${SWAP_PID:-}" ]]; then
        kill "${SWAP_PID}" 2>/dev/null || true
    fi
    rm -f /tmp/swap_dev.pid
    reset 2>/dev/null || true
    exit 0
}

trap cleanup SIGINT SIGTERM SIGQUIT

start_swap() {
    export TERM=xterm-256color
    echo "starting swap (debug mode, API=http://127.0.0.1:8081)..."
    CGO_ENABLED=0 go run -ldflags="-X main.APIURL=http://127.0.0.1:8081" ./cmd/swap --debug &
    SWAP_PID=$!
    echo "swap PID: $SWAP_PID"
    wait "$SWAP_PID"
    echo "swap process exited (exit code: $?)"
}

# Check for fswatch (macOS) or inotifywait (Linux)
FILE_WATCHER=""
if command -v fswatch &>/dev/null; then
    FILE_WATCHER="fswatch"
elif command -v inotifywait &>/dev/null; then
    FILE_WATCHER="inotifywait"
fi

if [[ -n "$FILE_WATCHER" ]]; then
    echo "using file watcher: $FILE_WATCHER"
    # Start the file watcher that kills swap on changes
    (
        if [[ "$FILE_WATCHER" == "fswatch" ]]; then
            fswatch -o -e ".*" -i "\.go$" ./app | while read -r _; do
                echo "File change detected, restarting swap..."
                if [[ -n "${SWAP_PID:-}" ]]; then
                    kill "$SWAP_PID" 2>/dev/null || true
                fi
            done
        else
            # inotifywait (Linux)
            while true; do
                inotifywait -q -r -e modify ./app --include '\.go$'
                echo "File change detected, restarting swap..."
                if [[ -n "${SWAP_PID:-}" ]]; then
                    kill "$SWAP_PID" 2>/dev/null || true
                fi
            done
        fi
    ) &
    WATCHER_PID=$!
    echo "watcher PID: $WATCHER_PID"
else
    echo "warning: no file watcher found (install fswatch for auto-reload)"
    echo "  macOS: brew install fswatch"
    echo "  Linux: apt install inotify-tools"
fi

# Start the app
start_swap

# After swap exits naturally
if [[ -n "${WATCHER_PID:-}" ]]; then
    kill "$WATCHER_PID" 2>/dev/null || true
    wait "$WATCHER_PID" 2>/dev/null || true
fi
echo "dev.sh exiting normally"
cleanup