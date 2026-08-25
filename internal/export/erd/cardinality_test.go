package erd

import (
	"testing"

	"github.com/shutx-net/jumping-json-flush/internal/model"
)

// notNull and nullable build the two kinds of column the derivation cares
// about. Every case below is a model.Table literal rather than a JSON fixture:
// what is under test is the derivation, and a literal shows the reader the
// exact shape that produces each answer. The fixtures exercise the same code
// through the renderer.
func notNull(name string) model.Column {
	return model.Column{Name: name, LogicalName: name, Type: "INTEGER"}
}

func nullable(name string) model.Column {
	return model.Column{Name: name, LogicalName: name, Type: "INTEGER", Nullable: true}
}

// TestChildEnd covers the child end alone: how many rows of the child point at
// one row of the parent, plus the flat fact that a child end is always
// optional. Nullability appears in some of these tables only to show that it
// changes nothing here; TestParentEnd is where it decides an answer.
func TestChildEnd(t *testing.T) {
	tests := []struct {
		name  string
		table model.Table
		fk    model.ForeignKey
		want  End
	}{
		{
			name: "a plain foreign key column is many, and optional as every child end is",
			table: model.Table{
				Name:       "order_lines",
				Columns:    []model.Column{notNull("id"), notNull("order_id")},
				PrimaryKey: &model.PrimaryKey{Columns: []string{"id"}},
			},
			fk:   model.ForeignKey{Columns: []string{"order_id"}},
			want: End{Many: true, Optional: true},
		},
		{
			name: "a foreign key that is the whole primary key is one",
			table: model.Table{
				Name:       "customer_profiles",
				Columns:    []model.Column{notNull("customer_id")},
				PrimaryKey: &model.PrimaryKey{Columns: []string{"customer_id"}},
			},
			fk:   model.ForeignKey{Columns: []string{"customer_id"}},
			want: End{Optional: true},
		},
		{
			name: "a composite foreign key equal to the primary key in a different order is one",
			table: model.Table{
				Name:       "shipment_lines",
				Columns:    []model.Column{notNull("order_id"), notNull("line_no")},
				PrimaryKey: &model.PrimaryKey{Columns: []string{"order_id", "line_no"}},
			},
			fk:   model.ForeignKey{Columns: []string{"line_no", "order_id"}},
			want: End{Optional: true},
		},
		{
			// The length check alone decides this one.
			name: "a strict subset of the primary key is many",
			table: model.Table{
				Name:       "order_lines",
				Columns:    []model.Column{notNull("order_id"), notNull("line_no")},
				PrimaryKey: &model.PrimaryKey{Columns: []string{"order_id", "line_no"}},
			},
			fk:   model.ForeignKey{Columns: []string{"order_id"}},
			want: End{Many: true, Optional: true},
		},
		{
			// The match is on the second of three unique keys, so the loop is
			// exercised past its first iteration.
			name: "a foreign key equal to a unique key is one",
			table: model.Table{
				Name:       "user_settings",
				Columns:    []model.Column{notNull("id"), notNull("user_id"), notNull("slug")},
				PrimaryKey: &model.PrimaryKey{Columns: []string{"id"}},
				UniqueKeys: []model.UniqueKey{
					{Columns: []string{"slug"}},
					{Columns: []string{"user_id"}},
					{Columns: []string{"id", "slug"}},
				},
			},
			fk:   model.ForeignKey{Columns: []string{"user_id"}},
			want: End{Optional: true},
		},
		{
			// A unique index enforces the same constraint as a unique key -
			// the schema calls indexes[].unique "Whether the index enforces
			// uniqueness" - so the relationship is genuinely 1:1 and the
			// notation must not depend on which spelling the author chose.
			name: "a foreign key covered by a unique index alone is one",
			table: model.Table{
				Name:       "user_settings",
				Columns:    []model.Column{notNull("id"), notNull("user_id")},
				PrimaryKey: &model.PrimaryKey{Columns: []string{"id"}},
				Indexes: []model.Index{
					{Name: "ix_user_settings_user", Columns: []string{"user_id"}, Unique: true},
				},
			},
			fk:   model.ForeignKey{Columns: []string{"user_id"}},
			want: End{Optional: true},
		},
		{
			name: "a non-unique index over the same columns leaves it many",
			table: model.Table{
				Name:       "order_lines",
				Columns:    []model.Column{notNull("id"), notNull("order_id")},
				PrimaryKey: &model.PrimaryKey{Columns: []string{"id"}},
				Indexes: []model.Index{
					{Name: "ix_order_lines_order", Columns: []string{"order_id"}},
				},
			},
			fk:   model.ForeignKey{Columns: []string{"order_id"}},
			want: End{Many: true, Optional: true},
		},
		{
			// A table without a primary key is explicitly representable, so the
			// nil check must come before the dereference.
			name: "a table with no keys at all is many",
			table: model.Table{
				Name:    "audit_log",
				Columns: []model.Column{notNull("order_id")},
			},
			fk:   model.ForeignKey{Columns: []string{"order_id"}},
			want: End{Many: true, Optional: true},
		},
		{
			// Ranging over the nil uniqueKeys slice executes zero times, which
			// is correct and must not be special-cased.
			name: "a primary key with no uniqueKeys slice at all",
			table: model.Table{
				Name:       "order_lines",
				Columns:    []model.Column{notNull("id"), notNull("order_id")},
				PrimaryKey: &model.PrimaryKey{Columns: []string{"id"}},
			},
			fk:   model.ForeignKey{Columns: []string{"order_id"}},
			want: End{Many: true, Optional: true},
		},
		{
			// The nullable column changes not one bit of this end: it is many
			// because order_id alone is not unique, and optional as every child
			// end is. Where line_no decides an answer is TestParentEnd.
			name: "a nullable column among several does not change the child end",
			table: model.Table{
				Name:       "order_lines",
				Columns:    []model.Column{notNull("order_id"), nullable("line_no")},
				PrimaryKey: &model.PrimaryKey{Columns: []string{"order_id"}},
			},
			fk:   model.ForeignKey{Columns: []string{"order_id", "line_no"}},
			want: End{Many: true, Optional: true},
		},
		{
			// A primary key column would not normally be nullable, but the
			// schema does not check it and the derivation must still be total.
			// The optional here owes nothing to that nullability; a child end
			// is optional whatever the columns say.
			name: "a nullable primary key column is still one, and optional like every other child end",
			table: model.Table{
				Name:       "customer_profiles",
				Columns:    []model.Column{nullable("customer_id")},
				PrimaryKey: &model.PrimaryKey{Columns: []string{"customer_id"}},
			},
			fk:   model.ForeignKey{Columns: []string{"customer_id"}},
			want: End{Optional: true},
		},
		{
			// A column the table does not define is no source of uniqueness
			// either, so this end is many. What must be preserved about such a
			// column is that it does not make the PARENT end optional, and
			// that is what TestParentEnd pins.
			name: "a column the table does not define adds no uniqueness",
			table: model.Table{
				Name:    "order_lines",
				Columns: []model.Column{nullable("order_id")},
			},
			fk:   model.ForeignKey{Columns: []string{"typo_id", "order_id"}},
			want: End{Many: true, Optional: true},
		},
		{
			// The derivation only ever looks at the child table, so pointing a
			// foreign key at the table itself needs no special case.
			name: "a self-referencing foreign key derives like any other",
			table: model.Table{
				Name:       "categories",
				Columns:    []model.Column{notNull("id"), nullable("parent_id")},
				PrimaryKey: &model.PrimaryKey{Columns: []string{"id"}},
			},
			fk: model.ForeignKey{
				Columns:    []string{"parent_id"},
				References: model.Reference{Table: "categories", Columns: []string{"id"}},
			},
			want: End{Many: true, Optional: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ChildEnd(&tt.table, &tt.fk); got != tt.want {
				t.Errorf("ChildEnd(%s, %v) = %+v, want %+v",
					tt.table.Name, tt.fk.Columns, got, tt.want)
			}
		})
	}
}

