package check

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/shutx-net/jumping-json-flush/internal/model"
	"github.com/shutx-net/jumping-json-flush/internal/schema"
)

// ---------------------------------------------------------------------------
// Document builders
// ---------------------------------------------------------------------------

// The documents in this file are Go literals rather than fixtures: what is
// under test is one document shape at a time, and a literal keeps the shape and
// the findings it must produce next to each other. The two committed fixtures
// exist for a different reason - see TestFixtures.

// col is a NOT NULL column. Only the name and the nullability matter here, so
// the logical name and the type are filled in with something plausible rather
// than being spelled out at every call site.
func col(name string) model.Column {
	return model.Column{Name: name, LogicalName: name, Type: "BIGINT"}
}

// nullable is col's counterpart for a column that accepts NULL.
func nullable(name string) model.Column {
	c := col(name)
	c.Nullable = true
	return c
}

// table builds a table with the given columns and nothing else.
func table(name string, cols ...model.Column) model.Table {
	return model.Table{Name: name, LogicalName: name, Columns: cols}
}

// document wraps tables into a document with a plausible header.
func document(tables ...model.Table) *model.Document {
	return &model.Document{
		FormatVersion: model.CurrentFormatVersion,
		Database:      model.Database{Name: "shop"},
		Tables:        tables,
	}
}

// rendered lays the findings out the way a reader sees them, so a failing test
// prints what jjf would have printed.
func rendered(findings []Finding) []string {
	var out []string
	for _, f := range findings {
		out = append(out, f.String())
	}
	return out
}

// ---------------------------------------------------------------------------
// The checks
// ---------------------------------------------------------------------------

