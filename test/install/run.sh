#!/bin/sh
# Tests of the install script for macOS and Linux (scripts/install.sh.tmpl)
# against a folder that stands in for a release. No network and no real home:
#
#   sh test/install/run.sh --release DIR
#
# DIR is a folder written by `go run ./internal/tools/release`, which renders
# install.sh next to the six archives and checksums.txt. scripts/test.sh makes
# one from stand-in programs: the install script never starts the program, so
# any file will do in its place, and no built sereno is needed here.
#
# Every start of the script goes through `env -i` with a temporary folder as
# HOME and as TMPDIR, and with SERENO_INSTALL_FROM naming the release folder.
# In front of PATH stands a folder with three stand-ins: uname, which answers
# what a case asks for, so that every system and processor is tried on any
# machine; and curl and wget, which leave a mark and fail, so that a test that
# reached for the network would be seen. One case lets the script download:
# there the stand-in curl copies from the release folder and records the
# address it was given.
#
# The script is run by the sh that runs this file: dash on Ubuntu, bash as sh
# on macOS, the sh of Git for Windows there. The exit code is 1 when any
# check failed.

set -u
here=$(cd "$(dirname "$0")" && pwd) || exit 2
repo=$(cd "$here/../.." && pwd) || exit 2

release=
while [ $# -gt 0 ]; do
    case $1 in
        --release) release=$2; shift 2 ;;
        *) printf 'run.sh: unknown argument %s\n' "$1" >&2; exit 2 ;;
    esac
done
if [ -z "$release" ] || [ ! -f "$release/install.sh" ]; then
    printf 'run.sh: --release DIR is required, and DIR must hold install.sh\n' >&2
    exit 2
fi
release=$(cd "$release" && pwd) || exit 2
version=$(sed -n "s/^version='\\(.*\\)'\$/\\1/p" "$release/install.sh")
repository=$(sed -n "s/^repository='\\(.*\\)'\$/\\1/p" "$release/install.sh")

failed=0
# report STATUS NAME prints one check line; it is called with $? of the
# conditions written in front of it.
report() {
    if [ "$1" -eq 0 ]; then
        printf 'ok - %s\n' "$2"
    else
        printf 'not ok - %s\n' "$2"
        failed=1
        if [ -s "$out" ]; then sed 's/^/#   out: /' "$out"; fi
        if [ -s "$err" ]; then sed 's/^/#   err: /' "$err"; fi
    fi
}

tmp=$(mktemp -d "${TMPDIR:-/tmp}/sereno-install-test-XXXXXX") || exit 1
tmp=$(cd "$tmp" && pwd -P)
trap 'rm -rf "$tmp"' EXIT
tools=$tmp/tools
scratch=$tmp/scratch
out=$tmp/out
err=$tmp/err
mkdir -p "$tools" "$scratch"
: > "$out"
: > "$err"

cat > "$tools/uname" << 'EOF'
#!/bin/sh
case ${1-} in
    -s) printf '%s\n' "${FAKE_SYSTEM:-Linux}" ;;
    -m) printf '%s\n' "${FAKE_MACHINE:-x86_64}" ;;
    *) printf 'the stand-in uname knows -s and -m only\n' >&2; exit 1 ;;
esac
EOF
# curl: without FAKE_RELEASE it marks the attempt and fails. With it, it
# takes the role of the network: the address is recorded, and the file of
# that name is copied from the folder.
cat > "$tools/curl" << 'EOF'
#!/bin/sh
if [ -z "${FAKE_RELEASE-}" ]; then
    printf 'curl %s\n' "$*" >> "$FAKE_MARK"
    exit 7
fi
output=
address=
while [ $# -gt 0 ]; do
    case $1 in
        --output) output=$2; shift 2 ;;
        --proto | --proto-redir) shift 2 ;;
        --*) shift ;;
        *) address=$1; shift ;;
    esac
