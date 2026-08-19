package xlsx

import "github.com/shutx-net/jumping-json-flush/internal/sml"

// This file decides what a jjf design document looks like. Colours, fonts,
// row heights and column widths appear here and nowhere else, so that neither
// internal/model nor internal/sml ever learns anything about presentation.

// Palette. Every value is opaque ARGB.
const (
	colorHeaderBG = sml.RGB("FF4472C4") // heading row background
	colorHeaderFG = sml.RGB("FFFFFFFF") // heading row text
	colorSection  = sml.RGB("FFD9E1F2") // label and constraint block background
	colorStripe   = sml.RGB("FFF2F2F2") // every other body row
	colorGrid     = sml.RGB("FF808080") // cell borders
)

// Fonts. Yu Gothic ships with Excel on Windows and on macOS in a Japanese
// installation; elsewhere the reader substitutes a font of its own.
const (
	fontName        = "Yu Gothic"
	fontSizeBody    = 11.0
	fontSizeHeading = 13.0
	fontSizeTitle   = 16.0
)

// Sheet names, in workbook order. Table sheets are named after the table.
const (
	sheetNameCover     = "表紙"
	sheetNameTableList = "テーブル一覧"
)

// Row heights, in points. Rows that hold wrapped free text are left without a
// height on purpose, so that the reader fits them to their contents.
const (
	heightCoverTitle = 34.0
	heightCoverRow   = 21.0
	heightSheetTitle = 28.0
	heightHeader     = 32.0
)

// Column widths, in characters of the default font's digit zero. A full-width
// Japanese character occupies about two of them, so a column that has to show n
// of them needs a width of roughly 2n+2.
const (
	widthNo          = 6.0
	widthPhysName    = 24.0
	widthLogicalName = 24.0
	widthType        = 26.0
	widthLength      = 10.0
	widthFlag        = 12.0
	widthDefault     = 20.0
	widthDescription = 48.0
	widthCount       = 12.0
	widthSheetName   = 26.0
	widthCoverLabel  = 24.0
	widthCoverValue  = 22.0
)

// styles is a pair of cell formats that differ only in the striping applied to
// alternating body rows.
type styles struct{ plain, striped sml.StyleID }

// pick returns the striped format for a striped row and the plain one for the
// rest.
func (s styles) pick(striped bool) sml.StyleID {
	if striped {
		return s.striped
	}
	return s.plain
}

// theme is the style catalogue of one workbook. Style ids are handed out per
// workbook, so a theme may only be used with the workbook newTheme was given.
type theme struct {
	coverTitle sml.StyleID // cover sheet banner
	sheetTitle sml.StyleID // title band of a table sheet
	label      sml.StyleID // cover sheet field label
	caption    sml.StyleID // free text below a title, without a border
	tableHdr   sml.StyleID // heading row of a table
	sectionHdr sml.StyleID // heading of a constraint block
	text       styles      // left aligned body text
	center     styles      // short values and numbers
	wrap       styles      // long free text
}

// newTheme resolves every style the generator uses against wb.
func newTheme(wb *sml.Workbook) *theme {
	grid := sml.Box(sml.EdgeThin, colorGrid)
	stripe := sml.Fill{Color: colorStripe}

	body := sml.Font{Name: fontName, Size: fontSizeBody}
	bold := body
	bold.Bold = true
	reversed := bold
	reversed.Color = colorHeaderFG

	left := sml.Alignment{Horizontal: sml.HAlignLeft, Vertical: sml.VAlignCenter}
	centered := sml.Alignment{Horizontal: sml.HAlignCenter, Vertical: sml.VAlignCenter}
	wrapped := sml.Alignment{Horizontal: sml.HAlignLeft, Vertical: sml.VAlignTop, WrapText: true}

	// pair registers the plain and the striped variant of one body format.
	pair := func(align sml.Alignment) styles {
		return styles{
			plain:   wb.Style(sml.Style{Font: body, Border: grid, Align: align}),
			striped: wb.Style(sml.Style{Font: body, Fill: stripe, Border: grid, Align: align}),
		}
	}

	return &theme{
		coverTitle: wb.Style(sml.Style{
			Font:   sml.Font{Name: fontName, Size: fontSizeTitle, Bold: true},
			Border: sml.Border{Bottom: sml.Edge{Style: sml.EdgeMedium, Color: colorHeaderBG}},
			Align:  centered,
		}),
		sheetTitle: wb.Style(sml.Style{
			Font:   sml.Font{Name: fontName, Size: fontSizeHeading, Bold: true, Color: colorHeaderFG},
			Fill:   sml.Fill{Color: colorHeaderBG},
			Border: grid,
			Align:  left,
		}),
		label: wb.Style(sml.Style{
			Font:   bold,
			Fill:   sml.Fill{Color: colorSection},
			Border: grid,
			Align:  left,
		}),
		caption: wb.Style(sml.Style{Font: body, Align: wrapped}),
		tableHdr: wb.Style(sml.Style{
			Font:   reversed,
			Fill:   sml.Fill{Color: colorHeaderBG},
			Border: grid,
			Align:  centered,
		}),
		sectionHdr: wb.Style(sml.Style{
			Font:   reversed,
			Fill:   sml.Fill{Color: colorHeaderBG},
			Border: grid,
			Align:  left,
		}),
		text:   pair(left),
		center: pair(centered),
		wrap:   pair(wrapped),
	}
}