func TestDocument(t *testing.T) {
	tests := []struct {
		name string
		doc  *model.Document
		want []string
	}{
		{
			name: "a self-consistent document has no findings",
			doc: document(model.Table{
				Name: "customers", LogicalName: "顧客",
				Columns:    []model.Column{col("id"), col("email")},
				PrimaryKey: &model.PrimaryKey{Name: "pk_customers", Columns: []string{"id"}},
				UniqueKeys: []model.UniqueKey{{Name: "uq_customers_email", Columns: []string{"email"}}},
			}),
			want: nil,
		},
		{
			name: "a primary key names a column the table does not define",
			doc: document(model.Table{
				Name: "customers", LogicalName: "顧客",
				Columns:    []model.Column{col("id")},
				PrimaryKey: &model.PrimaryKey{Name: "pk_customers", Columns: []string{"customer_id"}},
			}),
			want: []string{
				`primary key pk_customers on table customers: names column "customer_id", which the table does not define`,
			},
		},
		{
			name: "a unique key names a column the table does not define",
			doc: document(model.Table{
				Name: "customers", LogicalName: "顧客",
				Columns:    []model.Column{col("id")},
				UniqueKeys: []model.UniqueKey{{Name: "uq_customers_email", Columns: []string{"email"}}},
			}),
			want: []string{
				`unique key uq_customers_email on table customers: names column "email", which the table does not define`,
			},
		},
		{
			name: "an index names a column the table does not define",
			doc: document(model.Table{
				Name: "customers", LogicalName: "顧客",
				Columns: []model.Column{col("id")},
				Indexes: []model.Index{{Name: "ix_customers_email", Columns: []string{"email"}}},
			}),
			want: []string{
				`index ix_customers_email on table customers: names column "email", which the table does not define`,
			},
		},
		{
			name: "a foreign key names a column the table does not define",
			doc: document(
				model.Table{
					Name: "orders", LogicalName: "受注",
					Columns: []model.Column{col("id")},
					ForeignKeys: []model.ForeignKey{{
						Name: "fk_orders_customer", Columns: []string{"customer_id"},
						References: model.Reference{Table: "customers", Columns: []string{"id"}},
					}},
				},
				model.Table{
					Name: "customers", LogicalName: "顧客",
					Columns:    []model.Column{col("id")},
					PrimaryKey: &model.PrimaryKey{Columns: []string{"id"}},
				},
			),
			want: []string{
				`foreign key fk_orders_customer on table orders: names column "customer_id", which the table does not define`,
			},
		},
		{
			name: "every missing column of one constraint is reported, in column order",
			doc: document(model.Table{
				Name: "customers", LogicalName: "顧客",
				Columns:    []model.Column{col("id")},
				PrimaryKey: &model.PrimaryKey{Name: "pk_customers", Columns: []string{"tenant_id", "id", "region_id"}},
			}),
			want: []string{
				`primary key pk_customers on table customers: names column "tenant_id", which the table does not define`,
				`primary key pk_customers on table customers: names column "region_id", which the table does not define`,
			},
		},
		{
			name: "a foreign key names a table the document does not define",
			doc: document(model.Table{
				Name: "orders", LogicalName: "受注",
				Columns: []model.Column{col("customer_id")},
				ForeignKeys: []model.ForeignKey{{
					Name: "fk_orders_customer", Columns: []string{"customer_id"},
					References: model.Reference{Table: "customers", Columns: []string{"id"}},
				}},
			}),
			want: []string{
				`foreign key fk_orders_customer on table orders: references table "customers", which this document does not define`,
			},
		},
		{
			name: "a self-referencing foreign key finds its own table",
			doc: document(model.Table{
				Name: "employees", LogicalName: "従業員",
				Columns:    []model.Column{col("id"), nullable("manager_id")},
				PrimaryKey: &model.PrimaryKey{Columns: []string{"id"}},
				ForeignKeys: []model.ForeignKey{{
					Name: "fk_employees_manager", Columns: []string{"manager_id"},
					References: model.Reference{Table: "employees", Columns: []string{"id"}},
				}},
			}),
			want: nil,
		},
		{
			name: "a foreign key referencing fewer columns than it names",
			doc: document(
				model.Table{
					Name: "orders", LogicalName: "受注",
					Columns: []model.Column{col("tenant_id"), col("customer_id")},
					ForeignKeys: []model.ForeignKey{{
						Name: "fk_orders_customer", Columns: []string{"tenant_id", "customer_id"},
						References: model.Reference{Table: "customers", Columns: []string{"id"}},
					}},
				},
				model.Table{
					Name: "customers", LogicalName: "顧客",
					Columns:    []model.Column{col("id")},
					PrimaryKey: &model.PrimaryKey{Columns: []string{"id"}},
				},
			),
			want: []string{
				"foreign key fk_orders_customer on table orders: names 2 column(s) but references 1",
			},
		},
		{
			name: "a foreign key referencing more columns than it names",
			doc: document(
				model.Table{
					Name: "orders", LogicalName: "受注",
					Columns: []model.Column{col("customer_id")},
					ForeignKeys: []model.ForeignKey{{
						Name: "fk_orders_customer", Columns: []string{"customer_id"},
						References: model.Reference{Table: "customers", Columns: []string{"tenant_id", "id"}},
					}},
				},
				model.Table{
					Name: "customers", LogicalName: "顧客",
					Columns:    []model.Column{col("tenant_id"), col("id")},
					PrimaryKey: &model.PrimaryKey{Columns: []string{"tenant_id", "id"}},
				},
			),
			want: []string{
				"foreign key fk_orders_customer on table orders: names 1 column(s) but references 2",
			},
		},
		{
			name: "a foreign key onto columns nothing constrains to be unique",
			doc: document(
				model.Table{
					Name: "orders", LogicalName: "受注",
					Columns: []model.Column{col("customer_email")},
					ForeignKeys: []model.ForeignKey{{
						Name: "fk_orders_customer", Columns: []string{"customer_email"},
						References: model.Reference{Table: "customers", Columns: []string{"email"}},
					}},
				},
				model.Table{
					Name: "customers", LogicalName: "顧客",
					Columns:    []model.Column{col("id"), col("email")},
					PrimaryKey: &model.PrimaryKey{Columns: []string{"id"}},
				},
			),
			want: []string{
				"foreign key fk_orders_customer on table orders: references (email) of table customers, " +
					"which no primary key, unique key or unique index there constrains to be unique",
			},
		},
		{
			name: "a foreign key onto a primary key is a valid target",
			doc:  foreignKeyOnto(model.Table{PrimaryKey: &model.PrimaryKey{Columns: []string{"email"}}}),
			want: nil,
		},
		{
			name: "a foreign key onto a unique key is a valid target",
			doc:  foreignKeyOnto(model.Table{UniqueKeys: []model.UniqueKey{{Columns: []string{"email"}}}}),
			want: nil,
		},
		{
			name: "a foreign key onto a unique index is a valid target",
			doc:  foreignKeyOnto(model.Table{Indexes: []model.Index{{Name: "ix_customers_email", Columns: []string{"email"}, Unique: true}}}),
			want: nil,
		},
		{
			name: "a non-unique index is not a valid target",
			doc:  foreignKeyOnto(model.Table{Indexes: []model.Index{{Name: "ix_customers_email", Columns: []string{"email"}}}}),
			want: []string{
				"foreign key fk_orders_customer on table orders: references (email) of table customers, " +
					"which no primary key, unique key or unique index there constrains to be unique",
			},
		},
		{
			name: "the referenced columns are compared as a set, not in order",
			doc: document(
				model.Table{
					Name: "orders", LogicalName: "受注",
					Columns: []model.Column{col("region_code"), col("country_code")},
					ForeignKeys: []model.ForeignKey{{
						Name: "fk_orders_region", Columns: []string{"region_code", "country_code"},
						References: model.Reference{Table: "regions", Columns: []string{"region_code", "country_code"}},
					}},
				},
				model.Table{
					Name: "regions", LogicalName: "地域",
					Columns:    []model.Column{col("country_code"), col("region_code")},
					PrimaryKey: &model.PrimaryKey{Columns: []string{"country_code", "region_code"}},
				},
			),
			want: nil,
		},
		{
			name: "uniqueness is not reported when the target table is undefined",
			doc: document(model.Table{
				Name: "orders", LogicalName: "受注",
				Columns: []model.Column{col("customer_id")},
				ForeignKeys: []model.ForeignKey{{
					Name: "fk_orders_customer", Columns: []string{"customer_id"},
					References: model.Reference{Table: "customers", Columns: []string{"id"}},
				}},
			}),
			want: []string{
				`foreign key fk_orders_customer on table orders: references table "customers", which this document does not define`,
			},
		},
		{
			name: "uniqueness is not reported when the two ends disagree on how many columns",
			doc: document(
				model.Table{
					Name: "orders", LogicalName: "受注",
					Columns: []model.Column{col("customer_id")},
					ForeignKeys: []model.ForeignKey{{
						Name: "fk_orders_customer", Columns: []string{"customer_id"},
						References: model.Reference{Table: "customers", Columns: []string{"id", "email"}},
					}},
				},
				model.Table{
					Name: "customers", LogicalName: "顧客",
					Columns:    []model.Column{col("id"), col("email")},
					PrimaryKey: &model.PrimaryKey{Columns: []string{"id"}},
				},
			),
			want: []string{
				"foreign key fk_orders_customer on table orders: names 1 column(s) but references 2",
			},
		},
		{
			name: "a primary key column declared nullable",
			doc: document(model.Table{
				Name: "customers", LogicalName: "顧客",
				Columns:    []model.Column{nullable("id")},
				PrimaryKey: &model.PrimaryKey{Name: "pk_customers", Columns: []string{"id"}},
			}),
			want: []string{
				`primary key pk_customers on table customers: names column "id", which the table declares nullable`,
			},
		},
		{
			name: "a missing primary key column is reported once, not also as nullable",
			doc: document(model.Table{
				Name: "customers", LogicalName: "顧客",
				Columns:    []model.Column{col("email")},
				PrimaryKey: &model.PrimaryKey{Name: "pk_customers", Columns: []string{"id"}},
			}),
			want: []string{
				`primary key pk_customers on table customers: names column "id", which the table does not define`,
			},
		},
		{
			name: "a table defining the same column twice",
			doc:  document(table("customers", col("id"), col("email"), col("email"))),
			want: []string{
				`table customers: defines column "email" more than once`,
			},
		},
		{
			name: "a column defined three times is one mistake, not two",
			doc:  document(table("customers", col("email"), col("email"), col("email"))),
			want: []string{
				`table customers: defines column "email" more than once`,
			},
		},
		{
			name: "a primary key and an index sharing a name",
			doc: document(model.Table{
				Name: "customers", LogicalName: "顧客",
				Columns:    []model.Column{col("id"), col("email")},
				PrimaryKey: &model.PrimaryKey{Name: "customers_key", Columns: []string{"id"}},
				Indexes:    []model.Index{{Name: "customers_key", Columns: []string{"email"}}},
			}),
			want: []string{
				`table customers: declares more than one constraint or index called "customers_key"`,
			},
		},
		{
			name: "a unique key and a foreign key sharing a name",
			doc: document(model.Table{
				Name: "orders", LogicalName: "受注",
				Columns:    []model.Column{col("id"), col("code")},
				PrimaryKey: &model.PrimaryKey{Columns: []string{"id"}},
				UniqueKeys: []model.UniqueKey{{Name: "orders_key", Columns: []string{"code"}}},
				ForeignKeys: []model.ForeignKey{{
					Name: "orders_key", Columns: []string{"id"},
					References: model.Reference{Table: "orders", Columns: []string{"id"}},
				}},
			}),
			want: []string{
				`table orders: declares more than one constraint or index called "orders_key"`,
			},
		},
		{
			name: "several unnamed constraints in one table do not collide",
			doc: document(model.Table{
				Name: "orders", LogicalName: "受注",
				Columns:    []model.Column{col("id"), col("code")},
				PrimaryKey: &model.PrimaryKey{Columns: []string{"id"}},
				UniqueKeys: []model.UniqueKey{{Columns: []string{"code"}}, {Columns: []string{"id", "code"}}},
				ForeignKeys: []model.ForeignKey{{
					Columns:    []string{"id"},
					References: model.Reference{Table: "orders", Columns: []string{"id"}},
				}},
			}),
			want: nil,
		},
		{
			name: "an unnamed constraint is located by its 1-based position",
			doc: document(model.Table{
				Name: "orders", LogicalName: "受注",
				Columns:    []model.Column{col("id")},
				PrimaryKey: &model.PrimaryKey{Columns: []string{"code"}},
				UniqueKeys: []model.UniqueKey{{Columns: []string{"id"}}, {Columns: []string{"code"}}},
				ForeignKeys: []model.ForeignKey{{
					Columns:    []string{"code"},
					References: model.Reference{Table: "orders", Columns: []string{"id"}},
				}},
			}),
			want: []string{
				`primary key on table orders: names column "code", which the table does not define`,
				`unique key #2 on table orders: names column "code", which the table does not define`,
				`foreign key #1 on table orders: names column "code", which the table does not define`,
			},
		},
		{
			name: "findings across two tables come out in document order",
			doc: document(
				model.Table{
					Name: "orders", LogicalName: "受注",
					Columns:    []model.Column{nullable("id"), col("customer_id"), col("customer_id")},
					PrimaryKey: &model.PrimaryKey{Name: "pk_orders", Columns: []string{"id"}},
					ForeignKeys: []model.ForeignKey{{
						Name: "fk_orders_customer", Columns: []string{"customer_id"},
						References: model.Reference{Table: "customers", Columns: []string{"email"}},
					}},
					Indexes: []model.Index{{Name: "ix_orders_placed_at", Columns: []string{"placed_at"}}},
				},
				model.Table{
					Name: "customers", LogicalName: "顧客",
					Columns:    []model.Column{col("id"), col("email")},
					PrimaryKey: &model.PrimaryKey{Name: "pk_customers", Columns: []string{"id"}},
					Indexes:    []model.Index{{Name: "pk_customers", Columns: []string{"email"}}},
				},
			),
			want: []string{
				`table orders: defines column "customer_id" more than once`,
				`primary key pk_orders on table orders: names column "id", which the table declares nullable`,
				"foreign key fk_orders_customer on table orders: references (email) of table customers, " +
					"which no primary key, unique key or unique index there constrains to be unique",
				`index ix_orders_placed_at on table orders: names column "placed_at", which the table does not define`,
				`table customers: declares more than one constraint or index called "pk_customers"`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rendered(Document(tt.doc))
			if !slices.Equal(got, tt.want) {
				t.Errorf("findings got = %v, want %v", got, tt.want)
			}
		})
	}
}

