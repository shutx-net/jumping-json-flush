// Package jjf holds no Go code. The tests here guard install.sh, the one liner
// installer served from the default branch, against the release workflow whose
// assets it downloads.
//
// The script itself is shell and is linted by shellcheck in CI; nothing in it is
// executed from Go. What is checked here is the handful of facts the two files
// have to agree on and that no compiler, linter or reviewer reliably sees: the
// list of published targets, the name of every archive, the shape of a release
// tag, and the checksums file the installer refuses to skip. Rename an asset on
// the release side and the installer keeps requesting a URL that has stopped
// existing, with nothing to notice it until a user runs the command from the
// README.
//
// The install documentation is read for the same reason. A curl one liner nobody
// can copy is not an installation method, and it is the one part of this that is
// published to people rather than to machines. The detail lives under docs/, away
// from the READMEs, which is one more place for the script and what is written
// about it to drift apart.
package jjf

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const (
	installScript   = "install.sh"
	releaseWorkflow = ".github/workflows/release.yml"
	goMod           = "go.mod"
)

// Every document that shows the install command. The READMEs keep the one liner
// because it is the first thing a reader looks for; the pair below carries the
// rest. All four have to name the URL the script is actually served from.
var commandDocs = []string{
	"README.md", "README.ja.md",
	filepath.Join("docs", "install.md"), filepath.Join("docs", "install.ja.md"),
}

// The installation documentation the READMEs hand off to, one file per language.
// This is the only place the options are written out for a reader.
var installDocs = []string{
	filepath.Join("docs", "install.md"), filepath.Join("docs", "install.ja.md"),
}

// The constants install.sh declares, each written as NAME='value' on one line of
// its own, and the two shell assignments that build an archive name from them.
var (
	installRepo    = regexp.MustCompile(`(?m)^REPO='([^']*)'$`)
	installBinary  = regexp.MustCompile(`(?m)^BINARY='([^']*)'$`)
	installTargets = regexp.MustCompile(`(?m)^TARGETS='([^']*)'$`)
	installTag     = regexp.MustCompile(`(?m)^TAG_PATTERN='([^']*)'$`)
	installStem    = regexp.MustCompile(`(?m)^\s*stem="([^"]+)"$`)
	installSuffix  = regexp.MustCompile(`\$\{stem\}(\.[a-z.]+)"`)
)

// The same four values on the release side. The tag pattern is matched on its
// leading ^v, because the workflow runs a second grep -Eq for the plugin digest
// that would otherwise be picked up instead.
var (
	workflowTargets = regexp.MustCompile(`for target in ([^;]+); do`)
	workflowTag     = regexp.MustCompile(`grep -Eq '(\^v[^']+)'`)
	workflowName    = regexp.MustCompile(`(?m)^\s*name="([^"]+)"$`)
	workflowSuffix  = regexp.MustCompile(`\$\{name\}(\.[a-z.]+)"`)
)

// modulePath reads the module directive, the authoritative spelling of this
// repository's location.
var modulePath = regexp.MustCompile(`(?m)^module\s+(\S+)$`)

// installUsage captures the body of the here-document usage() prints, and the two
// patterns pick the interface out of it: the long options and the environment
// variables that stand in for them.
var (
	installUsage = regexp.MustCompile(`(?s)usage\(\) \{.*?<<EOF\n(.*?)\nEOF\n`)
	usageOption  = regexp.MustCompile(`--[a-z][a-z-]*`)
	usageEnv     = regexp.MustCompile(`JJF_[A-Z_]+`)
)

// TestInstallerIsPOSIXSh pins the interpreter. The script is documented as
// something to pipe into sh, and jjf advertises that it runs on alpine, where
// there is no bash to fall back to.
func TestInstallerIsPOSIXSh(t *testing.T) {
	src := read(t, installScript)
	if want := "#!/bin/sh\n"; !strings.HasPrefix(src, want) {
		first, _, _ := strings.Cut(src, "\n")
		t.Errorf("%s starts with %q, want %q; the script has to run under a plain POSIX sh",
			installScript, first, strings.TrimSuffix(want, "\n"))
	}
}

// topLevel classifies a line that begins in column 1: everything the installer
// declares at the top level has to be inert.
var (
	topLevelAssignment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
	topLevelFunction   = regexp.MustCompile(`^[a-z_][a-z0-9_]*\(\) \{$`)
)

