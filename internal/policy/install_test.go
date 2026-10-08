package policy

import (
	"regexp"
	"strings"
	"testing"
)

// The templates of the install scripts of a release (R41) and the script that
// builds a release from them.
const (
	installPowerShell = "scripts/install.ps1.tmpl"
	installShell      = "scripts/install.sh.tmpl"
	releaseScript     = "scripts/release.sh"
)

// codeLines returns the lines of a script that are not comments, with their
// numbers.
func codeLines(text string) map[int]string {
	lines := map[int]string{}
	for number, line := range strings.Split(text, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			lines[number+1] = line
		}
	}
	return lines
}

// TestInstallScriptsArePlainText: Windows PowerShell 5.1 reads a script
// without a byte order mark in the code page of the machine, and sh reads
// bytes; printable ASCII with line feeds is the same file everywhere.
func TestInstallScriptsArePlainText(t *testing.T) {
	for _, name := range []string{installPowerShell, installShell, releaseScript} {
		text := repositoryText(t, name)
		for i := 0; i < len(text); i++ {
			if b := text[i]; b != '\n' && (b < 0x20 || b > 0x7e) {
				t.Errorf("%s: byte %#x at offset %d is not printable ASCII", name, b, i)
				break
			}
		}
		if !strings.HasSuffix(text, "\n") {
			t.Errorf("%s does not end with a line feed", name)
		}
	}
	if !strings.HasPrefix(repositoryText(t, installShell), "#!/bin/sh\n") {
		t.Errorf("%s must be a script for sh", installShell)
	}
}

// TestInstallScriptsPlaceTheProgramAndNothingElse is R41 on the two templates:
// an install script places the program, and changes nothing else. The code of
// each is searched for the ways a script has of reaching further: PATH, a
// start-up file or a profile, the registry, a stored policy, more rights, a
// scheduled or a started program, code that is evaluated. It also must not
// start the program it has just placed.
func TestInstallScriptsPlaceTheProgramAndNothingElse(t *testing.T) {
	forbidden := map[string]map[string]string{
		installShell: {
			"a call for more rights":             `\b(sudo|doas|su|pkexec)\b`,
			"an append to a file":                `>>`,
			"a start-up file of a shell":         `\.(bashrc|bash_profile|bash_login|profile|zshrc|zshenv|zprofile|inputrc)\b|/etc/|fish`,
			"a change of PATH":                   `(^|[\s;])(export\s+)?PATH=`,
			"code that is evaluated or sourced":  `(^|[\s;&|])(eval|source|exec)\s|(^|[\s;&|])\.\s+["$/~A-Za-z]`,
			"a link, an owner or a special mode": `\b(ln|chown|chgrp|setfacl|xattr)\b|chmod\s+[0-7]*[+]?s|chmod\s+[2467][0-7]{3}`,
			"a service or a scheduled job":       `\b(crontab|systemctl|launchctl)\b`,
			"the program, started":               `(^|[;&|]\s*|\b(then|do|else)\s+)"?\$(target|staged|bin/sereno)"?(\s|$)`,
			"a download handed to a shell":       `\|\s*(sh|bash|zsh)\b`,
		},
		installPowerShell: {
			"a change of the environment":        `(?i)SetEnvironmentVariable|\bsetx\b|\$env:PATH\s*=|\[Environment\]::Set`,
			"the registry":                       `(?i)HKCU:|HKLM:|HKEY_|Microsoft\.Win32|Set-ItemProperty|New-ItemProperty|\breg(\.exe)?\s+(add|delete|import)`,
			"a stored policy or more rights":     `(?i)Set-ExecutionPolicy|-Verb\s+RunAs|Unblock-File`,
			"a profile":                          `(?i)\$PROFILE\b|Add-Content|Set-Content|Out-File`,
			"a started or scheduled program":     `(?i)Start-Process|Start-Job|Register-\w+|New-Service|schtasks|Invoke-Item|\bsc(\.exe)?\s`,
			"code that is evaluated":             `(?i)Invoke-Expression|\biex\b|ScriptBlock\]::Create|Add-Type\s+-TypeDefinition|\bImport-Module\b`,
			"the program, started":               `&\s*\$(target|script:staged|unpacked)\b`,
			"a link":                             `(?i)SymbolicLink|Junction|HardLink|mklink`,
			"a redirection that appends to file": `>>`,
		},
	}
	for name, rules := range forbidden {
		code := codeLines(repositoryText(t, name))
		if len(code) < 60 {
			t.Fatalf("only %d lines of code found in %s", len(code), name)
		}
		for what, pattern := range rules {
			expression := regexp.MustCompile(pattern)
			for number, line := range code {
				if found := expression.FindString(line); found != "" {
					t.Errorf("%s:%d has %s (%q):\n  %s", name, number, what, strings.TrimSpace(found), strings.TrimSpace(line))
				}
			}
		}
	}
}

