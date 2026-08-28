package postgres

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/shutx-net/jumping-json-flush/internal/exitcode"
	"github.com/shutx-net/jumping-json-flush/internal/model"
	"github.com/shutx-net/jumping-json-flush/internal/schema"
)

// testOptions are the options every test here imports with: the defaults plus a
// source name, so that the database name never has to be spelled out.
func testOptions() Options {
	opt := DefaultOptions()
	opt.Source = "shop.sql"
	return opt
}

// mustImport imports src and fails the test if the import did not succeed.
func mustImport(t *testing.T, src string) (*model.Document, []Diagnostic) {
	t.Helper()
	doc, warnings, err := Import([]byte(src), testOptions())
	if err != nil {
		t.Fatalf("Import returned error %v, want no error", err)
	}
	return doc, warnings
}

// dumpSource is a dump in the order pg_dump really writes one: every table
// first, then the sequences, then the constraints, then the indexes, then the
// comments. The foreign key therefore refers backwards and the primary key it
// resolves against is declared after the table it belongs to.
const dumpSource = `--
-- PostgreSQL database dump
--

-- Dumped by pg_dump version 16.13 (Ubuntu 16.13-0ubuntu0.24.04.1)

SET statement_timeout = 0;
SET default_table_access_method = heap;

CREATE TABLE public.users (
    id integer NOT NULL,
    email character varying(255) NOT NULL,
    created_at timestamp(3) without time zone DEFAULT now() NOT NULL
);

CREATE SEQUENCE public.users_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE public.users_id_seq OWNED BY public.users.id;

CREATE TABLE public.orders (
    id integer NOT NULL,
    user_id integer NOT NULL,
    total numeric(10,2),
    note text
);

ALTER TABLE ONLY public.users ALTER COLUMN id SET DEFAULT nextval('public.users_id_seq'::regclass);

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_email_key UNIQUE (email);

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.orders
    ADD CONSTRAINT orders_pkey PRIMARY KEY (id);

CREATE INDEX orders_user_id_idx ON public.orders USING btree (user_id);

ALTER TABLE ONLY public.orders
    ADD CONSTRAINT orders_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON UPDATE RESTRICT ON DELETE CASCADE;

COMMENT ON TABLE public.users IS 'ユーザー
サービスを使う人。';

COMMENT ON COLUMN public.users.email IS 'メールアドレス';
`

func TestImportTables(t *testing.T) {
	doc, warnings := mustImport(t, dumpSource)
	if len(warnings) != 0 {
		t.Errorf("warnings got = %v, want none", warnings)
	}

	want := &model.Document{
		Schema:        "https://raw.githubusercontent.com/shutx-net/jumping-json-flush/main/schema/db-design.schema.json",
		FormatVersion: model.CurrentFormatVersion,
		Database:      model.Database{Name: "shop", DBMS: model.DBMSPostgreSQL},
		Tables: []model.Table{
			{
				Name:        "users",
				LogicalName: "ユーザー",
				Description: "サービスを使う人。",
				Columns: []model.Column{
					{Name: "id", LogicalName: "id", Type: "INTEGER", AutoIncrement: true},
					{Name: "email", LogicalName: "メールアドレス", Type: "VARCHAR", Length: intp(255)},
					{
						Name: "created_at", LogicalName: "created_at", Type: "TIMESTAMP",
						Precision: intp(3), Default: strp("now()"),
					},
				},
				PrimaryKey: &model.PrimaryKey{Name: "users_pkey", Columns: []string{"id"}},
				UniqueKeys: []model.UniqueKey{{Name: "users_email_key", Columns: []string{"email"}}},
			},
			{
				Name:        "orders",
				LogicalName: "orders",
				Columns: []model.Column{
					{Name: "id", LogicalName: "id", Type: "INTEGER"},
					{Name: "user_id", LogicalName: "user_id", Type: "INTEGER"},
					{Name: "total", LogicalName: "total", Type: "NUMERIC", Precision: intp(10), Scale: intp(2), Nullable: true},
					{Name: "note", LogicalName: "note", Type: "TEXT", Nullable: true},
				},
				PrimaryKey: &model.PrimaryKey{Name: "orders_pkey", Columns: []string{"id"}},
				ForeignKeys: []model.ForeignKey{{
					Name:       "orders_user_id_fkey",
					Columns:    []string{"user_id"},
					References: model.Reference{Table: "users", Columns: []string{"id"}},
					OnUpdate:   model.ActionRestrict,
					OnDelete:   model.ActionCascade,
				}},
				Indexes: []model.Index{{Name: "orders_user_id_idx", Columns: []string{"user_id"}}},
			},
		},
	}
	if !reflect.DeepEqual(doc, want) {
		t.Errorf("Import got = %s\nwant %s", mustEncode(t, doc), mustEncode(t, want))
	}
}

// strp returns a pointer to s, for the optional Default field.
func strp(s string) *string { return &s }

// mustEncode renders a document for a failure message.
func mustEncode(t *testing.T, doc *model.Document) []byte {
	t.Helper()
	raw, err := model.Encode(doc)
	if err != nil {
		t.Fatalf("Encode returned error %v, want no error", err)
	}
	return raw
}

func TestImportAutoIncrement(t *testing.T) {
	tests := []struct {
		name        string
		src         string
		wantWarning string
	}{
		{
			name: "serial pseudo-type",
			src:  "CREATE TABLE public.t (\n  id serial NOT NULL\n);",
		},
		{
			name: "identity inline",
			src:  "CREATE TABLE public.t (\n  id integer GENERATED BY DEFAULT AS IDENTITY NOT NULL\n);",
		},
		{
			name: "identity always inline",
			src:  "CREATE TABLE public.t (\n  id integer GENERATED ALWAYS AS IDENTITY (INCREMENT 1 START 1) NOT NULL\n);",
		},
		{
			name: "identity added later",
			src: "CREATE TABLE public.t (\n  id integer NOT NULL\n);\n" +
				"ALTER TABLE public.t ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (SEQUENCE NAME public.t_id_seq);",
		},
		{
			name: "nextval inline with the cast",
			src:  "CREATE TABLE public.t (\n  id integer DEFAULT nextval('public.t_id_seq'::regclass) NOT NULL\n);",
		},
		{
			name: "nextval without the cast or the schema",
			src:  "CREATE TABLE public.t (\n  id integer DEFAULT nextval('t_id_seq') NOT NULL\n);",
		},
		{
			name: "nextval added later",
			src: "CREATE TABLE public.t (\n  id integer NOT NULL\n);\n" +
				"ALTER TABLE ONLY public.t ALTER COLUMN id SET DEFAULT nextval('public.t_id_seq'::regclass);",
		},
		{
			name: "nextval on a sequence owned by another column",
			src: "CREATE TABLE public.t (\n  id integer DEFAULT nextval('public.other_seq'::regclass) NOT NULL\n);\n" +
				"CREATE SEQUENCE public.other_seq;\n" +
				"ALTER SEQUENCE public.other_seq OWNED BY public.other.id;",
			wantWarning: "sequence public.other_seq is owned by other.id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, warnings := mustImport(t, tt.src)
			col := doc.Tables[0].Columns[0]
			if !col.AutoIncrement {
				t.Errorf("auto increment got = false, want true")
			}
			// autoIncrement already states what the nextval() said; keeping the
			// default as well would say it twice.
			if col.Default != nil {
				t.Errorf("default got = %q, want none", *col.Default)
			}
			if tt.wantWarning == "" {
				if len(warnings) != 0 {
					t.Errorf("warnings got = %v, want none", warnings)
				}
				return
			}
			if len(warnings) != 1 || !strings.Contains(warnings[0].Message, tt.wantWarning) {
				t.Errorf("warnings got = %v, want one containing %q", warnings, tt.wantWarning)
			}
		})
	}
}

