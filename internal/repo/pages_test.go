// The documentation site. See install_test.go for what this package is and how
// the paths below are resolved.
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
package repo

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

	entries, err := os.ReadDir(repoPath(docsDir))
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
		if !exists(docsDir, name) {
			t.Errorf("%s lists %q in its nav, which does not exist", mkdocsConfig, name)
		}
	}
}

// TestBlobLinksResolve checks the absolute links back into this repository. They
// exist because a relative link out of docs/ cannot be served by the site, and
// the cost of spelling them as URLs is that skills/skills_test.go, which only
// resolves relative links, stops seeing them.
func TestBlobLinksResolve(t *testing.T) {
	for _, doc := range repositoryMarkdown(t) {
		src, err := os.ReadFile(repoPath(doc))
		if err != nil {
			t.Errorf("%s: %v", doc, err)
			continue
		}
		for _, m := range blobLink.FindAllStringSubmatch(string(src), -1) {
			target, _, _ := strings.Cut(m[1], "#")
			// A URL written with a <placeholder> in it is showing the shape of a
			// link, not being one. DEVELOPERS.md documents this exact form.
			if target == "" || strings.ContainsAny(target, "<>") {
				continue
			}
			if !exists(target) {
				t.Errorf("%s links to %s, which is not in the repository", doc, m[0])
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

// repositoryMarkdown returns every markdown file this repository owns, named
// relative to the repository root.
//
// The walk is done over a file system rooted at the repository rather than over
// root as a path. Walking "../.." would hand the callback ".." as the name of the
// very first entry, which the hidden directory rule below would read as a dotted
// directory and skip, leaving the check to pass having looked at nothing.
func repositoryMarkdown(t *testing.T) []string {
	t.Helper()

	var found []string
	err := fs.WalkDir(os.DirFS(root), ".", func(path string, d fs.DirEntry, err error) error {
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
	if len(found) == 0 {
		t.Fatalf("no markdown found below %s; this check is reading the wrong directory", root)
	}
	return found
}
