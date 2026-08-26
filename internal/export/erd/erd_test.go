package erd

import (
	"slices"
	"testing"

	"github.com/shutx-net/jumping-json-flush/internal/model"
)

// ptr returns a pointer to n, so that a table row can declare an optional
// numeric column attribute inline.
func ptr(n int) *int { return &n }

func TestRenderType(t *testing.T) {
	tests := []struct {
		name string
		in   model.Column
		want string
	}{
		{"no size at all", model.Column{Type: "TEXT"}, "TEXT"},
		{"length", model.Column{Type: "VARCHAR", Length: ptr(255)}, "VARCHAR(255)"},
		{
			"precision and scale, one comma and no space",
			model.Column{Type: "NUMERIC", Precision: ptr(10), Scale: ptr(2)},
			"NUMERIC(10,2)",
		},
		{
			"precision alone",
			model.Column{Type: "NUMERIC", Precision: ptr(8)},
			"NUMERIC(8)",
		},
		// Scale is a pointer precisely so that an explicit zero is not mistaken
		// for absence.
		{
			"an explicit zero scale is printed",
			model.Column{Type: "NUMERIC", Precision: ptr(10), Scale: ptr(0)},
			"NUMERIC(10,0)",
		},
		{
			"a type name may contain spaces",
			model.Column{Type: "TIMESTAMP WITH TIME ZONE"},
			"TIMESTAMP WITH TIME ZONE",
		},
		// The schema permits both (dependentRequired only ties scale to
		// precision), and the precedence must match sizeOf, which also tests
		// Length first.
		{
			"length wins over precision",
			model.Column{Type: "NUMERIC", Length: ptr(4), Precision: ptr(10), Scale: ptr(2)},
			"NUMERIC(4)",
		},
		// Not reachable from a valid document, but the code must not crash:
		// the scale branch is not entered and the bare type name comes back.
		{
			"a scale without a precision renders as the bare type",
			model.Column{Type: "NUMERIC", Scale: ptr(2)},
			"NUMERIC",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RenderType(&tt.in); got != tt.want {
				t.Errorf("RenderType(%+v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// TestMarker pins the whole mapping, the two empty answers included, so that a
// wrong marker is one failing assertion about one column rather than one cell
// among four in one row of a golden file.
func TestMarker(t *testing.T) {
	// One table carries most of the cases so that the rows below differ in the
	// column asked about and nothing else: id is the primary key alone,
	// customer_id is a foreign key alone, tenant_id is in both, email is in a
	// unique key, note is in nothing, and coupon_id is in the SECOND foreign
	// key, which is the only row that walks the loop past its first iteration.
	orders := model.Table{
		Name: "orders",
		Columns: []model.Column{
			notNull("id"), notNull("tenant_id"), notNull("customer_id"),
			notNull("coupon_id"), notNull("email"), notNull("note"),
		},
		PrimaryKey: &model.PrimaryKey{Columns: []string{"id", "tenant_id"}},
		UniqueKeys: []model.UniqueKey{{Columns: []string{"email"}}},
		ForeignKeys: []model.ForeignKey{
			{Columns: []string{"customer_id", "tenant_id"}},
			{Columns: []string{"coupon_id"}},
		},
		Indexes: []model.Index{
			{Name: "ix_orders_note", Columns: []string{"note"}, Unique: true},
		},
	}

	// A table without a primary key is explicitly representable, so the nil
	// check must come before the dereference. Its one column is a foreign key
	// column, so the answer is FK rather than the empty string, which is what
	// tells a crash apart from a wrong answer.
	audit := model.Table{
		Name:        "audit_log",
		Columns:     []model.Column{notNull("order_id")},
		ForeignKeys: []model.ForeignKey{{Columns: []string{"order_id"}}},
	}

	tests := []struct {
		name   string
		table  model.Table
		column string
		want   string
	}{
		{"a primary key column alone", orders, "id", "PK"},
		{"a foreign key column alone", orders, "customer_id", "FK"},
		{"a column in both keys", orders, "tenant_id", "PK,FK"},
		{"a column in the second foreign key", orders, "coupon_id", "FK"},
		{"a column in no key at all", orders, "note", ""},
		// Unique keys and unique indexes deliberately get no marker: the
		// diagram shows the two kinds of key that shape the relationships.
		// note is covered by a unique index and still answers empty above.
		{"a unique key column gets no marker", orders, "email", ""},
		{"a column the table does not define", orders, "typo_id", ""},
		{"a table with no primary key at all", audit, "order_id", "FK"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Marker(&tt.table, tt.column); got != tt.want {
				t.Errorf("Marker(%s, %q) = %q, want %q", tt.table.Name, tt.column, got, tt.want)
			}
		})
	}
}

// TestEdgeLabel pins the fallback. It matters that the label is never empty:
// two foreign keys between the same pair of tables are told apart by nothing
// else, and internal/export/svg/testdata/edge.json holds exactly that pair.
func TestEdgeLabel(t *testing.T) {
	tests := []struct {
		name string
		fk   model.ForeignKey
		want string
	}{
		{
			"a named foreign key is called by its name",
			model.ForeignKey{Name: "fk_orders_customer", Columns: []string{"customer_id"}},
			"fk_orders_customer",
		},
		{
			"an unnamed foreign key falls back to its column",
			model.ForeignKey{Columns: []string{"customer_id"}},
			"customer_id",
		},
		{
			// Declaration order, not sorted: the document said (order_id,
			// line_no) and the label says the same, so a reader can match the
			// label against the JSON by eye.
			"an unnamed composite joins its columns in declaration order",
			model.ForeignKey{Columns: []string{"order_id", "line_no"}},
			"order_id, line_no",
		},
		{
			"a named composite still answers with the name",
			model.ForeignKey{Name: "fk_shipment_lines_order", Columns: []string{"order_id", "line_no"}},
			"fk_shipment_lines_order",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EdgeLabel(&tt.fk); got != tt.want {
				t.Errorf("EdgeLabel(%+v) = %q, want %q", tt.fk, got, tt.want)
			}
		})
	}
}

// TestUndefinedTargets is as much about the ORDER as about the membership. An
// exporter emits one stub per element, in this order, so a set that came back
// in a different order on a different run would move lines in a golden file
// while every stub in it stayed correct.
func TestUndefinedTargets(t *testing.T) {
	// refs builds a table whose foreign keys point, in order, at the tables
	// named. The relationships themselves are never read here, only their
	// targets, so the columns are left as bare names.
	refs := func(name string, targets ...string) model.Table {
		table := model.Table{Name: name, LogicalName: name, Columns: []model.Column{notNull("id")}}
		for _, target := range targets {
			table.ForeignKeys = append(table.ForeignKeys, model.ForeignKey{
				Columns:    []string{"id"},
				References: model.Reference{Table: target, Columns: []string{"id"}},
			})
		}
		return table
	}

	tests := []struct {
		name   string
		tables []model.Table
		want   []string
	}{
		{
			name:   "a document with no foreign keys at all",
			tables: []model.Table{refs("customers"), refs("orders")},
		},
		{
			name:   "every target is defined, self-reference included",
			tables: []model.Table{refs("orders", "customers"), refs("categories", "categories"), refs("customers")},
		},
		{
			name: "three foreign keys naming one undefined table yield one stub",
			tables: []model.Table{
				refs("orders", "customers"),
				refs("carts", "customers"),
				refs("tickets", "customers"),
			},
			want: []string{"customers"},
		},
		{
			// First-reference order is tables in document order, and within a
			// table its foreign keys in document order. Neither of the two
			// orders below is alphabetical, which is what makes this row able
			// to fail if the answer is ever sorted or built from a map.
			name: "two undefined targets come back in first-reference order",
			tables: []model.Table{
				refs("orders", "zulu", "alpha"),
				refs("carts", "mike", "alpha"),
			},
			want: []string{"zulu", "alpha", "mike"},
		},
		{
			// jjf folds identifier case nowhere, so a document defining orders
			// and referencing Orders is referencing a table it does not define.
			name:   "a reference differing only in case is undefined",
			tables: []model.Table{refs("orders"), refs("shipments", "Orders")},
			want:   []string{"Orders"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := model.Document{FormatVersion: model.CurrentFormatVersion, Tables: tt.tables}
			if got := UndefinedTargets(&doc); !slices.Equal(got, tt.want) {
				t.Errorf("UndefinedTargets() = %v, want %v", got, tt.want)
			}
		})
	}
}