// inertTopLevel are the top level lines that execute something and are allowed to
// anyway, because they change nothing outside the shell that is already running
// the script.
var inertTopLevel = []string{"set -eu", "}"}

// heredocStart captures the terminator of a here-document. Its body is data being
// fed to a command that has already been classified, and it is written flush left
// so that the help text is not indented, which is exactly what the scan below
// would otherwise read as a top level statement.
var heredocStart = regexp.MustCompile(`<<-?['"]?([A-Za-z_][A-Za-z0-9_]*)['"]?`)

// TestInstallerDoesNothingUntilItIsComplete is what makes the documented
// `curl ... | sh` safe against a download that is cut off part way through. The
// shell reads a pipe in chunks and runs whatever complete commands it has, so if
// anything at the top level did work, half a script would do half the install.
// Every top level line is a constant or a function definition, and the call that
// starts the work is the last line of the file.
func TestInstallerDoesNothingUntilItIsComplete(t *testing.T) {
	lines := strings.Split(strings.TrimRight(read(t, installScript), "\n"), "\n")

	const entry = `main "$@"`
	if last := lines[len(lines)-1]; last != entry {
		t.Errorf("%s ends with %q, want %q as its last line", installScript, last, entry)
	}

	terminator := ""
	for i, line := range lines[:len(lines)-1] {
		if terminator != "" {
			if strings.TrimSpace(line) == terminator {
				terminator = ""
			}
			continue
		}
		if m := heredocStart.FindStringSubmatch(line); m != nil {
			terminator = m[1]
		}

		switch {
		case line == "" || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t"):
			// Blank, or the body of a function.
		case strings.HasPrefix(line, "#"):
		case slices.Contains(inertTopLevel, line):
		case topLevelAssignment.MatchString(line), topLevelFunction.MatchString(line):
		default:
			t.Errorf("%s:%d: %q runs at the top level; only constants and function definitions may, so that a truncated download does nothing",
				installScript, i+1, line)
		}
	}
}

// TestInstallerTargetsMatchTheReleaseWorkflow keeps the platforms the script will
// download for equal to the platforms the release builds. A target added to the
// workflow alone is one the installer refuses on a machine that has an archive
// waiting for it; one removed there alone is a 404 halfway through an install.
func TestInstallerTargetsMatchTheReleaseWorkflow(t *testing.T) {
	got := strings.Fields(capture(t, installScript, installTargets))
	want := strings.Fields(capture(t, releaseWorkflow, workflowTargets))

	if !slices.Equal(got, want) {
		t.Errorf("the published targets disagree:\n  %s: %s\n  %s: %s",
			installScript, strings.Join(got, " "), releaseWorkflow, strings.Join(want, " "))
	}
}

// TestInstallerArchiveNameMatchesTheReleaseWorkflow compares the name the two
// files build for the same archive. They spell the pieces differently, because
// one is driven by GOOS and GOARCH and the other by uname, so each is reduced to
// a common shape before the comparison.
func TestInstallerArchiveNameMatchesTheReleaseWorkflow(t *testing.T) {
	shape := strings.NewReplacer(
		"${BINARY}", capture(t, installScript, installBinary),
		"${goos}", "${os}",
		"${goarch}", "${arch}",
	)

	got := shape.Replace(capture(t, installScript, installStem))
	want := shape.Replace(capture(t, releaseWorkflow, workflowName))
	if got != want {
		t.Errorf("the archive name disagrees:\n  %s builds %q\n  %s builds %q",
			installScript, got, releaseWorkflow, want)
	}
}

// TestInstallerArchiveSuffixesMatchTheReleaseWorkflow does the same for the two
// extensions, which decide whether the installer reaches for tar or for unzip.
func TestInstallerArchiveSuffixesMatchTheReleaseWorkflow(t *testing.T) {
	got := captureAll(t, installScript, installSuffix)
	want := captureAll(t, releaseWorkflow, workflowSuffix)

	if !slices.Equal(got, want) {
		t.Errorf("the archive suffixes disagree:\n  %s unpacks %s\n  %s produces %s",
			installScript, strings.Join(got, ", "), releaseWorkflow, strings.Join(want, ", "))
	}
}

// TestInstallerTagPatternMatchesTheReleaseWorkflow keeps the two gates identical.
// A tag goes straight into a download URL, so the installer validates it for the
// same reason the workflow does before publishing under it.
func TestInstallerTagPatternMatchesTheReleaseWorkflow(t *testing.T) {
	got := capture(t, installScript, installTag)
	want := capture(t, releaseWorkflow, workflowTag)

	if got != want {
		t.Errorf("the release tag pattern disagrees:\n  %s: %s\n  %s: %s",
			installScript, got, releaseWorkflow, want)
	}
}

