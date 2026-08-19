package sml

import "encoding/xml"

// xmlDecl is written in front of every part. xml.Header has no standalone="yes",
// which is what Excel itself emits, so the declaration is spelled out here.
const xmlDecl = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"

// XML namespaces of the parts that make up a SpreadsheetML package.
const (
	nsSpreadsheetML = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
	nsRelationships = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	nsContentTypes  = "http://schemas.openxmlformats.org/package/2006/content-types"
	nsPkgRels       = "http://schemas.openxmlformats.org/package/2006/relationships"
	nsCoreProps     = "http://schemas.openxmlformats.org/package/2006/metadata/core-properties"
	nsDC            = "http://purl.org/dc/elements/1.1/"
	nsDCTerms       = "http://purl.org/dc/terms/"
	nsXSI           = "http://www.w3.org/2001/XMLSchema-instance"
)

// Relationship types. core-properties is the odd one out: it lives under the
// package relationship namespace, not under officeDocument.
const (
	relTypeOfficeDoc = nsRelationships + "/officeDocument"
	relTypeWorksheet = nsRelationships + "/worksheet"
	relTypeStyles    = nsRelationships + "/styles"
	relTypeCoreProps = nsPkgRels + "/metadata/core-properties"
)

// Content types. core-properties uses the -package family, everything else uses
// the -officedocument family.
const (
	ctWorkbook  = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"
	ctWorksheet = "application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"
	ctStyles    = "application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"
	ctCoreProps = "application/vnd.openxmlformats-package.core-properties+xml"
	ctRels      = "application/vnd.openxmlformats-package.relationships+xml"
	ctXML       = "application/xml"
)

// Part names. PartName values inside [Content_Types].xml carry a leading slash,
// zip entry names never do.
const (
	partContentTypes = "[Content_Types].xml"
	partRootRels     = "_rels/.rels"
	partCoreProps    = "docProps/core.xml"
	partWorkbook     = "xl/workbook.xml"
	partWorkbookRels = "xl/_rels/workbook.xml.rels"
	partStyles       = "xl/styles.xml"
)

// ---------------------------------------------------------------------------
// [Content_Types].xml
// ---------------------------------------------------------------------------

// ctTypes mirrors CT_Types. Its content model is an xsd:choice, so Default and
// Override may be interleaved; Defaults are written first by convention.
type ctTypes struct {
	XMLName   xml.Name     `xml:"Types"`
	Xmlns     string       `xml:"xmlns,attr"`
	Defaults  []ctDefault  `xml:"Default"`
	Overrides []ctOverride `xml:"Override"`
}

type ctDefault struct {
	Extension   string `xml:"Extension,attr"`
	ContentType string `xml:"ContentType,attr"`
}

type ctOverride struct {
	PartName    string `xml:"PartName,attr"`
	ContentType string `xml:"ContentType,attr"`
}

// ---------------------------------------------------------------------------
// _rels/*.rels
// ---------------------------------------------------------------------------

// pkgRelationships mirrors the Relationships root of an OPC .rels part.
type pkgRelationships struct {
	XMLName xml.Name          `xml:"Relationships"`
	Xmlns   string            `xml:"xmlns,attr"`
	Rels    []pkgRelationship `xml:"Relationship"`
}

type pkgRelationship struct {
	ID     string `xml:"Id,attr"`
	Type   string `xml:"Type,attr"`
	Target string `xml:"Target,attr"`
}

// ---------------------------------------------------------------------------
// docProps/core.xml
// ---------------------------------------------------------------------------

// xlCoreProperties mirrors CT_CoreProperties. Its content model is an xsd:all,
// so child order is free; the order below follows the schema listing.
//
// The namespace prefixes are plain attributes rather than struct tag
// namespaces, because encoding/xml derives prefixes from the URI and would
// rename them.
type xlCoreProperties struct {
	XMLName      xml.Name  `xml:"cp:coreProperties"`
	XmlnsCp      string    `xml:"xmlns:cp,attr"`
	XmlnsDc      string    `xml:"xmlns:dc,attr"`
	XmlnsDcterms string    `xml:"xmlns:dcterms,attr"`
	XmlnsXsi     string    `xml:"xmlns:xsi,attr"`
	Created      *xlW3CDTF `xml:"dcterms:created,omitempty"`
	Creator      string    `xml:"dc:creator,omitempty"`
	Description  string    `xml:"dc:description,omitempty"`
	Title        string    `xml:"dc:title,omitempty"`
}

