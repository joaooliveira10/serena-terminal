#!/bin/sh
# Smoke run of a built sereno on macOS and Linux (scripts/smoke.ps1 is the one
# for Windows). scripts/test.sh builds the program and calls this file; it can
# also be pointed at any other build, for example a release archive:
#
#   sh scripts/smoke.sh SERENO [--rendered DIR] [--version TEXT]
#
# The program never sees the real home. Every start goes through `env -i`
# with a temporary folder as HOME, LOCALAPPDATA, APPDATA, USERPROFILE and the
# two SERENO_*_PROFILE_PATH variables, and with SERENO_SANDBOX set to it, so
# that sereno itself refuses any write outside. The folder is removed at the
# end.
#
# The first pass sets neither SERENO_HOME nor XDG_DATA_HOME, on purpose:
# sereno's folder is then the default one, ~/.local/share/sereno, which is
# what a machine without those variables gets, and it lies in the temporary
# home because HOME does. Only with the default folder does the program write
# the hook line in the form of R21 that names the folder through $HOME. The
# third pass sets both, to folders in the temporary home.
#
# Three passes, each compared with a checksum listing of the whole temporary
# home taken before it:
#   1. sereno's folder in its default place: apply --dry-run, apply, apply
#      again, status --json, theme light, check, export (into sereno's folder
#      and with --to), apply --hook bash, uninstall;
#   2. six `apply` started at the same moment, six times over: at least one
#      runs to its end, the others find nothing left to do or make way before
#      their first write, and afterwards nothing counts as edited or missing,
#      an apply has nothing to do and uninstall gives the home back (R13,
#      R39). How many made way is printed, not judged;
#   3. sereno's folder under a name with a quote, a space and a dollar sign,
#      with the program in its bin folder: apply --hook for the shells that
#      are installed, then a session of each that loads the hooked start-up
#      file and calls sereno through the function of the init file, then
#      uninstall.
#
# --rendered names a folder made by `go run ./internal/tools/golden --out
# DIR`: the init files apply writes are then compared with it byte for byte,
# which is what lets the shell tests run on the rendered files and still
# speak for the installed ones. --version is the text `sereno version` must
# print after the program name. The exit code is 1 when any check failed.

set -u
here=$(cd "$(dirname "$0")" && pwd) || exit 2
repo=$(cd "$here/.." && pwd) || exit 2

if [ $# -lt 1 ]; then printf 'smoke.sh: the path of a built sereno is required\n' >&2; exit 2; fi
program=$1
shift
rendered=
version=
while [ $# -gt 0 ]; do
    case $1 in
        --rendered) rendered=$2; shift 2 ;;
        --version) version=$2; shift 2 ;;
        *) printf 'smoke.sh: unknown argument %s\n' "$1" >&2; exit 2 ;;
    esac
done
if [ ! -f "$program" ]; then printf 'smoke.sh: %s does not exist\n' "$program" >&2; exit 2; fi

failed=0
# report STATUS NAME prints one check line; it is called with $? of the
# conditions written in front of it.
report() {
    if [ "$1" -eq 0 ]; then printf 'ok - %s\n' "$2"; else printf 'not ok - %s\n' "$2"; failed=1; fi
}

tmp=$(mktemp -d "${TMPDIR:-/tmp}/sereno-smoke-XXXXXX") || exit 1
tmp=$(cd "$tmp" && pwd -P)
trap 'rm -rf "$tmp"' EXIT
home=$tmp/home
mkdir -p "$home" "$tmp/bin"
# The program is run from a copy, so that nothing here depends on where the
# original is.
cp "$program" "$tmp/bin/sereno" && chmod +x "$tmp/bin/sereno" || exit 1
exe=$tmp/bin/sereno
root=$home/.local/share/sereno
out=$tmp/out

# moved is empty while sereno's folder is the default one, and set once it is
# moved to where $root says.
moved=

