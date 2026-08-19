// Package skills holds the Agent Skill that ships with jjf.
//
// It has no Go code of its own. The tests here guard the skill document against
// drifting from the authoritative JSON Schema and from the Agent Skills layout:
// the enum values the document reproduces on purpose, the frontmatter every host
// has to be able to read, the SKILL.md size budget, and the cross references
// between the files.
//
// The same enum drift check covers docs/db-design-guide.ja.md, the human-facing
// Japanese guide that carries the skill's content for a reader who is reviewing
// it rather than executing it. It is not part of the skill, but it reproduces the
// same enums and would otherwise go stale unnoticed.
//
// The link check reaches outside skills/ for the same reason: that guide, the two
// repository READMEs and DEVELOPERS.md point at each other and across
// directories, so a rename anywhere in the repository can leave them dangling
// with nothing to notice it. The READMEs of this directory are split by language
// the same way, and are checked as one more pair.
//
// The rename guard covers the same documents plus AGENTS.md. The command this
// repository ships was renamed once while the module path and the repository URLs
// kept the old name, which makes a plain search for it useless and a leftover
// mention easy to miss.
//
// The skill is distributed as a Claude Code plugin, so the tests also guard the
// two manifests at the repository root, .claude-plugin/plugin.json and
// .claude-plugin/marketplace.json. Their contents have to be bumped together at
// every release, and a mismatch there is invisible until a user installs a
// version that no longer exists.
package skills

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	schemadata "github.com/shutx-net/jumping-json-flush/schema"
)

// skillDir is the one skill directory. Its name is also the skill name, which is
// what makes the plugin invocation /jjf:db-design.
const skillDir = "db-design"

// skillFile is the entry point of a skill directory.
const skillFile = "SKILL.md"

// fieldsReference is the reference file that reproduces the schema's enum values.
var fieldsReference = filepath.Join("references", "fields.md")

// japaneseGuide is the human-facing Japanese guide. It sits outside skills/ on
// purpose: an agent reads the English skill, and this document exists so that a
// Japanese reader can review the same conventions. The English skill wins if the
// two ever disagree.
var japaneseGuide = filepath.Join("..", "docs", "db-design-guide.ja.md")

// The repository READMEs, one level above this package. English is the front
// page GitHub shows; the Japanese one is its mirror, following the same rule as
// the skill and japaneseGuide.
var (
	readmeEN = filepath.Join("..", "README.md")
	readmeJA = filepath.Join("..", "README.ja.md")
)

// developersDoc is the contributor documentation both READMEs hand off to. It
// exists in English only, so it is not half of a language pair, but its links
// still cross directories and it is still a document a rename can break.
var developersDoc = filepath.Join("..", "DEVELOPERS.md")

// The reference documentation the READMEs hand off to, three pairs of it. Unlike
// DEVELOPERS.md these are written for users and not for contributors, which is why
// each exists in both languages: a Japanese reader who followed a link out of
// README.ja.md must not land in English.
var (
	installEN = filepath.Join("..", "docs", "install.md")
	installJA = filepath.Join("..", "docs", "install.ja.md")
	usageEN   = filepath.Join("..", "docs", "usage.md")
	usageJA   = filepath.Join("..", "docs", "usage.ja.md")
	formatEN  = filepath.Join("..", "docs", "db-design-format.md")
	formatJA  = filepath.Join("..", "docs", "db-design-format.ja.md")
)

// The READMEs of this directory, which document how to install the skill. They
// are split by language exactly like the repository ones, so that each language
// reads front to back on its own.
const (
	skillsReadmeEN = "README.md"
	skillsReadmeJA = "README.ja.md"
)

// languagePairs are the documents this repository publishes as one file per
// language, each pair written as {English, Japanese}.
var languagePairs = [][2]string{
	{readmeEN, readmeJA},
	{installEN, installJA},
	{usageEN, usageJA},
	{formatEN, formatJA},
	{skillsReadmeEN, skillsReadmeJA},
}