done
printf '%s\n' "$address" >> "$FAKE_MARK"
cp "$FAKE_RELEASE/${address##*/}" "$output"
EOF
cat > "$tools/wget" << 'EOF'
#!/bin/sh
printf 'wget %s\n' "$*" >> "$FAKE_MARK"
exit 7
EOF
chmod +x "$tools/uname" "$tools/curl" "$tools/wget"
mark=$tmp/network-mark

# What a case sets before it calls attempt: the script, its arguments, the
# home and the folder the archives come from.
script=$release/install.sh
arguments=
home=$tmp/home
from=$release

# attempt [VAR=VALUE...]: one run of the script. Standard output goes to
# $out, standard error to $err and the exit code to $code.
attempt() {
    # shellcheck disable=SC2086
    env -i PATH="$tools:$PATH" HOME="$home" TMPDIR="$scratch" SERENO_INSTALL_FROM="$from" FAKE_MARK="$mark" "$@" \
        sh "$script" $arguments > "$out" 2> "$err"
    code=$?
}
# new_home NAME: an empty home of its own for a case.
new_home() {
    home=$tmp/$1
    mkdir "$home"
}
# A listing of every folder and file below the home, with a checksum of the
# files.
tree() {
    (cd "$home" && find . -print | LC_ALL=C sort | while IFS= read -r entry; do
        if [ -f "$entry" ]; then printf '%s %s\n' "$entry" "$(cksum < "$entry")"; else printf '%s <folder>\n' "$entry"; fi
    done)
}
# member OS ARCH: the program inside the archive of a target, on standard
# output.
member() { tar -xzOf "$release/sereno_${version}_$1_$2.tar.gz" sereno; }
# placed FILE OS ARCH: FILE is the program of that target, byte for byte.
placed() { [ -f "$1" ] && member "$2" "$3" | cmp -s - "$1"; }
said() { grep -q -F -e "$1" "$out"; }       # standard output has the text
complained() { grep -q -F -e "$1" "$err"; } # standard error has the text
tidy() { [ -z "$(ls -A "$scratch")" ]; }    # no temporary folder was left

printf '# install.sh of %s, run by sh on %s\n' "$version" "$(uname -s)"

# ---------------------------------------------------------------------------
# What the script refuses before it looks at anything.
new_home refusals
tree > "$tmp/tree.empty"

script=$repo/scripts/install.sh.tmpl
attempt
[ "$code" -eq 1 ] && complained 'this is the template of the install script' && tree | cmp -s - "$tmp/tree.empty"
report $? 'the template itself, not rendered by a release, installs nothing'
script=$release/install.sh

arguments=--help
attempt
[ "$code" -eq 0 ] && said 'Usage: sh install.sh [--uninstall]' && tree | cmp -s - "$tmp/tree.empty"
report $? '--help prints the usage and changes nothing'

arguments=--force
attempt
[ "$code" -eq 2 ] && complained 'unknown argument --force' && tree | cmp -s - "$tmp/tree.empty"
report $? 'an unknown argument is a usage error, exit code 2'
arguments=

attempt FAKE_SYSTEM=FreeBSD
[ "$code" -eq 1 ] && complained 'no sereno is built for FreeBSD'
first=$?
attempt FAKE_MACHINE=riscv64
[ "$first" -eq 0 ] && [ "$code" -eq 1 ] && complained 'no sereno is built for the processor riscv64'
first=$?
attempt FAKE_SYSTEM=MINGW64_NT-10.0-26200
[ "$first" -eq 0 ] && [ "$code" -eq 1 ] && complained 'use install.ps1' && tree | cmp -s - "$tmp/tree.empty"
report $? 'a system or a processor without a build is refused, and Windows is sent to install.ps1'

