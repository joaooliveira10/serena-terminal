#!/usr/bin/env bash
# Behaviour tests of init.bash (test plan T8; requirements R23, R28, R30, R31,
# R33 and R34).
#
#   bash --noprofile --norc test/shell/run.bash [--init FILE] [--golden DIR]
#        [--modes FILE] [--registry DIR] [--only TEXT]
#
# It runs under bash 3.2 and newer and tests the bash that runs it. It prints
# one line per check ("ok - ..." or "not ok - ...") and ends with exit code 1
# when a check failed.
#
# --init      a rendered init.bash; default testdata/golden/init/init.bash,
#             which the Go tests keep equal to what the renderer produces
# --golden    the folder with palette/<variant>.bin; default testdata/golden
# --modes     the shared truth table; default testdata/modes.tsv
# --registry  the stand-ins for /proc/registry; default testdata/registry
# --only      run only the sessions whose name contains this text
# --where     print the folder of the repository this file belongs to, and stop
#
# How it works: every scenario is a "session", a fresh interactive child bash
# started with --noprofile --norc that sources init.bash the way an rc file
# would and then looks at the result. Nothing outside a temporary folder is
# written: the children get an empty environment with that folder as HOME, no
# history file, and a PATH that is an empty folder, so a child that tried to
# start a program would fail. The system's appearance is never read from the
# machine: SERENO_REG_ROOT or SERENO_OS_MODE stand in for it.

set -u