// outsideDocs are the markdown documents this repository owns outside skills/.
// Their links are checked together with the skill's, because they are the entry
// points a reader arrives at and they point across directories, where a rename
// anywhere in the repository breaks them silently.
var outsideDocs = []string{
	readmeEN, readmeJA,
	installEN, installJA,
	usageEN, usageJA,
	formatEN, formatJA,
	japaneseGuide, developersDoc,
}

// AGENTS.md, the project policy document at the repository root, is not listed
// here: it carries no links. The rename guard reaches it anyway, by walking the
// whole repository.

const (
	// maxSkillLines is the SKILL.md budget. Agent Skills allows 500 lines; jjf
	// keeps to 200 so that detail is pushed into references/ and only loaded when
	// it is needed.
	maxSkillLines = 200
	// maxDescriptionRunes is the frontmatter description limit.
	maxDescriptionRunes = 1536
)

// portableFrontmatterKeys are the frontmatter fields every Agent Skills host
// understands. Claude Code accepts more, but this skill is distributed and has
// to load unchanged from a claude.ai upload and from the Agent SDK, so nothing
// outside this set is allowed.
var portableFrontmatterKeys = []string{
	"name", "description", "license", "compatibility", "allowed-tools", "metadata",
}

// requiredFrontmatterKeys must be present with a non-empty value.
var requiredFrontmatterKeys = []string{"name", "description"}

// enumDefs are the $defs entries of the authoritative schema whose values the
// field reference reproduces verbatim.
var enumDefs = []string{"dbms", "referentialAction"}

// binaryName is the CLI the skill drives. It is what an agent types, so every
// pre-approved Bash pattern of the frontmatter has to spell it exactly.
const binaryName = "jjf"

// allowedSubcommands are the subcommands the skill pre-approves: validate, which
// reads, and export, which writes a workbook at a path the user supplies.
var allowedSubcommands = []string{"validate", "export"}

func TestSkillFrontmatter(t *testing.T) {
	path := filepath.Join(skillDir, skillFile)
	fm, err := readFrontmatter(path)
	if err != nil {
		t.Fatal(err)
	}

	for _, key := range fm.keys {
		if !slices.Contains(portableFrontmatterKeys, key) {
			t.Errorf("%s: frontmatter key %q is not portable across Agent Skills hosts; allowed keys are %s",
				path, key, strings.Join(portableFrontmatterKeys, ", "))
		}
	}
	for _, key := range requiredFrontmatterKeys {
		if fm.values[key] == "" {
			t.Errorf("%s: frontmatter %q is missing or empty", path, key)
		}
	}
	if got := fm.values["name"]; got != skillDir {
		t.Errorf("%s: frontmatter name = %q, want %q, the directory name", path, got, skillDir)
	}
	if n := len([]rune(fm.values["description"])); n > maxDescriptionRunes {
		t.Errorf("%s: description is %d characters, want at most %d", path, n, maxDescriptionRunes)
	}
}

// TestSkillAllowedToolsNameTheBinary guards the pre-approved Bash patterns
// against a renamed CLI. A stale name here is invisible in review and shows up
// only at run time, as a permission prompt for a command the skill claimed to
// have cleared already.
func TestSkillAllowedToolsNameTheBinary(t *testing.T) {
	path := filepath.Join(skillDir, skillFile)
	fm, err := readFrontmatter(path)
	if err != nil {
		t.Fatal(err)
	}

	tools := fm.values["allowed-tools"]
	for _, sub := range allowedSubcommands {
		want := fmt.Sprintf("Bash(%s %s:*)", binaryName, sub)
		if !strings.Contains(tools, want) {
			t.Errorf("%s: allowed-tools = %q, want it to pre-approve %q", path, tools, want)
		}
	}
}

