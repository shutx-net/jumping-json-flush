package sml

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"testing"
)

// This file is a deliberately naive .xlsx reader used only by the tests. It is
// written against archive/zip and encoding/xml alone so the test suite pulls in
// no third-party dependency. Keep it simple: a clever reader that mirrors the
// writer's assumptions would validate nothing.

// pkg is an opened .xlsx package.
type pkg struct {
	files map[string]string
	order []string
}

// openPkg unzips b and returns its parts keyed by entry name.
func openPkg(t *testing.T, b []byte) *pkg {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	p := &pkg{files: make(map[string]string, len(zr.File))}
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
		if _, dup := p.files[f.Name]; dup {
			t.Fatalf("duplicate zip entry %s", f.Name)
		}
		p.files[f.Name] = string(data)
		p.order = append(p.order, f.Name)
	}
	return p
}

// part returns the body of a part, failing the test when it is missing.
func (p *pkg) part(t *testing.T, name string) string {
	t.Helper()
	body, ok := p.files[name]
	if !ok {
		t.Fatalf("part %s missing; have %v", name, p.order)
	}
	return body
}

// unmarshal decodes a part into v.
func (p *pkg) unmarshal(t *testing.T, name string, v any) {
	t.Helper()
	if err := xml.Unmarshal([]byte(p.part(t, name)), v); err != nil {
		t.Fatalf("unmarshal %s: %v", name, err)
	}
}

// ---------------------------------------------------------------------------
// Read models. These are intentionally separate from the writer's structs so a
// mistake in a writer struct tag cannot hide itself.
// ---------------------------------------------------------------------------

type rdTypes struct {
	Default []struct {
		Extension   string `xml:"Extension,attr"`
		ContentType string `xml:"ContentType,attr"`
	} `xml:"Default"`
	Override []struct {
		PartName    string `xml:"PartName,attr"`
		ContentType string `xml:"ContentType,attr"`
	} `xml:"Override"`
}

type rdRelationships struct {
	Relationship []struct {
		ID     string `xml:"Id,attr"`
		Type   string `xml:"Type,attr"`
		Target string `xml:"Target,attr"`
	} `xml:"Relationship"`
}

type rdWorkbook struct {
	Sheets struct {
		Sheet []struct {
			Name    string `xml:"name,attr"`
			SheetID int    `xml:"sheetId,attr"`
			RID     string `xml:"id,attr"`
		} `xml:"sheet"`
	} `xml:"sheets"`
}

type rdWorksheet struct {
	Dimension struct {
		Ref string `xml:"ref,attr"`
	} `xml:"dimension"`
	SheetViews struct {
		SheetView []struct {
			TabSelected int `xml:"tabSelected,attr"`
			Pane        *struct {
				XSplit      float64 `xml:"xSplit,attr"`
				YSplit      float64 `xml:"ySplit,attr"`
				TopLeftCell string  `xml:"topLeftCell,attr"`
				ActivePane  string  `xml:"activePane,attr"`
				State       string  `xml:"state,attr"`
			} `xml:"pane"`
			Selection *struct {
				Pane       string `xml:"pane,attr"`
				ActiveCell string `xml:"activeCell,attr"`
				Sqref      string `xml:"sqref,attr"`
			} `xml:"selection"`
		} `xml:"sheetView"`
	} `xml:"sheetViews"`
	SheetFormatPr *struct {
		DefaultRowHeight float64 `xml:"defaultRowHeight,attr"`
	} `xml:"sheetFormatPr"`
	Cols struct {
		Col []struct {
			Min         int     `xml:"min,attr"`
			Max         int     `xml:"max,attr"`
			Width       float64 `xml:"width,attr"`
			CustomWidth int     `xml:"customWidth,attr"`
		} `xml:"col"`
	} `xml:"cols"`
	SheetData struct {
		Row []rdRow `xml:"row"`
	} `xml:"sheetData"`
	MergeCells struct {
		Count     int `xml:"count,attr"`
		MergeCell []struct {
			Ref string `xml:"ref,attr"`
		} `xml:"mergeCell"`
	} `xml:"mergeCells"`
}

type rdRow struct {
	R            int      `xml:"r,attr"`
	Ht           float64  `xml:"ht,attr"`
	CustomHeight int      `xml:"customHeight,attr"`
	C            []rdCell `xml:"c"`
}

type rdCell struct {
	R  string `xml:"r,attr"`
	S  int    `xml:"s,attr"`
	T  string `xml:"t,attr"`
	V  string `xml:"v"`
	Is *struct {
		T string `xml:"t"`
	} `xml:"is"`
}

