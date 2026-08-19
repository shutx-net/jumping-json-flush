package sml

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"os/exec"
	"path"
	"slices"
	"strings"
	"testing"
)

// minimalWorkbook is the smallest useful workbook: one sheet with a string cell
// and a numeric cell.
func minimalWorkbook() *Workbook {
	wb := New()
	sh := wb.AddSheet("Sheet1")
	sh.SetString(1, 1, "hello", 0)
	sh.SetInt(2, 1, 1, 0)
	return wb
}

func TestZipMinimalEntries(t *testing.T) {
	p := writeAndOpen(t, minimalWorkbook())
	want := []string{
		partContentTypes,
		partRootRels,
		partWorkbook,
		partWorkbookRels,
		"xl/worksheets/sheet1.xml",
		partStyles,
	}
	got := slices.Clone(p.order)
	slices.Sort(got)
	sorted := slices.Clone(want)
	slices.Sort(sorted)
	if !slices.Equal(got, sorted) {
		t.Errorf("parts = %v, want %v", p.order, want)
	}
	// docProps is only written when properties are set.
	if _, ok := p.files[partCoreProps]; ok {
		t.Error("docProps/core.xml written for a workbook with no properties")
	}
	if p.order[0] != partContentTypes {
		t.Errorf("first entry = %q, want %q", p.order[0], partContentTypes)
	}
}

func TestZipEntriesWithProperties(t *testing.T) {
	wb := minimalWorkbook()
	wb.SetProperties(Properties{Title: "t"})
	p := writeAndOpen(t, wb)
	if _, ok := p.files[partCoreProps]; !ok {
		t.Fatalf("docProps/core.xml missing; have %v", p.order)
	}
}