// markdownFilesBelow returns every markdown file in the tree rooted at dir.
func markdownFilesBelow(t *testing.T, dir string) []string {
	t.Helper()

	var found []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && filepath.Ext(path) == ".md" {
			found = append(found, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return found
}

func TestSkillLength(t *testing.T) {
	path := filepath.Join(skillDir, skillFile)
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v", err)
	}
	if n := countLines(src); n > maxSkillLines {
		t.Errorf("%s is %d lines, want at most %d; move detail into references/",
			path, n, maxSkillLines)
	}
}

// TestFieldsReferenceListsEveryEnumValue is the drift check on the values the
// field reference duplicates so that an agent can write them correctly without
// opening the schema.
func TestFieldsReferenceListsEveryEnumValue(t *testing.T) {
	assertMentionsEveryEnumValue(t, filepath.Join(skillDir, fieldsReference))
}

// TestJapaneseGuideExists guards the link the two READMEs point a Japanese reader
// at. Losing the file would leave the skill readable only in English.
func TestJapaneseGuideExists(t *testing.T) {
	if _, err := os.Stat(japaneseGuide); err != nil {
		t.Errorf("%v; it is the Japanese counterpart of %s and is linked from README.md and skills/README.md",
			err, filepath.Join(skillDir, skillFile))
	}
}

// TestDesignFormatReferenceListsEveryEnumValue applies the same drift check to the
// format reference, in both languages. It is where the READMEs send a reader for
// every field and every rule, so a value missing from it is a value nobody is told
// about.
func TestDesignFormatReferenceListsEveryEnumValue(t *testing.T) {
	for _, path := range []string{formatEN, formatJA} {
		assertMentionsEveryEnumValue(t, path)
	}
}

// TestJapaneseGuideListsEveryEnumValue applies the same drift check to the
// Japanese guide. Adding a value to the schema and translating only the English
// reference would leave a Japanese reviewer reading a list the tool no longer
// accepts.
func TestJapaneseGuideListsEveryEnumValue(t *testing.T) {
	assertMentionsEveryEnumValue(t, japaneseGuide)
}

// assertMentionsEveryEnumValue checks that the document at path reproduces every
// value of the schema enums that the documentation duplicates on purpose.
func assertMentionsEveryEnumValue(t *testing.T, path string) {
	t.Helper()

	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v", err)
	}
	for _, name := range enumDefs {
		for _, v := range enumOf(t, name) {
			if !bytes.Contains(src, []byte(v)) {
				t.Errorf("%s does not mention %q, an enum value of $defs/%s in the schema",
					path, v, name)
			}
		}
	}
}

// TestRelativeLinksResolve checks that every relative link of every markdown file
// below skills/, and of the documents listed in outsideDocs, points at a file
// that exists.
func TestRelativeLinksResolve(t *testing.T) {
	docs := append(slices.Clone(outsideDocs), markdownFilesBelow(t, ".")...)
	for _, path := range docs {
		assertLinksResolve(t, path)
	}
}

// assertLinksResolve checks the relative links of the document at path against
// the file system, resolving each one the way a reader of that file does:
// relative to the directory the file itself sits in.
func assertLinksResolve(t *testing.T, path string) {
	t.Helper()

	src, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("%v", err)
		return
	}
	for _, m := range markdownLink.FindAllStringSubmatch(string(src), -1) {
		target, ok := localTarget(m[1])
		if !ok {
			continue
		}
		resolved := filepath.Join(filepath.Dir(path), target)
		if _, err := os.Stat(resolved); err != nil {
			t.Errorf("%s links to %q, which does not resolve: %v", path, m[1], err)
		}
	}
}

// TestLanguagePairsLinkToEachOther guards every document that exists in two
// languages: both halves of a pair have to be present, and each has to link to
// the other. GitHub renders README.md and nothing else when it shows a
// directory, so the Japanese mirror is reachable only through the link the
// English file carries, and a reader who landed on the mirror needs the way
// back. The installation pair is reached the same way, one link further in.
func TestLanguagePairsLinkToEachOther(t *testing.T) {
	for _, pair := range languagePairs {
		for _, order := range [][2]string{{pair[0], pair[1]}, {pair[1], pair[0]}} {
			from, to := order[0], filepath.Base(order[1])
			src, err := os.ReadFile(from)
			if err != nil {
				t.Errorf("%v; it is one half of a two language pair with %s", err, order[1])
				continue
			}
			if !linksTo(string(src), to) {
				t.Errorf("%s does not link to %s; the link is the only way a reader of one half reaches the other",
					from, to)
			}
		}
	}
}

// linksTo reports whether src carries a markdown link whose target is exactly
// file.
func linksTo(src, file string) bool {
	for _, m := range markdownLink.FindAllStringSubmatch(src, -1) {
		if target, ok := localTarget(m[1]); ok && target == file {
			return true
		}
	}
	return false
}