# The folder this file is in. On Windows the file may be named with
# backslashes, as PowerShell names it, and then %/* would cut nothing off.
here=${BASH_SOURCE[0]}
case ${OSTYPE-} in msys*|cygwin*) here=${here//\\//} ;; esac
case $here in */*) here=${here%/*} ;; *) here=. ;; esac
repo=$(cd "$here/../.." && pwd) || exit 2
init=$repo/testdata/golden/init/init.bash
golden=$repo/testdata/golden
modes=$repo/testdata/modes.tsv
registry=$repo/testdata/registry
only=
while [ $# -gt 0 ]; do
    case $1 in
        --init) init=$2; shift 2 ;;
        --golden) golden=$2; shift 2 ;;
        --modes) modes=$2; shift 2 ;;
        --registry) registry=$2; shift 2 ;;
        --only) only=$2; shift 2 ;;
        --where) printf '%s\n' "$repo"; exit 0 ;;
        *) printf 'run.bash: unknown argument %s\n' "$1" >&2; exit 2 ;;
    esac
done
for needed in "$init" "$golden/palette/dark.bin" "$modes" "$registry/0"; do
    if [ ! -e "$needed" ]; then printf 'run.bash: %s does not exist\n' "$needed" >&2; exit 2; fi
done

passed=0 failed=0 skipped=0
report() {
    case $1 in
        'ok - '*) passed=$((passed + 1)) ;;
        'not ok - '*) failed=$((failed + 1)) ;;
        'skip - '*) skipped=$((skipped + 1)) ;;
    esac
    printf '%s\n' "$1"
}
check() { # check NAME COMMAND...
    local name=$1
    shift
    if "$@"; then report "ok - $name"; else report "not ok - $name"; fi
}

windows=
case ${OSTYPE-} in msys*|cygwin*) windows=1 ;; esac

# ---------------------------------------------------------------------------
# The file itself, before anything runs it.

check "init.bash passes bash -n (bash $BASH_VERSION)" "$BASH" --noprofile --norc -n "$init"
other=$(LC_ALL=C tr -d '\n\040-\176' < "$init" | wc -c)
check 'init.bash holds only printable ASCII and line feeds' [ "$((other))" -eq 0 ]

# What the file never does, read off its text with the comments removed: no
# command substitution, pipe or background job (each would be a process), and
# nothing about keys, aliases, completion or history (R31).
code=$(sed -e 's/^[[:space:]]*#.*$//' "$init")
never() { # never NAME PATTERN
    local found
    found=$(printf '%s\n' "$code" | grep -n -E -e "$2" | head -n 3)
    if [ -z "$found" ]; then report "ok - init.bash has no $1"; else report "not ok - init.bash has no $1"; printf '#   %s\n' "$found"; fi
}
never 'command substitution' '\$\([^(]|`'
never 'pipe or background job' '[[:space:]]\|[[:space:]]|[^&>]&([^&>]|$)'
never 'eval, exec, source of another file, or trap' '(^|[;[:space:]])(eval|exec|source|trap|command|builtin|enable)[[:space:]]'
never 'key binding, alias, completion or history setting' '(^|[;[:space:]])(bind|alias|unalias|complete|compopt|compgen|history|fc)[[:space:]]|HIST[A-Z]*=|shopt -[su]|set [-+]o'
never 'network device' '/dev/(tcp|udp)'
never 'export other than LS_COLORS and LSCOLORS' 'export [^L]|export L[^S]|declare -x|typeset -x'

# A shell that is not interactive gets nothing at all.
before=$(compgen -v | grep -c '^__sereno' || :)
. "$init"
after=$(compgen -v | grep -c '^__sereno' || :)
funcs=$(compgen -A function | grep -c '^__sereno' || :)
check 'init.bash returns at once in a shell that is not interactive' [ "$before$after$funcs" = 000 ]

# The scripts of the repository find it whatever way they are named:
# PowerShell hands a file to Git Bash by a path with backslashes.
if [ -n "$windows" ]; then
    for script in test/shell/run.bash scripts/bench.sh; do
        named=$(cygpath -w "$repo/$script")
        check "$script finds the repository when it is named with backslashes" [ "$("$BASH" --noprofile --norc "$named" --where 2>&1)" = "$repo" ]
    done
fi

# ---------------------------------------------------------------------------
# The temporary world of the sessions.

tmp=$(mktemp -d "${TMPDIR:-/tmp}/sereno-bash-XXXXXX") || exit 1
tmp=$(cd "$tmp" && pwd -P)
cleanup() { rm -rf "$tmp"; }
trap cleanup EXIT
home=$tmp/home
empty=$tmp/empty-path
mkdir -p "$home" "$empty" "$tmp/sessions" "$tmp/homex" "$tmp/folder-for-mode/mode"

# ROOT in its default place, so that the hook line of R21 finds it.
base_env=()
if [ -n "$windows" ]; then
    root=$home/local/sereno
    base_env=("LOCALAPPDATA=$(cygpath -w "$home/local")")
    hook_line='[ -r "$LOCALAPPDATA/sereno/shell/init.bash" ] && . "$LOCALAPPDATA/sereno/shell/init.bash" # sereno:hook'
else
    root=$home/.local/share/sereno
    hook_line='[ -r "$HOME/.local/share/sereno/shell/init.bash" ] && . "$HOME/.local/share/sereno/shell/init.bash" # sereno:hook'
fi
mkdir -p "$root/shell"
cp "$init" "$root/shell/init.bash"
default_mode='mode=auto\nflavor=soft\ninband=profile\nkeys=profile\nprompt=on\n'

# Repositories without git: a branch, a detached head, CRLF, a worktree whose
# .git is a file, a .git file that leads nowhere, and two hostile names.
tree() { mkdir -p "${1%/*}"; printf "$2" > "$1"; }
tree "$home/work/keep" ''
tree "$home/repo/.git/HEAD" 'ref: refs/heads/feature/x\n'
tree "$home/repo/sub/deep/keep" ''
tree "$home/detached/.git/HEAD" '0123456789abcdef0123456789abcdef01234567\n'
tree "$home/crlf/.git/HEAD" 'ref: refs/heads/main\r\n'
tree "$home/main/.git/HEAD" 'ref: refs/heads/main\n'
tree "$home/main/.git/worktrees/wt/HEAD" 'ref: refs/heads/side\n'
tree "$home/worktree/.git" 'gitdir: ../main/.git/worktrees/wt\n'
tree "$home/absolute/.git" "gitdir: $home/main/.git/worktrees/wt\\n"
tree "$home/repo/broken/.git" 'gitdir: ../nowhere\n'
tree "$home/escapes/.git/HEAD" 'ref: refs/heads/a\033]0;x\007b\n'
mkdir -p "$home/hostile/.git"
printf '%s\n' 'ref: refs/heads/$(printf pwned>"$HOME/pwned")`printf pwned>"$HOME/pwned2"`' > "$home/hostile/.git/HEAD"
tree "$home/empty/.git/HEAD" ''
mkdir -p "$home/nohead/.git"

# Registry trees with values Windows does not write, for the reading rules.
odd=$tmp/registry
reg() { # reg TREE FLAGS WINDOW LIGHT, each a printf format or - for no file
    local d=$odd/$1
    mkdir -p "$d/Control Panel/Accessibility/HighContrast" "$d/Control Panel/Colors" "$d/Software/Microsoft/Windows/CurrentVersion/Themes/Personalize"
    [ "$2" = - ] || printf "$2" > "$d/Control Panel/Accessibility/HighContrast/Flags"
    [ "$3" = - ] || printf "$3" > "$d/Control Panel/Colors/Window"
    [ "$4" = - ] || printf "$4" > "$d/Software/Microsoft/Windows/CurrentVersion/Themes/Personalize/AppsUseLightTheme"
}
reg plain '1\n' '128 127 127\n' '\002\0\0\0'
reg edge '127' '127 127 127' '\0\001\0\0'
reg junk 'abc\0' '300 0 0\0' ''
reg long '99999999999\0' '1 2\0' -
reg four '3 4\0' '255 255 255 255\0' '\377'
reg spaces ' 5 \0' '  200\t200  200 \0' '\001'
mkdir -p "$odd/hollow"

# What every session script starts with.
cat > "$tmp/lib.bash" <<'LIB'
unset HISTFILE
set +o history
ok() { printf 'ok - %s\n' "$1"; }
not_ok() { printf 'not ok - %s\n' "$1"; if [ $# -gt 1 ]; then printf '#   %s\n' "$2"; fi; }
skip() { printf 'skip - %s\n' "$1"; }
show() { # show TEXT: REPLY becomes TEXT with its control characters spelled out
    REPLY=$1
    REPLY=${REPLY//$'\033'/<ESC>}; REPLY=${REPLY//$'\a'/<BEL>}; REPLY=${REPLY//$'\r'/<CR>}; REPLY=${REPLY//$'\n'/<LF>}
}
check() { local name=$1; shift; if "$@"; then ok "$name"; else not_ok "$name"; fi; }
check_eq() { # check_eq GOT WANT NAME
    if [ "$1" = "$2" ]; then ok "$3"; return 0; fi
    local got
    show "$1"; got=$REPLY
    show "$2"
    not_ok "$3" "got  $got"
    printf '#   want %s\n' "$REPLY"
}
slurp() { # slurp FILE: REPLY becomes its whole content
    REPLY=
    if [ -r "$1" ]; then IFS= read -r -d '' REPLY < "$1" || :; fi
}
palette() { slurp "$T_GOLDEN/palette/$1.bin"; }
status() { return "$1"; }
# after STATUS: runs the hook as bash would after a command that ended with
# STATUS. REPLY becomes what it wrote, T_RC what it returned.
after() {
    status "$1"
    __sereno_prompt > "$T_TMP/out" 2> "$T_TMP/err"
    T_RC=$?
    slurp "$T_TMP/err"
    if [ -n "$REPLY" ]; then not_ok 'the hook wrote to standard error' "$REPLY"; fi
    slurp "$T_TMP/out"
}
mode_file() { printf 'mode=%s\nflavor=%s\ninband=%s\nkeys=%s\nprompt=%s\n' "${1-auto}" "${2-soft}" "${3-profile}" "${4-profile}" "${5-on}" > "$T_ROOT/mode"; }
# ps1 BRANCH STATUS: REPLY becomes the PS1 of R31, with the branch part when
# BRANCH is 1 and the colour of the given exit status.
ps1() {
    REPLY=$'\\[\033[34m\\]\\w\\[\033[0m\\]'
    if [ "$1" = 1 ]; then REPLY=$REPLY$' \\[\033[35m\\]${__sereno_branch}\\[\033[0m\\]'; fi
    if [ "$2" = 0 ]; then REPLY=$REPLY$'\\n\\[\033[32m\\]>\\[\033[0m\\] '; else REPLY=$REPLY$'\\n\\[\033[31m\\]>\\[\033[0m\\] '; fi
}
# added BEFORE AFTER: REPLY becomes the words of AFTER that are not in BEFORE.
added() {
    local word
    REPLY=
    for word in $2; do
        case " $1 " in *" $word "*) ;; *) REPLY=${REPLY:+$REPLY }$word ;; esac
    done
}
words() { local IFS=$' \n'; set -f; set -- $1; set +f; REPLY=$*; }
source_init() { . "$T_ROOT/shell/init.bash" > "$T_TMP/out" 2> "$T_TMP/err"; }
LIB

# session NAME [VAR=VALUE...] : the body of the session is read from standard
# input. The mode file is written from $mode (a printf format, or "none").
mode=$default_mode
session() {
    local name=$1 file out err line lines=0
    shift
    cat > "$tmp/body"
    case $name in *"$only"*) ;; *) return 0 ;; esac
    printf '# session: %s\n' "$name"
    if [ "$mode" = none ]; then rm -f "$root/mode"; else printf "$mode" > "$root/mode"; fi
    file=$tmp/sessions/session.bash
    { printf '. "$T_LIB"\n'; cat "$tmp/body"; } > "$file"
    out=$tmp/sessions/stdout err=$tmp/sessions/stderr
    (cd "$home/work" && env -i ${base_env[@]+"${base_env[@]}"} PATH="$empty" HOME="$home" HISTFILE="$home/.bash_history" TERM=dumb BASH_SILENCE_DEPRECATION_WARNING=1 \
        T_LIB="$tmp/lib.bash" T_TMP="$tmp" T_ROOT="$root" T_HOME="$home" T_GOLDEN="$golden" T_MODES="$modes" T_REGISTRY="$registry" T_ODD="$odd" \
        "$@" "$BASH" --noprofile --norc -i "$file" < /dev/null > "$out" 2> "$err")
    local code=$?
    while IFS= read -r line || [ -n "$line" ]; do
        case $line in
            '') ;;
            'ok - '*|'not ok - '*|'skip - '*) report "$line"; lines=$((lines + 1)) ;;
            '#'*) printf '%s\n' "$line" ;;
            *) report "not ok - $name wrote something that is not a check line: $line" ;;
        esac
    done < "$out"
    # An interactive bash without a terminal says so twice; anything else on
    # standard error is a failure.
    line=$(grep -v -E 'cannot set terminal process group|no job control in this shell' "$err")
    if [ "$code" -eq 0 ] && [ -z "$line" ]; then report "ok - $name ran to its end without an error"
    else report "not ok - $name ran to its end without an error"; printf '#   exit code %s: %s\n' "$code" "$line"; fi
    check "$name reported at least one check" [ "$lines" -gt 0 ]
}

profile='WT_SESSION=test-session SERENO_SESSION=wt-profile'

# ---------------------------------------------------------------------------
session 'Serena profile session in Windows Terminal' $profile SERENO_OS_MODE=dark <<'BODY'
vars_before=$(compgen -v); funcs_before=$(compgen -A function); exported_before=$(compgen -e)
options_before=$(shopt -p; set +o; alias; complete -p 2> /dev/null)
flags_before=$-
here=$PWD
PS1='before> '
source_init
slurp "$T_TMP/out"; check_eq "$REPLY" '' 'init.bash writes nothing to standard output'
slurp "$T_TMP/err"; check_eq "$REPLY" '' 'init.bash writes nothing to standard error'
words "$vars_before"; old=$REPLY; words "$(compgen -v)"; added "$old vars_before funcs_before exported_before options_before flags_before here old REPLY" "$REPLY"
check_eq "$REPLY" 'LSCOLORS LS_COLORS PROMPT_COMMAND __sereno_branch __sereno_flavor __sereno_hc __sereno_hcl __sereno_inband __sereno_last __sereno_light __sereno_loaded __sereno_mode __sereno_profile __sereno_prompt_on __sereno_reg __sereno_reply __sereno_root __sereno_show __sereno_term __sereno_variant' 'init.bash creates the variables it needs and no other'
words "$funcs_before"; old=$REPLY; words "$(compgen -A function)"; added "$old" "$REPLY"
check_eq "$REPLY" '__sereno_byte __sereno_find_branch __sereno_follow __sereno_paint __sereno_prompt __sereno_resolve __sereno_set __sereno_settings __sereno_signals' 'init.bash defines its functions and no other'
words "$exported_before"; old=$REPLY; words "$(compgen -e)"; added "$old" "$REPLY"
check_eq "$REPLY" 'LSCOLORS LS_COLORS' 'init.bash exports LS_COLORS and LSCOLORS and nothing else'
check_eq "$(shopt -p; set +o; alias; complete -p 2> /dev/null)" "$options_before" 'init.bash leaves options, aliases and completions alone'
check_eq "$-" "$flags_before" 'init.bash leaves the shell flags alone'
check_eq "$PWD" "$here" 'init.bash leaves the directory alone'
check_eq "$PS1" 'before> ' 'init.bash does not set PS1 itself: the hook does, before each prompt'
check_eq "$PROMPT_COMMAND" '__sereno_prompt' 'PROMPT_COMMAND runs the hook'

# R31 and R30: the first prompt paints, and sets PS1.
after 0
palette dark; want=$REPLY; slurp "$T_TMP/out"
check_eq "$REPLY" "$want" 'the first prompt writes PALETTE of the resolved variant'
ps1 0 0; check_eq "$PS1" "$REPLY" 'PS1 outside a repository, after a command that worked'
check_eq "$T_RC" 0 'the hook returns the exit status it found: 0'
after 3
check_eq "$REPLY" '' 'the second prompt writes nothing: the variant did not change'
ps1 0 3; check_eq "$PS1" "$REPLY" 'PS1 after a command that failed'
check_eq "$T_RC" 3 'the hook returns the exit status it found: 3'

# R30: following the system, one variant after the other.
for step in light hc hc-light dark; do
    SERENO_OS_MODE=$step
    after 0; got=$REPLY; palette "$step"
    check_eq "$got" "$REPLY" "the system turns $step: the next prompt writes PALETTE of $step"
    after 0
    check_eq "$REPLY" '' "the system stays $step: nothing is written"
done

# R30: the mode file is read before every prompt.
mode_file light vivid; after 0; got=$REPLY; palette light-vivid
check_eq "$got" "$REPLY" 'the mode file now says light and vivid: the next prompt paints light-vivid'
mode_file hc vivid; after 0; got=$REPLY; palette hc
check_eq "$got" "$REPLY" 'the mode file now says hc: the flavour does not matter'
mode_file auto vivid; after 0; got=$REPLY; palette dark-vivid
check_eq "$got" "$REPLY" 'back to auto with vivid: dark-vivid, because the system is dark'
mode_file auto soft off; after 0; got=$REPLY; palette reset
check_eq "$got" "$REPLY" 'inband=off: the palette this session painted is handed back with RESET'
after 0; check_eq "$REPLY" '' 'inband=off: after that nothing is written'
mode_file auto soft always; after 0; got=$REPLY; palette dark
check_eq "$got" "$REPLY" 'inband=always: painted again'
printf 'mode=auto\r\nflavor=vivid\r\ninband=profile\r\nkeys=profile\r\nprompt=on\r\n' > "$T_ROOT/mode"
after 0; check_eq "$REPLY" '' 'a malformed mode file (CRLF) means the defaults: auto and soft, which is what is painted already'
mode_file light; after 0; got=$REPLY; palette light
check_eq "$got" "$REPLY" 'a valid file again: light'
: > "$T_ROOT/mode"
after 0; got=$REPLY; palette dark
check_eq "$got" "$REPLY" 'an empty mode file means the defaults: auto, so dark'
mode_file

# R31: the branch, read without git (PATH is an empty folder here).
branch() { # branch FOLDER WANT
    cd "$T_HOME/$1"
    after 0
    check_eq "$__sereno_branch" "$2" "branch in ~/$1 is ${2:-not shown}"
    if [ -n "$2" ]; then ps1 1 0; else ps1 0 0; fi
    check_eq "$PS1" "$REPLY" "PS1 in ~/$1 names the variable, never the branch"
}
branch repo feature/x
branch repo/sub/deep feature/x
branch detached 0123456
branch crlf main
branch worktree side
branch absolute side
branch repo/broken feature/x
branch escapes 'a]0;xb'
branch empty ''
branch nohead ''
branch work ''

# R31: a branch called like a command substitution is text.
cd "$T_HOME/hostile"
after 0
check_eq "$__sereno_branch" '$(printf pwned>"$HOME/pwned")`printf pwned>"$HOME/pwned2"`' 'a hostile branch name is kept as text'
ps1 1 0; check_eq "$PS1" "$REPLY" 'and PS1 does not contain it'
if [ "${BASH_VERSINFO[0]}" -gt 4 ] || { [ "${BASH_VERSINFO[0]}" -eq 4 ] && [ "${BASH_VERSINFO[1]}" -ge 4 ]; }; then
    eval 'shown=${PS1@P}'
    case $shown in *'$(printf pwned>"$HOME/pwned")`printf pwned>"$HOME/pwned2"`'*) ok 'expanded as a prompt, the hostile name is shown as it is' ;; *) show "$shown"; not_ok 'expanded as a prompt, the hostile name is shown as it is' "$REPLY" ;; esac
else
    skip "expanding PS1 as a prompt needs bash 4.4; this is $BASH_VERSION"
fi
check 'and nothing it names was run' [ ! -e "$HOME/pwned" ]
check 'neither the part in backticks' [ ! -e "$HOME/pwned2" ]
shopt -u promptvars
after 0
ps1 0 0; check_eq "$PS1" "$REPLY" 'with promptvars off the branch part is left out, since bash would show the variable name'
shopt -s promptvars
cd "$T_HOME/work"

# R23: sourcing twice is a no-op.
vars=$(compgen -v); funcs=$(compgen -A function); command_before=$PROMPT_COMMAND; last=$__sereno_last
source_init
slurp "$T_TMP/out"; out=$REPLY; slurp "$T_TMP/err"
check_eq "$out$REPLY" '' 'sourcing init.bash a second time prints nothing'
check_eq "$PROMPT_COMMAND" "$command_before" 'sourcing init.bash a second time leaves PROMPT_COMMAND alone'
words "$vars"; old=$REPLY; words "$(compgen -v)"; added "$old vars funcs command_before last out" "$REPLY"
check_eq "$REPLY/$__sereno_last" "/$last" 'sourcing init.bash a second time creates no variable and keeps the state'
check_eq "$(compgen -A function)" "$funcs" 'sourcing init.bash a second time defines no function'
check 'no sereno function without ROOT/bin/sereno' [ -z "$(type -t sereno)" ]
check_eq "$PATH" "$T_TMP/empty-path" 'PATH is an empty folder in this session, and init.bash left it alone'
BODY

# ---------------------------------------------------------------------------
session 'resolution table' <<'BODY'
source_init
wrong= rows=0
while IFS=$'\t' read -r m f hc hcl light variant; do
    [ "$m" = mode ] && continue
    rows=$((rows + 1))
    __sereno_resolve "$m" "$f" "$hc" "$hcl" "$light"
    [ "$__sereno_variant" = "$variant" ] || wrong="$wrong [$m $f $hc $hcl $light -> $__sereno_variant, want $variant]"
done < "$T_MODES"
check_eq "$rows$wrong" 80 'init.bash resolves all 80 rows of testdata/modes.tsv'
__sereno_resolve sepia loud 0 0 1
check_eq "$__sereno_variant" light 'an unknown mode resolves like auto and an unknown flavour like soft'

# The mode file: exactly five lines, anything else is the defaults.
settings() { # settings NAME WANT: the file is already written
    __sereno_settings
    check_eq "$__sereno_mode $__sereno_flavor $__sereno_inband $__sereno_prompt_on" "$2" "mode file with $1"
}
defaults='auto soft profile on'
mode_file hc-light vivid always off off; settings 'a valid file' 'hc-light vivid always off'
printf 'mode=dark\r\nflavor=soft\r\ninband=profile\r\nkeys=profile\r\nprompt=on\r\n' > "$T_ROOT/mode"; settings 'CRLF line endings' "$defaults"
printf 'mode=dark\nflavor=soft\ninband=profile\nkeys=profile\nprompt=on' > "$T_ROOT/mode"; settings 'no final newline' "$defaults"
printf 'mode=dark\nflavor=soft\ninband=profile\nkeys=profile\nprompt=on\nextra=1\n' > "$T_ROOT/mode"; settings 'a sixth line' "$defaults"
printf 'mode=dark\nflavor=soft\ninband=profile\nkeys=profile\nprompt=on\n\n' > "$T_ROOT/mode"; settings 'an empty sixth line' "$defaults"
printf 'mode=dark\nflavor=soft\ninband=profile\nkeys=profile\nprompt=on\nx' > "$T_ROOT/mode"; settings 'a sixth line without its end' "$defaults"
printf 'flavor=soft\nmode=dark\ninband=profile\nkeys=profile\nprompt=on\n' > "$T_ROOT/mode"; settings 'lines in another order' "$defaults"
mode_file sepia; settings 'an unknown value' "$defaults"
mode_file dark soft profile sometimes; settings 'an unknown value for keys' "$defaults"
printf 'mode=dark \nflavor=soft\ninband=profile\nkeys=profile\nprompt=on\n' > "$T_ROOT/mode"; settings 'a space after the value' "$defaults"
# One carriage return, in each of the five lines in turn, with values that
# are not the defaults: the whole file must be dropped every time.
for at in 1 2 3 4 5; do
    cr1= cr2= cr3= cr4= cr5=
    eval "cr$at=\$'\\r'"
    printf 'mode=light%s\nflavor=vivid%s\ninband=always%s\nkeys=off%s\nprompt=off%s\n' "$cr1" "$cr2" "$cr3" "$cr4" "$cr5" > "$T_ROOT/mode"
    settings "a carriage return in line $at" "$defaults"
done
printf 'mode=auto\r\nflavor=vivid\ninband=always\nkeys=off\nprompt=off\n' > "$T_ROOT/mode"; settings 'a carriage return after mode=auto' "$defaults"
: > "$T_ROOT/mode"; settings 'an empty file' "$defaults"
printf 'mode=light\nflavor=vivid\ninband=always\nkeys=off\n' > "$T_ROOT/mode"; settings 'four lines' "$defaults"
mode_file light loud; settings 'an unknown flavour' "$defaults"
printf 'mode= light\nflavor=vivid\ninband=always\nkeys=off\nprompt=off\n' > "$T_ROOT/mode"; settings 'a space after the equals sign' "$defaults"
# R11: the file is ASCII. With a byte order mark in front, as an editor may
# save it, it is malformed here as it is for sereno itself and in PowerShell.
printf '\357\273\277mode=light\nflavor=vivid\ninband=always\nkeys=off\nprompt=off\n' > "$T_ROOT/mode"; settings 'a byte order mark' "$defaults"
mode_file light vivid always off off; settings 'a valid file again' 'light vivid always off'
# A folder where the file should be: the session would say so on standard
# error if reading it made bash complain, and that fails the session.
__sereno_root=$T_TMP/folder-for-mode; settings 'a folder in its place' "$defaults"
BODY

# The table once more, through the registry: one session per tree, whose
# digit holds the three signals, and the ten settings of the mode file.
for digit in 0 1 2 3 4 5 6 7; do
    session "resolution through registry tree $digit" $profile "SERENO_REG_ROOT=$registry/$digit" "T_DIGIT=$digit" <<'BODY'
source_init
want_hc=$(( (T_DIGIT >> 2) & 1 )) want_hcl=$(( (T_DIGIT >> 1) & 1 )) want_light=$(( T_DIGIT & 1 ))
__sereno_signals
check_eq "$__sereno_hc $__sereno_hcl $__sereno_light" "$want_hc $want_hcl $want_light" "tree $T_DIGIT gives the signals $want_hc $want_hcl $want_light"
wrong= rows=0
while IFS=$'\t' read -r m f hc hcl light variant; do
    [ "$hc$hcl$light" = "$want_hc$want_hcl$want_light" ] || continue
    rows=$((rows + 1))
    mode_file "$m" "$f"
    __sereno_last=
    after 0; got=$REPLY; palette "$variant"
    [ "$got" = "$REPLY" ] || wrong="$wrong [$m $f -> $__sereno_variant, want $variant]"
done < "$T_MODES"
check_eq "$rows$wrong" 10 "with tree $T_DIGIT the ten settings of testdata/modes.tsv paint the variant of the table"
BODY
done

# ---------------------------------------------------------------------------
session 'reading the registry files' $profile <<'BODY'
source_init
signals() { # signals TREE WANT NAME
    __sereno_reg=$T_ODD/$1
    __sereno_signals
    check_eq "$__sereno_hc $__sereno_hcl $__sereno_light" "$2" "$3"
}
signals plain '1 1 1' 'values that end with a newline; a window colour just above the threshold; a first byte of 2'
signals edge '1 0 0' 'values without any end; a window colour of exactly 127 is not light; a first byte of 0'
signals junk '0 0 0' 'a flag that is no number, a colour out of range and an empty file count as off'
signals long '0 0 0' 'a flag too long for 32 bits, a colour with two numbers and a missing file count as off'
signals four '0 0 1' 'a flag with two numbers and a colour with four count as off; a first byte of 255 is light'
signals spaces '1 1 1' 'spaces and tabs around the numbers do not matter'
signals hollow '0 0 0' 'a tree without any value: everything is off'
BODY

session 'no registry to read' $profile "SERENO_REG_ROOT=$tmp/no-such-tree" <<'BODY'
source_init
check_eq "$__sereno_reg" '' 'without a registry tree there are no signals'
after 0
check_eq "$REPLY" '' 'so nothing is painted, even in a Serena profile session'
mode_file light
after 0
check_eq "$REPLY" '' 'not even for a fixed mode'
mode_file
SERENO_OS_MODE=hc-light
after 0; got=$REPLY; palette hc-light
check_eq "$got" "$REPLY" 'SERENO_OS_MODE replaces the signals, registry or not'
BODY

session 'hook session in Windows Terminal' WT_SESSION=test-session SERENO_OS_MODE=light <<'BODY'
source_init
after 0
check_eq "$REPLY" '' 'inband=profile outside the Serena profile: nothing is painted'
ps1 0 0; check_eq "$PS1" "$REPLY" 'the prompt is installed all the same'
mode_file auto soft always
after 0; got=$REPLY; palette light
check_eq "$got" "$REPLY" 'the mode file turns to inband=always: this session paints from the next prompt on'
BODY

session 'session outside Windows Terminal' SERENO_SESSION=wt-profile SERENO_OS_MODE=light <<'BODY'
source_init
check_eq "$PROMPT_COMMAND" '__sereno_prompt' 'without WT_SESSION the hook is installed for the prompt'
after 0
check_eq "$REPLY" '' 'without WT_SESSION nothing is painted'
ps1 0 0; check_eq "$PS1" "$REPLY" 'and the prompt is the Serena prompt'
BODY

session 'terminal of VS Code' $profile SERENO_OS_MODE=light TERM_PROGRAM=vscode <<'BODY'
source_init
after 0
check_eq "$REPLY" '' 'TERM_PROGRAM=vscode: nothing is painted, Serena Theme owns the palette there'
BODY

session 'session recognised by WT_PROFILE_ID' WT_SESSION=test-session 'WT_PROFILE_ID={6B2D8B49-31D3-5DEA-9087-93C26E1A033E}' SERENO_OS_MODE=hc <<'BODY'
source_init
after 0; got=$REPLY; palette hc
check_eq "$got" "$REPLY" 'WT_PROFILE_ID with the GUID of the profile, in capitals, makes a Serena profile session'
BODY

session 'another profile of Windows Terminal' WT_SESSION=test-session 'WT_PROFILE_ID={61c54bbd-c2c6-5271-96e7-009a87ff44bf}' SERENO_OS_MODE=hc <<'BODY'
source_init
after 0
check_eq "$REPLY" '' 'another WT_PROFILE_ID is not a Serena profile session'
BODY

for tool in STARSHIP_SHELL=bash POSH_PID=1 POSH_THEME=; do
    session "a prompt tool is present (${tool%%=*})" $profile SERENO_OS_MODE=dark "$tool" <<'BODY'
PS1='theirs> '
source_init
after 0; got=$REPLY; palette dark
check_eq "$PS1" 'theirs> ' 'the prompt of the tool is left alone'
check_eq "$got" "$REPLY" 'and the session still follows the system'
BODY
done

session 'a prompt tool outside Windows Terminal' STARSHIP_SHELL=bash <<'BODY'
PROMPT_COMMAND='theirs'
source_init
check_eq "$PROMPT_COMMAND" 'theirs' 'with nothing to do before a prompt, PROMPT_COMMAND is left alone'
BODY

mode='mode=auto\nflavor=soft\ninband=profile\nkeys=profile\nprompt=off\n'
session 'prompt=off' $profile SERENO_OS_MODE=dark <<'BODY'
PS1='mine> '
source_init
after 0; got=$REPLY; palette dark
check_eq "$PS1" 'mine> ' 'prompt=off: PS1 is left alone'
check_eq "$got" "$REPLY" 'prompt=off: the session still follows the system'
BODY
session 'prompt=off outside Windows Terminal' <<'BODY'
source_init
check_eq "${PROMPT_COMMAND-unset}" 'unset' 'prompt=off and no Windows Terminal: PROMPT_COMMAND is not even created'
BODY
mode=none
session 'no mode file' $profile SERENO_OS_MODE=light <<'BODY'
source_init
check_eq "$__sereno_mode $__sereno_flavor $__sereno_inband $__sereno_prompt_on" 'auto soft profile on' 'without a mode file the settings are those of a first apply'
after 0; got=$REPLY; palette light
check_eq "$got" "$REPLY" 'and the session follows the system'
BODY
mode=$default_mode

# R31: PROMPT_COMMAND keeps what it had.
session 'PROMPT_COMMAND with a command' <<'BODY'
log=
theirs() { log="$log theirs:$?"; }
PROMPT_COMMAND='theirs; theirs'
source_init
check_eq "$PROMPT_COMMAND" $'__sereno_prompt\ntheirs; theirs' 'the hook goes in front of the command that was there'
status 5
eval "$PROMPT_COMMAND"
check_eq "$log" ' theirs:5 theirs:0' 'and that command still sees the exit status of the last command'
ps1 0 5; check_eq "$PS1" "$REPLY" 'as does the Serena prompt'
BODY

session 'PROMPT_COMMAND that is empty' <<'BODY'
PROMPT_COMMAND=
source_init
check_eq "$PROMPT_COMMAND" '__sereno_prompt' 'an empty PROMPT_COMMAND becomes the hook'
BODY

session 'PROMPT_COMMAND that is an array' <<'BODY'
PROMPT_COMMAND=(first 'second one')
source_init
if [ "${BASH_VERSINFO[0]}" -gt 5 ] || { [ "${BASH_VERSINFO[0]}" -eq 5 ] && [ "${BASH_VERSINFO[1]}" -ge 1 ]; }; then
    check_eq "${#PROMPT_COMMAND[@]}:${PROMPT_COMMAND[0]}:${PROMPT_COMMAND[1]}:${PROMPT_COMMAND[2]}" '3:__sereno_prompt:first:second one' 'bash 5.1 and newer: the hook becomes the first element'
else
    check_eq "${#PROMPT_COMMAND[@]}:${PROMPT_COMMAND[0]}:${PROMPT_COMMAND[1]}" $'2:__sereno_prompt\nfirst:second one' 'before bash 5.1 only the first element runs: the hook goes in front of it'
fi
PROMPT_COMMAND=(only)
unset __sereno_loaded
source_init
check_eq "${#PROMPT_COMMAND[@]}:${PROMPT_COMMAND[0]}" $'1:__sereno_prompt\nonly' 'an array of one element keeps one element'
BODY

# R33: ls colours. Each case is a session of its own: LS_COLORS as given
# (or unset for "-"), and what it must be afterwards.
ls_case() { # ls_case NAME GIVEN WANT
    local given=()
    [ "$2" = - ] || given=("LS_COLORS=$2")
    session "ls colours: $1" ${given[@]+"${given[@]}"} "T_WANT=$3" "T_NAME=$1" <<'BODY'
source_init
check_eq "${LS_COLORS-unset}" "$T_WANT" "LS_COLORS, $T_NAME"
BODY
}
default_ls='di=1;34:ln=36:so=35:pi=33:ex=1;32:bd=1;33:cd=1;33:su=1;31:sg=1;33:tw=4;34:ow=4;34:st=4;34:or=31:mi=31'
ls_case 'unset' - "$default_ls"
# GNU ls uses its built-in table, backgrounds and all, for an LS_COLORS that
# is set but empty, exactly as for one that is unset.
ls_case 'empty' '' "$default_ls"
ls_case 'the built-in table of GNU ls' \
    'rs=0:di=01;34:ln=01;36:mh=00:pi=40;33:so=01;35:do=01;35:bd=40;33;01:cd=40;33;01:or=40;31;01:mi=00:su=37;41:sg=30;43:ca=30;41:tw=30;42:ow=34;42:st=37;44:ex=01;32:*.tar=01;31:' \
    'rs=0:di=01;34:ln=01;36:mh=00:pi=33:so=01;35:do=01;35:bd=1;33:cd=1;33:or=31:mi=00:su=1;31:sg=1;33:ca=31:tw=4;34:ow=4;34:st=4;34:ex=01;32:*.tar=01;31:'
ls_case 'without any background' 'di=01;34:*.zip=01;31:ex=32' 'di=01;34:*.zip=01;31:ex=32'
ls_case 'backgrounds on other entries' '*.log=44;37:di=1;44:*.x=42:ex=100;1;97:ln=01;36' '*.log=37:di=1:*.x=:ex=1;97:ln=01;36'
ls_case 'colour arguments of 38 and 48' '*.a=38;5;44:*.b=48;5;44;31:*.c=38;2;40;41;42:mi=38;5;41;45:do=38;5;44' '*.a=38;5;44:*.b=48;5;44;31:*.c=38;2;40;41;42:mi=31:do=38;5;44'
ls_case 'odd entries' ':x:=44::di=44:' ':x:=::di=:'
ls_case 'the twelve file types of R33' \
    'su=37;41:sg=30;43:tw=30;42:ow=34;42:st=37;44:ca=30;41:bd=40;33;01:cd=40;33;01:pi=40;33:or=40;31;01:mi=01;05;37;41:do=01;35;45' \
    'su=1;31:sg=1;33:tw=4;34:ow=4;34:st=4;34:ca=31:bd=1;33:cd=1;33:pi=33:or=31:mi=31:do=1;35'
ls_case 'the twelve file types without a background' 'su=31:ow=34:do=01;35:di=01;34' 'su=31:ow=34:do=01;35:di=01;34'
ls_case 'a single entry' 'ow=34;42' 'ow=4;34'
ls_case 'an entry without an equals sign' 'no-equals-sign:ow=34;42' 'no-equals-sign:ow=4;34'
ls_case 'a second equals sign in an entry' '*.a=b=42:ow=103' '*.a=b=42:ow=4;34'
ls_case 'a value that is a word' 'ln=target:ow=100;30' 'ln=target:ow=4;34'

session 'ls colours: set but empty and not exported' <<'BODY'
unset LS_COLORS LSCOLORS
LS_COLORS= LSCOLORS=
exported_before=$(compgen -e)
source_init
check_eq "$LS_COLORS" 'di=1;34:ln=36:so=35:pi=33:ex=1;32:bd=1;33:cd=1;33:su=1;31:sg=1;33:tw=4;34:ow=4;34:st=4;34:or=31:mi=31' 'an empty LS_COLORS counts as unset and gets the table of R33'
check_eq "$LSCOLORS" 'exfxcxdxbxDxDxBxDxExEx' 'an empty LSCOLORS counts as unset and gets the table without backgrounds'
words "$exported_before"; old=$REPLY; words "$(compgen -e)"; added "$old" "$REPLY"
check_eq "$REPLY" 'LSCOLORS LS_COLORS' 'and both are exported'
BODY

session 'ls colours: the rest of the environment' LSCOLORS=Gxfxcxdxbxegedabagacad GREP_COLORS='mt=01;41' <<'BODY'
exported_before=$(compgen -e)
source_init
check_eq "$LSCOLORS" 'Gxfxcxdxbxegedabagacad' 'an LSCOLORS that is set is left alone'
check_eq "$GREP_COLORS/${CLICOLOR-unset}" 'mt=01;41/unset' 'GREP_COLORS and CLICOLOR are not touched'
words "$exported_before"; old=$REPLY; words "$(compgen -e)"; added "$old" "$REPLY"
check_eq "$REPLY" 'LS_COLORS' 'only LS_COLORS is added to the environment'
BODY
session 'ls colours: LSCOLORS' <<'BODY'
source_init
check_eq "$LSCOLORS" 'exfxcxdxbxDxDxBxDxExEx' 'an unset LSCOLORS gets the table without backgrounds'
BODY

# Callers with strict settings.
session 'caller with set -u' $profile SERENO_OS_MODE=dark <<'BODY'
unset PS1 PROMPT_COMMAND LS_COLORS LSCOLORS
set -u
source_init
slurp "$T_TMP/err"; check_eq "$REPLY" '' 'init.bash reads no unset variable'
cd "$T_HOME/repo"; after 0; got=$REPLY; palette dark
check_eq "$got" "$REPLY" 'nor does the hook'
cd "$T_HOME/work"; after 1
check_eq "$T_RC" 1 'in or out of a repository'
set +u
BODY

session 'caller with set -e' $profile SERENO_OS_MODE=dark <<'BODY'
set -e
source_init
cd "$T_HOME/repo"
__sereno_prompt > "$T_TMP/out"
set +e
slurp "$T_TMP/out"; got=$REPLY; palette dark
check_eq "$got" "$REPLY" 'init.bash and the hook run to their end under set -e'
BODY

# R23: a variable of the user's that is read-only. An assignment would make
# bash print "readonly variable" and abandon the function at that point. A
# read-only PROMPT_COMMAND is an audit setting on shared machines.
session 'a read-only PROMPT_COMMAND' $profile SERENO_OS_MODE=dark 'LS_COLORS=ow=34;42' <<'BODY'
PROMPT_COMMAND='theirs'
readonly PROMPT_COMMAND
PS1='theirs> '
source_init
slurp "$T_TMP/out"; out=$REPLY; slurp "$T_TMP/err"
check_eq "$out$REPLY" '' 'a read-only PROMPT_COMMAND: init.bash prints nothing'
check_eq "$PROMPT_COMMAND" 'theirs' 'and it stays as it is'
check_eq "$__sereno_show$__sereno_term" '' 'the hook that could not be installed is not counted on'
check_eq "$LS_COLORS/$LSCOLORS" 'ow=4;34/exfxcxdxbxDxDxBxDxExEx' 'the colours of ls are set all the same'
check_eq "$PS1/$__sereno_loaded" 'theirs> /1' 'and the rest of the file ran'
BODY

session 'a read-only PROMPT_COMMAND without a value' $profile SERENO_OS_MODE=dark <<'BODY'
readonly PROMPT_COMMAND
source_init
slurp "$T_TMP/out"; out=$REPLY; slurp "$T_TMP/err"
check_eq "$out$REPLY" '' 'a read-only PROMPT_COMMAND that is not set: init.bash prints nothing'
check_eq "${PROMPT_COMMAND-unset}/$__sereno_show$__sereno_term/$LSCOLORS" 'unset//exfxcxdxbxDxDxBxDxExEx' 'it stays unset, and the rest of the file ran'
BODY

session 'a read-only PROMPT_COMMAND that is an array' $profile SERENO_OS_MODE=dark <<'BODY'
PROMPT_COMMAND=(first 'second one')
readonly PROMPT_COMMAND
source_init
slurp "$T_TMP/out"; out=$REPLY; slurp "$T_TMP/err"
check_eq "$out$REPLY" '' 'a read-only PROMPT_COMMAND that is an array: init.bash prints nothing'
check_eq "${#PROMPT_COMMAND[@]}:${PROMPT_COMMAND[0]}:${PROMPT_COMMAND[1]}/$__sereno_show$__sereno_term/$LSCOLORS" '2:first:second one//exfxcxdxbxDxDxBxDxExEx' 'it keeps its elements, and the rest of the file ran'
BODY

session 'a read-only PS1' $profile SERENO_OS_MODE=dark <<'BODY'
PS1='theirs> '
readonly PS1
source_init
slurp "$T_TMP/out"; out=$REPLY; slurp "$T_TMP/err"
check_eq "$out$REPLY" '' 'a read-only PS1: init.bash prints nothing'
cd "$T_HOME/repo"
after 3; got=$REPLY; palette dark
check_eq "$got" "$REPLY" 'the hook says nothing either, and the session follows the system'
check_eq "$PS1/$T_RC" 'theirs> /3' 'the prompt stays as it is, and the exit status is passed on'
after 0
check_eq "$REPLY/$PS1/$__sereno_show" '/theirs> /' 'and setting it is not tried again'
BODY

session 'a read-only LS_COLORS' 'LS_COLORS=ow=34;42' <<'BODY'
readonly LS_COLORS
source_init
slurp "$T_TMP/out"; out=$REPLY; slurp "$T_TMP/err"
check_eq "$out$REPLY" '' 'a read-only LS_COLORS: init.bash prints nothing'
check_eq "$LS_COLORS/$LSCOLORS/$PROMPT_COMMAND" 'ow=34;42/exfxcxdxbxDxDxBxDxExEx/__sereno_prompt' 'it stays as it is, and the rest of the file ran'
BODY

# Before bash 4.4, printf -v reports success for a variable it could not set.
# A printf that does the same stands in for such a bash here.
session 'a read-only PROMPT_COMMAND where printf does not report it' $profile SERENO_OS_MODE=dark <<'BODY'
printf() { builtin printf "$@"; return 0; }
PROMPT_COMMAND='theirs'
readonly PROMPT_COMMAND
source_init
slurp "$T_TMP/out"; out=$REPLY; slurp "$T_TMP/err"
check_eq "$out$REPLY" '' 'init.bash prints nothing'
check_eq "$PROMPT_COMMAND/$__sereno_show$__sereno_term" 'theirs/' 'and sees by the value that the hook could not be installed'
BODY

session 'a read-only PS1 where printf does not report it' $profile SERENO_OS_MODE=dark <<'BODY'
printf() { builtin printf "$@"; return 0; }
PS1='theirs> '
readonly PS1
source_init
after 0; got=$REPLY; palette dark
check_eq "$got" "$REPLY" 'the hook says nothing, and the session follows the system'
check_eq "$PS1/$__sereno_show" 'theirs> /' 'and it sees by the value that the prompt could not be set'
BODY

# With set -e, bash ends the shell on any failed write to a read-only
# variable, whatever the place of the command: not even `|| :` saves it.
session 'read-only variables in a shell with set -e' $profile SERENO_OS_MODE=dark 'LS_COLORS=ow=34;42' <<'BODY'
PS1='theirs> '
readonly LS_COLORS PS1
set -e
source_init
flags=$-
cd "$T_HOME/repo"
__sereno_prompt > "$T_TMP/out" 2> "$T_TMP/err"
flags=$flags/$-
set +e
slurp "$T_TMP/err"
check_eq "$REPLY" '' 'variables that cannot be set do not end a shell that runs with set -e, and nothing is said'
check_eq "$LS_COLORS/$LSCOLORS/$PROMPT_COMMAND/$PS1" 'ow=34;42/exfxcxdxbxDxDxBxDxExEx/__sereno_prompt/theirs> ' 'they stay as they are, and the rest ran'
case $flags in *e*/*e*) ok 'and set -e is in force again afterwards' ;; *) not_ok 'and set -e is in force again afterwards' "$flags" ;; esac
BODY

