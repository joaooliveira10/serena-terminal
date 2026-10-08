#!/bin/sh
# Local test run for macOS and Linux (scripts/test.ps1 is the one for Windows).
#
#   sh scripts/test.sh [--skip-go] [--bench]
#
# In order:
#   1. a record of the files of the real home that sereno could ever touch
#      (shell rc files, the PowerShell profile folder, sereno's own folder);
#   2. go vet and go test for the whole module (--skip-go leaves them out);
#   3. a fresh rendering of everything sereno generates, in a temporary folder;
#   4. the program itself: sereno is built into the temporary folder and
#      scripts/smoke.sh runs it there, in a home of its own (apply --dry-run,
#      apply, status --json, theme, check, export, hooks, uninstall, with the
#      whole tree of that home compared before and after);
#   5. the behaviour tests of the init files, each under the shell it is for:
#      test/shell/run.bash, test/shell/run.zsh when zsh is installed, and
#      test/shell/run.ps1 when PowerShell 7 is installed. A shell that is not
#      installed is reported as skipped, not as passed. They run on the
#      rendered files, which step 4 has compared byte for byte with the ones
#      `sereno apply` installs;
#   6. with --bench, the budgets of scripts/bench.sh;
#   7. the record of step 1 again: any difference fails the run.
#
# The program and the shells never see the real home: they are started with
# an empty environment and a temporary folder as HOME (the shells without
# their startup files), and SERENO_SANDBOX confines every write of sereno's
# own code to that folder. The exit code is 1 when any step failed.

set -u
cd "$(dirname "$0")/.." || exit 1
repo=$(pwd)

skip_go=
bench=
for argument in "$@"; do
    case $argument in
        --skip-go) skip_go=1 ;;
        --bench) bench=1 ;;
        *) printf 'test.sh: unknown argument %s\n' "$argument" >&2; exit 2 ;;
    esac
done

failed=0
step() { printf '\n== %s\n' "$1"; }
fail() { printf 'not ok - %s\n' "$1"; failed=1; }

# The real home, looked at and never written.
real_home=$HOME
real_data=${XDG_DATA_HOME:-$HOME/.local/share}/sereno
guard() {
    for file in "$real_home/.bashrc" "$real_home/.bash_profile" "$real_home/.bash_login" "$real_home/.profile" \
        "$real_home/.zshrc" "$real_home/.zprofile" "$real_home/.bash_history" "$real_home/.zsh_history" \
        "$real_home/.config/powershell/Microsoft.PowerShell_profile.ps1" "$real_home/.config/powershell/profile.ps1"; do
        if [ -f "$file" ]; then printf '%s %s\n' "$file" "$(cksum < "$file")"; else printf '%s absent\n' "$file"; fi
    done
    if [ -e "$real_data" ]; then
        printf '%s present\n' "$real_data"
        find "$real_data" -type f -exec cksum {} + 2> /dev/null | sort
    else
        printf '%s absent\n' "$real_data"
    fi
}

tmp=$(mktemp -d "${TMPDIR:-/tmp}/sereno-test-XXXXXX") || exit 1
tmp=$(cd "$tmp" && pwd -P)
trap 'rm -rf "$tmp"' EXIT
guard > "$tmp/guard.before"

if [ -z "$skip_go" ]; then
    step 'go vet ./...'
    go vet ./... || fail 'go vet ./...'
    step 'go test ./... -count=1'
    go test ./... -count=1 || fail 'go test ./...'
fi

step 'render into a temporary folder'
if SERENO_SANDBOX=$tmp go run ./internal/tools/golden --out "$tmp/rendered"; then
    printf 'ok - everything sereno generates was rendered\n'
else
    fail 'go run ./internal/tools/golden'
fi

# The program, built into the temporary folder and run in a home of its own.
# scripts/smoke.sh makes that home and starts the program through `env -i`.
step 'sereno: the program is built into the temporary folder'
if go build -trimpath -ldflags '-X main.version=0.0.0-test' -o "$tmp/bin/sereno" ./cmd/sereno; then
    printf 'ok - sereno was built\n'
    step 'sereno: smoke run in a temporary home'
    sh scripts/smoke.sh "$tmp/bin/sereno" --rendered "$tmp/rendered" --version 0.0.0-test || fail 'scripts/smoke.sh'
else
    fail 'go build ./cmd/sereno'
fi

# A shell under test starts from an empty environment: a temporary home, no
# history file, and nothing of the session that runs this script.
home=$tmp/home
mkdir -p "$home"
clean() {
    env -i PATH="$PATH" HOME="$home" TMPDIR="${TMPDIR:-/tmp}" HISTFILE="$home/.history" ZDOTDIR="$home" \
        XDG_DATA_HOME="$home/.local/share" XDG_CONFIG_HOME="$home/.config" XDG_CACHE_HOME="$home/.cache" \
        SERENO_SANDBOX="$tmp" POWERSHELL_TELEMETRY_OPTOUT=1 POWERSHELL_UPDATECHECK=Off "$@"
}

step 'init.bash under bash'
if command -v bash > /dev/null 2>&1; then
    clean bash --noprofile --norc test/shell/run.bash --init "$tmp/rendered/init/init.bash" --golden "$tmp/rendered" || fail 'test/shell/run.bash'
else
    printf 'skip - bash is not installed: test/shell/run.bash did not run\n'
fi

step 'init.zsh under zsh'
if command -v zsh > /dev/null 2>&1; then
    clean zsh -f test/shell/run.zsh --init "$tmp/rendered/init/init.zsh" --golden "$tmp/rendered" || fail 'test/shell/run.zsh'
else
    printf 'skip - zsh is not installed: test/shell/run.zsh did not run\n'
fi

step 'init.ps1 under PowerShell 7'
if command -v pwsh > /dev/null 2>&1; then
    clean pwsh -NoLogo -NoProfile -File test/shell/run.ps1 -Init "$tmp/rendered/init/init.ps1" -Golden "$tmp/rendered" || fail 'test/shell/run.ps1'
else
    printf 'skip - pwsh is not installed: test/shell/run.ps1 did not run\n'
fi

if [ -n "$bench" ]; then
    step 'budgets'
    clean bash --noprofile --norc scripts/bench.sh --init "$tmp/rendered/init" || fail 'scripts/bench.sh'
fi

step 'the real home'
guard > "$tmp/guard.after"
if cmp -s "$tmp/guard.before" "$tmp/guard.after"; then
    printf 'ok - no shell file of the real home changed, and %s is as it was\n' "$real_data"
else
    fail 'the real home changed during the run'
    diff "$tmp/guard.before" "$tmp/guard.after"
fi

printf '\n'
if [ "$failed" -ne 0 ]; then printf 'test.sh: FAILED\n'; exit 1; fi
printf 'test.sh: all steps passed\n'
