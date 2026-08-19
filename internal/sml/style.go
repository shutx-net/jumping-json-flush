package sml

import "strings"

// firstCustomNumFmtID is the lowest number format id available to documents.
// Ids 0 through 163 are reserved for Excel's built-in formats.
const firstCustomNumFmtID = 164

// Defaults applied when a Font leaves a field at its zero value. CT_Font's
// content model requires at least one child element, so both are always
// written out.
const (
	defaultFontName = "Calibri"
	defaultFontSize = 11.0
)

// StyleID identifies a cell format registered with a Workbook. The zero value
// is the workbook's default format.
type StyleID int

// Style is a cell format value. Every field is comparable so the whole struct
// can be a map key, which is what makes deduplication trivial.
type Style struct {
	Font   Font
	Fill   Fill
	Border Border
	Align  Alignment
	// NumFmt is an Excel format code. "" means General. Write raw quotes,
	// e.g. `yyyy"年"m"月"d"日"` - do not pre-escape them.
	NumFmt string
}

// RGB is an 8 digit ARGB hex value such as "FF4472C4". A 6 digit value is
// accepted and gets an opaque FF alpha channel prepended. "" and anything that
// is not hexadecimal mean automatic, and no colour is written.
type RGB string

// Font is a cell font. A zero Name or Size means the default.
type Font struct {
	Name      string
	Size      float64
	Bold      bool
	Italic    bool
	Underline bool
	Color     RGB
}

// Fill is a solid fill. An empty Color means no fill.
//
// The unexported pattern field exists so the two fill slots Excel reserves can
// be seeded without colliding with any value a caller can construct.
type Fill struct {
	Color   RGB
	pattern string
}

// Border is the set of edges drawn around a cell.
type Border struct {
	Left, Right, Top, Bottom Edge
}

// Edge is one side of a cell border.
type Edge struct {
	Style EdgeStyle
	Color RGB
}

// EdgeStyle is a border line style from ST_BorderStyle.
type EdgeStyle string

// Border line styles.
const (
	EdgeNone   EdgeStyle = ""
	EdgeThin   EdgeStyle = "thin"
	EdgeMedium EdgeStyle = "medium"
	EdgeThick  EdgeStyle = "thick"
	EdgeDouble EdgeStyle = "double"
)

// Box returns a Border with the same edge on all four sides.
func Box(style EdgeStyle, color RGB) Border {
	e := Edge{Style: style, Color: color}
	return Border{Left: e, Right: e, Top: e, Bottom: e}
}

// Alignment is the cell alignment. The zero value means Excel's default.
type Alignment struct {
	Horizontal HAlign
	Vertical   VAlign
	WrapText   bool
}

// HAlign is a horizontal alignment from ST_HorizontalAlignment.
type HAlign string

// Horizontal alignments.
const (
	HAlignDefault HAlign = ""
	HAlignLeft    HAlign = "left"
	HAlignCenter  HAlign = "center"
	HAlignRight   HAlign = "right"
)

// VAlign is a vertical alignment from ST_VerticalAlignment.
type VAlign string

// Vertical alignments.
const (
	VAlignDefault VAlign = ""
	VAlignTop     VAlign = "top"
	VAlignCenter  VAlign = "center"
	VAlignBottom  VAlign = "bottom"
)

// normalize canonicalises the colours in s so that two styles that render
// identically also deduplicate to a single entry in styles.xml.
func (s Style) normalize() Style {
	s.Font.Color = normalizeRGB(s.Font.Color)
	s.Fill.Color = normalizeRGB(s.Fill.Color)
	s.Fill.pattern = ""
	s.Border.Left = s.Border.Left.normalize()
	s.Border.Right = s.Border.Right.normalize()
	s.Border.Top = s.Border.Top.normalize()
	s.Border.Bottom = s.Border.Bottom.normalize()
	return s
}

// normalize drops the colour of an absent edge so that Edge{Style: EdgeNone}
// and Edge{Style: EdgeNone, Color: "FF000000"} are the same value.
func (e Edge) normalize() Edge {
	if e.Style == EdgeNone {
		return Edge{}
	}
	e.Color = normalizeRGB(e.Color)
	return e
}