func TestZipEntryHeaders(t *testing.T) {
	b := writeWorkbook(t, buildFullWorkbook())
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		if f.Method != zip.Deflate {
			t.Errorf("%s method = %d, want Deflate", f.Name, f.Method)
		}
		if !f.Modified.Equal(epoch) {
			t.Errorf("%s modified = %s, want %s", f.Name, f.Modified, epoch)
		}
		if strings.Contains(f.Name, `\`) || strings.HasPrefix(f.Name, "/") {
			t.Errorf("%s is not a slash separated relative path", f.Name)
		}
		if f.FileInfo().IsDir() {
			t.Errorf("%s is a directory entry; those are unnecessary", f.Name)
		}
	}
}

func TestZipContentTypesMatchParts(t *testing.T) {
	p := writeAndOpen(t, buildFullWorkbook())
	var types rdTypes
	p.unmarshal(t, partContentTypes, &types)

	defaults := make(map[string]string)
	for _, d := range types.Default {
		defaults[d.Extension] = d.ContentType
	}
	overrides := make(map[string]string)
	for _, o := range types.Override {
		if !strings.HasPrefix(o.PartName, "/") {
			t.Errorf("PartName %q must be absolute", o.PartName)
		}
		name := strings.TrimPrefix(o.PartName, "/")
		if _, ok := p.files[name]; !ok {
			t.Errorf("Override names %q, which is not in the package", o.PartName)
		}
		if _, dup := overrides[name]; dup {
			t.Errorf("duplicate Override for %q", o.PartName)
		}
		overrides[name] = o.ContentType
	}
	// Every part other than [Content_Types].xml itself must be typed, either by
	// an Override or by a Default for its extension.
	for _, name := range p.order {
		if name == partContentTypes {
			continue
		}
		if _, ok := overrides[name]; ok {
			continue
		}
		ext := strings.TrimPrefix(path.Ext(name), ".")
		if _, ok := defaults[ext]; !ok {
			t.Errorf("part %q has neither an Override nor a Default for %q", name, ext)
		}
	}
	if len(overrides) == 0 {
		t.Error("no Override entries at all")
	}
}

func TestZipRelationshipTargets(t *testing.T) {
	p := writeAndOpen(t, buildFullWorkbook())
	var found int
	for _, name := range p.order {
		if !strings.HasSuffix(name, ".rels") {
			continue
		}
		found++
		var rels rdRelationships
		p.unmarshal(t, name, &rels)
		if len(rels.Relationship) == 0 {
			t.Errorf("%s has no relationships", name)
		}
		// A .rels part lives in the _rels folder of the directory whose parts it
		// describes, and its targets are relative to that directory.
		base := path.Dir(path.Dir(name))
		if base == "." {
			base = ""
		}
		ids := make(map[string]bool)
		for _, r := range rels.Relationship {
			if ids[r.ID] {
				t.Errorf("%s: duplicate relationship id %q", name, r.ID)
			}
			ids[r.ID] = true
			target := path.Join(base, r.Target)
			if _, ok := p.files[target]; !ok {
				t.Errorf("%s: relationship %s targets %q (resolved to %q), which is not in the package",
					name, r.ID, r.Target, target)
			}
			if r.Type == "" {
				t.Errorf("%s: relationship %s has no type", name, r.ID)
			}
		}
	}
	if found != 2 {
		t.Errorf("found %d .rels parts, want 2", found)
	}
}

func TestZipWorkbookReferencesEverySheet(t *testing.T) {
	p := writeAndOpen(t, buildFullWorkbook())
	var book rdWorkbook
	p.unmarshal(t, partWorkbook, &book)
	var rels rdRelationships
	p.unmarshal(t, partWorkbookRels, &rels)

	target := make(map[string]string)
	for _, r := range rels.Relationship {
		target[r.ID] = r.Target
	}
	seen := make(map[int]bool)
	for i, s := range book.Sheets.Sheet {
		if s.SheetID != i+1 {
			t.Errorf("sheet %q has sheetId %d, want %d", s.Name, s.SheetID, i+1)
		}
		if seen[s.SheetID] {
			t.Errorf("duplicate sheetId %d", s.SheetID)
		}
		seen[s.SheetID] = true
		got, ok := target[s.RID]
		if !ok {
			t.Errorf("sheet %q references %q, which has no relationship", s.Name, s.RID)
			continue
		}
		if _, ok := p.files["xl/"+got]; !ok {
			t.Errorf("sheet %q resolves to %q, which is not in the package", s.Name, got)
		}
	}
	if len(book.Sheets.Sheet) != 3 {
		t.Errorf("sheets = %d, want 3", len(book.Sheets.Sheet))
	}
}

func TestZipDeterminism(t *testing.T) {
	first := writeWorkbook(t, buildFullWorkbook())
	second := writeWorkbook(t, buildFullWorkbook())
	if !bytes.Equal(first, second) {
		t.Fatalf("two runs differ: %d vs %d bytes", len(first), len(second))
	}
	// archive/zip stores the local wall clock, so a workbook built in another
	// time zone would differ unless the timestamp is pinned to UTC.
	t.Run("TZ=Asia/Tokyo", func(t *testing.T) {
		t.Setenv("TZ", "Asia/Tokyo")
		if got := writeWorkbook(t, buildFullWorkbook()); !bytes.Equal(first, got) {
			t.Errorf("output differs under a non-UTC time zone: %d vs %d bytes", len(first), len(got))
		}
	})
	t.Run("TZ=America/Los_Angeles", func(t *testing.T) {
		t.Setenv("TZ", "America/Los_Angeles")
		if got := writeWorkbook(t, buildFullWorkbook()); !bytes.Equal(first, got) {
			t.Errorf("output differs under a non-UTC time zone: %d vs %d bytes", len(first), len(got))
		}
	})
}

func TestZipNoSheets(t *testing.T) {
	var buf bytes.Buffer
	if _, err := New().WriteTo(&buf); !errors.Is(err, ErrNoSheets) {
		t.Errorf("WriteTo on an empty workbook = %v, want ErrNoSheets", err)
	}
}

// errWriter fails every write, standing in for a full disk or a closed pipe.
type errWriter struct{ err error }

func (w errWriter) Write([]byte) (int, error) { return 0, w.err }

func TestZipWriteError(t *testing.T) {
	want := errors.New("boom")
	n, err := minimalWorkbook().WriteTo(errWriter{want})
	if !errors.Is(err, want) {
		t.Errorf("WriteTo = %v, want %v", err, want)
	}
	if n != 0 {
		t.Errorf("WriteTo reported %d bytes written, want 0", n)
	}
}

func TestZipReadableAsArchive(t *testing.T) {
	b := writeWorkbook(t, buildFullWorkbook())
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", f.Name, err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read %s: %v", f.Name, err)
		}
		if !strings.HasPrefix(string(data), xmlDecl) {
			t.Errorf("%s does not start with the XML declaration", f.Name)
		}
	}
}

// TestPackageBoundary keeps internal/sml a generic SpreadsheetML writer: it must
// not learn anything about database design documents.
func TestPackageBoundary(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("the go tool is not available")
	}
	out, err := exec.Command("go", "list", "-deps", ".").Output()
	if err != nil {
		t.Skipf("go list: %v", err)
	}
	forbidden := []string{
		"github.com/shutx-net/jumping-json-flush/internal/model",
		"github.com/shutx-net/jumping-json-flush/internal/schema",
		"github.com/shutx-net/jumping-json-flush/internal/export/xlsx",
	}
	for _, dep := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if slices.Contains(forbidden, dep) {
			t.Errorf("internal/sml depends on %s", dep)
		}
	}
}