// TestInstallerRepoMatchesTheModulePath guards the one constant that decides
// every URL the script fetches. The module path is the same repository written
// once more, and this project has been renamed once already.
func TestInstallerRepoMatchesTheModulePath(t *testing.T) {
	got := capture(t, installScript, installRepo)
	want := strings.TrimPrefix(capture(t, goMod, modulePath), "github.com/")

	if got != want {
		t.Errorf("%s: REPO = %q, want %q, the module path of %s without its host",
			installScript, got, want, goMod)
	}
}

// TestReleaseWorkflowPublishesTheChecksumsTheInstallerVerifies covers the one
// asset the installer will not do without. It compares the digest of what it
// downloaded against checksums.txt and stops when the file is absent, so a
// release that stopped publishing it would install nothing at all.
func TestReleaseWorkflowPublishesTheChecksumsTheInstallerVerifies(t *testing.T) {
	const checksums = "checksums.txt"

	if !strings.Contains(read(t, installScript), checksums) {
		t.Fatalf("%s no longer reads %s; this check and the verification it guards are stale",
			installScript, checksums)
	}

	workflow := read(t, releaseWorkflow)
	for _, want := range []string{"> " + checksums, "dist/" + checksums} {
		if !strings.Contains(workflow, want) {
			t.Errorf("%s does not contain %q, so %s may no longer be built and uploaded; %s verifies against it and refuses to install without it",
				releaseWorkflow, want, checksums, installScript)
		}
	}
}

// TestDocsCarryTheInstallCommand checks that every document showing the command
// points at the URL the script is served from. It is a raw URL on the default
// branch and never a release asset: a URL with a tag in it would pin whoever
// copied the line to that release forever.
func TestDocsCarryTheInstallCommand(t *testing.T) {
	want := fmt.Sprintf("https://raw.githubusercontent.com/%s/main/%s",
		capture(t, installScript, installRepo), installScript)

	for _, path := range commandDocs {
		if !strings.Contains(read(t, path), want) {
			t.Errorf("%s does not document the install command; it has to contain %s", path, want)
		}
	}
}

// TestInstallDocsCoverEveryOption is the drift check the split created. The
// options are described in one place only now, so an option added to the script
// and nowhere else is an option nobody can find, and one removed from the script
// leaves the documentation offering something that no longer works.
func TestInstallDocsCoverEveryOption(t *testing.T) {
	usage := capture(t, installScript, installUsage)

	var names []string
	for _, re := range []*regexp.Regexp{usageOption, usageEnv} {
		for _, name := range re.FindAllString(usage, -1) {
			if !slices.Contains(names, name) {
				names = append(names, name)
			}
		}
	}
	slices.Sort(names)
	if len(names) == 0 {
		t.Fatalf("%s: no options found in the usage message; this check is reading the wrong thing", installScript)
	}

	for _, path := range installDocs {
		src := read(t, path)
		for _, name := range names {
			if !strings.Contains(src, name) {
				t.Errorf("%s does not document %s, which `%s --help` offers", path, name, installScript)
			}
		}
	}
}

// read returns the whole file at path.
func read(t *testing.T, path string) string {
	t.Helper()

	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v", err)
	}
	return string(src)
}

// capture returns the first submatch of re in the file at path. A pattern that
// stops matching is fatal rather than ignored: the line it reads has been
// rewritten, and the check it feeds would otherwise pass by no longer running.
func capture(t *testing.T, path string, re *regexp.Regexp) string {
	t.Helper()

	m := re.FindStringSubmatch(read(t, path))
	if m == nil {
		t.Fatalf("%s: nothing matches %s; the line this check reads has been rewritten", path, re)
	}
	return m[1]
}

// captureAll returns every distinct first submatch of re in the file at path,
// sorted so that the order the lines happen to appear in does not matter.
func captureAll(t *testing.T, path string, re *regexp.Regexp) []string {
	t.Helper()

	var found []string
	for _, m := range re.FindAllStringSubmatch(read(t, path), -1) {
		if !slices.Contains(found, m[1]) {
			found = append(found, m[1])
		}
	}
	if len(found) == 0 {
		t.Fatalf("%s: nothing matches %s; the lines this check reads have been rewritten", path, re)
	}
	slices.Sort(found)
	return found
}
