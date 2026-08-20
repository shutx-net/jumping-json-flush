package postgres

import (
	"errors"
	"reflect"
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
		{name: "too new", banner: "-- Dumped by pg_dump version 18.0\n", wantWarn: true},
		{name: "no banner", banner: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, warnings := mustImport(t, tt.banner+body)
			if got := len(warnings) == 1; got != tt.wantWarn {
				t.Fatalf("warnings got = %v, want a version warning = %v", warnings, tt.wantWarn)
			}
			if tt.wantWarn && !strings.Contains(warnings[0].Message, "jjf supports 13 to 17") {
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
