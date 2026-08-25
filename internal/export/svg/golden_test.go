package svg

import (
	"encoding/xml"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/shutx-net/jumping-json-flush/internal/model"
	"github.com/shutx-net/jumping-json-flush/internal/schema"
)

var update = flag.Bool("update", false, "update golden files")

// fixtures are the documents whose drawing is frozen.
//
// The first three are internal/export/dot's, so that two exporters can be
// compared on one document: full.json is the ordinary document, nofk.json the
// one with no relationship at all, edge.json the awkward shapes. Separate
// testdata per exporter is the existing precedent - internal/export/ddl's
// edge.json is already a different file from dot's - and here it is forced:
// svg's edge.json carries a rune dot's cannot (see D17 and the fixture's own
// description).
//
// cycle.json is new, because no fixture in the repository held a cycle between
// two DISTINCT tables or a relationship still spanning more than one rank after
// the ranking is tightened, so cycle breaking and the long-edge virtual chain
// had nothing rendering them. It carries both. It deliberately does NOT carry a
// same-rank relationship: every layering edge is built with a minimum length of
// one rank, so no jjf document can produce one, and a fixture pretending to
// would be a fiction.
var fixtures = []string{"full.json", "edge.json", "nofk.json", "cycle.json"}

// ---------------------------------------------------------------------------
// Fixtures and golden files
// ---------------------------------------------------------------------------

// loadDoc reads a fixture and decodes it. The fixture is checked against the
// schema on the way through, because a fixture that no longer conforms tests
// nothing worth knowing - and it would also be a document the CLI could never
// reach this exporter with.
func loadDoc(t *testing.T, name string) *model.Document {
	t.Helper()

	path := filepath.Join("testdata", name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	validator, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	if err := validator.Validate(path, raw); err != nil {
		t.Fatalf("fixture does not conform to the schema: %v", err)
	}
	doc, err := model.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// goldenName maps a fixture name to the name of its golden file.
func goldenName(fixture string) string {
	return strings.TrimSuffix(fixture, filepath.Ext(fixture)) + ".svg"
}

func checkGolden(t *testing.T, name, have string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(have), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run: go test ./internal/export/svg/ -update): %v", err)
	}
	if string(want) != have {
		t.Errorf("output mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", name, have, want)
	}
}

// TestGolden freezes the bytes of all four fixtures.
//
// What a golden is worth here is worth saying, because it is less than it looks:
// it proves the generator emits what it emitted, and after any layout change
// EVERY byte of all four files changes at once. Nothing in a golden says the
// drawing is right. invariant_test.go is where that is said, against the
// Geometry rather than against these bytes.
func TestGolden(t *testing.T) {
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			checkGolden(t, goldenName(name), render(t, loadDoc(t, name)))
		})
	}
}