// TestAnIdentityColumnIsNeverNullable covers #56's first case. Every arm of
// TestImportAutoIncrement writes NOT NULL out loud, which is what a real
// pg_dump does; these are the same shapes with it left off, which is what a
// hand-written file does.
//
// PostgreSQL has no nullable identity column. Measured on 16.13: an inline
// GENERATED ... AS IDENTITY and the serial pseudo-type both set attnotnull
// whether or not the statement said so, and ALTER TABLE ... ADD GENERATED is
// refused outright unless the column is already NOT NULL ("column ... must be
// declared NOT NULL before identity can be added").
//
// A nextval() default is NOT one of them, and that is the line this test
// draws: it is an ordinary default that happens to read from a sequence, and
// 16.13 leaves such a column nullable. jjf records it as autoIncrement all the
// same, so "autoIncrement implies NOT NULL" would have been the wrong rule.
func TestAnIdentityColumnIsNeverNullable(t *testing.T) {
	tests := []struct {
		name         string
		src          string
		wantNullable bool
	}{
		{
			name: "inline GENERATED BY DEFAULT",
			src:  "CREATE TABLE public.t (\n  id integer GENERATED BY DEFAULT AS IDENTITY\n);",
		},
		{
			name: "inline GENERATED ALWAYS",
			src:  "CREATE TABLE public.t (\n  id integer GENERATED ALWAYS AS IDENTITY\n);",
		},
		{
			name: "the serial pseudo-type",
			src:  "CREATE TABLE public.t (\n  id bigserial\n);",
		},
		{
			// SQL the server would refuse, because it wants the NOT NULL
			// first. Recorded as NOT NULL anyway: there is no state of the
			// database this could describe in which the column is nullable.
			name: "identity added by ALTER TABLE",
			src: "CREATE TABLE public.t (\n  id integer\n);\n" +
				"ALTER TABLE public.t ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY;",
		},
		{
			name:         "a nextval default is not an identity",
			src:          "CREATE TABLE public.t (\n  id integer DEFAULT nextval('public.s')\n);",
			wantNullable: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, warnings := mustImport(t, tt.src)
			if len(warnings) != 0 {
				t.Errorf("warnings got = %v, want none", warnings)
			}
			col := doc.Tables[0].Columns[0]
			if !col.AutoIncrement {
				t.Errorf("auto increment got = false, want true")
			}
			if col.Nullable != tt.wantNullable {
				t.Errorf("nullable got = %v, want %v", col.Nullable, tt.wantNullable)
			}
		})
	}
}

func TestImportLogicalNames(t *testing.T) {
	const table = "CREATE TABLE public.t (\n  a integer\n);\n"
	long := strings.Repeat("x", maxLogicalNameLength+1)
	huge := strings.Repeat("y", maxDescriptionLength+10)

	tests := []struct {
		name            string
		src             string
		wantLogical     string
		wantDescription string
		wantWarning     string
	}{
		{
			name:        "no comment falls back to the physical name",
			src:         table,
			wantLogical: "t",
		},
		{
			name:        "single line comment",
			src:         table + "COMMENT ON TABLE public.t IS 'テーブル';",
			wantLogical: "テーブル",
		},
		{
			name:            "multi line comment",
			src:             table + "COMMENT ON TABLE public.t IS '会員\n購入者のマスタ';",
			wantLogical:     "会員",
			wantDescription: "購入者のマスタ",
		},
		{
			name:        "empty comment falls back to the physical name",
			src:         table + "COMMENT ON TABLE public.t IS '';",
			wantLogical: "t",
		},
		{
			name:            "an over-long first line becomes the description",
			src:             table + "COMMENT ON TABLE public.t IS '" + long + "';",
			wantLogical:     "t",
			wantDescription: long,
			wantWarning:     "first line of the comment is longer than 255 characters",
		},
		{
			name:            "an over-long description is truncated",
			src:             table + "COMMENT ON TABLE public.t IS 'name\n" + huge + "';",
			wantLogical:     "name",
			wantDescription: strings.Repeat("y", maxDescriptionLength),
			wantWarning:     "description is longer than 2000 characters",
		},
		{
			name:        "a comment on a table that was not imported",
			src:         table + "COMMENT ON TABLE public.missing IS 'x';",
			wantLogical: "t",
			wantWarning: "refers to an object that was not imported",
		},
		{
			name:        "a comment on a column that was not imported",
			src:         table + "COMMENT ON COLUMN public.t.missing IS 'x';",
			wantLogical: "t",
			wantWarning: "refers to an object that was not imported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, warnings := mustImport(t, tt.src)
			got := doc.Tables[0]
			if got.LogicalName != tt.wantLogical {
				t.Errorf("logical name got = %q, want %q", got.LogicalName, tt.wantLogical)
			}
			if got.Description != tt.wantDescription {
				t.Errorf("description got = %q, want %q", got.Description, tt.wantDescription)
			}
			if tt.wantWarning == "" {
				if len(warnings) != 0 {
					t.Errorf("warnings got = %v, want none", warnings)
				}
				return
			}
			if len(warnings) != 1 || !strings.Contains(warnings[0].Message, tt.wantWarning) {
				t.Errorf("warnings got = %v, want one containing %q", warnings, tt.wantWarning)
			}
		})
	}

	// Every column of a table without comments is named after itself, because
	// the schema requires a logical name and a dump has nothing better.
	doc, _ := mustImport(t, table)
	if got := doc.Tables[0].Columns[0]; got.LogicalName != got.Name {
		t.Errorf("column logical name got = %q, want %q", got.LogicalName, got.Name)
	}
}

// twoTables is the preamble the constraint tests attach their constraints to.
const twoTables = `CREATE TABLE public.users (
    id integer NOT NULL,
    org_id integer,
    email character varying(255)
);
CREATE TABLE public.orgs (
    id integer NOT NULL,
    region text NOT NULL
);
ALTER TABLE ONLY public.orgs ADD CONSTRAINT orgs_pkey PRIMARY KEY (id);
`