// TestParentEnd covers the parent end: how many rows of the referenced table
// one child row points at. The maximum never depends on the document - a
// foreign key names one specific row - so every case here is about the minimum,
// which the nullability of the foreign key's own columns decides.
func TestParentEnd(t *testing.T) {
	tests := []struct {
		name  string
		table model.Table
		fk    model.ForeignKey
		want  End
	}{
		{
			name: "a NOT NULL foreign key column is one and mandatory",
			table: model.Table{
				Name:       "order_lines",
				Columns:    []model.Column{notNull("id"), notNull("order_id")},
				PrimaryKey: &model.PrimaryKey{Columns: []string{"id"}},
			},
			fk:   model.ForeignKey{Columns: []string{"order_id"}},
			want: End{},
		},
		{
			name: "a nullable foreign key column makes the parent optional",
			table: model.Table{
				Name:       "orders",
				Columns:    []model.Column{notNull("id"), nullable("coupon_id")},
				PrimaryKey: &model.PrimaryKey{Columns: []string{"id"}},
			},
			fk:   model.ForeignKey{Columns: []string{"coupon_id"}},
			want: End{Optional: true},
		},
		{
			// Uniqueness is the CHILD end's business. A foreign key that is
			// unique in its own table still points at one parent row, so the
			// parent end must not become many.
			name: "a unique foreign key does not make the parent many",
			table: model.Table{
				Name:       "customer_profiles",
				Columns:    []model.Column{notNull("customer_id")},
				PrimaryKey: &model.PrimaryKey{Columns: []string{"customer_id"}},
			},
			fk:   model.ForeignKey{Columns: []string{"customer_id"}},
			want: End{},
		},
		{
			name: "one nullable column among several makes it optional",
			table: model.Table{
				Name:       "order_lines",
				Columns:    []model.Column{notNull("order_id"), nullable("line_no")},
				PrimaryKey: &model.PrimaryKey{Columns: []string{"order_id"}},
			},
			fk:   model.ForeignKey{Columns: []string{"order_id", "line_no"}},
			want: End{Optional: true},
		},
		{
			name: "a composite whose every column is NOT NULL stays mandatory",
			table: model.Table{
				Name:       "shipment_lines",
				Columns:    []model.Column{notNull("order_id"), notNull("line_no")},
				PrimaryKey: &model.PrimaryKey{Columns: []string{"order_id", "line_no"}},
			},
			fk:   model.ForeignKey{Columns: []string{"order_id", "line_no"}},
			want: End{},
		},
		{
			// findColumn's rule: a column the table does not define counts as
			// NOT NULL, because absent information must not invent an optional
			// relationship and a typo must not silently redraw this end.
			name: "a column the table does not define counts as NOT NULL",
			table: model.Table{
				Name:    "order_lines",
				Columns: []model.Column{notNull("order_id")},
			},
			fk:   model.ForeignKey{Columns: []string{"typo_id", "order_id"}},
			want: End{},
		},
		{
			// Only the child table is read, so pointing the foreign key at the
			// table itself needs no special case here either.
			name: "a self-referencing foreign key derives like any other",
			table: model.Table{
				Name:       "categories",
				Columns:    []model.Column{notNull("id"), nullable("parent_id")},
				PrimaryKey: &model.PrimaryKey{Columns: []string{"id"}},
			},
			fk: model.ForeignKey{
				Columns:    []string{"parent_id"},
				References: model.Reference{Table: "categories", Columns: []string{"id"}},
			},
			want: End{Optional: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParentEnd(&tt.table, &tt.fk); got != tt.want {
				t.Errorf("ParentEnd(%s, %v) = %+v, want %+v",
					tt.table.Name, tt.fk.Columns, got, tt.want)
			}
		})
	}
}

