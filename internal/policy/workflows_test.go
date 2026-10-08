package policy

import (
	"regexp"
	"strings"
	"testing"
)

// The two workflows of the repository (T10, R41).
const (
	ciWorkflow      = ".github/workflows/ci.yml"
	releaseWorkflow = ".github/workflows/release.yml"
)

// job is one job of a workflow: its id and the lines below it.
type job struct {
	id   string
	text string
}

var jobID = regexp.MustCompile(`^  ([A-Za-z0-9_-]+):\s*$`)

// jobsOf splits the text of a workflow into its jobs, by indentation. The
// workflows of this repository are written with two spaces a level.
func jobsOf(t *testing.T, name, text string) []job {
	t.Helper()
	at := strings.Index(text, "\njobs:\n")
	if at < 0 {
		t.Fatalf("%s has no jobs", name)
	}
	var jobs []job
	for _, line := range strings.Split(text[at+len("\njobs:\n"):], "\n") {
		if match := jobID.FindStringSubmatch(line); match != nil {
			jobs = append(jobs, job{id: match[1]})
			continue
		}
		if len(jobs) > 0 {
			jobs[len(jobs)-1].text += line + "\n"
		}
	}
	return jobs
}

// permissionsOf lists what a block of YAML grants, as "name: level" lines,
// from the lines that follow its `permissions:` key at the given indent.
func permissionsOf(text, indent string) []string {
	var granted []string
	inside := false
	for _, line := range strings.Split(text, "\n") {
		switch {
		case line == indent+"permissions:":
			inside = true
		case inside && strings.HasPrefix(line, indent+"  ") && !strings.HasPrefix(line, indent+"   "):
			entry := strings.TrimSpace(line)
			if cut := strings.Index(entry, " #"); cut >= 0 {
				entry = strings.TrimSpace(entry[:cut])
			}
			granted = append(granted, entry)
		case inside:
			inside = false
		}
	}
	return granted
}

// TestWorkflowsPinTheirActionsAndLimitThemselves is R41 and T10 on the form
// of the workflow files: every action is named by a full commit with its
// version next to it; every job has a time limit; the repository is checked
// out without leaving its token on disk; nothing a stranger controls (the
// title of a pull request, the name of a branch) is pasted into a script;
// and no secret is asked for.
func TestWorkflowsPinTheirActionsAndLimitThemselves(t *testing.T) {
	uses := regexp.MustCompile(`^\s*(?:- )?uses:\s*(\S+)(.*)$`)
	pinned := regexp.MustCompile(`^actions/[a-z-]+@[0-9a-f]{40}$`)
	version := regexp.MustCompile(`^ # v\d+\.\d+\.\d+$`)
	for _, name := range []string{ciWorkflow, releaseWorkflow} {
		text := repositoryText(t, name)
		actions := 0
		for number, line := range strings.Split(text, "\n") {
			match := uses.FindStringSubmatch(line)
			if match == nil {
				continue
			}
			actions++
			if !pinned.MatchString(match[1]) {
				t.Errorf("%s:%d uses %s: every action is one of GitHub's own, named by a full commit", name, number+1, match[1])
			}
			if !version.MatchString(match[2]) {
				t.Errorf("%s:%d does not say in a comment which version that commit is (# vX.Y.Z)", name, number+1)
			}
		}
		if actions < 4 {
			t.Errorf("only %d actions found in %s; the pattern of this test no longer matches", actions, name)
		}
		if checkouts, kept := strings.Count(text, "uses: actions/checkout@"), strings.Count(text, "persist-credentials: false"); checkouts != kept {
			t.Errorf("%s checks the repository out %d times and switches the stored token off %d times", name, checkouts, kept)
		}
		for _, forbidden := range []string{"pull_request_target", "secrets.", "github.event.", "github.head_ref", "continue-on-error", "ACTIONS_ALLOW", "curl -k", "--insecure"} {
			if strings.Contains(text, forbidden) {
				t.Errorf("%s contains %q", name, forbidden)
			}
		}
		if !strings.Contains(text, "\nconcurrency:\n") || !strings.Contains(text, "\npermissions:") {
			t.Errorf("%s must limit its permissions and its concurrent runs at the top level", name)
		}
		jobs := jobsOf(t, name, text)
		if len(jobs) < 2 {
			t.Errorf("only %d jobs found in %s", len(jobs), name)
		}
		for _, j := range jobs {
			if !regexp.MustCompile(`(?m)^    timeout-minutes: \d+$`).MatchString(j.text) {
				t.Errorf("the job %s of %s has no timeout-minutes", j.id, name)
			}
			if !strings.Contains(j.text, "\n    runs-on: ") && !strings.HasPrefix(j.text, "    runs-on: ") {
				t.Errorf("the job %s of %s does not say where it runs", j.id, name)
			}
		}
	}
}