// foreignKeyOnto builds a document whose orders table points a foreign key at
// customers(email), with target carrying whatever makes - or fails to make -
// that column unique. Only the keys of target are read; its name and columns
// are supplied here so the four uniqueness cases differ in one thing alone.
func foreignKeyOnto(target model.Table) *model.Document {
	target.Name = "customers"
	target.LogicalName = "顧客"
	target.Columns = []model.Column{col("id"), col("email")}

	return document(
		model.Table{
			Name: "orders", LogicalName: "受注",
			Columns: []model.Column{col("customer_email")},
			ForeignKeys: []model.ForeignKey{{
				Name: "fk_orders_customer", Columns: []string{"customer_email"},
				References: model.Reference{Table: "customers", Columns: []string{"email"}},
			}},
		},
		target,
	)
}

// TestDocumentIsTotal covers the shapes a document that never passed JSON
// Schema validation can take. None of them may panic: jjf must be able to say
// something useful about a half-written document, and a caller that skipped
// validation is a caller this package still has to survive.
func TestDocumentIsTotal(t *testing.T) {
	tests := []struct {
		name string
		doc  *model.Document
	}{
		{name: "no tables at all", doc: document()},
		{name: "a table with no columns", doc: document(table("orders"))},
		{
			name: "a foreign key with empty column lists",
			doc: document(model.Table{
				Name: "orders", LogicalName: "受注",
				ForeignKeys: []model.ForeignKey{{}},
			}),
		},
		{
			name: "an empty primary key on a table with no columns",
			doc: document(model.Table{
				Name: "orders", LogicalName: "受注",
				PrimaryKey: &model.PrimaryKey{},
			}),
		},
		{
			name: "two tables sharing a name",
			doc: document(
				model.Table{
					Name: "orders", LogicalName: "受注",
					Columns:    []model.Column{col("id")},
					PrimaryKey: &model.PrimaryKey{Columns: []string{"id"}},
					ForeignKeys: []model.ForeignKey{{
						Columns:    []string{"id"},
						References: model.Reference{Table: "orders", Columns: []string{"id"}},
					}},
				},
				table("orders", col("id")),
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The assertion is that this returns at all. What it returns for a
			// document this broken is not a contract worth pinning.
			Document(tt.doc)
		})
	}
}