// xlW3CDTF is a dcterms date, which must carry an explicit xsi:type.
type xlW3CDTF struct {
	Type  string `xml:"xsi:type,attr"`
	Value string `xml:",chardata"`
}

// ---------------------------------------------------------------------------
// xl/workbook.xml
// ---------------------------------------------------------------------------

// xlWorkbook mirrors CT_Workbook. The XSD declares an xsd:sequence, so the
// field order below IS the element order. Do not reorder these fields.
//
//	fileVersion, fileSharing, workbookPr, workbookProtection, bookViews,
//	sheets, functionGroups, externalReferences, definedNames, calcPr, ...
type xlWorkbook struct {
	XMLName xml.Name `xml:"workbook"`
	Xmlns   string   `xml:"xmlns,attr"`
	XmlnsR  string   `xml:"xmlns:r,attr"`
	Sheets  xlSheets `xml:"sheets"`
}

type xlSheets struct {
	Sheet []xlSheet `xml:"sheet"`
}

type xlSheet struct {
	Name    string `xml:"name,attr"`
	SheetID int    `xml:"sheetId,attr"` // never omitempty: sheet ids are 1-based but meaningful
	RID     string `xml:"r:id,attr"`    // the colon is emitted verbatim
}

// ---------------------------------------------------------------------------
// xl/worksheets/sheetN.xml
// ---------------------------------------------------------------------------

// xlWorksheet mirrors CT_Worksheet. The XSD declares an xsd:sequence, so the
// field order below IS the element order. Do not reorder these fields.
//
//	sheetPr, dimension, sheetViews, sheetFormatPr, cols, sheetData,
//	sheetCalcPr, sheetProtection, protectedRanges, scenarios, autoFilter,
//	sortState, dataConsolidate, customSheetViews, mergeCells, phoneticPr,
//	conditionalFormatting, dataValidations, hyperlinks, printOptions,
//	pageMargins, pageSetup, headerFooter, ..., extLst
//
// cols comes BEFORE sheetData and mergeCells comes AFTER it. Getting this wrong
// is invisible to LibreOffice and openpyxl but makes Excel offer to repair the
// file, so the ordering is pinned by the type rather than by convention.
type xlWorksheet struct {
	XMLName       xml.Name       `xml:"worksheet"`
	Xmlns         string         `xml:"xmlns,attr"`
	Dimension     *xlDimension   `xml:"dimension,omitempty"`
	SheetViews    *xlSheetViews  `xml:"sheetViews,omitempty"`
	SheetFormatPr *xlSheetFormat `xml:"sheetFormatPr,omitempty"`
	Cols          *xlCols        `xml:"cols,omitempty"` // BEFORE sheetData
	SheetData     xlSheetData    `xml:"sheetData"`
	MergeCells    *xlMergeCells  `xml:"mergeCells,omitempty"` // AFTER sheetData
}

type xlDimension struct {
	Ref string `xml:"ref,attr"`
}

type xlSheetViews struct {
	SheetView []xlSheetView `xml:"sheetView"`
}

// xlSheetView mirrors CT_SheetView. XSD sequence:
//
//	pane, selection, pivotSelection, extLst
type xlSheetView struct {
	TabSelected    int          `xml:"tabSelected,attr,omitempty"`
	WorkbookViewID int          `xml:"workbookViewId,attr"` // never omitempty: 0 is the only usual value
	Pane           *xlPane      `xml:"pane,omitempty"`
	Selection      *xlSelection `xml:"selection,omitempty"`
}

// xlPane mirrors CT_Pane. A split of zero means "not split" and is omitted.
type xlPane struct {
	XSplit      float64 `xml:"xSplit,attr,omitempty"`
	YSplit      float64 `xml:"ySplit,attr,omitempty"`
	TopLeftCell string  `xml:"topLeftCell,attr,omitempty"`
	ActivePane  string  `xml:"activePane,attr,omitempty"`
	State       string  `xml:"state,attr,omitempty"`
}

type xlSelection struct {
	Pane       string `xml:"pane,attr,omitempty"`
	ActiveCell string `xml:"activeCell,attr,omitempty"`
	Sqref      string `xml:"sqref,attr,omitempty"`
}