# clean starts a program with an empty environment that points into the
# temporary home. With $moved set, SERENO_HOME is $root and XDG_DATA_HOME a
# folder of the home; without it neither variable is set, and sereno's folder
# is ~/.local/share/sereno of the temporary home.
clean() {
    if [ -n "$moved" ]; then set -- XDG_DATA_HOME="$home/.local/share" SERENO_HOME="$root" "$@"; fi
    env -i PATH="$PATH" HOME="$home" TMPDIR="$tmp" ZDOTDIR="$home" TERM="${TERM:-dumb}" \
        XDG_CONFIG_HOME="$home/.config" XDG_CACHE_HOME="$tmp/cache" \
        LOCALAPPDATA="$home/AppData/Local" APPDATA="$home/AppData/Roaming" USERPROFILE="$home" \
        SERENO_SANDBOX="$tmp" SERENO_OS_MODE=dark \
        SERENO_PS_PROFILE_PATH="$home/.config/powershell/WindowsPowerShell_profile.ps1" \
        SERENO_PWSH_PROFILE_PATH="$home/.config/powershell/Microsoft.PowerShell_profile.ps1" \
        POWERSHELL_TELEMETRY_OPTOUT=1 POWERSHELL_UPDATECHECK=Off BASH_SILENCE_DEPRECATION_WARNING=1 "$@"
}
# run starts the program, keeps its standard output in $out and its exit
# code in $code.
run() {
    clean "$exe" "$@" > "$out"
    code=$?
}

# A listing of every folder and file below the home, with a checksum of the
# files.
tree() {
    (cd "$home" && find . -print | LC_ALL=C sort | while IFS= read -r entry; do
        if [ -f "$entry" ]; then printf '%s %s\n' "$entry" "$(cksum < "$entry")"; else printf '%s <folder>\n' "$entry"; fi
    done)
}
# same_tree FILE: the home is what the listing in FILE says; when it is not,
# the entries that differ are shown.
same_tree() {
    tree > "$tmp/tree.now"
    if cmp -s "$1" "$tmp/tree.now"; then return 0; fi
    diff "$1" "$tmp/tree.now" | sed 's/^/#   /'
    return 1
}
lines() { grep -c -E -e "$1" "$out"; }   # how many lines of the output match
last() { tail -n 1 "$out"; }             # the last line of the output
files() { find "$1" -type f | wc -l | tr -d ' '; }

# ---------------------------------------------------------------------------
# 1. sereno's folder in its default place: neither SERENO_HOME nor
#    XDG_DATA_HOME is set.
printf '# pass 1: the default folder: apply, status, theme, check, export, a hook, uninstall\n'
tree > "$tmp/tree.before"

run version
wanted='^sereno [^ ]+ \([a-z0-9]+/[a-z0-9]+\)$'
if [ -n "$version" ]; then wanted="^sereno $version \\([a-z0-9]+/[a-z0-9]+\\)\$"; fi
[ "$code" -eq 0 ] && [ "$(wc -l < "$out" | tr -d ' ')" = 1 ] && [ "$(lines "$wanted")" = 1 ]
report $? "sereno version prints one line with the version ($(cat "$out"))"

run apply --dry-run --lang en
[ "$code" -eq 0 ] && [ "$(head -n 1 "$out" | grep -c -E '^sereno .+: plan$')" = 1 ] && [ "$(last)" = 'Nothing was changed.' ]
report $? 'apply --dry-run prints the plan and ends with "Nothing was changed."'
same_tree "$tmp/tree.before"
report $? 'apply --dry-run changed nothing'

run apply --lang en
[ "$code" -eq 0 ] && [ -f "$root/manifest.json" ] && [ -f "$root/mode" ] && [ -f "$root/shell/init.ps1" ] && [ -f "$root/shell/init.bash" ] && [ -f "$root/shell/init.zsh" ]
report $? 'apply wrote the manifest, the mode file and three init files'
[ "$(lines '^  create  /')" = 5 ]
report $? 'the plan has one create line for each of them'
if [ -n "$rendered" ]; then
    cmp -s "$root/shell/init.ps1" "$rendered/init/init.ps1" && cmp -s "$root/shell/init.bash" "$rendered/init/init.bash" && cmp -s "$root/shell/init.zsh" "$rendered/init/init.zsh"
    report $? 'the installed init files are the rendered ones, byte for byte'
fi
tree > "$tmp/tree.applied"

run apply --lang en
[ "$code" -eq 0 ] && [ "$(lines '^  (mkdir|create|replace|append|remove|rmdir) ')" = 0 ] && same_tree "$tmp/tree.applied"
report $? 'a second apply does nothing'

# The JSON is indented, one key to a line, in a fixed order (R37).
run status --json
[ "$code" -eq 0 ] && [ "$(lines '^  "applied": true,$')" = 1 ] && [ "$(lines '^    "mode": "auto",$')" = 1 ] && [ "$(lines '^  "variant": "dark",$')" = 1 ] &&
    [ "$(lines '^    "total": 4,$')" = 1 ] && [ "$(lines '^    "modified": 0,$')" = 1 ] && [ "$(lines '^  "hooks": \[\],$')" = 1 ] && [ "$(last)" = '}' ]
