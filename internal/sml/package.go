package sml

import (
	"archive/zip"
	"encoding/xml"
	"io"
	"time"
)

// epoch is the fixed timestamp stamped into every zip entry so that the same
// input always produces a byte-identical .xlsx. MS-DOS cannot represent dates
// before 1980, and archive/zip stores the local wall clock, so this must be UTC:
// with a local time the same instant yields different bytes per time zone.
var epoch = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)

// part is one member of the OPC package.
type part struct {
	// name is the zip entry name. It is always '/' separated and never starts
	// with a slash; path.Join or plain concatenation only, never filepath.Join,
	// which would use backslashes on Windows.
	name string
	// contentType is the [Content_Types].xml Override for this part. It is
	// empty when a Default extension already covers the part.
	contentType string
	// content is the struct encoded into the part.
	content any
}

// contentTypes builds [Content_Types].xml from the parts of the package. Parts
// that declare no content type are covered by a Default extension.
func contentTypes(parts []part) *ctTypes {
	t := &ctTypes{
		Xmlns: nsContentTypes,
		Defaults: []ctDefault{
			{Extension: "rels", ContentType: ctRels},
			{Extension: "xml", ContentType: ctXML},
		},
	}
	for _, p := range parts {
		if p.contentType == "" {
			continue
		}
		t.Overrides = append(t.Overrides, ctOverride{
			PartName:    "/" + p.name, // PartName is absolute, zip names are not
			ContentType: p.contentType,
		})
	}
	return t
}

// writePart streams v into a new zip entry as XML. The encoder writes straight
// into the zip entry with no intermediate buffer and no indentation:
// indentation would leak whitespace into cell text.
func writePart(zw *zip.Writer, name string, v any) error {
	pw, err := zw.CreateHeader(&zip.FileHeader{
		Name:     name,
		Method:   zip.Deflate,
		Modified: epoch,
	})
	if err != nil {
		return err
	}
	if _, err := io.WriteString(pw, xmlDecl); err != nil {
		return err
	}
	enc := xml.NewEncoder(pw)
	if err := enc.Encode(v); err != nil {
		return err
	}
	return enc.Flush()
}

// countingWriter counts the bytes written through it so WriteTo can report a
// byte count without buffering the package.
type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}
