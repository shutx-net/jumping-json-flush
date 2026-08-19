// Package sml writes SpreadsheetML (.xlsx) workbooks using only the standard
// library.
//
// It is a generic Office Open XML writer: it knows about sheets, cells, styles
// and sheet layout, and nothing about what a document describes. Presentation
// decisions such as colours, column widths and heading text belong to the
// caller.
//
// Output is deterministic. The same sequence of calls always produces the same
// bytes, independent of the host time zone, because every zip entry carries a
// fixed timestamp and every ordered emission walks insertion order rather than a
// Go map.
package sml

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"time"
)

// Properties are the optional document properties written to docProps/core.xml.
// A zero Properties writes no part at all.
type Properties struct {
	Title       string
	Creator     string
	Description string
	// Created is written only when non-zero. Leave it zero for byte-identical
	// output across runs.
	Created time.Time
}

// Workbook is a spreadsheet under construction. Use New to create one.
type Workbook struct {
	sheets []*Sheet
	names  nameAllocator
	styles styleRegistry
	props  Properties
}

// New returns an empty Workbook with the style slots Excel reserves already
// seeded.
func New() *Workbook {
	w := &Workbook{}
	w.styles.init()
	return w
}

// AddSheet appends a worksheet and returns it. The name is sanitised and made
// unique, so this never fails; call Sheet.Name to find out which name was
// actually used. The first sheet added is the selected one unless
// Sheet.SetTabSelected says otherwise.
func (w *Workbook) AddSheet(name string) *Sheet {
	s := newSheet(w, w.names.Allocate(name))
	s.tabSelected = len(w.sheets) == 0
	w.sheets = append(w.sheets, s)
	return s
}

// Style registers a cell format and returns the id that Sheet setters take.
// Registering an equal Style again returns the same id.
func (w *Workbook) Style(s Style) StyleID { return w.styles.style(s) }

// SetProperties sets the document properties.
func (w *Workbook) SetProperties(p Properties) { w.props = p }

// ErrNoSheets is returned by WriteTo for a workbook with no worksheets, which
// is not a valid spreadsheet.
var ErrNoSheets = errors.New("sml: workbook has no sheets")

// WriteTo writes the workbook to dst as an .xlsx package and reports how many
// bytes were written.
func (w *Workbook) WriteTo(dst io.Writer) (int64, error) {
	if len(w.sheets) == 0 {
		return 0, ErrNoSheets
	}
	cw := &countingWriter{w: dst}
	zw := zip.NewWriter(cw)
	parts := w.parts()
	// [Content_Types].xml goes first: not required by the specification, but it
	// is what every producer does and it costs nothing.
	if err := writePart(zw, partContentTypes, contentTypes(parts)); err != nil {
		return cw.n, err
	}
	for _, p := range parts {
		if err := writePart(zw, p.name, p.content); err != nil {
			return cw.n, err
		}
	}
	if err := zw.Close(); err != nil {
		return cw.n, err
	}
	return cw.n, nil
}

// parts lists the package members in the order they are written, excluding
// [Content_Types].xml which is derived from this list.
func (w *Workbook) parts() []part {
	parts := make([]part, 0, len(w.sheets)+5)

	// Package relationships. Targets here are relative to the package root.
	root := &pkgRelationships{Xmlns: nsPkgRels}
	root.Rels = append(root.Rels, pkgRelationship{
		ID:     "rId1",
		Type:   relTypeOfficeDoc,
		Target: partWorkbook,
	})
	withProps := w.props != (Properties{})
	if withProps {
		root.Rels = append(root.Rels, pkgRelationship{
			ID:     "rId2",
			Type:   relTypeCoreProps,
			Target: partCoreProps,
		})
	}
	parts = append(parts, part{name: partRootRels, content: root})
	if withProps {
		parts = append(parts, part{
			name:        partCoreProps,
			contentType: ctCoreProps,
			content:     w.props.xml(),
		})
	}

	// Workbook and its relationships. Targets here are relative to xl/.
	book := &xlWorkbook{Xmlns: nsSpreadsheetML, XmlnsR: nsRelationships}
	rels := &pkgRelationships{Xmlns: nsPkgRels}
	sheetParts := make([]part, 0, len(w.sheets))
	for i, s := range w.sheets {
		id := fmt.Sprintf("rId%d", i+1)
		target := fmt.Sprintf("worksheets/sheet%d.xml", i+1)
		book.Sheets.Sheet = append(book.Sheets.Sheet, xlSheet{
			Name:    s.name,
			SheetID: i + 1,
			RID:     id,
		})
		rels.Rels = append(rels.Rels, pkgRelationship{
			ID:     id,
			Type:   relTypeWorksheet,
			Target: target,
		})
		sheetParts = append(sheetParts, part{
			name:        "xl/" + target,
			contentType: ctWorksheet,
			content:     s.xml(),
		})
	}
	rels.Rels = append(rels.Rels, pkgRelationship{
		ID:     fmt.Sprintf("rId%d", len(w.sheets)+1),
		Type:   relTypeStyles,
		Target: "styles.xml",
	})

	parts = append(parts, part{name: partWorkbook, contentType: ctWorkbook, content: book})
	parts = append(parts, part{name: partWorkbookRels, content: rels})
	parts = append(parts, sheetParts...)
	parts = append(parts, part{name: partStyles, contentType: ctStyles, content: w.styles.xml()})
	return parts
}

// xml renders the document properties. Text is sanitised here for the same
// reason cell text is: encoding/xml would silently replace anything XML cannot
// represent.
func (p Properties) xml() *xlCoreProperties {
	out := &xlCoreProperties{
		XmlnsCp:      nsCoreProps,
		XmlnsDc:      nsDC,
		XmlnsDcterms: nsDCTerms,
		XmlnsXsi:     nsXSI,
		Creator:      sanitizeXMLText(p.Creator),
		Description:  sanitizeXMLText(p.Description),
		Title:        sanitizeXMLText(p.Title),
	}
	if !p.Created.IsZero() {
		out.Created = &xlW3CDTF{
			Type:  "dcterms:W3CDTF",
			Value: p.Created.UTC().Format("2006-01-02T15:04:05Z"),
		}
	}
	return out
}