report $? 'status --json reports the installation'

run theme light --lang en
theme_code=$code
run status --json
[ "$theme_code" -eq 0 ] && [ "$(head -n 1 "$root/mode")" = 'mode=light' ] && [ "$(lines '^  "variant": "light",$')" = 1 ] && [ "$(lines '^    "modified": 0,$')" = 1 ]
report $? 'theme light changes the mode file, and the manifest knows it'

run check --lang en
[ "$code" -eq 0 ] && [ "$(wc -l < "$out" | tr -d ' ')" = 7 ] && [ "$(last)" = 'Every value is at or above its floor.' ]
report $? 'check passes for the six variants'
run check --markdown
[ "$code" -eq 0 ] && cmp -s "$out" "$repo/docs/contrast.md"
report $? 'check --markdown prints docs/contrast.md'

run export --lang en
[ "$code" -eq 0 ] && [ "$(files "$root/themes")" = 24 ]
report $? 'export wrote 24 theme files into the folder of sereno'
run export --to "$tmp/themes" --target kitty --lang en
[ "$code" -eq 0 ] && [ "$(files "$tmp/themes")" = 6 ]
report $? 'export --to wrote the six kitty themes into a folder of the user'

# The hook line of the default folder is the line of R21, letter for letter:
# it names the folder through $HOME, and a session that has that line finds
# the init file by it.
hook=bash
if command -v "$hook" > /dev/null 2>&1; then
    run apply --hook "$hook" --lang en
    line='[ -r "$HOME/.local/share/sereno/shell/init.bash" ] && . "$HOME/.local/share/sereno/shell/init.bash" # sereno:hook'
    [ "$code" -eq 0 ] && [ "$(cat "$home/.bashrc")" = "$line" ]
    report $? 'apply --hook bash wrote the hook line of the default folder, as R21 has it'
    clean bash --noprofile --norc -i -c 'unset HISTFILE; . "$HOME/.bashrc"; printf "%s\n" "loaded=${__sereno_loaded-0}"' > "$out" 2> "$tmp/err"
    [ "$(head -n 1 "$out")" = loaded=1 ]
    report $? 'a bash session that has that line runs the init file of sereno'
else
    printf 'skip - bash is not installed: the hook line of the default folder was not tried\n'
fi

run status --lang pt-BR
[ "$code" -eq 0 ] && [ "$(lines '^aplicado +sim$')" = 1 ]
report $? 'status speaks Portuguese when asked'

run uninstall --lang en
[ "$code" -eq 0 ] && [ ! -e "$root" ]
report $? 'uninstall removed the folder of sereno'
same_tree "$tmp/tree.before"
report $? 'after uninstall the home is what it was, file for file'

# ---------------------------------------------------------------------------
# 2. Commands at the same time. One sereno writes the folder; another one that
#    comes at that moment makes way, with exit code 1 and before its first
#    write, and says so.
printf '# pass 2: six applies at the same time, six times\n'
whole=0
waited=0
for round in 1 2 3 4 5 6; do
    for n in 1 2 3 4 5 6; do
        # Each in a shell of its own, so that each has its own $out and $code.
        ( out=$tmp/together.$n.out; run apply --lang en; printf '%s\n' "$code" > "$tmp/together.$n.code" ) 2> "$tmp/together.$n.err" &
    done
    wait
    finished=0
    for n in 1 2 3 4 5 6; do
        case $(cat "$tmp/together.$n.code") in
            0) finished=$((finished + 1)) ;;
            1) waited=$((waited + 1))
                grep -q -e 'another sereno is working on this folder' -e ': changed while sereno was running' "$tmp/together.$n.err" ||
                { printf '#   round %s: exit code 1 without the reason: %s\n' "$round" "$(cat "$tmp/together.$n.err")"; whole=1; } ;;
            *) printf '#   round %s: exit code %s: %s\n' "$round" "$(cat "$tmp/together.$n.code")" "$(cat "$tmp/together.$n.err")"; whole=1 ;;
        esac
    done
    [ "$finished" -ge 1 ] || { printf '#   round %s: no apply ran to its end\n' "$round"; whole=1; }
    run status --json
    [ "$code" -eq 0 ] && [ "$(lines '^    "modified": 0,$')" = 1 ] && [ "$(lines '^    "missing": 0$')" = 1 ] ||
        { printf '#   round %s: status counts files as edited or missing\n' "$round"; whole=1; }
    run apply --lang en
    [ "$code" -eq 0 ] && [ "$(lines '^  (mkdir|create|replace|append|remove|rmdir) ')" = 0 ] ||
        { printf '#   round %s: the apply that follows ends with exit code %s or still has something to do\n' "$round" "$code"; whole=1; }
    run uninstall --lang en
    [ "$code" -eq 0 ] && same_tree "$tmp/tree.before" ||
        { printf '#   round %s: uninstall ends with exit code %s, or the home is not what it was\n' "$round" "$code"; whole=1; }