func TestImportConstraints(t *testing.T) {
	tests := []struct {
		name        string
		src         string
		wantWarning string
		check       func(t *testing.T, users model.Table)
	}{
		{
			name: "primary key forces its columns not null",
			src:  twoTables + "ALTER TABLE ONLY public.users ADD CONSTRAINT users_pkey PRIMARY KEY (id, org_id);",
			check: func(t *testing.T, users model.Table) {
				want := &model.PrimaryKey{Name: "users_pkey", Columns: []string{"id", "org_id"}}
				if !reflect.DeepEqual(users.PrimaryKey, want) {
					t.Errorf("primary key got = %v, want %v", users.PrimaryKey, want)
				}
				if users.Columns[1].Nullable {
					t.Error("org_id nullable got = true, want false")
				}
			},
		},
		{
			name: "multi column unique key",
			src:  twoTables + "ALTER TABLE ONLY public.users ADD CONSTRAINT users_uq UNIQUE (org_id, email);",
			check: func(t *testing.T, users model.Table) {
				want := []model.UniqueKey{{Name: "users_uq", Columns: []string{"org_id", "email"}}}
				if !reflect.DeepEqual(users.UniqueKeys, want) {
					t.Errorf("unique keys got = %v, want %v", users.UniqueKeys, want)
				}
			},
		},
		{
			name: "foreign key with both actions",
			src: twoTables + "ALTER TABLE ONLY public.users ADD CONSTRAINT users_org_fkey " +
				"FOREIGN KEY (org_id) REFERENCES public.orgs(id) ON UPDATE CASCADE ON DELETE SET NULL;",
			check: func(t *testing.T, users model.Table) {
				want := []model.ForeignKey{{
					Name:       "users_org_fkey",
					Columns:    []string{"org_id"},
					References: model.Reference{Table: "orgs", Columns: []string{"id"}},
					OnUpdate:   model.ActionCascade,
					OnDelete:   model.ActionSetNull,
				}}
				if !reflect.DeepEqual(users.ForeignKeys, want) {
					t.Errorf("foreign keys got = %v, want %v", users.ForeignKeys, want)
				}
			},
		},
		{
			name: "foreign key without a column list resolves to the primary key",
			src:  twoTables + "ALTER TABLE ONLY public.users ADD CONSTRAINT users_org_fkey FOREIGN KEY (org_id) REFERENCES public.orgs;",
			check: func(t *testing.T, users model.Table) {
				want := model.Reference{Table: "orgs", Columns: []string{"id"}}
				if !reflect.DeepEqual(users.ForeignKeys[0].References, want) {
					t.Errorf("references got = %v, want %v", users.ForeignKeys[0].References, want)
				}
			},
		},
		{
			name: "NO ACTION is kept when the dump wrote it",
			src: twoTables + "ALTER TABLE ONLY public.users ADD CONSTRAINT users_org_fkey " +
				"FOREIGN KEY (org_id) REFERENCES public.orgs(id) ON DELETE NO ACTION;",
			check: func(t *testing.T, users model.Table) {
				if got := users.ForeignKeys[0].OnDelete; got != model.ActionNoAction {
					t.Errorf("on delete got = %q, want %q", got, model.ActionNoAction)
				}
				if got := users.ForeignKeys[0].OnUpdate; got != "" {
					t.Errorf("on update got = %q, want it absent", got)
				}
			},
		},
		{
			name:        "foreign key without a column list against a table with no primary key",
			src:         twoTables + "ALTER TABLE ONLY public.users ADD CONSTRAINT users_self_fkey FOREIGN KEY (org_id) REFERENCES public.users;",
			wantWarning: "omits the referenced columns and users has no primary key",
		},
		{
			name:        "foreign key to an unknown table",
			src:         twoTables + "ALTER TABLE ONLY public.users ADD CONSTRAINT users_x_fkey FOREIGN KEY (org_id) REFERENCES public.nowhere(id);",
			wantWarning: "references table nowhere, which was not imported",
		},
		{
			name:        "foreign key with mismatched column counts",
			src:         twoTables + "ALTER TABLE ONLY public.users ADD CONSTRAINT users_org_fkey FOREIGN KEY (org_id) REFERENCES public.orgs(id, region);",
			wantWarning: "names 1 column(s) but references 2",
		},
		{
			name:        "primary key naming an unknown column",
			src:         twoTables + "ALTER TABLE ONLY public.users ADD CONSTRAINT users_pkey PRIMARY KEY (nope);",
			wantWarning: `primary key names unknown or repeated column "nope"`,
		},
		{
			name:        "unique key naming a column twice",
			src:         twoTables + "ALTER TABLE ONLY public.users ADD CONSTRAINT users_uq UNIQUE (org_id, org_id);",
			wantWarning: `unique key names unknown or repeated column "org_id"`,
		},
		{
			name: "a second primary key is ignored",
			src: twoTables + "ALTER TABLE ONLY public.users ADD CONSTRAINT users_pkey PRIMARY KEY (id);\n" +
				"ALTER TABLE ONLY public.users ADD CONSTRAINT users_pkey2 PRIMARY KEY (org_id);",
			wantWarning: "a second primary key is not imported",
			check: func(t *testing.T, users model.Table) {
				if users.PrimaryKey.Name != "users_pkey" {
					t.Errorf("primary key got = %v, want the first one", users.PrimaryKey.Name)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, warnings := mustImport(t, tt.src)
			if tt.check != nil {
				tt.check(t, doc.Tables[0])
			}
			if tt.wantWarning == "" {
				if len(warnings) != 0 {
					t.Errorf("warnings got = %v, want none", warnings)
				}
				return
			}
			if len(warnings) != 1 || !strings.Contains(warnings[0].Message, tt.wantWarning) {
				t.Errorf("warnings got = %v, want one containing %q", warnings, tt.wantWarning)
			}
		})
	}
}

// twoSchemasSource holds the same shape in two schemas, plus a foreign key that
// crosses from one into the other.
const twoSchemasSource = `CREATE TABLE public.users (
    id integer NOT NULL,
    log_id integer
);
CREATE TABLE audit.log (
    id integer NOT NULL
);
ALTER TABLE ONLY audit.log ADD CONSTRAINT log_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.users ADD CONSTRAINT users_log_fkey FOREIGN KEY (log_id) REFERENCES audit.log(id);
CREATE INDEX log_id_idx ON audit.log USING btree (id);
`

func TestImportSchemaFilter(t *testing.T) {
	t.Run("public by default", func(t *testing.T) {
		doc, warnings := mustImport(t, twoSchemasSource)
		if len(doc.Tables) != 1 || doc.Tables[0].Name != "users" {
			t.Fatalf("tables got = %v, want only users", doc.Tables)
		}
		if len(doc.Tables[0].ForeignKeys) != 0 {
			t.Errorf("foreign keys got = %v, want none", doc.Tables[0].ForeignKeys)
		}
		// The tables in audit are out of scope and silent; the foreign key that
		// crosses into them is a relationship that was really lost.
		if len(warnings) != 1 || !strings.Contains(warnings[0].Message, "outside schema public") {
			t.Errorf("warnings got = %v, want one about the cross-schema foreign key", warnings)
		}
	})

	t.Run("another schema", func(t *testing.T) {
		opt := testOptions()
		opt.Schema = "audit"
		doc, warnings, err := Import([]byte(twoSchemasSource), opt)
		if err != nil {
			t.Fatalf("Import returned error %v, want no error", err)
		}
		if len(doc.Tables) != 1 || doc.Tables[0].Name != "log" {
			t.Fatalf("tables got = %v, want only log", doc.Tables)
		}
		if len(doc.Tables[0].Indexes) != 1 {
			t.Errorf("indexes got = %v, want 1", doc.Tables[0].Indexes)
		}
		if len(warnings) != 0 {
			t.Errorf("warnings got = %v, want none", warnings)
		}
	})

	t.Run("a schema with no tables", func(t *testing.T) {
		opt := testOptions()
		opt.Schema = "empty"
		_, _, err := Import([]byte(twoSchemasSource), opt)
		if err == nil || !strings.Contains(err.Error(), `no tables found in schema "empty"`) {
			t.Fatalf("Import error got = %v, want one about an empty schema", err)
		}
		if got := exitcode.Of(err); got != exitcode.InvalidInput {
			t.Errorf("exit code got = %v, want %v", got, exitcode.InvalidInput)
		}
	})
}

func TestImportIdentifierErrors(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		wantMsg string
	}{
		{
			name:    "table name",
			src:     "CREATE TABLE public.\"user-profiles\" (\n  id integer\n);",
			wantMsg: `table name "user-profiles" cannot be represented`,
		},
		{
			name:    "non-ASCII table name",
			src:     "CREATE TABLE public.\"ユーザー\" (\n  id integer\n);",
			wantMsg: `table name "ユーザー" cannot be represented`,
		},
		{
			name:    "column name",
			src:     "CREATE TABLE public.t (\n  \"e-mail\" text\n);",
			wantMsg: `column name "e-mail" cannot be represented`,
		},
		{
			// A type whose name the format cannot hold stops the import in the
			// same way a column name does. It is not a hypothetical: a domain
			// or an enum may be named anything PostgreSQL can quote, and the
			// three messages this reaches are tested at the type layer without
			// anything showing that one of them reaches a user through Import.
			name:    "type name",
			src:     "CREATE TABLE public.t (\n  a public.\"my-type\"\n);",
			wantMsg: `t.a: type public.my-type cannot be written to a design document`,
		},
		{
			name: "index name",
			src: "CREATE TABLE public.t (\n  a integer\n);\n" +
				"CREATE INDEX \"idx-t\" ON public.t USING btree (a);",
			wantMsg: `index name "idx-t" cannot be represented`,
		},
		{
			name:    "duplicate table",
			src:     "CREATE TABLE public.t (a integer);\nCREATE TABLE public.t (b integer);",
			wantMsg: `table "t" is defined twice (first at line 1)`,
		},
		{
			// The schema's length limit, which is the half of validIdentifier
			// the rows above do not reach. PostgreSQL truncates identifiers at
			// 63 bytes unless it was compiled otherwise, so this arrives from a
			// hand-edited file rather than from a server - but the limit is
			// quoted in the message, and a limit nothing tests is a limit that
			// can drift away from the schema it claims to implement.
			name:    "an over-long table name",
			src:     "CREATE TABLE public.\"" + strings.Repeat("a", maxIdentifierLength+1) + "\" (\n  id integer\n);",
			wantMsg: "cannot be represented",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := Import([]byte(tt.src), testOptions())
			if err == nil {
				t.Fatalf("Import returned no error, want %q", tt.wantMsg)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error got = %q, want it to contain %q", err.Error(), tt.wantMsg)
			}
			var se *syntaxError
			if !errors.As(err, &se) {
				t.Fatalf("error type got = %T, want it to wrap *syntaxError", err)
			}
			if se.Line == 0 {
				t.Error("error line got = 0, want the line of the offending object")
			}
		})
	}

	// A constraint name is OPTIONAL in the schema, so an unusable one costs the
	// name and not the constraint.
	t.Run("constraint name is only a warning", func(t *testing.T) {
		doc, warnings := mustImport(t,
			"CREATE TABLE public.t (\n  a integer NOT NULL\n);\n"+
				"ALTER TABLE ONLY public.t ADD CONSTRAINT \"pk-t\" PRIMARY KEY (a);")
		pk := doc.Tables[0].PrimaryKey
		if pk == nil || pk.Name != "" || !reflect.DeepEqual(pk.Columns, []string{"a"}) {
			t.Fatalf("primary key got = %v, want an anonymous key on a", pk)
		}
		if len(warnings) != 1 || !strings.Contains(warnings[0].Message, `primary key name "pk-t" cannot be represented`) {
			t.Errorf("warnings got = %v, want one about the constraint name", warnings)
		}
	})
}