attempt HOME=
[ "$code" -eq 1 ] && complained 'HOME is not set to an absolute path'
first=$?
attempt SERENO_HOME=relative/folder
[ "$first" -eq 0 ] && [ "$code" -eq 1 ] && complained 'SERENO_HOME must be an absolute path' && tree | cmp -s - "$tmp/tree.empty"
report $? 'without a home folder, or with a relative SERENO_HOME, nothing is installed'

# ---------------------------------------------------------------------------
# A first install into the default folder, and the same again.
new_home default
printf 'export EDITOR=vi\n' > "$home/.bashrc"
printf 'export EDITOR=vi\n' > "$home/.zshrc"
printf 'PATH=$HOME/bin:$PATH\n' > "$home/.profile"
tree > "$tmp/tree.before"
target=$home/.local/share/sereno/bin/sereno

attempt
[ "$code" -eq 0 ] && placed "$target" linux amd64
report $? 'the program of linux/amd64 is placed in ~/.local/share/sereno/bin'
# Git for Windows has no permission bits: there a file may be run when its
# name or its first line says so, which a stand-in does not.
case $(uname -s) in
    MINGW* | MSYS* | CYGWIN*) printf 'skip - whether the program may be run: this file system keeps no permission bits\n' ;;
    *)
        [ -x "$target" ] && [ "$(find "$target" -perm -755 | wc -l | tr -d ' ')" = 1 ]
        report $? 'the program may be run by everybody (mode 755)'
        ;;
esac
{
    cat "$tmp/tree.before"
    printf './.local <folder>\n./.local/share <folder>\n./.local/share/sereno <folder>\n./.local/share/sereno/bin <folder>\n'
    printf './.local/share/sereno/bin/sereno %s\n' "$(cksum < "$target")"
} | LC_ALL=C sort > "$tmp/tree.wanted"
tree | cmp -s - "$tmp/tree.wanted"
report $? 'nothing else is written: no LICENSE, no README.md, and the start-up files are as they were'
said "sereno $version (linux/amd64) was placed at" && said "  $target" && said "  $target apply --dry-run" &&
    said 'sh install.sh --uninstall' && said "The SHA-256 of sereno_${version}_linux_amd64.tar.gz is the one of the release"
report $? 'it says where the program is, what was checked, the next command by its full path and how to remove it'
tidy && [ ! -e "$mark" ]
report $? 'its temporary folder is gone, and neither curl nor wget was called'

tree > "$tmp/tree.installed"
attempt
[ "$code" -eq 0 ] && said 'replaced the program that was at' && tree | cmp -s - "$tmp/tree.installed" && tidy
report $? 'a second run replaces the program with the same bytes and changes nothing else'

# ---------------------------------------------------------------------------
# What must stop it, with a program already in place: the program stays.
bad=$tmp/corrupted
mkdir "$bad"
cp "$release/sereno_${version}_linux_amd64.tar.gz" "$bad/"
printf 'x' >> "$bad/sereno_${version}_linux_amd64.tar.gz"
from=$bad
attempt
[ "$code" -eq 1 ] && complained 'is not the one of release' && complained 'nothing was installed' && complained '  expected ' && complained '  found    ' &&
    tree | cmp -s - "$tmp/tree.installed" && tidy
report $? 'an archive with one more byte is refused: the checksum is named, and the program that was there stays'

new_home corrupted-first
tree > "$tmp/tree.empty"
attempt
[ "$code" -eq 1 ] && complained 'nothing was installed' && tree | cmp -s - "$tmp/tree.empty" && tidy
report $? 'the same archive on a home without sereno leaves that home empty'

# An archive of the right name that is not the one of this release at all.
printf 'not an archive\n' > "$bad/sereno_${version}_linux_amd64.tar.gz"
attempt
[ "$code" -eq 1 ] && complained 'is not the one of release' && tree | cmp -s - "$tmp/tree.empty" && tidy
report $? 'a file that is no archive is refused by its checksum, before anything unpacks it'