// TestCIWorkflowCoversTheTestPlan keeps the CI workflow to what T10 lists.
// It cannot tell whether a step passes; it tells when one was dropped.
func TestCIWorkflowCoversTheTestPlan(t *testing.T) {
	text := repositoryText(t, ciWorkflow)
	if granted := permissionsOf(text, ""); len(granted) != 1 || granted[0] != "contents: read" {
		t.Errorf("%s grants %v at the top level, want contents: read and nothing else", ciWorkflow, granted)
	}
	if strings.Contains(text, ": write") {
		t.Errorf("%s grants a write permission somewhere", ciWorkflow)
	}
	for what, piece := range map[string]string{
		"pushes to main":                            "\n    branches:\n      - main\n      - build-0.1\n",
		"pull requests":                             "\n  pull_request:\n",
		"superseded runs are cancelled":             "cancel-in-progress: true",
		"the toolchain is never fetched on the way": "GOTOOLCHAIN: local",
		"Windows":                 "- windows-latest",
		"macOS":                   "- macos-latest",
		"Linux":                   "- ubuntu-latest",
		"the current Go":          "go-version: '1.27.x'",
		"the floor of go.mod":     "go-version: '1.24.x'",
		"go vet":                  "run: go vet ./...",
		"go test":                 "run: go test ./... -count=1",
		"the tests under Go 1.24": "go build ./...\n          go test ./... -count=1",
		"the wrapper of Windows with the budgets":     `-File scripts\test.ps1 -SkipGo -Bench`,
		"the budgets under PowerShell 7 on Windows":   `pwsh -NoLogo -NoProfile -NonInteractive -File scripts\bench.ps1 -SkipBash`,
		"the wrapper of macOS and Linux with budgets": "sh scripts/test.sh --skip-go --bench",
		"zsh on Ubuntu":                                 "sudo apt-get install --yes --no-install-recommends zsh",
		"the image version in the log":                  `echo "image: ${ImageOS:-unknown} ${ImageVersion:-unknown}"`,
		"gofmt":                                         "unformatted=$(gofmt -l .)",
		"the dependency rule of R1":                     "go list -deps ./cmd/sereno)",
		"neither net nor net/http":                      "grep -x -e net -e net/http",
		"six builds without cgo":                        "CGO_ENABLED=0 GOOS=${target%/*} GOARCH=${target#*/} go build ./...",
		"the race detector":                             "go test -race -count=1 ./internal/engine ./test/e2e",
		"the palette, imported again":                   "go run ./internal/tools/importpalette --themes",
		"the palette, compared":                         `cmp "$work/serena.json" "$file"`,
		"the tag of the palette still names its commit": `git ls-remote "https://$repo" "refs/tags/$tag^{}"`,
		"the release build, dry run":                    `sh scripts/release.sh "$VERSION" dist`,
		"the checksums of the dry run":                  "sha256sum --check checksums.txt",
		"the install script tests on real archives":     "sh test/install/run.sh --release dist",
		"the installed program, smoke-run":              `sh scripts/smoke.sh "$home/.local/share/sereno/bin/sereno" --version "$VERSION"`,
		"install.ps1 under Windows PowerShell 5.1":      `-File test\install\run.ps1 -Release $dist`,
		"install.ps1 under PowerShell 7":                "pwsh -NoLogo -NoProfile -NonInteractive -File test/install/run.ps1 -Release",
	} {
		if !strings.Contains(text, piece) {
			t.Errorf("%s no longer has: %s (%q)", ciWorkflow, what, piece)
		}
	}
	if n := strings.Count(text, "windows/amd64 windows/arm64 darwin/amd64 darwin/arm64 linux/amd64 linux/arm64"); n != 2 {
		t.Errorf("%s names the six targets %d times, want twice: for the dependency rule and for the builds", ciWorkflow, n)
	}
	ids := ""
	for _, j := range jobsOf(t, ciWorkflow, text) {
		ids += j.id + " "
	}
	if ids != "test checks floor palette release install " {
		t.Errorf("%s has the jobs %q", ciWorkflow, ids)
	}
}