// TestFixturesAreDeterministic renders each fixture twice. There are no time
// zone sub-tests as there are for the workbook: those exist because the zip
// format records local timestamps, and nothing in this package reads the clock
// at all. What could make two runs differ here is a map ranged over in the
// layout, which is why layout has TestLayoutIsDeterministic as well - this is
// the same property asserted at the bytes, where a reader can see it.
func TestFixturesAreDeterministic(t *testing.T) {
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			doc := loadDoc(t, name)
			if first, second := render(t, doc), render(t, doc); first != second {
				t.Errorf("two renders of %s differ", name)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Well-formedness
// ---------------------------------------------------------------------------

// TestFixturesAreWellFormedXML parses the WHOLE output of every fixture with
// encoding/xml, token by token.
//
// Unlike internal/export/dot's version of this test, which can only reach the
// HTML-like label payloads inside a DOT file, an SVG file IS an XML document, so
// this is the real check and not an approximation of one. It is what catches the
// case D16 exists for: edge.json carries U+0001 in a logical name, which the
// schema permits and jjf validate accepts, and a writer that passed the rune
// through would produce a file no renderer will parse. This test is why the
// escaping cannot regress silently even if the unit-level pin on
// xml.EscapeText's substitution were deleted.
func TestFixturesAreWellFormedXML(t *testing.T) {
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			out := render(t, loadDoc(t, name))

			if !utf8.ValidString(out) {
				t.Fatal("the output is not valid UTF-8")
			}

			dec := xml.NewDecoder(strings.NewReader(out))
			for {
				_, err := dec.Token()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					t.Fatalf("the output is not well-formed XML: %v", err)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// What each new fixture is for
// ---------------------------------------------------------------------------

// TestEdgeFixtureCoversItsCases names, one by one, the awkward cases the edge
// golden is supposed to show, so that a regression reports which case broke
// rather than dumping a diff of a file with several hundred elements in it.
func TestEdgeFixtureCoversItsCases(t *testing.T) {
	out := render(t, loadDoc(t, "edge.json"))

	tests := []struct {
		name string
		want string
	}{
		{
			"the U+FFFD standing in for the U+0001 the fixture carries, beside the escaped markup characters",
			">グラフ &amp; &lt;図&gt; &#34;面&#34;\uFFFD</text>",
		},
		{"a Japanese logical name, bold, in a header", "font-weight=\"bold\" text-anchor=\"middle\" fill=\"" + colourText + "\">辺</text>"},
		{"a column that is both a primary and a foreign key column", ">PK,FK</text>"},
		{"a precision and scale rendered into the type", ">NUMERIC(5,2)</text>"},
		{"a length winning over a precision", ">NUMERIC(10)</text>"},
		{"the stub's name", ">nodes</text>"},
		{"the stub's second line, the same sentence the DOT stub carries", ">" + stubNote + "</text>"},
		{"the self-reference's label, which rank doubling gives no node to hang on", ">fk_categories_parent</text>"},
		{"the first of two parallel relationships, labelled by its column list because it has no name", ">from_node</text>"},
		{"the second of the two, told apart from the first by nothing but this", ">fk_edge_to_node</text>"},
		{"the composite foreign key that matches a composite primary key", ">fk_edge_labels_edge</text>"},
		{"the foreign key naming a column its own table does not define", ">fk_node_stats_unknown_column</text>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(out, tt.want) {
				t.Errorf("the rendered edge fixture does not contain\n\t%s", tt.want)
			}
		})
	}

	// Three foreign keys name the same undefined table, and they share ONE
	// stub, as they do in the DOT output. The dashed stroke is what makes a
	// stub visible as one, so counting it counts stubs: the stub box's outline
	// and its header band are the only dashed rects in the drawing, one each.
	if got := strings.Count(out, "stroke-dasharray"); got != 2 {
		t.Errorf("the output holds %d dashed rect(s), want 2 - one stub's outline and its header band", got)
	}

	// And the grey stub ink appears nowhere else, which is what says the stub is
	// drawn differently rather than merely labelled differently.
	if got := strings.Count(out, colourStubLine); got == 0 {
		t.Error("nothing in the output is drawn in the stub's grey")
	}
}

// TestCycleFixtureCoversItsCases asserts the three things cycle.json exists for,
// on the Geometry rather than on the bytes: which end of a reversed relationship
// carries the crow's foot, and that a long relationship is drawn as a route with
// corners in it rather than as a straight line through whatever is in the way.
//
// The reversed one is the case with no other net under it. Reversing an edge's
// child and parent along with its layering direction would leave every geometry
// invariant satisfied and every golden internally consistent, and the diagram
// would describe a database in which the foreign key points the other way.
func TestCycleFixtureCoversItsCases(t *testing.T) {
	doc := loadDoc(t, "cycle.json")
	geo := layout(doc)

	// The document's own words for the cycle: customers.latest_payment_id
	// references payments, and payments.customer_id references customers.
	index := map[string]int{}
	for i, n := range geo.Nodes {
		if n.Kind == kindTable {
			index[n.Name] = i
		}
	}

	byLabel := map[string]*GeoEdge{}
	for i := range geo.Edges {
		byLabel[geo.Labels[geo.Edges[i].Label].Text] = &geo.Edges[i]
	}

	back := byLabel["fk_customers_latest_payment"]
	if back == nil {
		t.Fatal("the fixture no longer holds the relationship that closes the cycle")
	}
	if back.Child != index["customers"] || back.Parent != index["payments"] {
		t.Errorf("the reversed relationship runs %d -> %d, want customers (%d) -> payments (%d): breaking a cycle flips the LAYERING direction and nothing else",
			back.Child, back.Parent, index["customers"], index["payments"])
	}

	// The cycle was really broken, which shows in the coordinates: the child of
	// this one sits to the RIGHT of its parent, where every unreversed
	// relationship in the drawing has its child to the left.
	if geo.Nodes[back.Child].Rect.X <= geo.Nodes[back.Parent].Rect.X {
		t.Errorf("the reversed relationship's child is at x %d and its parent at x %d, so nothing was reversed here",
			geo.Nodes[back.Child].Rect.X, geo.Nodes[back.Parent].Rect.X)
	}
	// And its crow's foot is still on the child end: the child column is
	// nullable and not unique, so the child end is many-and-optional whichever
	// way the line runs.
	if !back.ChildEnd.Many || !back.ChildEnd.Optional {
		t.Errorf("the reversed relationship's child end is %+v, want many and optional", back.ChildEnd)
	}
	if back.ParentEnd.Many {
		t.Errorf("the reversed relationship's parent end is %+v, want the ONE end", back.ParentEnd)
	}

	// The long one: payments -> customers spans two ranks, because
	// payments -> orders -> customers is longer. A route across two ranks turns
	// at the label node in between and at the corridor beyond it, so it has
	// more than the two points a single-rank straight run needs.
	long := byLabel["fk_payments_customer"]
	if long == nil {
		t.Fatal("the fixture no longer holds the relationship that spans two ranks")
	}
	if len(long.Points) <= 2 {
		t.Errorf("the two-rank relationship is drawn as %d point(s): %+v", len(long.Points), long.Points)
	}
	if got := geo.Nodes[long.Parent].Rect.X - geo.Nodes[long.Child].Rect.X; got <= 0 {
		t.Errorf("payments is not to the left of customers (%d)", got)
	}
}