session 'a read-only LS_COLORS and LSCOLORS without a value' <<'BODY'
unset LS_COLORS LSCOLORS
readonly LS_COLORS LSCOLORS
source_init
slurp "$T_TMP/out"; out=$REPLY; slurp "$T_TMP/err"
check_eq "$out$REPLY" '' 'read-only LS_COLORS and LSCOLORS that are not set: init.bash prints nothing'
check_eq "${LS_COLORS-unset}/${LSCOLORS-unset}/$PROMPT_COMMAND" 'unset/unset/__sereno_prompt' 'they stay unset, and the rest of the file ran'
BODY

# R23 and R30: a file that another program holds at the moment of a prompt.
# Only Windows lets a program keep every reader out of a file, and Windows
# PowerShell does the holding here, from outside the session: it waits for the
# session to say it is ready, opens the mode file and HEAD without sharing,
# and lets go when the session says so.
holder=
if [ -n "$windows" ] && [ -n "${SYSTEMROOT-}" ]; then
    holder=$(cygpath -u "$SYSTEMROOT")/System32/WindowsPowerShell/v1.0/powershell.exe
    [ -x "$holder" ] || holder=
fi
case 'files held by another program' in
    *"$only"*)
        if [ -z "$holder" ]; then
            report 'skip - files held by another program: only Windows keeps a reader out of a file, and Windows PowerShell does the holding'
        else
            cat > "$tmp/hold.ps1" <<'HOLD'