// normalizeRGB upper-cases c and widens a 6 digit RGB value to 8 digit ARGB.
// Anything that is not 6 or 8 hexadecimal digits becomes "" (automatic).
func normalizeRGB(c RGB) RGB {
	s := strings.ToUpper(string(c))
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] >= '0' && s[i] <= '9', s[i] >= 'A' && s[i] <= 'F':
		default:
			return ""
		}
	}
	switch len(s) {
	case 6:
		return RGB("FF" + s)
	case 8:
		return RGB(s)
	default:
		return ""
	}
}

// indexer maps values to 0-based indices, returning the same index for equal
// values and preserving insertion order. Ranging over a Go map is random, so
// every ordered emission must walk indexer.order rather than indexer.idx.
type indexer[T comparable] struct {
	idx   map[T]int
	order []T
}

func (i *indexer[T]) intern(v T) int {
	if i.idx == nil {
		i.idx = make(map[T]int)
	}
	if n, ok := i.idx[v]; ok {
		return n
	}
	n := len(i.order)
	i.idx[v] = n
	i.order = append(i.order, v)
	return n
}

// xfKey is one cellXfs entry. Every field must be comparable so the whole
// struct can be a map key for deduplication.
type xfKey struct {
	numFmtID int // an ID value, NOT an index into numFmts
	fontID   int // an index into fonts
	fillID   int // an index into fills
	borderID int // an index into borders
	align    Alignment
}

// styleRegistry interns the pieces of styles.xml and hands out cellXfs indices.
type styleRegistry struct {
	numFmts indexer[string]
	fonts   indexer[Font]
	fills   indexer[Fill]
	borders indexer[Border]
	xfs     indexer[xfKey]
}

// init seeds the slots Excel reserves.
//
//	fonts[0]   default font
//	fills[0]   patternType="none"     (reserved by Excel)
//	fills[1]   patternType="gray125"  (reserved by Excel)
//	borders[0] no borders
//	cellXfs[0] default format
//
// Skipping the two reserved fill slots produces a file that validates against
// the official XSD and reads correctly in LibreOffice, but renders with the
// wrong colours in Excel.
func (r *styleRegistry) init() {
	r.fonts.intern(Font{})
	r.fills.intern(Fill{})                        // patternType="none"
	r.fills.intern(Fill{pattern: patternGray125}) // unreachable from outside
	r.borders.intern(Border{})
	r.xfs.intern(xfKey{})
}

const (
	patternNone    = "none"
	patternSolid   = "solid"
	patternGray125 = "gray125"
)

// style registers s and returns the cellXfs index that cells refer to.
func (r *styleRegistry) style(s Style) StyleID {
	s = s.normalize()
	return StyleID(r.xfs.intern(xfKey{
		numFmtID: r.numFmtID(s.NumFmt),
		fontID:   r.fonts.intern(s.Font),
		fillID:   r.fills.intern(s.Fill),
		borderID: r.borders.intern(s.Border),
		align:    s.Align,
	}))
}

// numFmtID returns the number format id for an Excel format code. The result is
// an id value rather than an index, so custom codes start at 164.
func (r *styleRegistry) numFmtID(code string) int {
	if code == "" {
		return 0 // General
	}
	return firstCustomNumFmtID + r.numFmts.intern(code)
}