from=$tmp/nowhere
mkdir "$from"
attempt
[ "$code" -eq 1 ] && complained "holds no sereno_${version}_linux_amd64.tar.gz" && tree | cmp -s - "$tmp/tree.empty" && tidy
report $? 'a folder without the archive installs nothing'
from=$release

# A folder where the program goes.
new_home in-the-way
mkdir -p "$home/.local/share/sereno/bin/sereno"
printf 'mine\n' > "$home/.local/share/sereno/bin/sereno/notes.txt"
tree > "$tmp/tree.in-the-way"
attempt
[ "$code" -eq 1 ] && complained 'is in the way and is not a file' && tree | cmp -s - "$tmp/tree.in-the-way" && tidy
report $? 'a folder under the name of the program is left alone, and nothing is installed'
new_home without-line
tree > "$tmp/tree.empty"

# A script whose list of checksums has no line for this system.
grep -v -F "sereno_${version}_linux_amd64.tar.gz" "$release/install.sh" > "$tmp/install-without-line.sh"
script=$tmp/install-without-line.sh
attempt
[ "$code" -eq 1 ] && complained "carries no checksum for sereno_${version}_linux_amd64.tar.gz" && tree | cmp -s - "$tmp/tree.empty" && tidy
first=$?
attempt FAKE_MACHINE=aarch64
[ "$first" -eq 0 ] && [ "$code" -eq 0 ] && placed "$home/.local/share/sereno/bin/sereno" linux arm64
report $? 'without a checksum line for the archive nothing is installed; the other systems of that script still are'
script=$release/install.sh