param([string]$Folder, [string]$First, [string]$Second)
$wait = { param($name, $there) for ($i = 0; ($i -lt 600) -and ((Test-Path -LiteralPath (Join-Path $Folder $name)) -ne $there); $i++) { Start-Sleep -Milliseconds 50 } }
& $wait 'hold-ready' $true
$a = [IO.File]::Open($First, [IO.FileMode]::Open, [IO.FileAccess]::ReadWrite, [IO.FileShare]::None)
$b = [IO.File]::Open($Second, [IO.FileMode]::Open, [IO.FileAccess]::ReadWrite, [IO.FileShare]::None)
[IO.File]::WriteAllText((Join-Path $Folder 'hold-on'), '')
& $wait 'hold-done' $true
$a.Dispose(); $b.Dispose()
[IO.File]::Delete((Join-Path $Folder 'hold-on'))
HOLD
            rm -f "$tmp/hold-ready" "$tmp/hold-on" "$tmp/hold-done"
            "$holder" -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "$(cygpath -w "$tmp/hold.ps1")" \
                -Folder "$(cygpath -w "$tmp")" -First "$(cygpath -w "$root/mode")" -Second "$(cygpath -w "$home/repo/.git/HEAD")" < /dev/null > "$tmp/hold.log" 2>&1 &
        fi ;;
