#!/usr/bin/env zsh
# Behaviour tests of init.zsh (test plan T8; requirements R23, R28, R30, R32,
# R33 and R34).
#
#   zsh -f test/shell/run.zsh [--init FILE] [--golden DIR] [--modes FILE]
#       [--registry DIR] [--only TEXT]
#
# It prints one line per check ("ok - ..." or "not ok - ...") and ends with
# exit code 1 when a check failed. scripts/test.sh and scripts/test.ps1 run it
# when zsh is installed and say so when it is not.
#
# zsh is tier 2: neither the machine this was written on nor its WSL has zsh,
# so this file has only ever run in CI. The part of init.zsh that decides
# colours and modes is the same text as in init.bash and is exercised by
# run.bash on every system; what is specific to zsh is tested here.
#
# How it works: every scenario is a "session", a fresh interactive child zsh
# started with -f (no startup files) that sources init.zsh the way .zshrc
# would and then looks at the result. Nothing outside a temporary folder is
# written: the children get an empty environment with that folder as HOME and
# ZDOTDIR, no history file, and a PATH that is an empty folder, so a child
# that tried to start a program would fail. The system's appearance is never
# read from the machine: SERENO_REG_ROOT or SERENO_OS_MODE stand in for it.

emulate -R zsh
setopt no_unset

here=${0:A:h}
repo=${here:h:h}
init=$repo/testdata/golden/init/init.zsh
golden=$repo/testdata/golden
modes=$repo/testdata/modes.tsv
registry=$repo/testdata/registry
only=
while (( $# )); do
    case $1 in
        --init) init=$2; shift 2 ;;
        --golden) golden=$2; shift 2 ;;
        --modes) modes=$2; shift 2 ;;
        --registry) registry=$2; shift 2 ;;
        --only) only=$2; shift 2 ;;
        *) print -u2 -r -- "run.zsh: unknown argument $1"; exit 2 ;;
    esac
done
for needed in $init $golden/palette/dark.bin $modes $registry/0; do
    if [[ ! -e $needed ]]; then print -u2 -r -- "run.zsh: $needed does not exist"; exit 2; fi
done
zsh=${commands[zsh]:-zsh}

integer passed=0 failed=0 skipped=0
report() {
    case $1 in
        'ok - '*) (( passed += 1 )) ;;
        'not ok - '*) (( failed += 1 )) ;;
        'skip - '*) (( skipped += 1 )) ;;
    esac
    print -r -- $1
}
check() { # check NAME COMMAND...
    local name=$1
    shift
    if "$@"; then report "ok - $name"; else report "not ok - $name"; fi
}

# ---------------------------------------------------------------------------
# The file itself, before anything runs it.

check "init.zsh passes zsh -n (zsh $ZSH_VERSION)" $zsh -f -n $init
other=$(LC_ALL=C tr -d '\n\040-\176' < $init | wc -c)
check 'init.zsh holds only printable ASCII and line feeds' [ $(( other )) -eq 0 ]
code=$(sed -e 's/^[[:space:]]*#.*$//' $init)
never() { # never NAME PATTERN
    local found
    found=$(print -r -- $code | grep -n -E -e $2 | head -n 3)
    if [[ -z $found ]]; then report "ok - init.zsh has no $1"; else report "not ok - init.zsh has no $1"; print -r -- "#   $found"; fi
}
never 'command substitution' '\$\([^(]|`'
never 'pipe or background job' '[[:space:]]\|[[:space:]]|[^&>]&([^&>]|$)'
never 'eval, exec, source of another file, or trap' '(^|[;[:space:]])(eval|exec|source|trap|command|zmodload|autoload)[[:space:]]'
never 'key binding, alias, completion or history setting' '(^|[;[:space:]])(bindkey|alias|unalias|compdef|compinit|zle|fc)[[:space:]]|HIST[A-Z]*=|SAVEHIST'
never 'network device' '/dev/(tcp|udp)|ztcp|zsocket'

