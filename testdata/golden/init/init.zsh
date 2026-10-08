# Serena Terminal: session setup for zsh.
#
# Written by `sereno apply`, removed by `sereno uninstall`. Do not edit: the
# next apply would leave an edited copy alone and stop updating it.
# The file is the same for every user. It prints nothing, starts no process,
# opens no connection and paints with the 16 palette slots only. The one
# exception are the palette strings in __sereno_paint, which hand the Serena
# colours to Windows Terminal when the session follows the system.

[[ -o interactive ]] || return 0
(( ${+__sereno_loaded} )) && return 0
typeset -g __sereno_loaded=1

# ROOT is the folder above the one this file is in.
typeset -g __sereno_root="${${(%):-%x}:A:h:h}"

# What follows is the same text in init.bash and init.zsh, written in what
# bash 3.2 and zsh have in common. It uses builtins only: no command
# substitution, no pipe, no program.

# Reads ROOT/mode: five lines in a fixed order. A missing or malformed file
# means the values of a first apply. Every file below is looked at before it
# is read, and what the shell would say about one it cannot open after all is
# sent away first: a redirection from a folder, or from a file that another
# program holds at that moment, must not put a message on the screen.
__sereno_settings() {
    local a= b= c= d= f= x= opened=
    if [ -f "$__sereno_root/mode" ] && [ -r "$__sereno_root/mode" ]; then
        {
            opened=1
            IFS= read -r a && IFS= read -r b && IFS= read -r c && IFS= read -r d && IFS= read -r f || a=
            if IFS= read -r x || [ -n "$x" ]; then a=; fi
        } 2> /dev/null < "$__sereno_root/mode" || :
        # There, readable, and still not opened: held by another program.
        # The settings read before stay, and the next prompt reads again.
        if [ -z "$opened" ] && [ -n "${__sereno_mode-}" ]; then return 0; fi
    fi
    __sereno_mode=auto __sereno_flavor=soft __sereno_inband=profile __sereno_prompt_on=on
    case $a in mode=auto|mode=dark|mode=light|mode=hc|mode=hc-light) ;; *) return 0 ;; esac
    case $b in flavor=soft|flavor=vivid) ;; *) return 0 ;; esac
    case $c in inband=profile|inband=always|inband=off) ;; *) return 0 ;; esac
    case $d in keys=profile|keys=always|keys=off) ;; *) return 0 ;; esac
    case $f in prompt=on|prompt=off) ;; *) return 0 ;; esac
    __sereno_mode=${a#mode=} __sereno_flavor=${b#flavor=} __sereno_inband=${c#inband=} __sereno_prompt_on=${f#prompt=}
    return 0
}

# The three signals of the system, as 0 or 1: a contrast theme is on, its
# window colour is light, applications use the light theme. SERENO_OS_MODE
# replaces them. Otherwise they are read from the registry files of Git Bash;
# a value that cannot be read counts as off. Returns 1 when there is nothing
# to read them from.
__sereno_signals() {
    local file= x= r= g= b=
    __sereno_hc=0 __sereno_hcl=0 __sereno_light=0
    case ${SERENO_OS_MODE-} in
        dark) return 0 ;;
        light) __sereno_light=1; return 0 ;;
        hc) __sereno_hc=1; return 0 ;;
        hc-light) __sereno_hc=1 __sereno_hcl=1; return 0 ;;
    esac
    [ -n "$__sereno_reg" ] || return 1

    # Flags is text, for example 126; bit 0 means a contrast theme is on.
    file=$__sereno_reg/'Control Panel/Accessibility/HighContrast/Flags'
    if [ -f "$file" ] && [ -r "$file" ]; then
        IFS=$' \t' read -r r x < "$file" || :
        r=${r%$'\0'} x=${x%$'\0'}
        r=${r%$'\r'}
        case $r in
            ''|*[!0-9]*) ;;
            *)
                if [ -z "$x" ] && [ "${#r}" -le 10 ] && [ "$((10#$r))" -le 4294967295 ]; then
                    case $r in *[13579]) __sereno_hc=1 ;; esac
                fi ;;
        esac
    fi

    # Window is "R G B" in decimal; the theme is a light one when
    # 0.2126 R + 0.7152 G + 0.0722 B is above 127.
    file=$__sereno_reg/'Control Panel/Colors/Window'
    if [ -f "$file" ] && [ -r "$file" ]; then
        r= g= b= x=
        IFS=$' \t' read -r r g b x < "$file" || :
        r=${r%$'\0'} g=${g%$'\0'} b=${b%$'\0'} x=${x%$'\0'}
        b=${b%$'\r'}
        case $r:$g:$b:$x in
            *[!0-9:]*|:*|*::*|*[!:]) ;;
            *)
                if [ "${#r}" -le 3 ] && [ "${#g}" -le 3 ] && [ "${#b}" -le 3 ]; then
                    r=$((10#$r)) g=$((10#$g)) b=$((10#$b))
                    if [ "$r" -le 255 ] && [ "$g" -le 255 ] && [ "$b" -le 255 ] && [ "$((2126 * r + 7152 * g + 722 * b))" -gt 1270000 ]; then
                        __sereno_hcl=1
                    fi
                fi ;;
        esac
    fi

    # AppsUseLightTheme is a number of four raw bytes.
    __sereno_byte "$__sereno_reg/Software/Microsoft/Windows/CurrentVersion/Themes/Personalize/AppsUseLightTheme"
    if [ "$__sereno_reply" = 1 ]; then __sereno_light=1; fi
    return 0
}

# The resolution table: mode, flavour and the three signals give a variant.
__sereno_resolve() {
    local v=dark
    case $1 in
        hc) __sereno_variant=hc; return 0 ;;
        hc-light) __sereno_variant=hc-light; return 0 ;;
        dark) ;;
        light) v=light ;;
        *)
            if [ "$3" = 1 ]; then
                if [ "$4" = 1 ]; then __sereno_variant=hc-light; else __sereno_variant=hc; fi
                return 0
            fi
            if [ "$5" = 1 ]; then v=light; fi ;;
    esac
    if [ "$2" = vivid ]; then v=$v-vivid; fi
    __sereno_variant=$v
    return 0
}