// xlSheetFormat mirrors CT_SheetFormatPr. defaultRowHeight is required by the
// XSD, so this element is only emitted when a default row height is set.
type xlSheetFormat struct {
	DefaultRowHeight float64 `xml:"defaultRowHeight,attr"`
	CustomHeight     int     `xml:"customHeight,attr,omitempty"`
}

type xlCols struct {
	Col []xlCol `xml:"col"`
}

// xlCol mirrors CT_Col. min and max are 1-based and inclusive; without
// customWidth Excel falls back to the default column width.
type xlCol struct {
	Min         int     `xml:"min,attr"` // never omitempty
	Max         int     `xml:"max,attr"` // never omitempty
	Width       float64 `xml:"width,attr,omitempty"`
	CustomWidth int     `xml:"customWidth,attr,omitempty"`
}

type xlSheetData struct {
	Row []xlRow `xml:"row"`
}

// xlRow mirrors CT_Row. ht is in points and needs customHeight to stick.
type xlRow struct {
	R            int      `xml:"r,attr"` // never omitempty: the row number
	Ht           float64  `xml:"ht,attr,omitempty"`
	CustomHeight int      `xml:"customHeight,attr,omitempty"`
	C            []xlCell `xml:"c"`
}

// xlCell mirrors CT_Cell. XSD sequence:
//
//	f, v, is, extLst
//
// v must be written before is. Numeric cells carry no t attribute at all,
// because the default cell type is "n".
type xlCell struct {
	R  string       `xml:"r,attr"` // never omitempty: the A1 reference
	S  int          `xml:"s,attr,omitempty"`
	T  string       `xml:"t,attr,omitempty"`
	V  string       `xml:"v,omitempty"`
	Is *xlInlineStr `xml:"is,omitempty"`
}

// xlInlineStr mirrors CT_Rst restricted to plain text.
type xlInlineStr struct {
	T xlText `xml:"t"`
}

// xlText carries cell text. Leading or trailing whitespace needs
// xml:space="preserve" to survive a round trip.
type xlText struct {
	Space string `xml:"xml:space,attr,omitempty"`
	Value string `xml:",chardata"`
}

type xlMergeCells struct {
	Count     int           `xml:"count,attr"`
	MergeCell []xlMergeCell `xml:"mergeCell"`
}

type xlMergeCell struct {
	Ref string `xml:"ref,attr"`
}

// ---------------------------------------------------------------------------
// xl/styles.xml
// ---------------------------------------------------------------------------

// xlStyleSheet mirrors CT_Stylesheet. The XSD declares an xsd:sequence, so the
// field order below IS the element order. Do not reorder these fields.
//
//	numFmts, fonts, fills, borders, cellStyleXfs, cellXfs, cellStyles,
//	dxfs, tableStyles, colors, extLst
type xlStyleSheet struct {
	XMLName      xml.Name       `xml:"styleSheet"`
	Xmlns        string         `xml:"xmlns,attr"`
	NumFmts      *xlNumFmts     `xml:"numFmts,omitempty"`
	Fonts        xlFonts        `xml:"fonts"`
	Fills        xlFills        `xml:"fills"`
	Borders      xlBorders      `xml:"borders"`
	CellStyleXfs xlCellStyleXfs `xml:"cellStyleXfs"`
	CellXfs      xlCellXfs      `xml:"cellXfs"`
	CellStyles   xlCellStyles   `xml:"cellStyles"`
}

type xlNumFmts struct {
	Count  int        `xml:"count,attr"`
	NumFmt []xlNumFmt `xml:"numFmt"`
}

// xlNumFmt holds a raw Excel format code. Never pre-escape it: encoding/xml
// escapes the value, and escaping twice turns the code into literal text.
type xlNumFmt struct {
	NumFmtID   int    `xml:"numFmtId,attr"` // never omitempty: an ID value, not an index
	FormatCode string `xml:"formatCode,attr"`
}

type xlFonts struct {
	Count int      `xml:"count,attr"`
	Font  []xlFont `xml:"font"`
}

// xlFont mirrors CT_Font, whose content model is an xsd:choice, so child order
// is free. The order below matches what Excel emits. The choice requires at
// least one child, so sz and name are always written.
type xlFont struct {
	B     *xlBoolProp `xml:"b,omitempty"`
	I     *xlBoolProp `xml:"i,omitempty"`
	U     *xlBoolProp `xml:"u,omitempty"`
	Sz    xlValF      `xml:"sz"`
	Color *xlColor    `xml:"color,omitempty"`
	Name  xlValS      `xml:"name"`
}

