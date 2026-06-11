#!/bin/sh
set -eu

cd "$(dirname "$0")"
REPO_DIR=$(pwd -P)
CURRENT_USER=$(id -un)
CONTENT_PUBLISHER_DIR=${CONTENT_PUBLISHER_DIR:-$HOME/Projects/content-publisher}
MARK2NOTE_DIR=${MARK2NOTE_DIR:-$HOME/Projects/mark2note}
APPLY=0
FORCE=0
INCLUDE_SERVICE=0
INCLUDE_POTENTIAL=0

usage() {
  cat <<'EOF'
Usage: ./run-stop.sh [--apply] [--force] [--include-service] [--include-potential]

Safely inspect or stop news-briefing one-shot runs, publish hooks, and attributed downstream commands.
Default mode is a dry run and sends no signals.

Options:
  --apply              Send TERM to matching processes.
  --force              With --apply, send KILL after a short wait to original PIDs still alive.
  --include-service    Include news-briefing serve. By default the supervised service is not stopped.
  --include-potential  Include unattributed mark2note processes shown as potential downstream.
  --help               Show this help.
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --apply) APPLY=1 ;;
    --force) FORCE=1 ;;
    --include-service) INCLUDE_SERVICE=1 ;;
    --include-potential) INCLUDE_POTENTIAL=1 ;;
    --help) usage; exit 0 ;;
    *) printf 'unknown option: %s\n' "$1" >&2; usage >&2; exit 2 ;;
  esac
  shift
done

if [ "$FORCE" -eq 1 ] && [ "$APPLY" -ne 1 ]; then
  printf '%s\n' '--force requires --apply' >&2
  exit 2
fi

MATCHES=$(mktemp "${TMPDIR:-/tmp}/news-briefing-run-stop.XXXXXX")
trap 'rm -f "$MATCHES"' EXIT HUP INT TERM

ps -axo pid=,ppid=,user=,etime=,command= | awk \
  -v current_user="$CURRENT_USER" \
  -v repo="$REPO_DIR" \
  -v mark2note_dir="$MARK2NOTE_DIR" '
function command_from_line(line) {
  sub(/^[[:space:]]*[0-9]+[[:space:]]+[0-9]+[[:space:]]+[^[:space:]]+[[:space:]]+[^[:space:]]+[[:space:]]+/, "", line)
  return line
}
function has_source(cmd) {
  return index(cmd, "--source news-briefing") || index(cmd, "--source=news-briefing")
}
function is_service_cmd(cmd) {
  return index(cmd, repo "/news-briefing serve") || index(cmd, "./news-briefing serve")
}
function is_oneshot_cmd(cmd) {
  return index(cmd, repo "/news-briefing deep") || index(cmd, repo "/news-briefing publish") || index(cmd, "./news-briefing deep") || index(cmd, "./news-briefing publish")
}
function is_hook_cmd(cmd) {
  return has_source(cmd) && index(cmd, "content-publisher") && (index(cmd, "/bin/sh -c") || index(cmd, " sh -c ") || index(cmd, "./content-publisher \"$@\"") || index(cmd, "cd \"$HOME/Projects/content-publisher\""))
}
function is_content_publisher_cmd(cmd) {
  return has_source(cmd) && index(cmd, "content-publisher") && index(cmd, "publish-all") && !is_hook_cmd(cmd)
}
function is_mark2note_cmd(cmd) {
  return index(cmd, mark2note_dir "/mark2note") || index(cmd, "./mark2note")
}
function has_attributed_ancestor(pid, parent, depth) {
  parent = ppid[pid]
  for (depth = 0; parent != "" && depth < 32; depth++) {
    if (hook[parent] || content[parent]) {
      return 1
    }
    parent = ppid[parent]
  }
  return 0
}
function emit(category, pid) {
  printf "%s\t%s\t%s\n", category, pid, cmd[pid]
}
{
  if ($3 != current_user) {
    next
  }
  line = $0
  pid = $1
  ppid[pid] = $2
  cmd[pid] = command_from_line(line)
  if (index(cmd[pid], "run-status.sh") || index(cmd[pid], "run-stop.sh")) {
    next
  }
  order[++n] = pid
}
END {
  for (i = 1; i <= n; i++) {
    pid = order[i]
    if (is_service_cmd(cmd[pid])) {
      type[pid] = "service"
    } else if (is_oneshot_cmd(cmd[pid])) {
      type[pid] = "oneshot"
    } else if (is_hook_cmd(cmd[pid])) {
      type[pid] = "hook"
      hook[pid] = 1
    } else if (is_content_publisher_cmd(cmd[pid])) {
      type[pid] = "content"
      content[pid] = 1
    }
  }
  for (i = 1; i <= n; i++) {
    pid = order[i]
    if (type[pid] == "" && is_mark2note_cmd(cmd[pid])) {
      if (has_attributed_ancestor(pid)) {
        type[pid] = "mark2note"
      } else {
        type[pid] = "potential"
      }
    }
  }
  for (i = 1; i <= n; i++) if (type[order[i]] == "mark2note") emit("mark2note", order[i])
  for (i = 1; i <= n; i++) if (type[order[i]] == "content") emit("content-publisher", order[i])
  for (i = 1; i <= n; i++) if (type[order[i]] == "hook") emit("publish-hook", order[i])
  for (i = 1; i <= n; i++) if (type[order[i]] == "oneshot") emit("one-shot", order[i])
  for (i = 1; i <= n; i++) if (type[order[i]] == "potential") emit("potential-mark2note", order[i])
  for (i = 1; i <= n; i++) if (type[order[i]] == "service") emit("service", order[i])
}' > "$MATCHES"

if [ ! -s "$MATCHES" ]; then
  printf '%s\n' 'No matching news-briefing run/publish/downstream processes found.'
  exit 0
fi

if [ "$APPLY" -ne 1 ]; then
  printf '%s\n' 'Dry run: no processes will be stopped. Re-run with --apply to stop matching processes.'
fi

selected=$(mktemp "${TMPDIR:-/tmp}/news-briefing-run-stop-selected.XXXXXX")
trap 'rm -f "$MATCHES" "$selected"' EXIT HUP INT TERM

while IFS='	' read -r category pid cmd; do
  case "$category" in
    service)
      [ "$INCLUDE_SERVICE" -eq 1 ] || continue
      ;;
    potential-mark2note)
      [ "$INCLUDE_POTENTIAL" -eq 1 ] || continue
      ;;
  esac
  printf '%s\t%s\t%s\n' "$category" "$pid" "$cmd" >> "$selected"
done < "$MATCHES"

if [ ! -s "$selected" ]; then
  printf '%s\n' 'No selected processes after applying safety filters.'
  printf '%s\n' 'Use --include-service for news-briefing serve or --include-potential for unattributed mark2note.'
  exit 0
fi

while IFS='	' read -r category pid cmd; do
  printf '%s pid=%s cmd=%s\n' "$category" "$pid" "$cmd"
  if [ "$APPLY" -eq 1 ]; then
    kill -TERM "$pid" 2>/dev/null || true
  fi
done < "$selected"

if [ "$APPLY" -eq 1 ] && [ "$FORCE" -eq 1 ]; then
  sleep 2
  while IFS='	' read -r category pid cmd; do
    if kill -0 "$pid" 2>/dev/null; then
      printf 'force-kill pid=%s cmd=%s\n' "$pid" "$cmd"
      kill -KILL "$pid" 2>/dev/null || true
    fi
  done < "$selected"
fi