# Writes PALETTE of a variant to the terminal: OSC 10, 11, 12, 17 and 4;0-15,
# each ending in ESC \. With the argument reset it writes RESET.
__sereno_paint() {
    case $1 in
        dark) printf '\033]10;rgb:d4/d4/dc\033\\\033]11;rgb:16/17/1d\033\\\033]12;rgb:c4/a7/e7\033\\\033]17;rgb:3a/3f/5a\033\\\033]4;0;rgb:26/28/33\033\\\033]4;1;rgb:e8/8b/9a\033\\\033]4;2;rgb:a6/d1/89\033\\\033]4;3;rgb:e8/c8/89\033\\\033]4;4;rgb:8c/b4/e8\033\\\033]4;5;rgb:c4/a7/e7\033\\\033]4;6;rgb:91/d1/d7\033\\\033]4;7;rgb:d4/d4/dc\033\\\033]4;8;rgb:7b/7f/94\033\\\033]4;9;rgb:f0/a3/af\033\\\033]4;10;rgb:bc/de/a4\033\\\033]4;11;rgb:f0/d6/a3\033\\\033]4;12;rgb:a9/c8/f0\033\\\033]4;13;rgb:d2/bb/ee\033\\\033]4;14;rgb:ae/e0/e3\033\\\033]4;15;rgb:ee/ee/f3\033\\' ;;
        dark-vivid) printf '\033]10;rgb:d4/d4/dc\033\\\033]11;rgb:13/14/1b\033\\\033]12;rgb:b5/82/ef\033\\\033]17;rgb:30/35/57\033\\\033]4;0;rgb:23/25/31\033\\\033]4;1;rgb:f1/6e/92\033\\\033]4;2;rgb:a3/e2/8d\033\\\033]4;3;rgb:df/bb/5b\033\\\033]4;4;rgb:79/b6/f4\033\\\033]4;5;rgb:b5/82/ef\033\\\033]4;6;rgb:4e/d8/dc\033\\\033]4;7;rgb:d4/d4/dc\033\\\033]4;8;rgb:78/7d/96\033\\\033]4;9;rgb:f9/9b/af\033\\\033]4;10;rgb:bb/f9/a6\033\\\033]4;11;rgb:f5/da/7a\033\\\033]4;12;rgb:a4/d0/fa\033\\\033]4;13;rgb:cc/a7/f8\033\\\033]4;14;rgb:84/f0/f0\033\\\033]4;15;rgb:ee/ee/f3\033\\' ;;
        light) printf '\033]10;rgb:32/34/45\033\\\033]11;rgb:ec/ed/f3\033\\\033]12;rgb:7b/48/9e\033\\\033]17;rgb:d6/de/ff\033\\\033]4;0;rgb:32/34/45\033\\\033]4;1;rgb:9f/21/31\033\\\033]4;2;rgb:30/63/24\033\\\033]4;3;rgb:82/56/0a\033\\\033]4;4;rgb:14/55/a2\033\\\033]4;5;rgb:72/3c/90\033\\\033]4;6;rgb:06/61/66\033\\\033]4;7;rgb:52/54/62\033\\\033]4;8;rgb:67/6a/7d\033\\\033]4;9;rgb:d2/1f/27\033\\\033]4;10;rgb:12/7c/2a\033\\\033]4;11;rgb:7b/6c/00\033\\\033]4;12;rgb:00/6e/be\033\\\033]4;13;rgb:a4/41/b3\033\\\033]4;14;rgb:15/78/77\033\\\033]4;15;rgb:1f/21/30\033\\' ;;
        light-vivid) printf '\033]10;rgb:32/34/45\033\\\033]11;rgb:ec/ed/f3\033\\\033]12;rgb:89/25/b0\033\\\033]17;rgb:d6/de/ff\033\\\033]4;0;rgb:32/34/45\033\\\033]4;1;rgb:9f/21/31\033\\\033]4;2;rgb:25/65/13\033\\\033]4;3;rgb:82/56/0a\033\\\033]4;4;rgb:14/55/a2\033\\\033]4;5;rgb:77/32/9b\033\\\033]4;6;rgb:06/61/66\033\\\033]4;7;rgb:52/54/62\033\\\033]4;8;rgb:67/6a/7d\033\\\033]4;9;rgb:d2/1f/27\033\\\033]4;10;rgb:12/7c/2a\033\\\033]4;11;rgb:7b/6c/00\033\\\033]4;12;rgb:00/6e/be\033\\\033]4;13;rgb:a4/41/b3\033\\\033]4;14;rgb:15/78/77\033\\\033]4;15;rgb:1f/21/30\033\\' ;;
        hc) printf '\033]10;rgb:f2/f2/f8\033\\\033]11;rgb:0b/0c/12\033\\\033]12;rgb:f2/f2/f8\033\\\033]17;rgb:cb/b8/ff\033\\\033]4;0;rgb:2f/31/40\033\\\033]4;1;rgb:ff/7a/8c\033\\\033]4;2;rgb:8f/e0/7a\033\\\033]4;3;rgb:f8/d4/57\033\\\033]4;4;rgb:6a/ae/ff\033\\\033]4;5;rgb:dc/b0/ff\033\\\033]4;6;rgb:4f/d2/d6\033\\\033]4;7;rgb:d8/d9/e2\033\\\033]4;8;rgb:a4/a6/b8\033\\\033]4;9;rgb:ff/a3/ae\033\\\033]4;10;rgb:b6/f0/9a\033\\\033]4;11;rgb:ff/e5/8f\033\\\033]4;12;rgb:9c/c7/ff\033\\\033]4;13;rgb:eb/d2/ff\033\\\033]4;14;rgb:8b/e7/ea\033\\\033]4;15;rgb:ff/ff/ff\033\\' ;;
        hc-light) printf '\033]10;rgb:0b/0c/12\033\\\033]11;rgb:ff/ff/ff\033\\\033]12;rgb:0b/0c/12\033\\\033]17;rgb:3b/2d/86\033\\\033]4;0;rgb:0b/0c/12\033\\\033]4;1;rgb:a1/00/28\033\\\033]4;2;rgb:1f/55/24\033\\\033]4;3;rgb:65/50/00\033\\\033]4;4;rgb:23/43/a3\033\\\033]4;5;rgb:6a/1b/8a\033\\\033]4;6;rgb:00/5d/69\033\\\033]4;7;rgb:50/51/5b\033\\\033]4;8;rgb:4a/4b/5a\033\\\033]4;9;rgb:8f/1d/3f\033\\\033]4;10;rgb:2a/5f/1e\033\\\033]4;11;rgb:5c/4a/00\033\\\033]4;12;rgb:3b/3f/ae\033\\\033]4;13;rgb:7a/2a/9e\033\\\033]4;14;rgb:0b/5a/73\033\\\033]4;15;rgb:55/56/60\033\\' ;;
        reset) printf '\033]104\033\\\033]110\033\\\033]111\033\\\033]112\033\\\033]117\033\\' ;;
    esac
    return 0
}