// xml renders the registry as styles.xml. Every loop walks insertion order, so
// the same set of styles always produces the same bytes.
func (r *styleRegistry) xml() *xlStyleSheet {
	ss := &xlStyleSheet{Xmlns: nsSpreadsheetML}

	if n := len(r.numFmts.order); n > 0 {
		nf := &xlNumFmts{Count: n, NumFmt: make([]xlNumFmt, 0, n)}
		for i, code := range r.numFmts.order {
			nf.NumFmt = append(nf.NumFmt, xlNumFmt{
				NumFmtID:   firstCustomNumFmtID + i,
				FormatCode: code,
			})
		}
		ss.NumFmts = nf
	}

	ss.Fonts = xlFonts{Count: len(r.fonts.order), Font: make([]xlFont, 0, len(r.fonts.order))}
	for _, f := range r.fonts.order {
		ss.Fonts.Font = append(ss.Fonts.Font, fontXML(f))
	}

	ss.Fills = xlFills{Count: len(r.fills.order), Fill: make([]xlFill, 0, len(r.fills.order))}
	for _, f := range r.fills.order {
		ss.Fills.Fill = append(ss.Fills.Fill, fillXML(f))
	}

	ss.Borders = xlBorders{Count: len(r.borders.order), Border: make([]xlBorder, 0, len(r.borders.order))}
	for _, b := range r.borders.order {
		ss.Borders.Border = append(ss.Borders.Border, borderXML(b))
	}

	ss.CellStyleXfs = xlCellStyleXfs{Count: 1, Xf: []xlStyleXf{{}}}

	ss.CellXfs = xlCellXfs{Count: len(r.xfs.order), Xf: make([]xlXf, 0, len(r.xfs.order))}
	for _, k := range r.xfs.order {
		ss.CellXfs.Xf = append(ss.CellXfs.Xf, xfXML(k))
	}

	ss.CellStyles = xlCellStyles{
		Count:     1,
		CellStyle: []xlCellStyle{{Name: "Normal", XfID: 0, BuiltinID: 0}},
	}
	return ss
}

func fontXML(f Font) xlFont {
	x := xlFont{}
	if f.Bold {
		x.B = &xlBoolProp{}
	}
	if f.Italic {
		x.I = &xlBoolProp{}
	}
	if f.Underline {
		x.U = &xlBoolProp{}
	}
	size := f.Size
	if size == 0 {
		size = defaultFontSize
	}
	x.Sz = xlValF{Val: size}
	x.Color = colorXML(f.Color)
	name := f.Name
	if name == "" {
		name = defaultFontName
	}
	x.Name = xlValS{Val: name}
	return x
}

func fillXML(f Fill) xlFill {
	switch {
	case f.pattern != "":
		return xlFill{PatternFill: xlPatternFill{PatternType: f.pattern}}
	case f.Color == "":
		return xlFill{PatternFill: xlPatternFill{PatternType: patternNone}}
	default:
		return xlFill{PatternFill: xlPatternFill{
			PatternType: patternSolid,
			FgColor:     &xlColor{RGB: string(f.Color)},
			BgColor:     &xlColor{Indexed: 64},
		}}
	}
}

func borderXML(b Border) xlBorder {
	return xlBorder{
		Left:   edgeXML(b.Left),
		Right:  edgeXML(b.Right),
		Top:    edgeXML(b.Top),
		Bottom: edgeXML(b.Bottom),
	}
}

func edgeXML(e Edge) xlBorderPr {
	if e.Style == EdgeNone {
		return xlBorderPr{}
	}
	return xlBorderPr{Style: string(e.Style), Color: colorXML(e.Color)}
}

func xfXML(k xfKey) xlXf {
	x := xlXf{
		NumFmtID: k.numFmtID,
		FontID:   k.fontID,
		FillID:   k.fillID,
		BorderID: k.borderID,
	}
	// apply* attributes are optional, but writing them keeps Excel from
	// treating an absent flag as "inherit from the parent style".
	if k.numFmtID != 0 {
		x.ApplyNumberFormat = 1
	}
	if k.fontID != 0 {
		x.ApplyFont = 1
	}
	if k.fillID != 0 {
		x.ApplyFill = 1
	}
	if k.borderID != 0 {
		x.ApplyBorder = 1
	}
	if k.align != (Alignment{}) {
		x.ApplyAlignment = 1
		a := &xlAlignment{
			Horizontal: string(k.align.Horizontal),
			Vertical:   string(k.align.Vertical),
		}
		if k.align.WrapText {
			a.WrapText = 1
		}
		x.Alignment = a
	}
	return x
}

// colorXML returns a colour element, or nil when the colour is automatic.
func colorXML(c RGB) *xlColor {
	if c == "" {
		return nil
	}
	return &xlColor{RGB: string(c)}
}