func TestImportDatabaseName(t *testing.T) {
	const src = "CREATE TABLE public.t (a integer);"

	tests := []struct {
		name     string
		opt      Options
		src      string
		want     string
		wantErr  string
		wantCode exitcode.Code
	}{
		{
			name: "from the source file name",
			opt:  Options{Source: "/tmp/schema.sql"},
			src:  src,
			want: "schema",
		},
		{
			name: "from the option",
			opt:  Options{Source: "schema.sql", Database: "shop"},
			src:  src,
			want: "shop",
		},
		{
			name: "from a connect line",
			opt:  Options{Source: "dump.sql"},
			src:  "\\connect mydb\n" + src,
			want: "mydb",
		},
		{
			name:    "an unusable file name",
			opt:     Options{Source: "my-db.sql"},
			src:     src,
			wantErr: "pass -database",
		},
		{
			name:    "no source at all",
			opt:     Options{},
			src:     src,
			wantErr: "cannot tell what the database is called",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, _, err := Import([]byte(tt.src), tt.opt)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Import error got = %v, want one containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Import returned error %v, want no error", err)
			}
			if doc.Database.Name != tt.want {
				t.Errorf("database name got = %q, want %q", doc.Database.Name, tt.want)
			}
		})
	}
}

func TestImportDefaults(t *testing.T) {
	long := strings.Repeat("a", maxDefaultLength)

	tests := []struct {
		name        string
		src         string
		want        *string
		wantWarning string
	}{
		{
			name: "a multi-line expression is collapsed",
			src:  "CREATE TABLE public.t (\n  a integer DEFAULT (1\n  +\n  2)\n);",
			want: strp("(1 + 2)"),
		},
		{
			name: "a literal keeps its quoting",
			src:  "CREATE TABLE public.t (\n  a jsonb DEFAULT '{}'::jsonb\n);",
			want: strp("'{}'::jsonb"),
		},
		{
			name: "an array constructor",
			src:  "CREATE TABLE public.t (\n  a text[] DEFAULT ARRAY[]::text[]\n);",
			want: strp("ARRAY[]::text[]"),
		},
		{
			name: "an explicit NULL default",
			src:  "CREATE TABLE public.t (\n  a integer DEFAULT NULL\n);",
			want: strp("NULL"),
		},
		{
			name:        "an over-long expression is dropped",
			src:         "CREATE TABLE public.t (\n  a text DEFAULT '" + long + "'\n);",
			wantWarning: "default expression is longer than 255 characters",
		},
		{
			name: "the later statement wins",
			src: "CREATE TABLE public.t (\n  a integer DEFAULT 1\n);\n" +
				"ALTER TABLE ONLY public.t ALTER COLUMN a SET DEFAULT 2;",
			want: strp("2"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, warnings := mustImport(t, tt.src)
			got := doc.Tables[0].Columns[0].Default
			switch {
			case tt.want == nil && got != nil:
				t.Errorf("default got = %q, want none", *got)
			case tt.want != nil && got == nil:
				t.Errorf("default got = none, want %q", *tt.want)
			case tt.want != nil && *got != *tt.want:
				t.Errorf("default got = %q, want %q", *got, *tt.want)
			}
			if tt.wantWarning == "" {
				if len(warnings) != 0 {
					t.Errorf("warnings got = %v, want none", warnings)
				}
				return
			}
			if len(warnings) != 1 || !strings.Contains(warnings[0].Message, tt.wantWarning) {
				t.Errorf("warnings got = %v, want one containing %q", warnings, tt.wantWarning)
			}
		})
	}
}

