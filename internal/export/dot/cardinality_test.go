package dot

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

func TestChildEnd(t *testing.T) {
	tests := []struct {
		name  string
		table model.Table
		fk    model.ForeignKey
		want  end
	}{
		{
			name: "a plain foreign key column is many and mandatory",
			table: model.Table{
				Name:       "order_lines",
				Columns:    []model.Column{notNull("id"), notNull("order_id")},
				PrimaryKey: &model.PrimaryKey{Columns: []string{"id"}},
			},
			fk:   model.ForeignKey{Columns: []string{"order_id"}},
			want: end{many: true},
		},
		{
			name: "a foreign key that is the whole primary key is one",
			table: model.Table{
				Name:       "customer_profiles",
				Columns:    []model.Column{notNull("customer_id")},
				PrimaryKey: &model.PrimaryKey{Columns: []string{"customer_id"}},
			},
			fk:   model.ForeignKey{Columns: []string{"customer_id"}},
			want: end{},
		},
		{
			name: "a composite foreign key equal to the primary key in a different order is one",
			table: model.Table{
				Name:       "shipment_lines",
				Columns:    []model.Column{notNull("order_id"), notNull("line_no")},
				PrimaryKey: &model.PrimaryKey{Columns: []string{"order_id", "line_no"}},
			},
			fk:   model.ForeignKey{Columns: []string{"line_no", "order_id"}},
			want: end{},
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
			want: end{many: true},
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
			want: end{},
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
			want: end{},
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
			want: end{many: true},
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
			want: end{many: true},
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
			want: end{many: true},
		},
		{
			name: "one nullable column among several makes it optional",
			table: model.Table{
				Name:       "order_lines",
				Columns:    []model.Column{notNull("order_id"), nullable("line_no")},
				PrimaryKey: &model.PrimaryKey{Columns: []string{"order_id"}},
			},
			fk:   model.ForeignKey{Columns: []string{"order_id", "line_no"}},
			want: end{many: true, optional: true},
		},
		{
			// A primary key column would not normally be nullable, but the
			// schema does not check it and the derivation must still be total.
			name: "one and optional",
			table: model.Table{
				Name:       "customer_profiles",
				Columns:    []model.Column{nullable("customer_id")},
				PrimaryKey: &model.PrimaryKey{Columns: []string{"customer_id"}},
			},
			fk:   model.ForeignKey{Columns: []string{"customer_id"}},
			want: end{optional: true},
		},
		{
			// The unknown column contributes nothing; the answer is optional
			// because of the real, nullable one.
			name: "a column the table does not define contributes nothing",
			table: model.Table{
				Name:    "order_lines",
				Columns: []model.Column{nullable("order_id")},
			},
			fk:   model.ForeignKey{Columns: []string{"typo_id", "order_id"}},
			want: end{many: true, optional: true},
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
			want: end{many: true, optional: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := childEnd(&tt.table, &tt.fk); got != tt.want {
				t.Errorf("childEnd(%s, %v) = %+v (%s), want %+v (%s)",
					tt.table.Name, tt.fk.Columns, got, got.arrow(), tt.want, tt.want.arrow())
			}
		})
	}
}

// TestEndArrow pins all four outcomes, so a fifth state cannot be added
// without a failing test.
func TestEndArrow(t *testing.T) {
	tests := []struct {
		name string
		in   end
		want string
	}{
		{"many and optional", end{many: true, optional: true}, "crowodot"},
		{"many and mandatory", end{many: true}, "crowtee"},
		{"one and optional", end{optional: true}, "teeodot"},
		{"one and mandatory", end{}, "teetee"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.in.arrow(); got != tt.want {
				t.Errorf("end%+v.arrow() = %v, want %v", tt.in, got, tt.want)
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
