// The documentation site.
//
// docs/ is published with MkDocs, and mkdocs.yml carries the page map by hand
// because every generator that derives one automatically wants per-page front
// matter, which renders as a raw YAML table when the same file is read on
// github.com. A hand written map is the right trade for that, and this is what
// pays for it: nothing else notices a page that was added to docs/ and never
// listed, or an entry left pointing at a file that has been renamed.
//
// The site is also why several links in docs/ are absolute github.com URLs
// instead of relative paths. A relative link that leaves docs/ cannot be served,
// so those targets are named by URL - and a URL is exactly the kind of link that
// rots silently, which the last check here is for.
package jjf

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const (
	mkdocsConfig = "mkdocs.yml"
	docsDir      = "docs"
)

// navSection captures the nav: block, which runs to the first line that starts a
// new top level key.
var navSection = regexp.MustCompile(`(?m)^nav:\n((?:[ \t-].*\n?)*)`)

// navTarget captures the document each nav entry points at.
var navTarget = regexp.MustCompile(`(?m):\s*(\S+\.md)\s*$`)

// blobLink captures the repository path of a link into this repository on
// github.com. Only blob URLs name a file; a release asset or a raw URL does not
// have to exist in the working tree.
var blobLink = regexp.MustCompile(`https://github\.com/shutx-net/jumping-json-flush/blob/main/([^)\s"']+)`)

// TestNavListsEveryDocument is the completeness half of the page map. A document
// left out of the nav is still built and still reachable by its URL, so it looks
// fine to whoever wrote it and is invisible to everybody else.
func TestNavListsEveryDocument(t *testing.T) {
	listed := navDocuments(t)

	entries, err := os.ReadDir(docsDir)
	if err != nil {
		t.Fatalf("%v", err)
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
			continue
		}
		if !slices.Contains(listed, e.Name()) {
			t.Errorf("%s/%s is not in the nav of %s; it would be published as a page nothing links to",
				docsDir, e.Name(), mkdocsConfig)
		}
	}
}

// TestNavPointsAtDocumentsThatExist is the other half. MkDocs is run with
// --strict in the workflow and would fail on this too, but that is after the
// change has been pushed to the default branch.
func TestNavPointsAtDocumentsThatExist(t *testing.T) {
	for _, name := range navDocuments(t) {
		if _, err := os.Stat(filepath.Join(docsDir, name)); err != nil {
			t.Errorf("%s lists %q in its nav, which does not exist: %v", mkdocsConfig, name, err)
		}
	}
}

// TestBlobLinksResolve checks the absolute links back into this repository. They
// exist because a relative link out of docs/ cannot be served by the site, and
// the cost of spelling them as URLs is that skills/skills_test.go, which only
// resolves relative links, stops seeing them.
func TestBlobLinksResolve(t *testing.T) {
	for _, doc := range repositoryMarkdown(t) {
		src, err := os.ReadFile(doc)
		if err != nil {
			t.Errorf("%v", err)
			continue
		}
		for _, m := range blobLink.FindAllStringSubmatch(string(src), -1) {
			target, _, _ := strings.Cut(m[1], "#")
			// A URL written with a <placeholder> in it is showing the shape of a
			// link, not being one. DEVELOPERS.md documents this exact form.
			if target == "" || strings.ContainsAny(target, "<>") {
				continue
			}
			if _, err := os.Stat(target); err != nil {
				t.Errorf("%s links to %s, which is not in the repository: %v", doc, m[0], err)
			}
		}
	}
}

// navDocuments returns the file name of every entry of the nav, in order.
func navDocuments(t *testing.T) []string {
	t.Helper()

	block := navSection.FindStringSubmatch(read(t, mkdocsConfig))
	if block == nil {
		t.Fatalf("%s has no nav: block; the page map is the reason the file exists", mkdocsConfig)
	}

	var names []string
	for _, m := range navTarget.FindAllStringSubmatch(block[1], -1) {
		names = append(names, m[1])
	}
	if len(names) == 0 {
		t.Fatalf("%s: the nav block lists no document; this check is reading the wrong thing", mkdocsConfig)
	}
	return names
}

// repositoryMarkdown returns every markdown file this repository owns.
func repositoryMarkdown(t *testing.T) []string {
	t.Helper()

	var found []string
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && strings.HasPrefix(d.Name(), ".") && path != "." {
			return fs.SkipDir
		}
		if !d.IsDir() && filepath.Ext(path) == ".md" {
			found = append(found, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	return found
}