func TestImportPgDumpVersionWarning(t *testing.T) {
	const body = "\nCREATE TABLE public.t (a integer);\n"

	tests := []struct {
		name     string
		banner   string
		wantWarn bool
	}{
		{name: "too old", banner: "-- Dumped by pg_dump version 12.20\n", wantWarn: true},
		{name: "oldest supported", banner: "-- Dumped by pg_dump version 13.14\n"},
		{name: "current", banner: "-- Dumped by pg_dump version 16.13 (Ubuntu 16.13-0ubuntu0.24.04.1)\n"},
		{name: "newest supported", banner: "-- Dumped by pg_dump version 18.6 (Ubuntu 18.6-1.pgdg24.04+2)\n"},
		{name: "too new", banner: "-- Dumped by pg_dump version 19.0\n", wantWarn: true},
		{name: "no banner", banner: ""},
		// A banner whose version is not a number is treated as no banner at
		// all. checkDumpVersion says a file with nothing readable to check is a
		// legitimate input; warning about it would put a line on the standard
		// error of every hand-assembled dump.
		{name: "a banner with no readable version", banner: "-- Dumped by pg_dump version x.y\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, warnings := mustImport(t, tt.banner+body)
			if got := len(warnings) == 1; got != tt.wantWarn {
				t.Fatalf("warnings got = %v, want a version warning = %v", warnings, tt.wantWarn)
			}
			if tt.wantWarn && !strings.Contains(warnings[0].Message, "jjf supports 13 to 18") {
				t.Errorf("warning got = %q, want it to name the supported range", warnings[0].Message)
			}
		})
	}
}

func TestImportIsDeterministic(t *testing.T) {
	first, firstWarnings := mustImport(t, dumpSource)
	second, secondWarnings := mustImport(t, dumpSource)

	if a, b := mustEncode(t, first), mustEncode(t, second); string(a) != string(b) {
		t.Errorf("two imports of the same dump differ:\n%s\n%s", a, b)
	}
	if !reflect.DeepEqual(firstWarnings, secondWarnings) {
		t.Errorf("diagnostics got = %v and %v, want them equal", firstWarnings, secondWarnings)
	}
}

// notNullInItsOwnStatement states one column's nullability inline and the other
// two in separate ALTER TABLE statements, one in each direction.
const notNullInItsOwnStatement = `CREATE TABLE public.t (
  a integer,
  b integer NOT NULL,
  c integer
);
ALTER TABLE ONLY public.t ALTER COLUMN a SET NOT NULL;
ALTER TABLE ONLY public.t ALTER COLUMN b DROP NOT NULL;`

// TestNotNullStatedInItsOwnStatement is the highest-stake case in this file and
// the one whose absence was easiest to miss: the parser has always had a test
// for this statement and the resolver has never had one, so the function that
// carries the answer from one to the other had never run.
//
// What it protects is a wrong answer that says nothing. A column the dump
// declares NOT NULL would come out nullable; the document would satisfy the
// JSON Schema, jjf validate would find nothing to say about it, and the
// workbook and the diagram would both calmly describe a nullable column. There
// is no error, no warning and no golden diff, because no captured dump contains
// the statement - pg_dump writes NOT NULL inline.
//
// Both directions are asserted because the resolver's whole body is one
// assignment from the parsed Drop flag; testing SET alone would pass just as
// well if the flag were ignored. Column c is the control.
func TestNotNullStatedInItsOwnStatement(t *testing.T) {
	doc, warnings := mustImport(t, notNullInItsOwnStatement)
	if len(warnings) != 0 {
		t.Errorf("warnings got = %v, want none", warnings)
	}
	want := []struct {
		name     string
		nullable bool
	}{
		{name: "a", nullable: false},
		{name: "b", nullable: true},
		{name: "c", nullable: true},
	}
	cols := doc.Tables[0].Columns
	if len(cols) != len(want) {
		t.Fatalf("columns got = %+v, want %v", cols, len(want))
	}
	for i, w := range want {
		if cols[i].Name != w.name {
			t.Fatalf("column %d got = %q, want %q", i, cols[i].Name, w.name)
		}
		if cols[i].Nullable != w.nullable {
			t.Errorf("column %s nullable got = %v, want %v", w.name, cols[i].Nullable, w.nullable)
		}
	}
}

// TestAStatementNamingAColumnThatWasNotImported says one rule three times: a
// statement about a column the dump never created is reported and skipped, and
// the table it names keeps everything else.
//
// This is what a dump truncated between its CREATE TABLE and its ALTER TABLE
// statements looks like, and what a file assembled by hand from two dumps
// produces. Three functions implement the rule and the test is one table so
// that a fourth, added later, has an obvious place to go.
func TestAStatementNamingAColumnThatWasNotImported(t *testing.T) {
	tests := []struct {
		name        string
		statement   string
		wantMessage string
	}{
		{
			name:        "NOT NULL",
			statement:   "ALTER TABLE ONLY public.t ALTER COLUMN nosuch SET NOT NULL;",
			wantMessage: "t.nosuch: NOT NULL names a column that was not imported",
		},
		{
			name:        "DEFAULT",
			statement:   "ALTER TABLE ONLY public.t ALTER COLUMN nosuch SET DEFAULT 0;",
			wantMessage: "t.nosuch: DEFAULT names a column that was not imported",
		},
		{
			name:        "IDENTITY",
			statement:   "ALTER TABLE ONLY public.t ALTER COLUMN nosuch ADD GENERATED BY DEFAULT AS IDENTITY;",
			wantMessage: "t.nosuch: IDENTITY names a column that was not imported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, warnings := mustImport(t, "CREATE TABLE public.t (a integer);\n"+tt.statement)
			if len(warnings) != 1 {
				t.Fatalf("warnings got = %v, want exactly 1", warnings)
			}
			if !strings.Contains(warnings[0].Message, tt.wantMessage) {
				t.Errorf("warning got = %q, want it to contain %q", warnings[0].Message, tt.wantMessage)
			}
			if warnings[0].Line != 2 {
				t.Errorf("warning line got = %v, want 2", warnings[0].Line)
			}
			if len(doc.Tables) != 1 || len(doc.Tables[0].Columns) != 1 ||
				doc.Tables[0].Columns[0].Name != "a" {
				t.Errorf("tables got = %+v, want table t with only column a", doc.Tables)
			}
		})
	}
}