// TestInstallScriptsCheckBeforeTheyUnpack: the SHA-256 of the archive is
// compared with the value the script carries before anything opens the
// archive, the archive comes from the release the script belongs to, and only
// the one file that is the program leaves it.
func TestInstallScriptsCheckBeforeTheyUnpack(t *testing.T) {
	const address = `https://github.com/$repository/releases/download/$version/$asset`
	for name, order := range map[string][]string{
		installShell: {
			`asset=sereno_${version}_${os}_${arch}.tar.gz`,
			`if [ -z "$expected" ]; then`,
			`origin=` + address,
			`if [ "$actual" != "$expected" ]; then`,
			`tar -xzf "$tmp/$asset" -C "$tmp/unpacked" sereno `,
			`mv -f "$staged" "$target"`,
		},
		installPowerShell: {
			`$asset = "sereno_${version}_windows_${arch}.zip"`,
			`if (-not $expected) { throw`,
			`$origin = "` + address + `"`,
			`if ($actual -cne $expected) {`,
			`[IO.Compression.ZipFile]::OpenRead($archive)`,
			`if ($candidate.FullName -ceq 'sereno.exe')`,
			`Move-Item -LiteralPath $script:staged -Destination $target -Force`,
		},
	} {
		text := repositoryText(t, name)
		last := -1
		for _, piece := range order {
			at := strings.Index(text, piece)
			switch {
			case at < 0:
				t.Errorf("%s no longer has: %s", name, piece)
			case at < last:
				t.Errorf("%s has this before the step that must come first: %s", name, piece)
			}
			if at > last {
				last = at
			}
		}
		// The placeholders the release build fills in, each where a value is
		// given: once as the value of its variable.
		for _, assignment := range []string{"'__VERSION__'\n", "'__REPOSITORY__'\n", "\n__CHECKSUMS__\n"} {
			if strings.Count(text, assignment) != 1 {
				t.Errorf("%s must give %s once", name, strings.TrimSpace(assignment))
			}
		}
		// A file that was not rendered installs nothing: the test is on the
		// shape of a version, never on the placeholder, which the rendering
		// would replace on both sides of a comparison.
		if !strings.Contains(text, "this is the template of the install script") {
			t.Errorf("%s does not refuse to run as a template", name)
		}
	}
}

// TestTheReleaseScriptBuildsAndNeverRuns: scripts/release.sh builds the six
// programs for a release and hands them to the tool that packs them. It never
// starts what it built (five of the six could not run on the machine that
// builds them, and the sixth has no sandbox there), and it publishes nothing.
func TestTheReleaseScriptBuildsAndNeverRuns(t *testing.T) {
	text := repositoryText(t, releaseScript)
	for number, line := range codeLines(text) {
		if !strings.Contains(line, "$work") {
			continue
		}
		allowed := strings.HasPrefix(line, `work=$(mktemp -d `) || strings.HasPrefix(line, `trap 'rm -rf "$work"' EXIT`) ||
			strings.Contains(line, ` -o "$work/${os}_$arch/$name" ./cmd/sereno`) || strings.Contains(line, ` --binaries "$work" --out "$out" `)
		if !allowed {
			t.Errorf("%s:%d uses the folder of the built programs other than to build into it and to pack from it:\n  %s", releaseScript, number, strings.TrimSpace(line))
		}
	}
	for what, piece := range map[string]string{
		"the check of the version, first":  `go run ./internal/tools/release --version "$version" --check`,
		"the build line of R41":            `CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -ldflags "-s -w -X main.version=$version" -o `,
		"the six targets":                  "for target in darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64; do",
		"the repository of the downloads":  `--repository "${GITHUB_REPOSITORY:-joaooliveira10/serena-terminal}"`,
		"a stop at the first failing step": "\nset -eu\n",
	} {
		if !strings.Contains(text, piece) {
			t.Errorf("%s no longer has %s: %s", releaseScript, what, piece)
		}
	}
	for _, forbidden := range []string{"gh ", "git push", "git tag", "curl", "wget", "sudo"} {
		for number, line := range codeLines(text) {
			if strings.Contains(line, forbidden) {
				t.Errorf("%s:%d has %q: the script builds a folder and nothing else", releaseScript, number, strings.TrimSpace(forbidden))
			}
		}
	}
}