// The plugin manifests live at the repository root, one level above this
// package.
var (
	pluginManifestPath      = filepath.Join("..", ".claude-plugin", "plugin.json")
	marketplaceManifestPath = filepath.Join("..", ".claude-plugin", "marketplace.json")
)

const (
	// pluginName is the plugin's identifier. It namespaces the skill, so this is
	// the jjf of /jjf:db-design.
	pluginName = "jjf"
	// marketplaceName is the catalog's identifier, the part after the @ in
	// /plugin install jjf@jjf-tools.
	marketplaceName = "jjf-tools"
	// archiveSourceType is the only source type this repository distributes from.
	// It downloads a zip over HTTPS, which is what lets a user install without
	// git, npm or Node.
	archiveSourceType = "archive"
)

// releaseVersion is the shape both manifests have to write: a release tag without
// its leading v. It matches the tag gate of .github/workflows/release.yml.
var releaseVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$`)

// sha256Digest is the archive pin: 64 hex characters, either case.
var sha256Digest = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

// pluginManifest is the part of .claude-plugin/plugin.json the tests read.
type pluginManifest struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	License     string `json:"license"`
}

// marketplaceManifest is the part of .claude-plugin/marketplace.json the tests
// read.
type marketplaceManifest struct {
	Name  string `json:"name"`
	Owner struct {
		Name string `json:"name"`
	} `json:"owner"`
	Plugins []marketplaceEntry `json:"plugins"`
}

// marketplaceEntry is one plugin listed by the catalog. Source stays raw because
// the format allows either a relative path string or a source object, and an
// object is the only form this repository accepts.
type marketplaceEntry struct {
	Name        string          `json:"name"`
	Version     string          `json:"version"`
	Description string          `json:"description"`
	License     string          `json:"license"`
	Source      json.RawMessage `json:"source"`
}

// archiveSource is the source object of an archive entry.
type archiveSource struct {
	Source string `json:"source"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

func TestPluginManifest(t *testing.T) {
	m := readPluginManifest(t)

	if m.Name != pluginName {
		t.Errorf("%s: name = %q, want %q; the name namespaces the skill, so a change here renames the /%s:%s invocation",
			pluginManifestPath, m.Name, pluginName, pluginName, skillDir)
	}
	if !releaseVersion.MatchString(m.Version) {
		t.Errorf("%s: version = %q, want a release version without a leading v, such as 1.2.3 or 1.2.3-rc.1",
			pluginManifestPath, m.Version)
	}
	if m.Description == "" {
		t.Errorf("%s: description is missing or empty", pluginManifestPath)
	}
}

func TestMarketplaceManifest(t *testing.T) {
	m := readMarketplaceManifest(t)

	if m.Name != marketplaceName {
		t.Errorf("%s: name = %q, want %q; users type it as /plugin install <plugin>@%s",
			marketplaceManifestPath, m.Name, marketplaceName, marketplaceName)
	}
	if m.Owner.Name == "" {
		t.Errorf("%s: owner.name is missing or empty, and it is required", marketplaceManifestPath)
	}
	if len(m.Plugins) == 0 {
		t.Fatalf("%s: plugins is empty, and at least one entry is required", marketplaceManifestPath)
	}
	for i, e := range m.Plugins {
		if e.Name == "" {
			t.Errorf("%s: plugins[%d].name is missing or empty, and it is required", marketplaceManifestPath, i)
		}
		if len(e.Source) == 0 {
			t.Errorf("%s: plugins[%d].source is missing, and it is required", marketplaceManifestPath, i)
		}
	}
}