// tableWithNoColumns declares an empty table, everything a later statement can
// say about it, and one real table.
//
// Two tables are needed rather than one: build refuses a dump in which no table
// survives, so a lone empty table would exercise that error instead of the
// warn-and-skip this is about. The second table also carries the column the
// skipped table's constraints and index refer to, so the statements are
// well-formed and are dropped for the reason under test rather than for a
// second one.
const tableWithNoColumns = `CREATE TABLE public.empty (
);
CREATE TABLE public.t (
  a integer NOT NULL
);
ALTER TABLE ONLY public.t ADD CONSTRAINT t_pk PRIMARY KEY (a);
ALTER TABLE ONLY public.empty ADD CONSTRAINT empty_pk PRIMARY KEY (a);
ALTER TABLE ONLY public.empty ADD CONSTRAINT empty_fk FOREIGN KEY (a) REFERENCES public.t(a);
CREATE INDEX empty_idx ON public.empty USING btree (a);`

// TestATableWithNoColumnsIsReportedAndSkipped covers a legal PostgreSQL table
// that the design format has no way to write down - the schema requires at
// least one column - and, more interestingly, what the importer says about the
// statements that follow it.
//
// It says nothing. The table has already been reported once; repeating the
// complaint for each of its keys and indexes would bury the line that matters
// under lines that add nothing. The single-warning assertion is what states
// that decision, and it is the only place it is written down.
func TestATableWithNoColumnsIsReportedAndSkipped(t *testing.T) {
	doc, warnings := mustImport(t, tableWithNoColumns)
	if len(warnings) != 1 {
		t.Fatalf("warnings got = %v, want exactly 1", warnings)
	}
	if want := "table empty has no columns; not imported"; !strings.Contains(warnings[0].Message, want) {
		t.Errorf("warning got = %q, want it to contain %q", warnings[0].Message, want)
	}
	if warnings[0].Line != 1 {
		t.Errorf("warning line got = %v, want 1", warnings[0].Line)
	}
	if len(doc.Tables) != 1 || doc.Tables[0].Name != "t" {
		t.Fatalf("tables got = %+v, want only t", doc.Tables)
	}
	if doc.Tables[0].PrimaryKey == nil || doc.Tables[0].PrimaryKey.Name != "t_pk" {
		t.Errorf("primary key of t got = %v, want t_pk", doc.Tables[0].PrimaryKey)
	}
}

// foreignKeyTarget is a usable target for the failing foreign keys below: a
// table that exists, in the schema being imported, with a primary key. Every
// case in TestForeignKeysThatCannotBeResolved has to fail for the ONE reason it
// names, and applyForeignKey's guards shadow each other - an unresolvable local
// column returns before the target is ever looked at - so a case whose fixture
// tripped an earlier guard would pass while proving something else.
const foreignKeyTarget = `CREATE TABLE public.u (
  id integer NOT NULL,
  other integer NOT NULL
);
ALTER TABLE ONLY public.u ADD CONSTRAINT u_pk PRIMARY KEY (id);
CREATE TABLE public.t (
  a integer NOT NULL,
  b integer NOT NULL
);
ALTER TABLE ONLY public.t ADD CONSTRAINT t_uq UNIQUE (b);
`

func TestForeignKeysThatCannotBeResolved(t *testing.T) {
	tests := []struct {
		name        string
		constraint  string
		wantMessage string
	}{
		{
			// Both rows below reach the same guard, because resolvable answers
			// no for two different reasons and the message cannot tell them
			// apart. Two rows are worth it: a change that stopped catching
			// repeats would still pass the first one.
			name:        "a local column the table does not have",
			constraint:  "FOREIGN KEY (nosuch) REFERENCES public.u(id)",
			wantMessage: `table t: foreign key t_fk names unknown or repeated column "nosuch"; not imported`,
		},
		{
			name:        "the same local column twice",
			constraint:  "FOREIGN KEY (a, a) REFERENCES public.u(id, other)",
			wantMessage: `table t: foreign key t_fk names unknown or repeated column "a"; not imported`,
		},
		{
			name:        "a referenced column the target does not have",
			constraint:  "FOREIGN KEY (a) REFERENCES public.u(nosuch)",
			wantMessage: `table t: foreign key t_fk references unknown or repeated column u."nosuch"; not imported`,
		},
		{
			name:        "the same referenced column twice",
			constraint:  "FOREIGN KEY (a, b) REFERENCES public.u(id, id)",
			wantMessage: `table t: foreign key t_fk references unknown or repeated column u."id"; not imported`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, warnings := mustImport(t, foreignKeyTarget+
				"ALTER TABLE ONLY public.t ADD CONSTRAINT t_fk "+tt.constraint+";")
			if len(warnings) != 1 {
				t.Fatalf("warnings got = %v, want exactly 1", warnings)
			}
			if got := warnings[0].Message; got != tt.wantMessage {
				t.Errorf("warning got = %q, want %q", got, tt.wantMessage)
			}
			table := doc.Tables[1]
			if table.Name != "t" {
				t.Fatalf("second table got = %q, want %q", table.Name, "t")
			}
			if len(table.ForeignKeys) != 0 {
				t.Errorf("foreign keys got = %+v, want none", table.ForeignKeys)
			}
			// The neighbour. applyConstraints walks the whole list and each
			// failure returns from applyForeignKey alone; a change that
			// returned one frame higher would take this unique key with it and
			// say nothing.
			want := []model.UniqueKey{{Name: "t_uq", Columns: []string{"b"}}}
			if !reflect.DeepEqual(table.UniqueKeys, want) {
				t.Errorf("unique keys got = %v, want %v", table.UniqueKeys, want)
			}
		})
	}
}

// indexesThatCannotBeImported declares three indexes on one table: one with no
// name, one naming a column the table does not have, one naming a column twice,
// and - last, deliberately - one that is perfectly good.
const indexesThatCannotBeImported = `CREATE TABLE public.t (
  a integer NOT NULL,
  b integer NOT NULL
);
CREATE INDEX ON public.t USING btree (a);
CREATE INDEX t_x ON public.t USING btree (nosuch);
CREATE INDEX t_y ON public.t USING btree (a, a);
CREATE INDEX t_ab ON public.t USING btree (a, b);`