# ---------------------------------------------------------------------------
# The temporary world of the sessions.

tmp=$(mktemp -d ${TMPDIR:-/tmp}/sereno-zsh-XXXXXX) || exit 1
tmp=${tmp:A}
trap 'rm -rf $tmp' EXIT
home=$tmp/home
empty=$tmp/empty-path
root=$home/.local/share/sereno
mkdir -p $home/work $empty $tmp/sessions $root/shell $tmp/folder-for-mode/mode
cp $init $root/shell/init.zsh
hook_line='[ -r "$HOME/.local/share/sereno/shell/init.zsh" ] && . "$HOME/.local/share/sereno/shell/init.zsh" # sereno:hook'
default_mode='mode=auto\nflavor=soft\ninband=profile\nkeys=profile\nprompt=on\n'

# The format is quoted: zsh drops a word that is an empty parameter without
# quotes, and printf would be left without its format ("not enough arguments").
tree() { mkdir -p ${1:h}; printf "$2" > $1; }
tree $home/repo/.git/HEAD 'ref: refs/heads/feature/x\n'
tree $home/repo/sub/deep/keep ''
tree $home/detached/.git/HEAD '0123456789abcdef0123456789abcdef01234567\n'
tree $home/main/.git/HEAD 'ref: refs/heads/main\n'
tree $home/main/.git/worktrees/wt/HEAD 'ref: refs/heads/side\n'
tree $home/worktree/.git 'gitdir: ../main/.git/worktrees/wt\n'
tree $home/escapes/.git/HEAD 'ref: refs/heads/a\033]0;x\007b\n'
mkdir -p $home/hostile/.git
print -r -- 'ref: refs/heads/$(print pwned>$HOME/pwned)`print pwned>$HOME/pwned2`%F{1}' > $home/hostile/.git/HEAD

odd=$tmp/registry
reg() { # reg TREE FLAGS WINDOW LIGHT, each a printf format or - for no file
    local d=$odd/$1
    mkdir -p "$d/Control Panel/Accessibility/HighContrast" "$d/Control Panel/Colors" "$d/Software/Microsoft/Windows/CurrentVersion/Themes/Personalize"
    [[ $2 = - ]] || printf "$2" > "$d/Control Panel/Accessibility/HighContrast/Flags"
    [[ $3 = - ]] || printf "$3" > "$d/Control Panel/Colors/Window"
    [[ $4 = - ]] || printf "$4" > "$d/Software/Microsoft/Windows/CurrentVersion/Themes/Personalize/AppsUseLightTheme"
}
reg plain '1\n' '128 127 127\n' '\002\0\0\0'
reg edge '127' '127 127 127' '\0\001\0\0'
reg junk 'abc\0' '300 0 0\0' ''
reg four '3 4\0' '255 255 255 255\0' '\377'