// xlBoolProp mirrors CT_BooleanProperty, whose val attribute defaults to true,
// so the bare element is enough to turn the property on.
type xlBoolProp struct{}

type xlValF struct {
	Val float64 `xml:"val,attr"`
}

type xlValS struct {
	Val string `xml:"val,attr"`
}

// xlColor mirrors CT_Color. rgb is always 8 digit ARGB.
type xlColor struct {
	RGB     string `xml:"rgb,attr,omitempty"`
	Indexed int    `xml:"indexed,attr,omitempty"`
}

type xlFills struct {
	Count int      `xml:"count,attr"`
	Fill  []xlFill `xml:"fill"`
}

type xlFill struct {
	PatternFill xlPatternFill `xml:"patternFill"`
}

// xlPatternFill mirrors CT_PatternFill. XSD sequence: fgColor, bgColor.
// For patternType="solid" fgColor is the paint colour and bgColor
// indexed="64" is the conventional companion.
type xlPatternFill struct {
	PatternType string   `xml:"patternType,attr,omitempty"`
	FgColor     *xlColor `xml:"fgColor,omitempty"`
	BgColor     *xlColor `xml:"bgColor,omitempty"`
}

type xlBorders struct {
	Count  int        `xml:"count,attr"`
	Border []xlBorder `xml:"border"`
}

// xlBorder mirrors CT_Border. XSD sequence:
//
//	left, right, top, bottom, diagonal, vertical, horizontal
type xlBorder struct {
	Left     xlBorderPr `xml:"left"`
	Right    xlBorderPr `xml:"right"`
	Top      xlBorderPr `xml:"top"`
	Bottom   xlBorderPr `xml:"bottom"`
	Diagonal xlBorderPr `xml:"diagonal"`
}

// xlBorderPr mirrors CT_BorderPr, whose style attribute defaults to "none".
type xlBorderPr struct {
	Style string   `xml:"style,attr,omitempty"`
	Color *xlColor `xml:"color,omitempty"`
}

type xlCellStyleXfs struct {
	Count int         `xml:"count,attr"`
	Xf    []xlStyleXf `xml:"xf"`
}

// xlStyleXf is a cellStyleXfs entry. Unlike a cellXfs entry it carries no xfId,
// because it would only point back at itself.
type xlStyleXf struct {
	NumFmtID int `xml:"numFmtId,attr"` // never omitempty
	FontID   int `xml:"fontId,attr"`   // never omitempty
	FillID   int `xml:"fillId,attr"`   // never omitempty
	BorderID int `xml:"borderId,attr"` // never omitempty
}

type xlCellXfs struct {
	Count int    `xml:"count,attr"`
	Xf    []xlXf `xml:"xf"`
}

// xlXf mirrors CT_Xf. XSD sequence:
//
//	alignment, protection, extLst
//
// numFmtId is an ID value while fontId, fillId and borderId are indices into
// the corresponding arrays. None of them may be omitted: a zero there is a
// meaningful reference, not an absent one.
type xlXf struct {
	NumFmtID          int          `xml:"numFmtId,attr"`
	FontID            int          `xml:"fontId,attr"`
	FillID            int          `xml:"fillId,attr"`
	BorderID          int          `xml:"borderId,attr"`
	XfID              int          `xml:"xfId,attr"`
	ApplyNumberFormat int          `xml:"applyNumberFormat,attr,omitempty"`
	ApplyFont         int          `xml:"applyFont,attr,omitempty"`
	ApplyFill         int          `xml:"applyFill,attr,omitempty"`
	ApplyBorder       int          `xml:"applyBorder,attr,omitempty"`
	ApplyAlignment    int          `xml:"applyAlignment,attr,omitempty"`
	Alignment         *xlAlignment `xml:"alignment,omitempty"`
}

type xlAlignment struct {
	Horizontal string `xml:"horizontal,attr,omitempty"`
	Vertical   string `xml:"vertical,attr,omitempty"`
	WrapText   int    `xml:"wrapText,attr,omitempty"`
}

type xlCellStyles struct {
	Count     int           `xml:"count,attr"`
	CellStyle []xlCellStyle `xml:"cellStyle"`
}

type xlCellStyle struct {
	Name      string `xml:"name,attr"`
	XfID      int    `xml:"xfId,attr"`      // never omitempty
	BuiltinID int    `xml:"builtinId,attr"` // never omitempty
}