esac
if [ -n "$holder" ]; then
    mode='mode=light\nflavor=soft\ninband=always\nkeys=profile\nprompt=on\n'
    session 'files held by another program' WT_SESSION=test-session SERENO_OS_MODE=dark <<'BODY'
# until_file FILE STATE: waits for FILE to be there (1) or gone (0). The
# session has builtins only, and bash 3.2 no way of waiting a moment, so it
# looks again and again, for a while that is long enough for PowerShell to
# start.
until_file() {
    local n=0
    while [ "$n" -lt 400000 ]; do
        if [ -e "$1" ]; then [ "$2" = 0 ] || return 0; else [ "$2" = 1 ] || return 0; fi
        n=$((n + 1))
    done
    return 1
}
source_init
cd "$T_HOME/repo"
after 0; got=$REPLY; palette light
check_eq "$got/$__sereno_branch" "$REPLY/feature/x" 'before anything is held: the session paints what the mode file says and shows the branch'
: > "$T_TMP/hold-ready"
check 'another program now holds the mode file and HEAD' until_file "$T_TMP/hold-on" 1
after 4
check_eq "$REPLY" '' 'while they are held the hook writes nothing: the settings read before stay in force'
check_eq "$__sereno_mode $__sereno_inband/$__sereno_branch/$T_RC" 'light always//4' 'no branch is shown at that prompt, and the exit status is passed on'
: > "$T_TMP/hold-done"
check 'the other program let go' until_file "$T_TMP/hold-on" 0
after 0
check_eq "$REPLY/$__sereno_branch" '/feature/x' 'at the next prompt everything is as before'
BODY
    mode=$default_mode
    wait
