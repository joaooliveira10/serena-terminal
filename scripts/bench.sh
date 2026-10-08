#!/usr/bin/env bash
# Budgets of requirement R42 for init.bash and init.zsh, measured on this
# machine, and for init.ps1 where PowerShell 7 is installed (outside Windows;
# on Windows scripts/bench.ps1 measures PowerShell and calls this file).
#
#   bash scripts/bench.sh [--init DIR] [--runs N] [--calls N] [--ci]
#
# For each of bash and zsh it measures
#   1. what sourcing the init file costs at the start of an interactive
#      shell: the time around the sourcing, taken inside a fresh shell; the
#      median of such shells. Printed below it and not judged: the median
#      time of whole shells with and without the file. The difference of
#      those two cannot decide a budget: starting a shell takes many times as
#      long as the file and varies by more than the file costs;
#   2. what the hook costs before one prompt with the follow check active
#      (inside a repository, signals read from a stand-in registry): the mean
#      of the given number of calls, made with PATH emptied, which they could
#      not survive if they started a program.
# Budgets: 60 ms and 10 ms. The exit code is 1 when one is not met.
#
# --init DIR  a folder with rendered init files (init.bash, init.zsh,
#             init.ps1); default: render them from the sources with
#             `go run ./internal/tools/golden`
# --ci        hand the looser gates of CI to scripts/bench.ps1
# --where     print the folder of the repository this file belongs to, and stop
#
# Everything runs from a temporary folder that is removed at the end. The
# shells get an empty environment with that folder as HOME and no history
# file. It works with bash 3.2: times come from the `time` keyword.

set -u

