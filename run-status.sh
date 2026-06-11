#!/bin/sh
set -eu

cd "$(dirname "$0")"
REPO_DIR=$(pwd -P)
CURRENT_USER=$(id -un)
CONTENT_PUBLISHER_DIR=${CONTENT_PUBLISHER_DIR:-$HOME/Projects/content-publisher}
MARK2NOTE_DIR=${MARK2NOTE_DIR:-$HOME/Projects/mark2note}

ps -axo pid=,ppid=,user=,etime=,command= | awk \
  -v current_user="$CURRENT_USER" \
  -v repo="$REPO_DIR" \
  -v content_publisher_dir="$CONTENT_PUBLISHER_DIR" \
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
function print_proc(pid) {
  printf "  pid=%s ppid=%s etime=%s cmd=%s\n", pid, ppid[pid], etime[pid], cmd[pid]
}
function print_group(label, category, i, pid, count) {
  count = 0
  for (i = 1; i <= n; i++) {
    pid = order[i]
    if (type[pid] == category) {
      if (count == 0) {
        printf "\n%s:\n", label
      }
      print_proc(pid)
      count++
      found = 1
    }
  }
}
{
  if ($3 != current_user) {
    next
  }
  line = $0
  pid = $1
  ppid[pid] = $2
  etime[pid] = $4
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
  print_group("news-briefing service", "service")
  print_group("news-briefing one-shot", "oneshot")
  print_group("news-briefing publish hook shell", "hook")
  print_group("content-publisher downstream", "content")
  print_group("mark2note downstream", "mark2note")
  print_group("potential mark2note downstream (not stopped by default)", "potential")
  if (!found) {
    print "No matching news-briefing run/publish/downstream processes found."
  }
}'