fi

# R23: where ROOT is, whatever the way the file is named.
session 'the file named in other ways' <<'BODY'
flavor() { # flavor NAME: the mode file says vivid; finding it proves ROOT is right
    slurp "$T_TMP/err"
    check_eq "$REPLY/$__sereno_flavor" '/vivid' "ROOT is found when the file is sourced as $1"
    unset __sereno_loaded
}
mode_file auto vivid
cd "$T_ROOT/shell"; . ./init.bash 2> "$T_TMP/err"; flavor './init.bash'
. init.bash 2> "$T_TMP/err"; flavor 'init.bash, found in the current directory'
cd "$T_ROOT"; . shell/init.bash 2> "$T_TMP/err"; flavor 'shell/init.bash'
cd "$T_ROOT/shell"; . ../shell/./init.bash 2> "$T_TMP/err"; flavor '../shell/./init.bash'
cd "$T_HOME"; . "$T_ROOT/shell/init.bash" 2> "$T_TMP/err"; flavor 'an absolute path'
cd /; __sereno_settings; check_eq "$__sereno_flavor" vivid 'and it stays found after a change of directory'
BODY

session 'the hook line of R21' "T_LINE=$hook_line" <<'BODY'
mode_file auto vivid
printf '%s\n' "$T_LINE" > "$T_TMP/rc"
. "$T_TMP/rc" > "$T_TMP/out" 2> "$T_TMP/err"
slurp "$T_TMP/out"; out=$REPLY; slurp "$T_TMP/err"
check_eq "$out$REPLY" '' 'the hook line of R21, in an rc file, prints nothing'
check_eq "${__sereno_loaded-}/${__sereno_flavor-}" '1/vivid' 'and loads init.bash from ROOT in its default place'
BODY

# R34: the sereno function.
mkdir -p "$root/bin"
printf '#!/bin/sh\nprintf "%%s|" "$@"\n' > "$root/bin/sereno"
chmod +x "$root/bin/sereno"
session 'sereno in ROOT bin and not on PATH' <<'BODY'
source_init
check_eq "$(type -t sereno)" 'function' 'init.bash defines the function sereno'
check_eq "$(sereno one 'two words' '' 2> "$T_TMP/err")" 'one|two words||' 'and it calls the program in ROOT/bin with the arguments it was given'
check_eq "$PATH" "$T_TMP/empty-path" 'PATH is not touched'
BODY
mkdir -p "$tmp/on-path"
printf '#!/bin/sh\n' > "$tmp/on-path/sereno"
chmod +x "$tmp/on-path/sereno"
session 'sereno in ROOT bin and on PATH' "PATH=$empty:$tmp/on-path" <<'BODY'
source_init
check_eq "$(type -t sereno)" 'file' 'a program called sereno is on PATH: init.bash defines no function'
BODY
rm -rf "$root/bin"

printf 'run.bash: %s passed, %s failed, %s skipped (bash %s)\n' "$passed" "$failed" "$skipped" "$BASH_VERSION"
[ "$failed" -eq 0 ]