# The folder this file is in. On Windows the file may be named with
# backslashes, as scripts/bench.ps1 could name it, and then %/* would cut
# nothing off.
here=${BASH_SOURCE[0]}
case ${OSTYPE-} in msys*|cygwin*) here=${here//\\//} ;; esac
case $here in */*) here=${here%/*} ;; *) here=. ;; esac
repo=$(cd "$here/.." && pwd) || exit 2
rendered=
runs=9
calls=200
ci=
while [ $# -gt 0 ]; do
    case $1 in
        --init) rendered=$2; shift 2 ;;
        --runs) runs=$2; shift 2 ;;
        --calls) calls=$2; shift 2 ;;
        --ci) ci=1; shift ;;
        --where) printf '%s\n' "$repo"; exit 0 ;;
        *) printf 'bench.sh: unknown argument %s\n' "$1" >&2; exit 2 ;;
    esac
done
[ -n "${CI-}" ] && ci=1

tmp=$(mktemp -d "${TMPDIR:-/tmp}/sereno-bench-XXXXXX") || exit 1
tmp=$(cd "$tmp" && pwd -P)
trap 'rm -rf "$tmp"' EXIT
home=$tmp/home
root=$home/.local/share/sereno
empty=$tmp/empty-path
mkdir -p "$home/work/.git" "$root/shell" "$empty"
printf 'ref: refs/heads/main\n' > "$home/work/.git/HEAD"
printf 'mode=auto\nflavor=soft\ninband=profile\nkeys=profile\nprompt=on\n' > "$root/mode"

if [ -z "$rendered" ]; then
    rendered=$tmp/rendered
    if ! (cd "$repo" && SERENO_SANDBOX=$tmp go run ./internal/tools/golden --out "$rendered" > "$tmp/render.log" 2>&1); then
        cat "$tmp/render.log" >&2
        exit 1
    fi
    rendered=$rendered/init
fi
# The stand-in registry is copied next to everything else, so that the
# measurement does not depend on the disk the repository is on.
registry=$tmp/registry
cp -R "$repo/testdata/registry/1" "$registry" || exit 1
init_budget=60
prompt_budget=10
failed=0

# median FILE: the middle of the numbers in FILE, one per line.
median() { sort -n "$1" | awk '{ v[NR] = $1 } END { if (NR % 2) print v[(NR + 1) / 2]; else print (v[NR / 2] + v[NR / 2 + 1]) / 2 }'; }
# over VALUE BUDGET: true when VALUE is above BUDGET.
over() { awk -v a="$1" -v b="$2" 'BEGIN { exit !(a > b) }'; }

# measure NAME SHELL FLAGS... : FLAGS start the shell without startup files
# and interactive; the last argument of a call is the script to run.
measure() {
    local name=$1 shell=$2 init hook i inside with without added per total
    shift 2
    init=$root/shell/init.$name
    cp "$rendered/init.$name" "$init" || exit 1
    hook=__sereno_prompt
    [ "$name" = zsh ] && hook=__sereno_precmd
    printf ':\n' > "$tmp/without.$name"
    printf '. "%s"\n' "$init" > "$tmp/with.$name"
    # The sourcing, timed inside the shell; the seconds go to standard error.
    if [ "$name" = zsh ]; then
        printf 'typeset -F SECONDS\nstart=$SECONDS\n. "%s"\nprintf "%%.3f\\n" $(( SECONDS - start )) >&2\n' "$init" > "$tmp/timed.$name"
    else
        printf 'TIMEFORMAT=%%3R\ntime { . "%s"; }\n' "$init" > "$tmp/timed.$name"
    fi
    # The hook, timed inside the shell; the seconds go to standard error.
    # The first call paints and is left out. bash 3.2 has no clock finer
    # than a second except the one behind `time`; zsh has SECONDS.
    if [ "$name" = zsh ]; then
        cat > "$tmp/prompt.$name" <<EOF
. "$init"
cd "$home/work"
$hook > /dev/null
typeset -F SECONDS
start=\$SECONDS
i=0
while [ \$i -lt $calls ]; do $hook; i=\$((i + 1)); done > /dev/null
printf '%.3f\n' \$(( SECONDS - start )) >&2
EOF
    else
        cat > "$tmp/prompt.$name" <<EOF
. "$init"
cd "$home/work"
$hook > /dev/null
TIMEFORMAT=%3R
i=0
time { while [ \$i -lt $calls ]; do $hook; i=\$((i + 1)); done > /dev/null; }
EOF
    fi
    run() { # run SCRIPT [VAR=VALUE...]: one fresh shell with an empty environment
        local script=$1
        shift
        (cd "$home" && env -i PATH="$PATH" HOME="$home" ZDOTDIR="$home" HISTFILE="$home/.history" TERM=xterm BASH_SILENCE_DEPRECATION_WARNING=1 \
            WT_SESSION=bench SERENO_SESSION=wt-profile SERENO_REG_ROOT="$registry" "$@" "$shell" "${flags[@]}" "$script" < /dev/null > /dev/null 2> "$tmp/stderr")
    }
    local flags=("$@")
    : > "$tmp/with.times"; : > "$tmp/without.times"; : > "$tmp/inside.times"
    run "$tmp/with.$name"
    TIMEFORMAT=%3R
    i=0
    while [ "$i" -lt "$runs" ]; do
        { time run "$tmp/without.$name"; } 2>> "$tmp/without.times"
        { time run "$tmp/with.$name"; } 2>> "$tmp/with.times"
        run "$tmp/timed.$name"
        grep -E '^[0-9]+[.,][0-9]+$' "$tmp/stderr" | tail -n 1 >> "$tmp/inside.times"
        i=$((i + 1))
    done
    if [ "$(wc -l < "$tmp/inside.times")" -ne "$runs" ]; then
        printf 'init.%s: no time was reported for the sourcing\n' "$name"
        cat "$tmp/stderr"
        failed=1
        return
    fi
    # Some locales print a comma in the times.
    inside=$(tr , . < "$tmp/inside.times" > "$tmp/t" && median "$tmp/t")
    inside=$(awk -v a="$inside" 'BEGIN { printf "%.1f", a * 1000 }')
    with=$(tr , . < "$tmp/with.times" > "$tmp/t" && median "$tmp/t")
    without=$(tr , . < "$tmp/without.times" > "$tmp/t" && median "$tmp/t")
    added=$(awk -v a="$with" -v b="$without" 'BEGIN { printf "%.1f", (a - b) * 1000 }')
    if over "$inside" "$init_budget"; then
        printf 'init.%s: %s ms (median of %s shells, timed around the sourcing inside each)  OVER the budget of %s\n' "$name" "$inside" "$runs" "$init_budget"
        failed=1
    else
        printf 'init.%s: %s ms (median of %s shells, timed around the sourcing inside each)  ok, budget %s\n' "$name" "$inside" "$runs" "$init_budget"
    fi
    printf '    not judged: a whole shell, %s s with init.%s and %s s without (medians); the difference, %s ms, moves by more than init.%s costs\n' "$with" "$name" "$without" "$added" "$name"

    if ! run "$tmp/prompt.$name" PATH="$empty"; then
        printf 'init.%s: the hook failed with PATH emptied\n' "$name"
        cat "$tmp/stderr"
        failed=1
        return
    fi
    total=$(grep -E '^[0-9]+[.,][0-9]+$' "$tmp/stderr" | tail -n 1 | tr , .)
    if [ -z "$total" ]; then
        printf 'init.%s: no time was reported for the hook\n' "$name"
        cat "$tmp/stderr"
        failed=1
        return
    fi
    per=$(awk -v t="$total" -v n="$calls" 'BEGIN { printf "%.3f", t * 1000 / n }')
    if over "$per" "$prompt_budget"; then
        printf 'hook of init.%s, follow check active, PATH emptied: %s ms (mean of %s calls)  OVER the budget of %s\n' "$name" "$per" "$calls" "$prompt_budget"
        failed=1
    else
        printf 'hook of init.%s, follow check active, PATH emptied: %s ms (mean of %s calls)  ok, budget %s\n' "$name" "$per" "$calls" "$prompt_budget"
    fi
}

printf 'sereno bench: bash %s, %s shells per series, budgets %s ms and %s ms\n' "$BASH_VERSION" "$runs" "$init_budget" "$prompt_budget"
measure bash "$BASH" --noprofile --norc -i
if command -v zsh > /dev/null 2>&1; then
    measure zsh "$(command -v zsh)" -f -i
else
    printf 'init.zsh: skipped, zsh is not installed\n'
fi

# PowerShell 7 outside Windows; on Windows bench.ps1 is the entry point.
case ${OSTYPE-} in
    msys*|cygwin*) ;;
    *)
        if command -v pwsh > /dev/null 2>&1; then
            arguments=(-NoLogo -NoProfile -File "$repo/scripts/bench.ps1" -Init "$rendered/init.ps1" -SkipBash)
            [ -n "$ci" ] && arguments+=(-Ci)
            pwsh "${arguments[@]}" || failed=1
        else
            printf 'init.ps1: skipped, pwsh is not installed\n'
        fi ;;
esac

if [ "$failed" -ne 0 ]; then printf 'sereno bench: a budget was not met\n'; exit 1; fi
printf 'sereno bench: all budgets met\n'