// TestEndsOfEachForeignKeyShape is the regression table of the issue this
// derivation was fixed for, written as the two ends each foreign key shape
// derives. It says nothing the two tests above do not, and is worth its place
// because it says it about BOTH ends at once: an inference that lands a minimum
// on the wrong end swaps two values in one row here, where a reader sees the
// whole relationship, rather than flipping one bool in a table about one side.
// internal/export/dot's TestArrowStringsOfEachForeignKeyShape is the same table
// again in the arrow types that reach the .dot file.
func TestEndsOfEachForeignKeyShape(t *testing.T) {
	tests := []struct {
		name          string
		table         model.Table
		fk            model.ForeignKey
		child, parent End
	}{
		{
			// non-unique, NOT NULL: 0..N children, exactly one parent.
			name: "a non-unique NOT NULL foreign key",
			table: model.Table{
				Name:       "orders",
				Columns:    []model.Column{notNull("id"), notNull("customer_id")},
				PrimaryKey: &model.PrimaryKey{Columns: []string{"id"}},
			},
			fk:     model.ForeignKey{Columns: []string{"customer_id"}},
			child:  End{Many: true, Optional: true},
			parent: End{},
		},
		{
			// non-unique, nullable: 0..N children, 0..1 parent. This is the
			// case the issue opens with: orders.coupon_id.
			name: "a non-unique nullable foreign key",
			table: model.Table{
				Name:       "orders",
				Columns:    []model.Column{notNull("id"), nullable("coupon_id")},
				PrimaryKey: &model.PrimaryKey{Columns: []string{"id"}},
			},
			fk:     model.ForeignKey{Columns: []string{"coupon_id"}},
			child:  End{Many: true, Optional: true},
			parent: End{Optional: true},
		},
		{
			// unique, NOT NULL: 0..1 children, exactly one parent.
			name: "a unique NOT NULL foreign key",
			table: model.Table{
				Name:       "customer_profiles",
				Columns:    []model.Column{notNull("id"), notNull("customer_id")},
				PrimaryKey: &model.PrimaryKey{Columns: []string{"id"}},
				UniqueKeys: []model.UniqueKey{{Columns: []string{"customer_id"}}},
			},
			fk:     model.ForeignKey{Columns: []string{"customer_id"}},
			child:  End{Optional: true},
			parent: End{},
		},
		{
			// unique, nullable: 0..1 children, 0..1 parent. A nullable unique
			// column is odd but the schema does not forbid it, so the
			// derivation has to answer.
			name: "a unique nullable foreign key",
			table: model.Table{
				Name:       "customer_profiles",
				Columns:    []model.Column{notNull("id"), nullable("customer_id")},
				PrimaryKey: &model.PrimaryKey{Columns: []string{"id"}},
				UniqueKeys: []model.UniqueKey{{Columns: []string{"customer_id"}}},
			},
			fk:     model.ForeignKey{Columns: []string{"customer_id"}},
			child:  End{Optional: true},
			parent: End{Optional: true},
		},
		{
			// A composite foreign key equal to the composite primary key is
			// unique, and one nullable column among its columns is enough to
			// make the parent end optional.
			name: "a composite foreign key with one nullable column",
			table: model.Table{
				Name:       "shipment_lines",
				Columns:    []model.Column{notNull("order_id"), nullable("line_no")},
				PrimaryKey: &model.PrimaryKey{Columns: []string{"order_id", "line_no"}},
			},
			fk:     model.ForeignKey{Columns: []string{"order_id", "line_no"}},
			child:  End{Optional: true},
			parent: End{Optional: true},
		},
		{
			name: "a self-referencing foreign key",
			table: model.Table{
				Name:       "categories",
				Columns:    []model.Column{notNull("id"), nullable("parent_id")},
				PrimaryKey: &model.PrimaryKey{Columns: []string{"id"}},
			},
			fk: model.ForeignKey{
				Columns:    []string{"parent_id"},
				References: model.Reference{Table: "categories", Columns: []string{"id"}},
			},
			child:  End{Many: true, Optional: true},
			parent: End{Optional: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ChildEnd(&tt.table, &tt.fk); got != tt.child {
				t.Errorf("ChildEnd(%s, %v) = %+v, want %+v",
					tt.table.Name, tt.fk.Columns, got, tt.child)
			}
			if got := ParentEnd(&tt.table, &tt.fk); got != tt.parent {
				t.Errorf("ParentEnd(%s, %v) = %+v, want %+v",
					tt.table.Name, tt.fk.Columns, got, tt.parent)
			}
		})
	}
}

