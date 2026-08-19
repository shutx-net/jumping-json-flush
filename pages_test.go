package jjf

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// pagesConfig is the whole of the GitHub Pages setup. The site is built by GitHub
// from the default branch, so nothing in this repository runs the build and
// nothing here can fail loudly when it goes wrong: a document dropped from the
// site still renders perfectly on github.com, and the broken link only exists for
// the reader who followed it from the published site.
const pagesConfig = "_config.yml"

// excludeList captures the items of the exclude: block, which is a flat YAML list
// of quoted or bare scalars. A parser is not worth a dependency for that, and
// skills_test.go reads the skill frontmatter by hand for the same reason.
var excludeList = regexp.MustCompile(`(?m)^exclude:\n((?:[ \t]*-[ \t]*\S+\n)+)`)

// pagesLink captures the target of an inline markdown link.
var pagesLink = regexp.MustCompile(`\[[^\]]*\]\(([^)\s]+)`)

// pagesRemote matches a link target that names no file on disk: one with a URL
// scheme, a protocol-relative host, or a bare fragment.
var pagesRemote = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9+.-]*:|//|#)`)

// TestPagesConfigPublishesEveryLinkedFile is the check the published site cannot
// make for itself. Jekyll copies whatever it is not told to skip, so the exclude
// list is the one place where a file silently stops being served while every test
// that only looks at the file system keeps passing.
func TestPagesConfigPublishesEveryLinkedFile(t *testing.T) {
	patterns := pagesExcludes(t)

	for _, doc := range publishedMarkdown(t, patterns) {
		src, err := os.ReadFile(doc)
		if err != nil {
			t.Errorf("%v", err)
			continue
		}
		for _, m := range pagesLink.FindAllStringSubmatch(string(src), -1) {
			target, ok := localLinkTarget(m[1])
			if !ok {
				continue
			}
			resolved := filepath.ToSlash(filepath.Join(filepath.Dir(doc), target))
			if pagesExcluded(resolved, patterns) {
				t.Errorf("%s links to %q, which %s excludes from the site; the link works in the repository and 404s on the published page",
					doc, m[1], pagesConfig)
			}
		}
	}
}

// TestPagesConfigKeepsTheGoSourceOutOfTheSite is the other half. The exclude list
// is there because this repository is mostly Go and none of it belongs on a
// documentation site; an entry dropped from it would publish the whole tree
// without anything looking wrong.
func TestPagesConfigKeepsTheGoSourceOutOfTheSite(t *testing.T) {
	patterns := pagesExcludes(t)

	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Jekyll skips anything whose name starts with a dot on its own.
		if strings.HasPrefix(d.Name(), ".") && path != "." {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() && pagesExcluded(filepath.ToSlash(path)+"/", patterns) {
			return fs.SkipDir
		}
		if !d.IsDir() && filepath.Ext(path) == ".go" && !pagesExcluded(filepath.ToSlash(path), patterns) {
			t.Errorf("%s would be published as part of the site; add it, or the directory it is in, to the exclude list of %s",
				path, pagesConfig)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}

// pagesExcludes returns the exclude list of the Pages configuration.
func pagesExcludes(t *testing.T) []string {
	t.Helper()

	m := excludeList.FindStringSubmatch(read(t, pagesConfig))
	if m == nil {
		t.Fatalf("%s has no exclude: block; without one Jekyll publishes the whole repository, and this check would pass by no longer reading anything",
			pagesConfig)
	}

	var patterns []string
	for _, line := range strings.Split(strings.TrimSpace(m[1]), "\n") {
		item := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "-"))
		patterns = append(patterns, strings.Trim(item, `"'`))
	}
	return patterns
}

// publishedMarkdown returns every markdown file the site serves as a page.
func publishedMarkdown(t *testing.T, patterns []string) []string {
	t.Helper()

	var found []string
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(d.Name(), ".") && path != "." {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		slash := filepath.ToSlash(path)
		if d.IsDir() {
			if pagesExcluded(slash+"/", patterns) {
				return fs.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) == ".md" && !pagesExcluded(slash, patterns) {
			found = append(found, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(found) == 0 {
		t.Fatal("no markdown is published at all; this check is reading the wrong tree")
	}
	return found
}

// pagesExcluded reports whether the site leaves path out. A directory is passed
// with its trailing slash, the way it is written in the configuration.
func pagesExcluded(path string, patterns []string) bool {
	// Jekyll never publishes anything whose name starts with a dot, and that rule
	// is not written down anywhere in the configuration. It is what keeps
	// .github/ and .claude-plugin/ off the site, so a relative link into one of
	// them resolves in a clone and 404s on the published page.
	for _, segment := range strings.Split(strings.TrimSuffix(path, "/"), "/") {
		if strings.HasPrefix(segment, ".") && segment != "." && segment != ".." {
			return true
		}
	}

	for _, p := range patterns {
		switch {
		case strings.HasSuffix(p, "/"):
			if path == p || strings.HasPrefix(path, p) {
				return true
			}
		case strings.HasPrefix(p, "**/"):
			if ok, _ := filepath.Match(strings.TrimPrefix(p, "**/"), filepath.Base(path)); ok {
				return true
			}
		case strings.ContainsAny(p, "*?["):
			if ok, _ := filepath.Match(p, path); ok {
				return true
			}
		default:
			if path == p {
				return true
			}
		}
	}
	return false
}

// localLinkTarget reduces a link target to the path it names, reporting false when
// it names no local file at all.
func localLinkTarget(target string) (string, bool) {
	if pagesRemote.MatchString(target) {
		return "", false
	}
	path, _, _ := strings.Cut(target, "#")
	path, _, _ = strings.Cut(path, "?")
	return path, path != ""
}