// TestMarketplaceSourceIsArchive pins the distribution method. A relative path
// source cannot work here anyway: the catalog is served as a bare
// marketplace.json over HTTPS, so there is no clone for a path to resolve
// against.
func TestMarketplaceSourceIsArchive(t *testing.T) {
	entry := pluginEntry(t, readMarketplaceManifest(t))
	src := archiveSourceOf(t, entry)

	if src.Source != archiveSourceType {
		t.Errorf("%s: plugins[%q].source.source = %q, want %q",
			marketplaceManifestPath, entry.Name, src.Source, archiveSourceType)
	}
	if !strings.HasPrefix(src.URL, "https://") {
		t.Errorf("%s: plugins[%q].source.url = %q, want an https:// URL; Claude Code refuses any other scheme",
			marketplaceManifestPath, entry.Name, src.URL)
	}
	// An absent pin installs unpinned, which is the state between building a
	// release and committing its digest. A malformed one is never valid.
	if src.SHA256 != "" && !sha256Digest.MatchString(src.SHA256) {
		t.Errorf("%s: plugins[%q].source.sha256 = %q, want 64 hex characters",
			marketplaceManifestPath, entry.Name, src.SHA256)
	}
}

// TestManifestsAgree is the release drift check. plugin.json wins over the
// marketplace entry, so a version bumped in only one of the two files silently
// keeps every user on the previous release.
func TestManifestsAgree(t *testing.T) {
	plugin := readPluginManifest(t)
	entry := pluginEntry(t, readMarketplaceManifest(t))

	if entry.Version != plugin.Version {
		t.Errorf("version disagrees: %s has %q, %s has %q; bump both in the commit the release tag points at",
			pluginManifestPath, plugin.Version, marketplaceManifestPath, entry.Version)
	}
	if entry.Description != plugin.Description {
		t.Errorf("description disagrees:\n  %s: %q\n  %s: %q",
			pluginManifestPath, plugin.Description, marketplaceManifestPath, entry.Description)
	}
	if entry.License != "" && entry.License != plugin.License {
		t.Errorf("license disagrees: %s has %q, %s has %q",
			pluginManifestPath, plugin.License, marketplaceManifestPath, entry.License)
	}

	// The archive is a release asset named after its tag, so the URL and the
	// version can only ever be bumped together.
	src := archiveSourceOf(t, entry)
	want := fmt.Sprintf("/jjf-plugin-v%s.zip", entry.Version)
	if !strings.HasSuffix(src.URL, want) {
		t.Errorf("%s: plugins[%q].source.url = %q, want it to end in %q for version %q",
			marketplaceManifestPath, entry.Name, src.URL, want, entry.Version)
	}
}

// readPluginManifest decodes .claude-plugin/plugin.json.
func readPluginManifest(t *testing.T) pluginManifest {
	t.Helper()

	var m pluginManifest
	decodeJSON(t, pluginManifestPath, &m)
	return m
}

// readMarketplaceManifest decodes .claude-plugin/marketplace.json.
func readMarketplaceManifest(t *testing.T) marketplaceManifest {
	t.Helper()

	var m marketplaceManifest
	decodeJSON(t, marketplaceManifestPath, &m)
	return m
}

// decodeJSON reads the file at path into v, failing the test on a read error or
// on anything that is not valid JSON of the expected shape.
func decodeJSON(t *testing.T, path string, v any) {
	t.Helper()

	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v", err)
	}
	if err := json.Unmarshal(src, v); err != nil {
		t.Fatalf("%s is not valid JSON of the expected shape: %v", path, err)
	}
}

// pluginEntry returns the catalog entry for this repository's plugin.
func pluginEntry(t *testing.T, m marketplaceManifest) marketplaceEntry {
	t.Helper()

	for _, e := range m.Plugins {
		if e.Name == pluginName {
			return e
		}
	}
	t.Fatalf("%s lists no plugin named %q", marketplaceManifestPath, pluginName)
	return marketplaceEntry{}
}

// archiveSourceOf decodes the source object of a catalog entry.
func archiveSourceOf(t *testing.T, e marketplaceEntry) archiveSource {
	t.Helper()

	var src archiveSource
	if err := json.Unmarshal(e.Source, &src); err != nil {
		t.Fatalf("%s: plugins[%q].source is not a source object: %v; a relative path source cannot be used, because the catalog is served as a bare marketplace.json and is never cloned",
			marketplaceManifestPath, e.Name, err)
	}
	return src
}

// markdownLink captures the target of an inline markdown link.
var markdownLink = regexp.MustCompile(`\[[^\]]*\]\(([^)\s]+)`)