// TestReleaseWorkflowBuildsADraftWithTheRightsItNeeds is R41 on the release
// workflow: it starts from a tag v*, refuses a tag that is not a version
// before anything is built, gives each job only the rights it needs, and
// ends in a draft that the owner publishes. The job that may write never
// checks the repository out and runs none of its code.
func TestReleaseWorkflowBuildsADraftWithTheRightsItNeeds(t *testing.T) {
	text := repositoryText(t, releaseWorkflow)
	if !strings.Contains(text, "\non:\n  push:\n    tags:\n      - 'v*'\n\n") {
		t.Errorf("%s does not start from a tag v*, and from that alone", releaseWorkflow)
	}
	if !strings.Contains(text, "\npermissions: {}\n") {
		t.Errorf("%s must grant nothing at the top level; each job names its own rights", releaseWorkflow)
	}
	jobs := jobsOf(t, releaseWorkflow, text)
	if len(jobs) != 2 || jobs[0].id != "build" || jobs[1].id != "publish" {
		t.Fatalf("%s has the jobs %v, want build and publish", releaseWorkflow, jobs)
	}
	build, publish := jobs[0].text, jobs[1].text

	if granted := permissionsOf(build, "    "); len(granted) != 1 || granted[0] != "contents: read" {
		t.Errorf("the build job is granted %v, want contents: read and nothing else", granted)
	}
	check := strings.Index(build, `go run ./internal/tools/release --version "$GITHUB_REF_NAME" --check`)
	pack := strings.Index(build, `sh scripts/release.sh "$GITHUB_REF_NAME" dist`)
	if check < 0 || pack < 0 || check > pack {
		t.Error("the build job must refuse a tag that is not a version before it builds anything")
	}
	if tests := strings.Index(build, "go test ./... -count=1"); tests < 0 || tests > pack {
		t.Error("the build job must run the tests before it builds the release")
	}
	if !strings.Contains(build, `--version "$GITHUB_REF_NAME"`+"\n") && !strings.Contains(build, `sh scripts/smoke.sh "$home/.local/share/sereno/bin/sereno" --version "$GITHUB_REF_NAME"`) {
		t.Error("the build job must run the program it built and compare its version with the tag")
	}

	granted := strings.Join(permissionsOf(publish, "    "), ", ")
	if granted != "contents: write, id-token: write, attestations: write" {
		t.Errorf("the publish job is granted %q, want the three rights of R41 and nothing else", granted)
	}
	if !strings.Contains(publish, "    needs: build\n") {
		t.Error("the publish job must wait for the build job")
	}
	for what, piece := range map[string]string{
		"a checkout of the repository": "actions/checkout",
		"the Go toolchain":             "actions/setup-go",
		"a script of the repository":   "scripts/",
		"the go command":               "go run",
	} {
		if strings.Contains(publish, piece) {
			t.Errorf("the publish job, which may write, has %s (%s)", what, piece)
		}
	}
	attest := strings.Index(publish, "uses: actions/attest-build-provenance@")
	release := strings.Index(publish, `gh release create "$GITHUB_REF_NAME" dist/* --draft --verify-tag`)
	if attest < 0 || release < 0 || attest > release {
		t.Error("the publish job must attest the files and then create a draft release of the tag, checking that the tag exists")
	}
	if !strings.Contains(publish, "subject-path: dist/*\n") {
		t.Error("the attestation must cover every file of the release")
	}
	if strings.Count(text, "gh release") != 1 || strings.Contains(text, "--draft=false") || strings.Contains(text, "gh release edit") {
		t.Errorf("%s must create one draft release and never publish it", releaseWorkflow)
	}
}

// TestDependabotWatchesTheActions: the pinned commits are kept up to date by
// proposals, not by hand.
func TestDependabotWatchesTheActions(t *testing.T) {
	text := repositoryText(t, ".github/dependabot.yml")
	for _, piece := range []string{"version: 2\n", "package-ecosystem: github-actions\n", "directory: /\n", "interval: weekly\n"} {
		if !strings.Contains(text, piece) {
			t.Errorf(".github/dependabot.yml has no %q", strings.TrimSpace(piece))
		}
	}
}