# What every session script starts with.
cat > $tmp/lib.zsh <<'LIB'
unset HISTFILE
ok() { print -r -- "ok - $1" }
not_ok() { print -r -- "not ok - $1"; if (( $# > 1 )); then print -r -- "#   $2"; fi }
skip() { print -r -- "skip - $1" }
show() { # show TEXT: REPLY becomes TEXT with its control characters spelled out
    REPLY=$1
    REPLY=${REPLY//$'\033'/<ESC>}; REPLY=${REPLY//$'\a'/<BEL>}; REPLY=${REPLY//$'\r'/<CR>}; REPLY=${REPLY//$'\n'/<LF>}
}
check() { local name=$1; shift; if "$@"; then ok $name; else not_ok $name; fi }
check_eq() { # check_eq GOT WANT NAME
    if [[ $1 == $2 ]]; then ok $3; return 0; fi
    local got
    show $1; got=$REPLY
    show $2
    not_ok $3 "got  $got"
    print -r -- "#   want $REPLY"
}
slurp() { REPLY=; if [[ -r $1 ]]; then REPLY="$(<$1)"; fi }
palette() { slurp $T_GOLDEN/palette/$1.bin }
# hook: runs the function zsh would run before a prompt. REPLY becomes what
# it wrote.
hook() {
    __sereno_precmd > $T_TMP/out 2> $T_TMP/err
    slurp $T_TMP/err
    if [[ -n $REPLY ]]; then not_ok 'the hook wrote to standard error' $REPLY; fi
    slurp $T_TMP/out
}
mode_file() { printf 'mode=%s\nflavor=%s\ninband=%s\nkeys=%s\nprompt=%s\n' ${1-auto} ${2-soft} ${3-profile} ${4-profile} ${5-on} > $T_ROOT/mode }
source_init() { . $T_ROOT/shell/init.zsh > $T_TMP/out 2> $T_TMP/err }
LIB

# session NAME [VAR=VALUE...] : the body is read from standard input. The
# mode file is written from $mode (a printf format, or "none").
mode=$default_mode
session() {
    local name=$1 file out err line code
    integer lines=0
    shift
    cat > $tmp/body
    [[ $name == *$only* ]] || return 0
    print -r -- "# session: $name"
    if [[ $mode == none ]]; then rm -f $root/mode; else printf $mode > $root/mode; fi
    file=$tmp/sessions/session.zsh
    { print -r -- '. $T_LIB'; cat $tmp/body } > $file
    out=$tmp/sessions/stdout err=$tmp/sessions/stderr
    (cd $home/work && env -i PATH=$empty HOME=$home ZDOTDIR=$home TERM=xterm \
        T_LIB=$tmp/lib.zsh T_TMP=$tmp T_ROOT=$root T_HOME=$home T_GOLDEN=$golden T_MODES=$modes T_REGISTRY=$registry T_ODD=$odd \
        "$@" $zsh -f -i $file < /dev/null > $out 2> $err)
    code=$?
    while IFS= read -r line || [[ -n $line ]]; do
        case $line in
            '') ;;
            'ok - '*|'not ok - '*|'skip - '*) report $line; (( lines += 1 )) ;;
            '#'*) print -r -- $line ;;
            *) report "not ok - $name wrote something that is not a check line: $line" ;;
        esac
    done < $out
    line="$(<$err)"
    if (( code == 0 )) && [[ -z $line ]]; then report "ok - $name ran to its end without an error"
    else report "not ok - $name ran to its end without an error"; print -r -- "#   exit code $code: $line"; fi
    check "$name reported at least one check" [ $lines -gt 0 ]
}

profile=(WT_SESSION=test-session SERENO_SESSION=wt-profile)
prompt_text='%F{4}%~%f%(1V. %F{5}%1v%f.)'$'\n''%(?.%F{2}.%F{1})>%f '