// TestIndexesThatCannotBeImportedAreReportedOneByOne asserts three warnings in
// source order and then the thing the warnings do not say: the good index
// declared after all of them arrived.
//
// applyIndexes continues past each failure, and the whole value of the test is
// that a change turning one of those continues into a return - or into an error
// - would drop every index after the first bad one without a word about it.
// Declaring the good index LAST is what makes the assertion mean that.
//
// An index with no name is a warning here while an unrepresentable index NAME
// is an error, and the asymmetry is deliberate: the schema requires index.name,
// so there is nothing to fall back on, but an index PostgreSQL named
// server-side was never written down in the dump at all.
func TestIndexesThatCannotBeImportedAreReportedOneByOne(t *testing.T) {
	doc, warnings := mustImport(t, indexesThatCannotBeImported)

	want := []string{
		"line 5: table t: an index without a name cannot be imported",
		`line 6: table t: index t_x names unknown or repeated column "nosuch"; not imported`,
		`line 7: table t: index t_y names unknown or repeated column "a"; not imported`,
	}
	if got := messages(warnings); !slices.Equal(got, want) {
		t.Fatalf("warnings got = %v, want %v", got, want)
	}
	got := doc.Tables[0].Indexes
	wantIndexes := []model.Index{{Name: "t_ab", Columns: []string{"a", "b"}}}
	if !reflect.DeepEqual(got, wantIndexes) {
		t.Errorf("indexes got = %v, want %v", got, wantIndexes)
	}
}

// setDefaultForeignKey is the one referential action the captured dumps do not
// carry. pg_dump writes it for a foreign key declared that way, and a missing
// arm in the action table would not fail anything - the field would simply be
// absent from the document, which is the quiet kind of wrong this whole file is
// about.
const setDefaultForeignKey = `CREATE TABLE public.u (
  id integer NOT NULL
);
ALTER TABLE ONLY public.u ADD CONSTRAINT u_pk PRIMARY KEY (id);
CREATE TABLE public.t (
  a integer
);
ALTER TABLE ONLY public.t ADD CONSTRAINT t_fk FOREIGN KEY (a) REFERENCES public.u(id) ON DELETE SET DEFAULT;`

func TestSetDefaultSurvivesAsAReferentialAction(t *testing.T) {
	doc, warnings := mustImport(t, setDefaultForeignKey)
	if len(warnings) != 0 {
		t.Errorf("warnings got = %v, want none", warnings)
	}
	want := []model.ForeignKey{{
		Name:       "t_fk",
		Columns:    []string{"a"},
		References: model.Reference{Table: "u", Columns: []string{"id"}},
		OnDelete:   model.ActionSetDefault,
	}}
	if got := doc.Tables[1].ForeignKeys; !reflect.DeepEqual(got, want) {
		t.Errorf("foreign keys got = %v, want %v", got, want)
	}
}

// TestTheFirstDefinitionOfARepeatedColumnWins pins a tie-break that is stated in
// a comment and decided nowhere else. PostgreSQL rejects a table that names a
// column twice, so this arrives only from a dump assembled or edited by hand -
// but when it does, the importer has to choose, and choosing the LAST definition
// would give the column a type the first half of the file contradicts.
func TestTheFirstDefinitionOfARepeatedColumnWins(t *testing.T) {
	doc, _ := mustImport(t, "CREATE TABLE public.t (\n  a integer NOT NULL,\n  a text\n);\n"+
		"ALTER TABLE ONLY public.t ALTER COLUMN a SET DEFAULT 7;")
	cols := doc.Tables[0].Columns
	if len(cols) != 2 {
		t.Fatalf("columns got = %+v, want both definitions kept as written", cols)
	}
	// The resolver's index holds the FIRST definition, so a later statement
	// about the column reaches that one.
	if cols[0].Type != "INTEGER" || cols[0].Nullable {
		t.Errorf("first column got = %+v, want a NOT NULL INTEGER", cols[0])
	}
	if cols[0].Default == nil || *cols[0].Default != "7" {
		t.Errorf("first column default got = %v, want 7", cols[0].Default)
	}
	if cols[1].Default != nil {
		t.Errorf("second column default got = %v, want none", cols[1].Default)
	}
}

// TestNextvalThatIsNotASequenceReference covers the guard that decides whether a
// column auto-increments or merely calls a function, which is a decision the
// document states as a fact about the database. Getting it wrong in either
// direction is material: a column wrongly marked autoIncrement loses its default
// expression entirely, since the two are never written together.
func TestNextvalThatIsNotASequenceReference(t *testing.T) {
	tests := []struct {
		name string
		expr string
	}{
		{
			// The nextval is only part of a larger expression, which is the
			// case sequenceFromNextval's own comment names.
			name: "nextval inside an arithmetic expression",
			expr: "nextval('public.s'::regclass) + 1",
		},
		{
			name: "nextval with a second argument",
			expr: "nextval('public.s'::regclass, 1)",
		},
		{
			name: "a function that is not nextval",
			expr: "other('public.s'::regclass)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, warnings := mustImport(t, "CREATE SEQUENCE public.s;\n"+
				"CREATE TABLE public.t (a integer DEFAULT "+tt.expr+");")
			if len(warnings) != 0 {
				t.Errorf("warnings got = %v, want none", warnings)
			}
			col := doc.Tables[0].Columns[0]
			if col.AutoIncrement {
				t.Errorf("column got = %+v, want it not marked auto increment", col)
			}
			if col.Default == nil || *col.Default != tt.expr {
				t.Errorf("default got = %v, want %q", col.Default, tt.expr)
			}
		})
	}
}

// sameNameInTwoSchemas gives two schemas a table with the same bare name and
// then says everything about the one that is NOT being imported.
const sameNameInTwoSchemas = `CREATE TABLE public.t (
  a integer
);
CREATE TABLE audit.t (
  a integer
);
ALTER TABLE ONLY audit.t ALTER COLUMN a SET NOT NULL;
ALTER TABLE ONLY audit.t ALTER COLUMN a SET DEFAULT 1;
COMMENT ON TABLE audit.t IS '監査';
COMMENT ON COLUMN audit.t.a IS '監査列';`

// TestStatementsAboutAnotherSchemaDoNotReachThisOne guards a failure that would
// be invisible in every other test in this file, because every other test has
// one schema.
//
// The resolver looks a column up by the table's BARE name - the index is keyed
// that way - so the schema filter is the only thing standing between audit.t's
// statements and public.t's columns. Drop it from any one of the three
// functions here and this dump would produce a public.t whose column is NOT
// NULL, carries a default of 1 and is named after an audit table, with no
// warning anywhere. Two tables with the same name in different schemas is
// ordinary in a database that separates its audit trail, so the input is not
// contrived.
func TestStatementsAboutAnotherSchemaDoNotReachThisOne(t *testing.T) {
	doc, warnings := mustImport(t, sameNameInTwoSchemas)
	if len(warnings) != 0 {
		t.Errorf("warnings got = %v, want none", warnings)
	}
	if len(doc.Tables) != 1 || doc.Tables[0].Name != "t" {
		t.Fatalf("tables got = %+v, want only public.t", doc.Tables)
	}
	table := doc.Tables[0]
	if table.LogicalName != "t" {
		t.Errorf("logical name got = %q, want %q: the comment belongs to the other schema", table.LogicalName, "t")
	}
	col := table.Columns[0]
	if !col.Nullable {
		t.Error("column a nullable got = false, want true: the NOT NULL belongs to the other schema")
	}
	if col.Default != nil {
		t.Errorf("column a default got = %q, want none", *col.Default)
	}
	if col.LogicalName != "a" {
		t.Errorf("column a logical name got = %q, want %q", col.LogicalName, "a")
	}
}