// remoteTarget matches a link target that does not name a file on disk: one with
// a URL scheme, a protocol-relative host, or a bare fragment.
var remoteTarget = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9+.-]*:|//|#)`)

// localTarget reduces a link target to the path it names, reporting false when it
// names no local file at all.
func localTarget(target string) (string, bool) {
	if remoteTarget.MatchString(target) {
		return "", false
	}
	path, _, _ := strings.Cut(target, "#")
	path, _, _ = strings.Cut(path, "?")
	return path, path != ""
}

// enumOf returns the enum values of one $defs entry of the embedded schema, which
// is the authoritative definition of the document structure.
func enumOf(t *testing.T, def string) []string {
	t.Helper()

	var doc struct {
		Defs map[string]struct {
			Enum []string `json:"enum"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(schemadata.DBDesign, &doc); err != nil {
		t.Fatalf("decode embedded schema: %v", err)
	}
	d, ok := doc.Defs[def]
	if !ok {
		t.Fatalf("embedded schema has no $defs/%s", def)
	}
	if len(d.Enum) == 0 {
		t.Fatalf("$defs/%s of the embedded schema has no enum values", def)
	}
	return d.Enum
}

// countLines counts lines the way wc -l does, plus a final unterminated one.
func countLines(src []byte) int {
	n := bytes.Count(src, []byte("\n"))
	if len(src) > 0 && !bytes.HasSuffix(src, []byte("\n")) {
		n++
	}
	return n
}

// frontmatter is the YAML block at the top of a SKILL.md.
//
// A YAML parser is deliberately not added as a dependency. Skill frontmatter is a
// flat list of keys, and folding every continuation line into the key it belongs
// to is enough for the checks here.
type frontmatter struct {
	keys   []string          // top level keys, in the order they appear
	values map[string]string // scalar of each key, nested lines folded onto one line
}

// readFrontmatter reads the frontmatter of the SKILL.md at path.
func readFrontmatter(path string) (*frontmatter, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	fm, err := parseFrontmatter(src)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return fm, nil
}

// parseFrontmatter reads the --- delimited block at the start of src.
func parseFrontmatter(src []byte) (*frontmatter, error) {
	lines := strings.Split(strings.ReplaceAll(string(src), "\r\n", "\n"), "\n")
	if len(lines) == 0 || lines[0] != "---" {
		return nil, errors.New("no YAML frontmatter; the file does not start with a --- line")
	}

	fm := &frontmatter{values: make(map[string]string)}
	key := ""
	for i, line := range lines[1:] {
		lineNo := i + 2
		switch {
		case line == "---":
			return fm, nil
		case strings.TrimSpace(line) == "":
			continue
		case line[0] == ' ', line[0] == '\t', strings.HasPrefix(line, "- "):
			// A nested mapping, a list item or the body of a block scalar. Fold it
			// onto the value of the key it belongs to.
			if key == "" {
				return nil, fmt.Errorf("line %d belongs to no key", lineNo)
			}
			fm.values[key] = strings.TrimSpace(fm.values[key] + " " + strings.TrimSpace(line))
		default:
			name, value, ok := strings.Cut(line, ":")
			if !ok {
				return nil, fmt.Errorf("line %d is not a key: %q", lineNo, line)
			}
			key = strings.TrimSpace(name)
			if _, dup := fm.values[key]; dup {
				return nil, fmt.Errorf("line %d repeats key %q", lineNo, key)
			}
			fm.keys = append(fm.keys, key)
			fm.values[key] = scalarValue(value)
		}
	}
	return nil, errors.New("frontmatter is not terminated by a --- line")
}

// scalarValue normalises the value written after a key: a block scalar indicator
// becomes empty, waiting for the lines folded onto it, and matching surrounding
// quotes are dropped.
func scalarValue(v string) string {
	v = strings.TrimSpace(v)
	if slices.Contains([]string{">", ">-", ">+", "|", "|-", "|+"}, v) {
		return ""
	}
	for _, q := range []string{`"`, "'"} {
		if len(v) >= 2 && strings.HasPrefix(v, q) && strings.HasSuffix(v, q) {
			return v[1 : len(v)-1]
		}
	}
	return v
}