done
report "$whole" 'each time one apply ran to its end, the others had nothing left to do or made way, and uninstall gave the home back'
printf '# %s of 36 applies made way for another one\n' "$waited"

# ---------------------------------------------------------------------------
# 3. A folder with an awkward name, hooks, and sessions that load them.
printf '# pass 3: hooks from a folder with a quote, a space and a dollar sign\n'
moved=1
root="$home/o'neil \$HOME folder/sereno"
# The place the install script will use (R41). apply and uninstall leave it
# alone (R10), and the init files offer the program from there (R34).
mkdir -p "$root/bin"
cp "$exe" "$root/bin/sereno"
tree > "$tmp/tree.before"

hooks=
count=0
for shell in bash zsh pwsh; do
    if command -v "$shell" > /dev/null 2>&1; then hooks=${hooks:+$hooks,}$shell; count=$((count + 1)); fi
done
if [ -z "$hooks" ]; then
    printf 'skip - neither bash, zsh nor pwsh is installed: no hook was tried\n'
else
    run apply --hook "$hooks" --lang en
    [ "$code" -eq 0 ] && [ "$(lines '^          \+ .* # sereno:hook$')" = "$count" ]
    report $? "apply --hook $hooks added one hook line for each shell"
    tree > "$tmp/tree.hooked"

    # Each session loads its start-up file the way the shell would, says
    # whether the file of sereno ran, and calls the program through the
    # function that file defines.
    case ,$hooks, in *,bash,*)
        clean bash --noprofile --norc -i -c 'unset HISTFILE; . "$HOME/.bashrc"; printf "%s\n" "loaded=${__sereno_loaded-0}"; sereno version' > "$out" 2> "$tmp/err"
        [ "$(head -n 1 "$out")" = loaded=1 ] && [ "$(lines '^sereno [^ ]+ \(')" = 1 ]
        report $? 'a bash session that loads the hooked .bashrc has sereno, and the sereno function reaches the program'
        ;;
    esac
    case ,$hooks, in *,zsh,*)
        clean zsh -f -i -c 'unset HISTFILE; . "${ZDOTDIR:-$HOME}/.zshrc"; print -r -- "loaded=${__sereno_loaded-0}"; sereno version' > "$out" 2> "$tmp/err"
        [ "$(head -n 1 "$out")" = loaded=1 ] && [ "$(lines '^sereno [^ ]+ \(')" = 1 ]
        report $? 'a zsh session that loads the hooked .zshrc has sereno, and the sereno function reaches the program'
        ;;
    esac
    case ,$hooks, in *,pwsh,*)
        clean pwsh -NoLogo -NoProfile -NonInteractive -Command '. $env:SERENO_PWSH_PROFILE_PATH; if ($null -ne $global:__sereno) { sereno version }' > "$out" 2> "$tmp/err"
        [ "$(lines '^sereno [^ ]+ \(')" = 1 ]
        report $? 'a PowerShell session that loads the hooked profile has sereno, and the sereno function reaches the program'
        ;;
    esac
    same_tree "$tmp/tree.hooked"
    report $? 'the sessions wrote nothing into the home'
fi

run uninstall --lang en
[ "$code" -eq 0 ] && [ -f "$root/bin/sereno" ] && [ ! -e "$root/manifest.json" ]
report $? 'uninstall left the bin folder alone and removed the rest'
same_tree "$tmp/tree.before"
report $? 'after uninstall the home is what it was, file for file'

if [ "$failed" -ne 0 ]; then printf 'smoke.sh: FAILED\n'; exit 1; fi
printf 'smoke.sh: all checks passed\n'