# ---------------------------------------------------------------------------
# Every system and processor takes its own archive.
all=0
for pair in Linux:x86_64:linux:amd64 Linux:amd64:linux:amd64 Linux:aarch64:linux:arm64 Linux:arm64:linux:arm64 Darwin:x86_64:darwin:amd64 Darwin:arm64:darwin:arm64; do
    system=${pair%%:*}
    rest=${pair#*:}
    machine=${rest%%:*}
    rest=${rest#*:}
    os=${rest%%:*}
    arch=${rest#*:}
    new_home "each-$system-$machine"
    attempt FAKE_SYSTEM="$system" FAKE_MACHINE="$machine"
    if [ "$code" -ne 0 ] || ! placed "$home/.local/share/sereno/bin/sereno" "$os" "$arch" || ! said "($os/$arch)"; then
        printf '#   %s on %s did not get the program of %s/%s\n' "$system" "$machine" "$os" "$arch"
        all=1
    fi
done
report "$all" 'Linux and Darwin on x86_64, amd64, aarch64 and arm64 each get the archive built for them'

# ---------------------------------------------------------------------------
# Where sereno's folder is: the rule of sereno itself.
new_home xdg
attempt XDG_DATA_HOME="$home/data"
[ "$code" -eq 0 ] && placed "$home/data/sereno/bin/sereno" linux amd64 && [ ! -e "$home/.local" ]
first=$?
attempt XDG_DATA_HOME=relative
[ "$first" -eq 0 ] && [ "$code" -eq 0 ] && placed "$home/.local/share/sereno/bin/sereno" linux amd64
report $? 'XDG_DATA_HOME moves the folder when it is absolute and is ignored when it is not'

new_home moved
moved="$home/o'neil \$HOME folder/sereno"
attempt SERENO_HOME="$moved" XDG_DATA_HOME="$home/data"
[ "$code" -eq 0 ] && placed "$moved/bin/sereno" linux amd64 && [ ! -e "$home/data" ] && [ ! -e "$home/.local" ]
report $? 'SERENO_HOME wins over both, also with a quote, a space and a dollar sign in its name'
# The next command is printed so that a shell reads the path back as it is.
line=$(grep -F ' apply --dry-run' "$out")
eval "set -- $line"
[ "$#" -eq 3 ] && [ "$1" = "$moved/bin/sereno" ] && [ "$2" = apply ] && [ "$3" = --dry-run ]
report $? 'the next command names that path in a form a shell takes'

# ---------------------------------------------------------------------------
# --uninstall.
new_home removal
arguments=--uninstall
attempt
[ "$code" -eq 0 ] && said 'Nothing to remove' && [ -z "$(ls -A "$home")" ]
report $? '--uninstall on a home without sereno removes nothing and creates nothing'

arguments=
attempt
tree > "$tmp/tree.installed"
root=$home/.local/share/sereno
printf '{}\n' > "$root/manifest.json"
arguments=--uninstall
attempt
[ "$code" -eq 1 ] && complained 'sereno is still applied here' && complained "$root/bin/sereno uninstall" && [ -f "$root/bin/sereno" ]
report $? '--uninstall leaves the program while a manifest says sereno is applied, and names the command to run first'
rm "$root/manifest.json"

printf 'mine\n' > "$root/bin/notes.txt"
attempt
[ "$code" -eq 0 ] && [ ! -e "$root/bin/sereno" ] && [ "$(cat "$root/bin/notes.txt")" = mine ] && said "$root/bin was left: it holds other files."
report $? '--uninstall removes the program and leaves a bin folder that holds a file of the user'
rm "$root/bin/notes.txt"

arguments=
attempt
arguments=--uninstall
attempt
[ "$code" -eq 0 ] && said "Removed $root/bin/sereno" && said "Removed $root/bin" && said "Removed $root" && [ ! -e "$root" ] && [ -d "$home/.local/share" ]
report $? '--uninstall removes the program, its bin folder and the empty folder of sereno; the folders above stay'
[ "$(find "$home" -type f | wc -l | tr -d ' ')" = 0 ] && tidy
report $? 'after it the home holds no file'
arguments=

# ---------------------------------------------------------------------------
# The download: the address, and the same check afterwards.
new_home download
rm -f "$mark"
attempt SERENO_INSTALL_FROM= FAKE_RELEASE="$release" FAKE_SYSTEM=Darwin FAKE_MACHINE=arm64
[ "$code" -eq 0 ] && placed "$home/.local/share/sereno/bin/sereno" darwin arm64 &&
    [ "$(cat "$mark")" = "https://github.com/$repository/releases/download/$version/sereno_${version}_darwin_arm64.tar.gz" ]
report $? 'without SERENO_INSTALL_FROM the archive is asked for at the address of the release, once, and checked'
rm -f "$mark"
new_home download-bad
attempt SERENO_INSTALL_FROM= FAKE_RELEASE="$bad"
[ "$code" -eq 1 ] && complained 'is not the one of release' && [ -z "$(ls -A "$home")" ] && tidy
report $? 'a download that is not the archive of the release is refused like a local one'
rm -f "$mark"
new_home download-fails
attempt SERENO_INSTALL_FROM=
[ "$code" -eq 1 ] && complained 'could not download https://github.com/' && [ -z "$(ls -A "$home")" ] && tidy
report $? 'a download that fails installs nothing'
rm -f "$mark"

# ---------------------------------------------------------------------------
# The real uname of this machine, once.
new_home real
env -i PATH="$PATH" HOME="$home" TMPDIR="$scratch" SERENO_INSTALL_FROM="$release" sh "$release/install.sh" > "$out" 2> "$err"
code=$?
case $(uname -s) in
    Linux | Darwin)
        [ "$code" -eq 0 ] && [ -f "$home/.local/share/sereno/bin/sereno" ] && said 'was placed at'
        report $? "this machine ($(uname -s) on $(uname -m)) is recognised and gets an archive"
        ;;
    *)
        [ "$code" -eq 1 ] && complained 'use install.ps1' && [ -z "$(ls -A "$home")" ]
        report $? "this machine ($(uname -s)) is sent to install.ps1"
        ;;
esac

if [ "$failed" -ne 0 ]; then printf 'test/install/run.sh: FAILED\n'; exit 1; fi
printf 'test/install/run.sh: all checks passed\n'