// importedSources lists every dump this package's tests import successfully.
// TestImportProducesAValidDocument runs all of them through the JSON Schema,
// which is the one invariant the whole importer exists to preserve.
func importedSources() []struct{ name, src string } {
	return []struct{ name, src string }{
		{"pg_dump order", dumpSource},
		{"create table", createTableSource + "\nCOMMENT ON TABLE public.users IS 'ユーザー\n利用者';"},
		{"two tables", twoTables},
		{"two schemas", twoSchemasSource},
		{"serial", "CREATE TABLE public.t (id serial NOT NULL, name text);"},
		{"identity", "CREATE TABLE public.t (id integer GENERATED ALWAYS AS IDENTITY NOT NULL);"},
		{"array columns", "CREATE TABLE public.t (a text[], b character varying(10)[], c integer);"},
		{"every warning kind", "CREATE TABLE public.t (\n" +
			"  a integer CHECK (a > 0),\n" +
			"  b numeric(10,2) DEFAULT 1.5,\n" +
			"  c timestamp(3) with time zone\n" +
			") INHERITS (public.parent);\n" +
			"CREATE INDEX t_a_idx ON public.t USING gin (a) WHERE (a IS NOT NULL);"},
		{"inline column constraints", inlineConstraintSource},
		{"tolerated column options", "CREATE TABLE public.t (\n" +
			"  a text COLLATE pg_catalog.\"C\" NOT NULL,\n" +
			"  b integer FUTURE_OPTION 7 DEFAULT 0,\n" +
			"  c integer NULL\n);"},
		{"not null in its own statement", notNullInItsOwnStatement},
		{"a table with no columns", tableWithNoColumns},
		{"an unresolvable foreign key", foreignKeyTarget +
			"ALTER TABLE ONLY public.t ADD CONSTRAINT t_fk FOREIGN KEY (nosuch) REFERENCES public.u(id);"},
		{"indexes that cannot be imported", indexesThatCannotBeImported},
		{"on delete set default", setDefaultForeignKey},
		{"the same table name in two schemas", sameNameInTwoSchemas},
	}
}

// inlineConstraintSource writes every key on the column it belongs to, which is
// what a hand-made file does and pg_dump never does. public.u is declared with
// its own primary key first because a foreign key to a table with none is
// dropped with a warning, and this source is here to be imported whole.
const inlineConstraintSource = `CREATE TABLE public.u (id integer NOT NULL, CONSTRAINT u_pk PRIMARY KEY (id));
CREATE TABLE public.t (
  id integer CONSTRAINT t_pk PRIMARY KEY,
  code text CONSTRAINT t_code_uq UNIQUE,
  owner integer CONSTRAINT t_owner_fk REFERENCES public.u(id) ON DELETE CASCADE
);`

// TestInlineConstraintsReachTheDocument is the second half of the argument that
// stmt_test.go's TestInlineColumnConstraintsBecomeTableConstraints opens. That
// test proves the constraints were built; this one proves they survived
// resolution and reached the document, which is where a reader of the xlsx or
// the DDL would notice them missing - and where nothing would be printed if they
// were.
//
// The nullability assertion is the one that is not a restatement of the input:
// the column definition never said NOT NULL, and the column comes out not
// nullable anyway, because a primary key forces its own columns.
func TestInlineConstraintsReachTheDocument(t *testing.T) {
	doc, warnings := mustImport(t, inlineConstraintSource)
	if len(warnings) != 0 {
		t.Errorf("warnings got = %v, want none", warnings)
	}
	if len(doc.Tables) != 2 {
		t.Fatalf("tables got = %v, want 2", len(doc.Tables))
	}
	table := doc.Tables[1]
	if table.Name != "t" {
		t.Fatalf("second table got = %q, want %q", table.Name, "t")
	}

	wantPK := &model.PrimaryKey{Name: "t_pk", Columns: []string{"id"}}
	if !reflect.DeepEqual(table.PrimaryKey, wantPK) {
		t.Errorf("primary key got = %v, want %v", table.PrimaryKey, wantPK)
	}
	wantUQ := []model.UniqueKey{{Name: "t_code_uq", Columns: []string{"code"}}}
	if !reflect.DeepEqual(table.UniqueKeys, wantUQ) {
		t.Errorf("unique keys got = %v, want %v", table.UniqueKeys, wantUQ)
	}
	wantFK := []model.ForeignKey{{
		Name:       "t_owner_fk",
		Columns:    []string{"owner"},
		References: model.Reference{Table: "u", Columns: []string{"id"}},
		OnDelete:   model.ActionCascade,
	}}
	if !reflect.DeepEqual(table.ForeignKeys, wantFK) {
		t.Errorf("foreign keys got = %v, want %v", table.ForeignKeys, wantFK)
	}
	if table.Columns[0].Nullable {
		t.Error("id nullable got = true, want false: a primary key forces its columns not null")
	}
}

func TestImportProducesAValidDocument(t *testing.T) {
	validator, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator returned error %v, want no error", err)
	}

	for _, tt := range importedSources() {
		t.Run(tt.name, func(t *testing.T) {
			doc, _ := mustImport(t, tt.src)
			raw := mustEncode(t, doc)
			if err := validator.Validate(tt.name, raw); err != nil {
				var ide *schema.InvalidDocumentError
				if errors.As(err, &ide) {
					var report strings.Builder
					ide.WriteReport(&report)
					t.Fatalf("the imported document does not conform to the schema:\n%s\n%s", report.String(), raw)
				}
				t.Fatalf("validating the imported document returned error %v, want no error", err)
			}
		})
	}
}

func TestNormalizeDefault(t *testing.T) {
	tests := []struct {
		name string
		expr string
		want string
	}{
		{"single line is untouched", "now()", "now()"},
		{"a wrapped expression folds onto one line", "(a\n  +\n  b)", "(a + b)"},
		{"a tab counts as separation", "a\t+\tb", "a + b"},
		{"the cast pg_dump writes keeps its spacing", "nextval('users_id_seq'::regclass)", "nextval('users_id_seq'::regclass)"},
		{"spacing inside a string literal survives", "'a  b'", "'a  b'"},
		{"a newline inside a string literal survives", "'a\nb'::text", "'a\nb'::text"},
		{"a doubled quote inside a literal survives", "'it''s  here'", "'it''s  here'"},
		{"a dollar quoted literal survives whole", "$tag$a  b$tag$", "$tag$a  b$tag$"},
		{"a quoted identifier keeps its case", `"MixedCase"`, `"MixedCase"`},
		{"a line comment is dropped rather than folded in", "1 -- why\n+ 2", "1 + 2"},
		{"surrounding whitespace is trimmed", "  now()  ", "now()"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeDefault(tt.expr); got != tt.want {
				t.Errorf("normalizeDefault(%q) = %q, want %q", tt.expr, got, tt.want)
			}
		})
	}
}