func TestSameColumnSet(t *testing.T) {
	tests := []struct {
		name string
		a, b []string
		want bool
	}{
		{"identical", []string{"a", "b"}, []string{"a", "b"}, true},
		{"the same set in a different order", []string{"b", "a"}, []string{"a", "b"}, true},
		{"different lengths", []string{"a"}, []string{"a", "b"}, false},
		{"the same length, different names", []string{"a", "c"}, []string{"a", "b"}, false},
		// jjf folds identifier case nowhere, so these are two different columns.
		{"case differences are not folded", []string{"USER_ID"}, []string{"user_id"}, false},
		// Neither empty case can arise ($defs/columnNameList has minItems 1),
		// but neither may panic.
		{"both empty", nil, nil, true},
		{"one empty", nil, []string{"a"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sameColumnSet(tt.a, tt.b); got != tt.want {
				t.Errorf("sameColumnSet(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestFindColumn(t *testing.T) {
	// The schema does not forbid two columns of a table sharing a name, so the
	// duplicate below is a legal document and the first match must win.
	table := model.Table{
		Name: "order_lines",
		Columns: []model.Column{
			notNull("id"),
			nullable("order_id"),
			notNull("order_id"),
		},
	}

	tests := []struct {
		name         string
		column       string
		wantFound    bool
		wantNullable bool
	}{
		{"a column that exists", "id", true, false},
		{"the first of two columns with one name wins", "order_id", true, true},
		{"a column that does not exist", "typo_id", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findColumn(&table, tt.column)
			if !tt.wantFound {
				if got != nil {
					t.Fatalf("findColumn(%q) = %+v, want nil", tt.column, *got)
				}
				return
			}
			if got == nil {
				t.Fatalf("findColumn(%q) = nil, want the column", tt.column)
			}
			if got.Name != tt.column {
				t.Errorf("findColumn(%q).Name = %v, want %v", tt.column, got.Name, tt.column)
			}
			if got.Nullable != tt.wantNullable {
				t.Errorf("findColumn(%q).Nullable = %v, want %v", tt.column, got.Nullable, tt.wantNullable)
			}
		})
	}
}