# ---------------------------------------------------------------------------
session 'Serena profile session in Windows Terminal' $profile SERENO_OS_MODE=dark "T_PROMPT=$prompt_text" <<'BODY'
zmodload zsh/parameter
before=(${(k)parameters}); functions_before=(${(k)functions}); here=$PWD
options_before=$(setopt; alias; bindkey 2> /dev/null)
PROMPT='before> '
source_init
slurp $T_TMP/out; check_eq "$REPLY" '' 'init.zsh writes nothing to standard output'
slurp $T_TMP/err; check_eq "$REPLY" '' 'init.zsh writes nothing to standard error'
# The names are sorted before they are joined into one word: between double
# quotes zsh joins an array first, and (o) then has a single word to sort.
new=(${(k)parameters}); new=(${new:|before}); new=(${new:#(before|functions_before|here|options_before|new|REPLY)}); new=(${(o)new})
check_eq "${new[*]}" 'LSCOLORS LS_COLORS __sereno_branch __sereno_flavor __sereno_hc __sereno_hcl __sereno_inband __sereno_last __sereno_light __sereno_loaded __sereno_mode __sereno_profile __sereno_prompt_on __sereno_reg __sereno_reply __sereno_root __sereno_show __sereno_term __sereno_variant precmd_functions' 'init.zsh creates the parameters it needs and no other'
new=(${(k)functions}); new=(${new:|functions_before}); new=(${(o)new})
check_eq "${new[*]}" '__sereno_byte __sereno_find_branch __sereno_follow __sereno_paint __sereno_precmd __sereno_resolve __sereno_settings __sereno_signals' 'init.zsh defines its functions and no other'
check_eq "$(setopt; alias; bindkey 2> /dev/null)" "$options_before" 'init.zsh leaves options, aliases and key bindings alone'
check_eq "$PWD" "$here" 'init.zsh leaves the directory alone'
check_eq "$PROMPT" 'before> ' 'init.zsh does not set PROMPT itself: the hook does, before each prompt'
check_eq "${precmd_functions[-1]}" '__sereno_precmd' 'the hook is the last of precmd_functions'
check_eq "$__sereno_root" "$T_ROOT" 'ROOT is the folder above the one init.zsh is in'

# R32 and R30: the first prompt paints, and sets PROMPT.
hook; got=$REPLY; palette dark
check_eq "$got" "$REPLY" 'the first prompt writes PALETTE of the resolved variant'
check_eq "$PROMPT" "$T_PROMPT" 'PROMPT is the one of R32'
check_eq "${psvar[1]-}" '' 'outside a repository psvar[1] is empty'
hook; check_eq "$REPLY" '' 'the second prompt writes nothing: the variant did not change'

for step in light hc hc-light dark; do
    SERENO_OS_MODE=$step
    hook; got=$REPLY; palette $step
    check_eq "$got" "$REPLY" "the system turns $step: the next prompt writes PALETTE of $step"
    hook; check_eq "$REPLY" '' "the system stays $step: nothing is written"
done

mode_file light vivid; hook; got=$REPLY; palette light-vivid
check_eq "$got" "$REPLY" 'the mode file now says light and vivid: the next prompt paints light-vivid'
mode_file auto soft off; hook; got=$REPLY; palette reset
check_eq "$got" "$REPLY" 'inband=off: the palette this session painted is handed back with RESET'
hook; check_eq "$REPLY" '' 'inband=off: after that nothing is written'
mode_file auto soft always; hook; got=$REPLY; palette dark
check_eq "$got" "$REPLY" 'inband=always: painted again'
mode_file

# R32: the branch reaches the prompt through psvar.
branch() { # branch FOLDER WANT
    cd $T_HOME/$1
    hook
    check_eq "${psvar[1]-}" "$2" "branch in ~/$1 is ${2:-not shown}"
}
branch repo feature/x
branch repo/sub/deep feature/x
branch detached 0123456
branch worktree side
branch escapes 'a]0;xb'
branch work ''

# A branch called like a command substitution, with a prompt escape for good
# measure, is text.
cd $T_HOME/hostile
hook
hostile='$(print pwned>$HOME/pwned)`print pwned>$HOME/pwned2`%F{1}'
check_eq "${psvar[1]-}" "$hostile" 'a hostile branch name is kept as text'
check_eq "$PROMPT" "$T_PROMPT" 'and PROMPT does not contain it'
setopt prompt_subst
shown=${(%)PROMPT}
unsetopt prompt_subst
if [[ $shown == *$hostile* ]]; then ok 'expanded as a prompt, even with prompt_subst, the hostile name is shown as it is'; else show $shown; not_ok 'expanded as a prompt, even with prompt_subst, the hostile name is shown as it is' $REPLY; fi
check 'and nothing it names was run' [ ! -e $HOME/pwned -a ! -e $HOME/pwned2 ]
cd $T_HOME/work

# R23: sourcing twice is a no-op.
count=${#precmd_functions}; last=$__sereno_last
source_init
slurp $T_TMP/out; out=$REPLY; slurp $T_TMP/err
check_eq "$out$REPLY" '' 'sourcing init.zsh a second time prints nothing'
check_eq "${#precmd_functions}/$__sereno_last" "$count/$last" 'sourcing init.zsh a second time adds no hook and keeps the state'
check 'no sereno function without ROOT/bin/sereno' [ -z "${functions[sereno]-}" ]
check_eq "$PATH" "$T_TMP/empty-path" 'PATH is an empty folder in this session, and init.zsh left it alone'
BODY

session 'resolution table' <<'BODY'
source_init
wrong= ; integer rows=0
while IFS=$'\t' read -r m f hc hcl light variant; do
    [[ $m == mode ]] && continue
    (( rows += 1 ))
    __sereno_resolve $m $f $hc $hcl $light
    [[ $__sereno_variant == $variant ]] || wrong="$wrong [$m $f $hc $hcl $light -> $__sereno_variant, want $variant]"
done < $T_MODES
check_eq "$rows$wrong" 80 'init.zsh resolves all 80 rows of testdata/modes.tsv'

# The mode file: exactly five lines, anything else is the defaults.
settings() { # settings NAME WANT: the file is already written
    __sereno_settings
    check_eq "$__sereno_mode $__sereno_flavor $__sereno_inband $__sereno_prompt_on" "$2" "mode file with $1"
}
defaults='auto soft profile on'
mode_file hc-light vivid always off off; settings 'a valid file' 'hc-light vivid always off'
printf 'mode=dark\nflavor=soft\ninband=profile\nkeys=profile\nprompt=on' > $T_ROOT/mode; settings 'no final newline' $defaults
printf 'mode=dark\nflavor=soft\ninband=profile\nkeys=profile\nprompt=on\nextra=1\n' > $T_ROOT/mode; settings 'a sixth line' $defaults
printf 'flavor=soft\nmode=dark\ninband=profile\nkeys=profile\nprompt=on\n' > $T_ROOT/mode; settings 'lines in another order' $defaults
mode_file sepia; settings 'an unknown value' $defaults
for at in 1 2 3 4 5; do
    cr=('' '' '' '' '')
    cr[$at]=$'\r'
    printf 'mode=light%s\nflavor=vivid%s\ninband=always%s\nkeys=off%s\nprompt=off%s\n' "${cr[@]}" > $T_ROOT/mode
    settings "a carriage return in line $at" $defaults
done
: > $T_ROOT/mode; settings 'an empty file' $defaults
# R11: the file is ASCII. With a byte order mark in front, as an editor may
# save it, it is malformed here as it is for sereno itself and in PowerShell.
printf '\357\273\277mode=light\nflavor=vivid\ninband=always\nkeys=off\nprompt=off\n' > $T_ROOT/mode; settings 'a byte order mark' $defaults
mode_file light vivid always off off; settings 'a valid file again' 'light vivid always off'
# A folder where the file should be: the session would say so on standard
# error if reading it made zsh complain, and that fails the session.
__sereno_root=$T_TMP/folder-for-mode; settings 'a folder in its place' $defaults
BODY

for digit in 0 1 2 3 4 5 6 7; do
    session "resolution through registry tree $digit" $profile SERENO_REG_ROOT=$registry/$digit T_DIGIT=$digit <<'BODY'
source_init
integer want_hc=$(( (T_DIGIT >> 2) & 1 )) want_hcl=$(( (T_DIGIT >> 1) & 1 )) want_light=$(( T_DIGIT & 1 ))
__sereno_signals
check_eq "$__sereno_hc $__sereno_hcl $__sereno_light" "$want_hc $want_hcl $want_light" "tree $T_DIGIT gives the signals $want_hc $want_hcl $want_light"
wrong= ; integer rows=0
while IFS=$'\t' read -r m f hc hcl light variant; do
    [[ $hc$hcl$light == $want_hc$want_hcl$want_light ]] || continue
    (( rows += 1 ))
    mode_file $m $f
    __sereno_last=
    hook; got=$REPLY; palette $variant
    [[ $got == $REPLY ]] || wrong="$wrong [$m $f -> $__sereno_variant, want $variant]"
done < $T_MODES
check_eq "$rows$wrong" 10 "with tree $T_DIGIT the ten settings of testdata/modes.tsv paint the variant of the table"
BODY
done

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
signals four '0 0 1' 'a flag with two numbers and a colour with four count as off; a first byte of 255 is light'
BODY

session 'no registry to read' $profile SERENO_REG_ROOT=$tmp/no-such-tree <<'BODY'
source_init
hook
check_eq "$REPLY" '' 'without a registry tree nothing is painted, even in a Serena profile session'
SERENO_OS_MODE=hc-light
hook; got=$REPLY; palette hc-light
check_eq "$got" "$REPLY" 'SERENO_OS_MODE replaces the signals, registry or not'
BODY

session 'hook session in Windows Terminal' WT_SESSION=test-session SERENO_OS_MODE=light <<'BODY'
source_init
hook
check_eq "$REPLY" '' 'inband=profile outside the Serena profile: nothing is painted'
mode_file auto soft always
hook; got=$REPLY; palette light
check_eq "$got" "$REPLY" 'the mode file turns to inband=always: this session paints from the next prompt on'
BODY

session 'terminal of VS Code' $profile SERENO_OS_MODE=light TERM_PROGRAM=vscode <<'BODY'
source_init
hook
check_eq "$REPLY" '' 'TERM_PROGRAM=vscode: nothing is painted, Serena Theme owns the palette there'
BODY

session 'session recognised by WT_PROFILE_ID' WT_SESSION=test-session 'WT_PROFILE_ID={6B2D8B49-31D3-5DEA-9087-93C26E1A033E}' SERENO_OS_MODE=hc <<'BODY'
source_init
hook; got=$REPLY; palette hc
check_eq "$got" "$REPLY" 'WT_PROFILE_ID with the GUID of the profile, in capitals, makes a Serena profile session'
BODY

for tool in STARSHIP_SHELL=zsh POSH_PID=1 POSH_THEME= ZSH=/somewhere P9K_VERSION=1; do
    session "a prompt tool is present (${tool%%=*})" $profile SERENO_OS_MODE=dark $tool <<'BODY'
PROMPT='theirs> '
source_init
hook; got=$REPLY; palette dark
check_eq "$PROMPT" 'theirs> ' 'the prompt of the tool is left alone'
check_eq "$got" "$REPLY" 'and the session still follows the system'
BODY
done

mode='mode=auto\nflavor=soft\ninband=profile\nkeys=profile\nprompt=off\n'
session 'prompt=off outside Windows Terminal' <<'BODY'
source_init
check_eq "${#precmd_functions}" 0 'prompt=off and no Windows Terminal: no hook is installed'
BODY
mode=$default_mode

session 'precmd_functions that has entries' <<'BODY'
theirs() { : }
precmd_functions=(theirs)
source_init
check_eq "${precmd_functions[*]}" 'theirs __sereno_precmd' 'the hook is appended to what was there'
BODY

ls_case() { # ls_case NAME GIVEN WANT
    local -a given
    [[ $2 == - ]] || given=("LS_COLORS=$2")
    session "ls colours: $1" $given "T_WANT=$3" "T_NAME=$1" <<'BODY'
source_init
check_eq "${LS_COLORS-unset}" "$T_WANT" "LS_COLORS, $T_NAME"
BODY
}
default_ls='di=1;34:ln=36:so=35:pi=33:ex=1;32:bd=1;33:cd=1;33:su=1;31:sg=1;33:tw=4;34:ow=4;34:st=4;34:or=31:mi=31'
ls_case 'unset' - $default_ls
# GNU ls uses its built-in table, backgrounds and all, for an LS_COLORS that
# is set but empty, exactly as for one that is unset.
ls_case 'empty' '' $default_ls
ls_case 'the built-in table of GNU ls' \
    'rs=0:di=01;34:ln=01;36:mh=00:pi=40;33:so=01;35:do=01;35:bd=40;33;01:cd=40;33;01:or=40;31;01:mi=00:su=37;41:sg=30;43:ca=30;41:tw=30;42:ow=34;42:st=37;44:ex=01;32:*.tar=01;31:' \
    'rs=0:di=01;34:ln=01;36:mh=00:pi=33:so=01;35:do=01;35:bd=1;33:cd=1;33:or=31:mi=00:su=1;31:sg=1;33:ca=31:tw=4;34:ow=4;34:st=4;34:ex=01;32:*.tar=01;31:'
ls_case 'colour arguments of 38 and 48' '*.a=38;5;44:*.b=48;5;44;31:mi=38;5;41;45' '*.a=38;5;44:*.b=48;5;44;31:mi=31'

session 'ls colours: LSCOLORS' <<'BODY'
zmodload zsh/parameter
source_init
check_eq "$LSCOLORS" 'exfxcxdxbxDxDxBxDxExEx' 'an unset LSCOLORS gets the table without backgrounds'
check_eq "${parameters[LS_COLORS]}/${parameters[LSCOLORS]}" 'scalar-export/scalar-export' 'both are exported'
BODY

session 'ls colours: set but empty and not exported' <<'BODY'
zmodload zsh/parameter
unset LS_COLORS LSCOLORS
LS_COLORS= LSCOLORS=
source_init
check_eq "$LS_COLORS" 'di=1;34:ln=36:so=35:pi=33:ex=1;32:bd=1;33:cd=1;33:su=1;31:sg=1;33:tw=4;34:ow=4;34:st=4;34:or=31:mi=31' 'an empty LS_COLORS counts as unset and gets the table of R33'
check_eq "$LSCOLORS" 'exfxcxdxbxDxDxBxDxExEx' 'an empty LSCOLORS counts as unset and gets the table without backgrounds'
check_eq "${parameters[LS_COLORS]}/${parameters[LSCOLORS]}" 'scalar-export/scalar-export' 'and both are exported'
BODY

# R23: a parameter of the user's that is read-only. An assignment would make
# zsh print "read-only variable" and abandon what it was running.
session 'a read-only PROMPT' $profile SERENO_OS_MODE=dark <<'BODY'
PROMPT='theirs> '
typeset -r PROMPT
source_init
slurp $T_TMP/out; out=$REPLY; slurp $T_TMP/err
check_eq "$out$REPLY" '' 'a read-only PROMPT: init.zsh prints nothing'
cd $T_HOME/repo
hook; got=$REPLY; palette dark
check_eq "$got" "$REPLY" 'the hook says nothing either, and the session follows the system'
check_eq "$PROMPT/$__sereno_show" 'theirs> /' 'the prompt stays as it is, and setting it is not tried again'
BODY

session 'a read-only psvar' $profile SERENO_OS_MODE=dark <<'BODY'
PROMPT='theirs> '
typeset -r psvar
source_init
cd $T_HOME/repo
hook; got=$REPLY; palette dark
check_eq "$got" "$REPLY" 'a read-only psvar: the hook says nothing, and the session follows the system'
check_eq "$PROMPT/$__sereno_show" 'theirs> /' 'the prompt stays as it is: without psvar it could not show the branch'
BODY

session 'a read-only precmd_functions' $profile SERENO_OS_MODE=dark <<'BODY'
typeset -ga precmd_functions
typeset -r precmd_functions
source_init
slurp $T_TMP/out; out=$REPLY; slurp $T_TMP/err
check_eq "$out$REPLY" '' 'a read-only precmd_functions: init.zsh prints nothing'
check_eq "${#precmd_functions}/$__sereno_show$__sereno_term/$LSCOLORS" '0//exfxcxdxbxDxDxBxDxExEx' 'no hook is installed or counted on, and the rest of the file ran'
BODY

session 'a read-only LS_COLORS' 'LS_COLORS=ow=34;42' <<'BODY'
typeset -r LS_COLORS
source_init
slurp $T_TMP/out; out=$REPLY; slurp $T_TMP/err
check_eq "$out$REPLY" '' 'a read-only LS_COLORS: init.zsh prints nothing'
check_eq "$LS_COLORS/$LSCOLORS/${precmd_functions[-1]}" 'ow=34;42/exfxcxdxbxDxDxBxDxExEx/__sereno_precmd' 'it stays as it is, and the rest of the file ran'
BODY

session 'a read-only LS_COLORS and LSCOLORS without a value' <<'BODY'
unset LS_COLORS LSCOLORS
typeset -r LS_COLORS LSCOLORS
source_init
slurp $T_TMP/out; out=$REPLY; slurp $T_TMP/err
check_eq "$out$REPLY" '' 'read-only LS_COLORS and LSCOLORS that are not set: init.zsh prints nothing'
check_eq "$LS_COLORS/$LSCOLORS/${precmd_functions[-1]}" '//__sereno_precmd' 'they stay empty, and the rest of the file ran'
BODY

# A caller with options that change how zsh reads code: the functions of
# init.zsh set their own.
session 'caller with other options' $profile SERENO_OS_MODE=dark <<'BODY'
source_init
setopt ksh_arrays sh_word_split no_unset glob_subst
cd $T_HOME/repo
__sereno_precmd > $T_TMP/out 2> $T_TMP/err
unsetopt ksh_arrays sh_word_split no_unset glob_subst
slurp $T_TMP/err; check_eq "$REPLY" '' 'the hook runs without a word under ksh_arrays, sh_word_split, no_unset and glob_subst'
slurp $T_TMP/out; got=$REPLY; palette dark
check_eq "$got/${psvar[1]-}" "$REPLY/feature/x" 'and does the same as ever'
BODY

session 'the hook line of R21' "T_LINE=$hook_line" <<'BODY'
mode_file auto vivid
print -r -- $T_LINE > $T_TMP/rc
. $T_TMP/rc > $T_TMP/out 2> $T_TMP/err
slurp $T_TMP/out; out=$REPLY; slurp $T_TMP/err
check_eq "$out$REPLY" '' 'the hook line of R21, in an rc file, prints nothing'
check_eq "${__sereno_loaded-}/${__sereno_flavor-}" '1/vivid' 'and loads init.zsh from ROOT in its default place'
BODY

mkdir -p $root/bin
printf '#!/bin/sh\nprintf "%%s|" "$@"\n' > $root/bin/sereno
chmod +x $root/bin/sereno
session 'sereno in ROOT bin and not on PATH' <<'BODY'
source_init
check 'init.zsh defines the function sereno' [ -n "${functions[sereno]-}" ]
check_eq "$(sereno one 'two words' '' 2> $T_TMP/err)" 'one|two words||' 'and it calls the program in ROOT/bin with the arguments it was given'
BODY
mkdir -p $tmp/on-path
printf '#!/bin/sh\n' > $tmp/on-path/sereno
chmod +x $tmp/on-path/sereno
session 'sereno in ROOT bin and on PATH' PATH=$empty:$tmp/on-path <<'BODY'
source_init
check 'a program called sereno is on PATH: init.zsh defines no function' [ -z "${functions[sereno]-}" ]
BODY
rm -rf $root/bin

print -r -- "run.zsh: $passed passed, $failed failed, $skipped skipped (zsh $ZSH_VERSION)"
(( failed == 0 ))
