package dot

import (
	"testing"

	"github.com/shutx-net/jumping-json-flush/internal/export/erd"
	"github.com/shutx-net/jumping-json-flush/internal/model"
)

// notNull and nullable build the two kinds of column the derivation cares
// about. Every case below is a model.Table literal rather than a JSON fixture:
// what is under test is the DOT spelling of the derivation, and a literal shows
// the reader the exact shape that produces each answer. The fixtures exercise
// the same code through the renderer.
func notNull(name string) model.Column {
	return model.Column{Name: name, LogicalName: name, Type: "INTEGER"}
}

func nullable(name string) model.Column {
	return model.Column{Name: name, LogicalName: name, Type: "INTEGER", Nullable: true}
}

// TestArrowStringsOfEachForeignKeyShape is the DOT half of the regression table
// erd's TestEndsOfEachForeignKeyShape holds: the same six foreign key shapes,
// asserted as the arrow types that reach the .dot file. The derivation is erd's
// to test; what belongs here is that each end reaches graphviz as the right
// arrow type, on the right end of the edge - an inference that lands a minimum
// on the wrong end swaps two strings in one row here, where a reader sees the
// whole relationship.
func TestArrowStringsOfEachForeignKeyShape(t *testing.T) {
	tests := []struct {
		name          string
		table         model.Table
		fk            model.ForeignKey
		child, parent string
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
			child:  "crowodot",
			parent: "teetee",
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
			child:  "crowodot",
			parent: "teeodot",
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
			child:  "teeodot",
			parent: "teetee",
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
			child:  "teeodot",
			parent: "teeodot",
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
			child:  "teeodot",
			parent: "teeodot",
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
			child:  "crowodot",
			parent: "teeodot",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := arrow(erd.ChildEnd(&tt.table, &tt.fk)); got != tt.child {
				t.Errorf("arrow(erd.ChildEnd(%s, %v)) = %v, want %v",
					tt.table.Name, tt.fk.Columns, got, tt.child)
			}
			if got := arrow(erd.ParentEnd(&tt.table, &tt.fk)); got != tt.parent {
				t.Errorf("arrow(erd.ParentEnd(%s, %v)) = %v, want %v",
					tt.table.Name, tt.fk.Columns, got, tt.parent)
			}
		})
	}
}

// TestEndArrow pins all four outcomes, so a fifth state cannot be added
// without a failing test - and so the crowtee case cannot be deleted as dead
// code. Neither derivation reaches it: a child end is always optional and a
// parent end is never many. arrow is a total mapping over the type all the
// same.
func TestEndArrow(t *testing.T) {
	tests := []struct {
		name string
		in   erd.End
		want string
	}{
		{"many and optional", erd.End{Many: true, Optional: true}, "crowodot"},
		{"many and mandatory", erd.End{Many: true}, "crowtee"},
		{"one and optional", erd.End{Optional: true}, "teeodot"},
		{"one and mandatory", erd.End{}, "teetee"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := arrow(tt.in); got != tt.want {
				t.Errorf("arrow(erd.End%+v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