type rdStyleSheet struct {
	NumFmts struct {
		Count  int `xml:"count,attr"`
		NumFmt []struct {
			NumFmtID   int    `xml:"numFmtId,attr"`
			FormatCode string `xml:"formatCode,attr"`
		} `xml:"numFmt"`
	} `xml:"numFmts"`
	Fonts struct {
		Count int `xml:"count,attr"`
		Font  []struct {
			B  *struct{} `xml:"b"`
			I  *struct{} `xml:"i"`
			U  *struct{} `xml:"u"`
			Sz struct {
				Val float64 `xml:"val,attr"`
			} `xml:"sz"`
			Color *struct {
				RGB string `xml:"rgb,attr"`
			} `xml:"color"`
			Name struct {
				Val string `xml:"val,attr"`
			} `xml:"name"`
		} `xml:"font"`
	} `xml:"fonts"`
	Fills struct {
		Count int `xml:"count,attr"`
		Fill  []struct {
			PatternFill struct {
				PatternType string `xml:"patternType,attr"`
				FgColor     *struct {
					RGB string `xml:"rgb,attr"`
				} `xml:"fgColor"`
				BgColor *struct {
					Indexed int `xml:"indexed,attr"`
				} `xml:"bgColor"`
			} `xml:"patternFill"`
		} `xml:"fill"`
	} `xml:"fills"`
	Borders struct {
		Count  int        `xml:"count,attr"`
		Border []rdBorder `xml:"border"`
	} `xml:"borders"`
	CellStyleXfs struct {
		Count int `xml:"count,attr"`
	} `xml:"cellStyleXfs"`
	CellXfs struct {
		Count int    `xml:"count,attr"`
		Xf    []rdXf `xml:"xf"`
	} `xml:"cellXfs"`
	CellStyles struct {
		Count int `xml:"count,attr"`
	} `xml:"cellStyles"`
}

type rdBorder struct {
	Left   rdBorderPr `xml:"left"`
	Right  rdBorderPr `xml:"right"`
	Top    rdBorderPr `xml:"top"`
	Bottom rdBorderPr `xml:"bottom"`
}

type rdBorderPr struct {
	Style string `xml:"style,attr"`
	Color *struct {
		RGB string `xml:"rgb,attr"`
	} `xml:"color"`
}

type rdXf struct {
	NumFmtID  int `xml:"numFmtId,attr"`
	FontID    int `xml:"fontId,attr"`
	FillID    int `xml:"fillId,attr"`
	BorderID  int `xml:"borderId,attr"`
	Alignment *struct {
		Horizontal string `xml:"horizontal,attr"`
		Vertical   string `xml:"vertical,attr"`
		WrapText   int    `xml:"wrapText,attr"`
	} `xml:"alignment"`
}

// ---------------------------------------------------------------------------
// Convenience accessors
// ---------------------------------------------------------------------------

// sheet reads the nth worksheet, counting from 1 as the part names do.
func (p *pkg) sheet(t *testing.T, n int) *rdWorksheet {
	t.Helper()
	var ws rdWorksheet
	p.unmarshal(t, fmt.Sprintf("xl/worksheets/sheet%d.xml", n), &ws)
	return &ws
}

func (p *pkg) styles(t *testing.T) *rdStyleSheet {
	t.Helper()
	var ss rdStyleSheet
	p.unmarshal(t, partStyles, &ss)
	return &ss
}

// cells flattens a worksheet into an A1-keyed map.
func (ws *rdWorksheet) cells() map[string]rdCell {
	out := make(map[string]rdCell)
	for _, r := range ws.SheetData.Row {
		for _, c := range r.C {
			out[c.R] = c
		}
	}
	return out
}

// text returns the value of a cell as the reader sees it: the inline string for
// text cells, the raw <v> for everything else.
func (c rdCell) text() string {
	if c.Is != nil {
		return c.Is.T
	}
	return c.V
}

// refs lists the cell references in document order, which is what the ordering
// assertions compare against.
func (ws *rdWorksheet) refs() []string {
	var out []string
	for _, r := range ws.SheetData.Row {
		for _, c := range r.C {
			out = append(out, c.R)
		}
	}
	return out
}

// mergeRefs lists the merge ranges of a worksheet.
func (ws *rdWorksheet) mergeRefs() []string {
	out := make([]string, 0, len(ws.MergeCells.MergeCell))
	for _, m := range ws.MergeCells.MergeCell {
		out = append(out, m.Ref)
	}
	return out
}

// elementOrder returns the local names of the direct children of the root
// element of an XML document. Element order is the one class of OOXML mistake
// that no reader complains about, so the tests check it explicitly.
func elementOrder(t *testing.T, doc string) []string {
	t.Helper()
	dec := xml.NewDecoder(strings.NewReader(doc))
	var out []string
	depth := 0
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		switch tok := tok.(type) {
		case xml.StartElement:
			depth++
			if depth == 2 {
				out = append(out, tok.Name.Local)
			}
		case xml.EndElement:
			depth--
		}
	}
}