// TestDocumentIsDeterministic mirrors TestFixtureImportIsDeterministic in
// internal/importer/postgres: the findings are produced by walking slices, so
// two runs over the same document must agree exactly, order included.
func TestDocumentIsDeterministic(t *testing.T) {
	doc := decodeFixture(t, filepath.Join("testdata", "invalid", "every_check.json"))

	first := rendered(Document(doc))
	second := rendered(Document(doc))
	if !slices.Equal(first, second) {
		t.Errorf("two runs over the same document differ:\n%v\n%v", first, second)
	}
}

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// decodeFixture reads a committed document and decodes it.
func decodeFixture(t *testing.T, path string) *model.Document {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := model.Decode(raw)
	if err != nil {
		t.Fatalf("decoding %s: %v", path, err)
	}
	return doc
}

// fixturePaths lists every committed document under testdata/kind.
func fixturePaths(t *testing.T, kind string) []string {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join("testdata", kind, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatalf("no documents found under testdata/%s", kind)
	}
	return paths
}

func TestFixtures(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		for _, path := range fixturePaths(t, "valid") {
			t.Run(filepath.Base(path), func(t *testing.T) {
				if got := rendered(Document(decodeFixture(t, path))); got != nil {
					t.Errorf("findings got = %v, want none", got)
				}
			})
		}
	})

	t.Run("every_check.json", func(t *testing.T) {
		want := []string{
			`table accounts: defines column "email" more than once`,
			`primary key pk_accounts on table accounts: names column "id", which the table declares nullable`,
			`table accounts: declares more than one constraint or index called "pk_accounts"`,
			`foreign key fk_orders_ghost on table orders: references table "ghosts", which this document does not define`,
			"foreign key fk_orders_arity on table orders: names 1 column(s) but references 2",
			"foreign key fk_orders_code on table orders: references (email) of table accounts, " +
				"which no primary key, unique key or unique index there constrains to be unique",
			`index ix_orders_missing on table orders: names column "shipped_at", which the table does not define`,
		}
		doc := decodeFixture(t, filepath.Join("testdata", "invalid", "every_check.json"))
		if got := rendered(Document(doc)); !slices.Equal(got, want) {
			t.Errorf("findings got = %v, want %v", got, want)
		}
	})
}

