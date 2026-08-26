package svg

import (
	"bufio"
	"io"

	"github.com/shutx-net/jumping-json-flush/internal/exitcode"
	"github.com/shutx-net/jumping-json-flush/internal/model"
)

// Export draws doc and writes it to dst as an SVG image.
//
// The three stages are the two representations and the file: layout turns the
// document into a Geometry, buildScene turns that into a flat list of painted
// primitives, and writeScene turns those into XML. Each one throws information
// away - the Geometry knows no font, the Scene knows no rank - which is what
// lets the invariant suite make its ten statements about the middle one and the
// writer make none at all.
//
// Nothing below this function returns an error. They all write into the
// bufio.Writer, which latches its first write error and turns every later write
// into a no-op, so Flush is the single place a failure can surface. Threading an
// error through every writer, or wrapping dst in a sticky-error type, would add
// code and a type for no extra information.
//
// Nothing is checked and nothing is reported, ever. A document whose foreign key
// names no table it defines is legal, and it gets drawn as a dashed stub: the
// picture shows exactly what the JSON claims. jjf validate is where a document
// is spoken about.
func Export(dst io.Writer, doc *model.Document) error {
	bw := bufio.NewWriter(dst)
	writeScene(bw, buildScene(layout(doc)))
	if err := bw.Flush(); err != nil {
		return exitcode.Wrap(exitcode.OutputFailed, "write diagram", err)
	}
	return nil
}