# Following the system, run before every prompt inside Windows Terminal: read
# the mode file, resolve the variant and, only when it is not the one painted
# last, write its palette. When the mode file no longer allows painting, the
# palette this session painted is handed back once.
__sereno_follow() {
    local on=
    __sereno_settings
    case $__sereno_inband in
        always) on=1 ;;
        profile) on=$__sereno_profile ;;
    esac
    if [ -z "$on" ]; then
        if [ -n "$__sereno_last" ]; then __sereno_last=; __sereno_paint reset; fi
        return 0
    fi
    __sereno_signals || return 0
    __sereno_resolve "$__sereno_mode" "$__sereno_flavor" "$__sereno_hc" "$__sereno_hcl" "$__sereno_light"
    if [ "$__sereno_variant" != "$__sereno_last" ]; then
        __sereno_last=$__sereno_variant
        __sereno_paint "$__sereno_variant"
    fi
    return 0
}

# Finds the branch of the repository that holds the directory $1 without
# running git: walk up to a .git directory, or to a .git file whose gitdir
# line names one, and read HEAD there. The result goes to __sereno_branch,
# empty when there is none.
__sereno_find_branch() {
    local dir=$1 git= line= head=
    __sereno_branch=
    while :; do
        git=$dir/.git
        if [ -d "$git" ]; then head=$git/HEAD; break; fi
        if [ -f "$git" ] && [ -r "$git" ]; then
            line=
            IFS= read -r line 2> /dev/null < "$git" || :
            line=${line%$'\r'}
            case $line in
                'gitdir: '*)
                    line=${line#gitdir: }
                    case $line in /*|[A-Za-z]:[\\/]*) ;; *) line=$dir/$line ;; esac
                    if [ -d "$line" ]; then head=$line/HEAD; break; fi ;;
            esac
        fi
        [ -n "$dir" ] || break
        case $dir in */*) dir=${dir%/*} ;; *) dir= ;; esac
    done
    if [ -z "$head" ] || [ ! -f "$head" ] || [ ! -r "$head" ]; then return 0; fi
    line=
    IFS= read -r line 2> /dev/null < "$head" || :
    line=${line%$'\r'}
    case $line in
        'ref: refs/heads/'*) __sereno_branch=${line#ref: refs/heads/} ;;
        *) __sereno_branch=${line:0:7} ;;
    esac
    __sereno_branch=${__sereno_branch//[[:cntrl:]]/}
    return 0
}

# One entry of LS_COLORS whose value may hold a background. $1 is the key and
# $2 the value; __sereno_reply receives the value to use. A background
# parameter is 40-47 or 100-107; the numbers after 38 and 48 are colour
# arguments and are not looked at. The file types ls paints with a background
# by default get a foreground of their own, any other entry just loses the
# parameter.
__sereno_ls_value() {
    local rest=$2 p= out= sep= skip=0 bad=
    while :; do
        p=${rest%%;*}
        if [ "$skip" = 9 ]; then
            case $p in 5) skip=1 ;; 2) skip=3 ;; *) skip=0 ;; esac
            out=$out$sep$p sep=';'
        elif [ "$skip" -gt 0 ]; then
            skip=$((skip - 1))
            out=$out$sep$p sep=';'
        else
            case $p in
                4[0-7]|10[0-7]) bad=1 ;;
                38|48) skip=9; out=$out$sep$p sep=';' ;;
                *) out=$out$sep$p sep=';' ;;
            esac
        fi
        case $rest in *';'*) rest=${rest#*;} ;; *) break ;; esac
    done
    __sereno_reply=$2
    [ -n "$bad" ] || return 0
    __sereno_reply=$out
    case $1 in
        su) __sereno_reply='1;31' ;;
        sg|bd|cd) __sereno_reply='1;33' ;;
        tw|ow|st) __sereno_reply='4;34' ;;
        ca|or|mi) __sereno_reply=31 ;;
        pi) __sereno_reply=33 ;;
        do) __sereno_reply='1;35' ;;
    esac
    return 0
}

# Colours of ls. GNU ls paints some file types as a foreground slot on a
# background slot, which cannot be read on any Serena variant; these settings
# use foregrounds only. Nothing else about ls is touched. A variable that is
# set but empty counts as unset, because that is how ls takes it: it then
# uses its built-in table, backgrounds and all. The variables are set through
# __sereno_set, which says nothing when one of them is read-only.
__sereno_ls_colors() {
    local rest= out= sep= entry= key= val=
    if [ -z "${LS_COLORS-}" ]; then
        __sereno_set LS_COLORS 'di=1;34:ln=36:so=35:pi=33:ex=1;32:bd=1;33:cd=1;33:su=1;31:sg=1;33:tw=4;34:ow=4;34:st=4;34:or=31:mi=31' && export LS_COLORS
    else
        rest=$LS_COLORS
        while :; do
            entry=${rest%%:*}
            case $entry in
                *=*)
                    key=${entry%%=*} val=${entry#*=}
                    case ";$val;" in
                        *';4'[0-7]';'*|*';10'[0-7]';'*)
                            __sereno_ls_value "$key" "$val"
                            entry=$key=$__sereno_reply ;;
                    esac ;;
            esac
            out=$out$sep$entry sep=:
            case $rest in *:*) rest=${rest#*:} ;; *) break ;; esac
        done
        if [ "$out" != "$LS_COLORS" ]; then __sereno_set LS_COLORS "$out" || :; fi
    fi
    if [ -z "${LSCOLORS-}" ]; then
        __sereno_set LSCOLORS exfxcxdxbxDxDxBxDxExEx && export LSCOLORS
    fi
    return 0
}

# Sets __sereno_reply to 1 when the first byte of the file $1 is not zero,
# else to 0.
__sereno_byte() {
    emulate -L zsh
    setopt no_multibyte
    local b=
    __sereno_reply=0
    [[ -f $1 && -r $1 ]] || return 0
    read -r -k 1 -u 0 b < "$1" || :
    if [[ -n $b ]] && (( #b != 0 )); then __sereno_reply=1; fi
    return 0
}

# Sets the variable $1 of the user to $2 and returns 1 when it cannot be set.
# An assignment to a read-only variable makes zsh print an error and abandon
# what it was running; the type of the variable is looked at first.
__sereno_set() {
    emulate -L zsh
    [[ ${(Pt)1} == *readonly* ]] && return 1
    typeset -g "$1=$2"
}

# Runs before every prompt. zsh keeps the exit status of the last command for
# the prompt itself, so %(?...) below still sees it.
__sereno_precmd() {
    emulate -L zsh
    if [[ -n $__sereno_term ]]; then __sereno_follow; fi
    if [[ -n $__sereno_show ]]; then
        # A PROMPT or psvar that is read-only keeps the prompt it has, for
        # the session.
        if [[ ${(t)PROMPT} == *readonly* || ${(t)psvar} == *readonly* ]]; then
            __sereno_show=
            return 0
        fi
        __sereno_find_branch "$PWD"
        # The branch reaches the prompt through psvar, which is shown as
        # text: a branch called like a command substitution is not run.
        psvar[1]=$__sereno_branch
        PROMPT='%F{4}%~%f%(1V. %F{5}%1v%f.)'$'\n''%(?.%F{2}.%F{1})>%f '
    fi
    return 0
}

__sereno_init() {
    emulate -L zsh
    typeset -g __sereno_term= __sereno_profile= __sereno_show= __sereno_last= __sereno_branch=
    typeset -g __sereno_hc=0 __sereno_hcl=0 __sereno_light=0 __sereno_variant= __sereno_reply=
    typeset -g __sereno_mode=auto __sereno_flavor=soft __sereno_inband=profile __sereno_prompt_on=on

    # Where the signals of the system can be read: the registry as an MSYS
    # zsh shows it. Elsewhere there is nothing to follow.
    typeset -g __sereno_reg=${SERENO_REG_ROOT:-/proc/registry/HKEY_CURRENT_USER}
    [[ -d $__sereno_reg ]] || __sereno_reg=

    if [[ -n ${WT_SESSION-} && ${TERM_PROGRAM-} != vscode ]]; then __sereno_term=1; fi
    if [[ ${SERENO_SESSION-} = wt-profile ]]; then __sereno_profile=1; fi
    case ${WT_PROFILE_ID-} in '{'6[bB]2[dD]8[bB]49-31[dD]3-5[dD][eE][aA]-9087-93[cC]26[eE]1[aA]033[eE]'}') __sereno_profile=1 ;; esac

    __sereno_settings
    if [[ $__sereno_prompt_on = on ]] && (( ! ${+STARSHIP_SHELL} && ! ${+POSH_PID} && ! ${+POSH_THEME} && ! ${+ZSH} && ! ${+P9K_VERSION} )); then
        __sereno_show=1
    fi

    # A precmd_functions that is read-only cannot take the hook: the session
    # then keeps its prompt and does not follow the system.
    if [[ -n $__sereno_show$__sereno_term ]]; then
        if [[ ${(t)precmd_functions} == *readonly* ]]; then
            __sereno_show= __sereno_term=
        else
            typeset -ga precmd_functions
            precmd_functions+=(__sereno_precmd)
        fi
    fi

    __sereno_ls_colors

    # The sereno command, when the install script placed it in ROOT/bin and
    # no program of that name is on PATH. PATH itself is not touched.
    if [[ -f $__sereno_root/bin/sereno || -f $__sereno_root/bin/sereno.exe ]]; then
        if ! whence -p sereno > /dev/null 2>&1; then
            function sereno { "$__sereno_root/bin/sereno" "$@" }
        fi
    fi
    return 0
}
__sereno_init
unfunction __sereno_init __sereno_ls_colors __sereno_ls_value __sereno_set