// TestFixturesConformToTheSchema is the point of the fixtures: they prove the
// checker catches what JSON Schema CANNOT express. A fixture that is merely
// schema-invalid would be testing something else entirely, so both the valid
// and the invalid one have to pass "jjf validate" structurally.
//
// Importing internal/schema from a test in this package is not a cycle: schema
// does not import the checker, and it never will.
func TestFixturesConformToTheSchema(t *testing.T) {
	validator, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}

	for _, kind := range []string{"valid", "invalid"} {
		for _, path := range fixturePaths(t, kind) {
			t.Run(filepath.Join(kind, filepath.Base(path)), func(t *testing.T) {
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := validator.Validate(path, raw); err != nil {
					var ide *schema.InvalidDocumentError
					if errors.As(err, &ide) {
						var report strings.Builder
						ide.WriteReport(&report)
						t.Fatalf("%s does not conform to the jjf database design schema:\n%s", path, &report)
					}
					t.Fatalf("validating %s: %v", path, err)
				}
			})
		}
	}
}

// TestExampleDocumentIsSelfConsistent guards examples/db-design.example.json.
//
// The example is what people and AI agents copy from. A sample that contradicts
// itself would teach the wrong document and would make "jjf validate" warn
// about the very file the README points at.
func TestExampleDocumentIsSelfConsistent(t *testing.T) {
	path := filepath.Join("..", "..", "examples", "db-design.example.json")
	if got := rendered(Document(decodeFixture(t, path))); got != nil {
		t.Errorf("the shipped example contradicts itself:\n%s", strings.Join(got, "\n"))
	}
}
