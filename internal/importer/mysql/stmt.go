package mysql

import "strings"

// ---------------------------------------------------------------------------
// Dispatch
// ---------------------------------------------------------------------------

// stmtForm is the statement shape jjf recognised, if any.
type stmtForm uint8

// Statement forms. formSkip is the default on purpose: a dump contains an
// open-ended set of statements - SET, LOCK TABLES, UNLOCK TABLES, DROP TABLE,
// CREATE DATABASE / VIEW / TRIGGER / PROCEDURE / FUNCTION / EVENT, INSERT,
// GRANT and more, and more again in the next MySQL release - so what jjf maps
// is enumerated and everything else is skipped, never the other way round.
//
// USE is the one statement that is read rather than skipped without saying
// anything a table needs, and it is read from the token stream here rather than
// from raw bytes because, unlike the banner, it is a real statement.
const (
	formSkip stmtForm = iota
	formCreateTable
	formCreateIndex
	formAlterTable
	formUse
)

// isTableQualifier reports whether w is one of the words that may sit between
// CREATE and TABLE. It is a switch rather than a package-level slice so that
// nothing in the process can mutate it.
//
// One word, where the PostgreSQL sibling has five: MySQL has no UNLOGGED and no
// GLOBAL or LOCAL TEMP, and its temporary tables are spelled one way only.
func isTableQualifier(w string) bool { return w == "temporary" }

// classify identifies a statement from its leading words alone, consuming
// nothing.
//
// The trigger and view statements a dump contains reach this having been
// unwrapped from their executable comments, so they arrive as
// "CREATE DEFINER = ... TRIGGER ..." and "CREATE ALGORITHM = ... VIEW ...".
// Neither has TABLE or INDEX in the position tested below, so both fall through
// to formSkip without a rule of their own.
func classify(p *parser) stmtForm {
	switch p.wordAt(0) {
	case "create":
		n := 1
		for isTableQualifier(p.wordAt(n)) {
			n++
		}
		switch {
		case p.wordAt(n) == "table":
			return formCreateTable
		case p.wordAt(1) == "index",
			p.wordAt(1) == "unique" && p.wordAt(2) == "index",
			p.wordAt(1) == "fulltext" && p.wordAt(2) == "index",
			p.wordAt(1) == "spatial" && p.wordAt(2) == "index":
			return formCreateIndex
		}
	case "alter":
		if p.wordAt(1) == "table" {
			return formAlterTable
		}
	case "use":
		return formUse
	}
	return formSkip
}

// skippable reports whether a statement is one jjf does not map. Skipping is
// silent: "mysqldump --no-data" on a small schema already contains dozens of
// SET statements and a LOCK TABLES per table, and a warning for each would bury
// the warnings that matter.
func skippable(toks []token) bool { return classify(&parser{toks: toks}) == formSkip }

// dispatch routes one statement to its handler.
func (p *parser) dispatch(src []byte, out *myDump, d *diagList) error {
	switch classify(p) {
	case formCreateTable:
		return p.parseCreateTable(src, out, d)
	case formCreateIndex:
		return p.parseCreateIndex(out, d)
	case formAlterTable:
		return p.parseAlterTable(src, out, d)
	case formUse:
		return p.parseUse(out)
	}
	return nil
}

// parseUse records the database a dump connected to.
//
// Only the FIRST one is taken, which is what makes a USE beat the banner
// without a dump of several databases silently becoming the last of them. See
// parse, where the order of the two reads is the precedence rule.
func (p *parser) parseUse(out *myDump) error {
	p.acceptWord("use")
	name, err := p.identifier()
	if err != nil {
		return err
	}
	if out.Database == "" {
		out.Database = name
	}
	return nil
}

// ---------------------------------------------------------------------------
// CREATE TABLE
// ---------------------------------------------------------------------------

// parseCreateTable reads a table definition with its columns, its keys, its
// indexes and its comments.
//
// Everything is here, which is the shape of the input rather than a choice:
// mysqldump writes a table's columns, primary key, unique keys, plain keys,
// foreign keys, per-column COMMENT and table COMMENT all inside one statement,
// and puts AUTO_INCREMENT on the column. The PostgreSQL sibling's
// parseCreateTable reads a fraction of that and waits for the ALTER TABLE and
// COMMENT ON statements to say the rest.
func (p *parser) parseCreateTable(src []byte, out *myDump, d *diagList) error {
	line := p.peek().line
	p.acceptWord("create")
	for isTableQualifier(p.wordAt(0)) {
		p.next()
	}
	if !p.acceptWord("table") {
		return errAt(p.peek().line, "expected TABLE, got %s", describe(p.peek()))
	}
	p.acceptWords("if", "not", "exists")

	name, err := p.qualifiedName()
	if err != nil {
		return err
	}
	// CREATE TABLE t LIKE other and CREATE TABLE t (LIKE other) copy a
	// definition this statement does not contain, and CREATE TABLE t AS SELECT
	// gets its columns from a query. In all three there is nothing here to
	// import, so the table is reported and dropped rather than invented.
	if p.atWord("like") || (p.peek().isPunct("(") && p.wordAt(1) == "like") {
		d.warnf(line, "table %s: LIKE copies a definition this dump does not state; not imported", name)
		return nil
	}
	if !p.peek().isPunct("(") {
		d.warnf(line, "table %s: a table defined by a query is not imported", name)
		return nil
	}

	t := myTable{Name: name, Line: line}
	if err := p.expectPunct("("); err != nil {
		return err
	}
	if !p.acceptPunct(")") { // tolerated, although MySQL wants a column
		for {
			if isConstraintStart(p) {
				elem, err := p.parseTableConstraint(name, d)
				if err != nil {
					return err
				}
				switch elem.Kind {
				case elementConstraint:
					t.Constraints = append(t.Constraints, elem.Constraint)
				case elementIndex:
					t.Indexes = append(t.Indexes, elem.Index)
				}
			} else if err := p.parseColumnDefinition(src, &t, d); err != nil {
				return err
			}
			if p.acceptPunct(",") {
				continue
			}
			if p.acceptPunct(")") {
				break
			}
			return errAt(p.peek().line, "expected %q or %q in table definition, got %s", ",", ")", describe(p.peek()))
		}
	}

	if err := p.parseTableOptions(&t, d); err != nil {
		return err
	}
	out.Tables = append(out.Tables, t)
	return nil
}

// isConstraintStart reports whether the cursor is on a table element that is a
// key, an index or a constraint rather than a column definition.
//
// Every one of these eight words is RESERVED in MySQL, so a column can only be
// called one of them when it is quoted in backticks - and a backtick-quoted
// name is a kindQuotedIdent, which wordAt never returns. That is why this
// needs no lookahead escape hatch, where the PostgreSQL sibling has to check
// what follows EXCLUDE because EXCLUDE is not reserved there.
func isConstraintStart(p *parser) bool {
	switch p.wordAt(0) {
	case "constraint", "primary", "unique", "foreign", "check", "key", "index", "fulltext", "spatial":
		return true
	}
	return false
}

// parseColumnDefinition reads one column and the attributes written on it.
// Inline PRIMARY KEY, UNIQUE and REFERENCES clauses become ordinary constraints
// on the table, so that build.go has only one shape to resolve.
func (p *parser) parseColumnDefinition(src []byte, t *myTable, d *diagList) error {
	line := p.peek().line
	name, err := p.identifier()
	if err != nil {
		return err
	}
	typ, err := p.parseTypeName()
	if err != nil {
		return err
	}

	col := myColumn{Name: name, Line: line, Type: typ}
	label := "column " + t.Name.String() + "." + name

	for !p.done() && !p.peek().isPunct(",") && !p.peek().isPunct(")") {
		at := p.peek().line
		switch {
		case p.acceptWord("constraint"):
			// MySQL allows a symbol here only in front of a column CHECK, which
			// is dropped below, so the name has nowhere to go and is consumed
			// rather than carried.
			if !p.atWord("check") {
				if _, err := p.identifier(); err != nil {
					return err
				}
			}
		case p.acceptWords("not", "null"):
			col.NotNull = true
		case p.acceptWord("null"):
			// The default already; stated explicitly by mysqldump only inside
			// a DEFAULT clause, which the branch below reads instead.
		case p.acceptWord("default"):
			col.HasDefault = true
			col.Default = p.exprText(src, stopAtColumnAttribute(p.i))
		case p.acceptWord("auto_increment"):
			col.AutoIncrement = true
		case p.acceptWords("on", "update"):
			// ON UPDATE CURRENT_TIMESTAMP is an automatic update rule, not a
			// default value, and it is recorded as a flag for that reason.
			// Folding it into the default text would put something in a field
			// the schema defines as a DEFAULT expression that no database
			// accepts there, and the exporter copies defaults out verbatim, so
			// the round trip would then emit a script MySQL refuses.
			col.OnUpdateCurrentTimestamp = true
			if !p.done() && !p.peek().isPunct(",") && !p.peek().isPunct(")") {
				p.next()
			}
			if p.peek().isPunct("(") {
				if err := p.skipBalancedParens(); err != nil {
					return err
				}
			}
		case p.acceptWords("primary", "key"), p.acceptWord("key"):
			// A bare KEY in a column definition is MySQL's synonym for PRIMARY
			// KEY, not for INDEX, which is the one place the two words part.
			t.Constraints = append(t.Constraints, myConstraint{
				Kind: constraintPrimaryKey, NameLine: at,
				Table: t.Name, Columns: []string{name}, Line: at,
			})
		case p.acceptWord("unique"):
			p.acceptWord("key")
			p.acceptWord("index")
			t.Constraints = append(t.Constraints, myConstraint{
				Kind: constraintUnique, NameLine: at,
				Table: t.Name, Columns: []string{name}, Line: at,
			})
		case p.acceptWord("references"):
			c := myConstraint{
				Kind: constraintForeign, NameLine: at,
				Table: t.Name, Columns: []string{name}, Line: at,
			}
			if err := p.parseReferences(&c, d); err != nil {
				return err
			}
			t.Constraints = append(t.Constraints, c)
		case p.acceptWord("check"):
			d.warnf(at, "%s: check constraint is not imported", label)
			if err := p.skipBalancedParens(); err != nil {
				return err
			}
			// NOT ENFORCED is one optional phrase, so it is read whole
			// or not at all. Two independent accepts would take the NOT
			// of a following NOT NULL for its head and leave the NULL to
			// be read as an explicit one, costing the column its NOT NULL
			// and warning about nothing. ENFORCED alone is legal and is
			// still accepted; a lone NOT is not, so a NOT left here
			// belongs to the NOT NULL the loop reads next.
			if !p.acceptWords("not", "enforced") {
				p.acceptWord("enforced")
			}
		case p.acceptWord("comment"):
			text, err := p.stringValue("COMMENT")
			if err != nil {
				return err
			}
			col.Comment = text
		case p.acceptWords("generated", "always", "as"), p.acceptWord("as"):
			d.warnf(at, "%s: generated column is imported as an ordinary column", label)
			col.Generated = true
			if err := p.skipBalancedParens(); err != nil {
				return err
			}
			p.acceptWord("stored")
			p.acceptWord("virtual")
		case p.acceptWord("collate"), p.acceptWords("character", "set"), p.acceptWord("charset"):
			if _, err := p.identifier(); err != nil {
				return err
			}
		case p.acceptWord("srid"), p.acceptWord("column_format"), p.acceptWord("storage"):
			p.next()
		default:
			// An unrecognised column attribute is skipped rather than
			// rejected: tolerance here is what keeps a dump from a newer MySQL
			// working, and MySQL's attribute list is long and still growing.
			//
			// Whatever it carries in parentheses is skipped WITH it. One token
			// at a time would walk onto the ")" that closes those parentheses,
			// which the loop condition above reads as the end of the column
			// list - and every column and key written after this one would
			// then be swallowed by parseTableOptions without a word.
			if p.peek().isPunct("(") {
				if err := p.skipBalancedParens(); err != nil {
					return err
				}
			} else {
				p.next()
			}
		}
	}
	t.Columns = append(t.Columns, col)
	return nil
}

// stringValue reads the string a COMMENT clause or a string-valued option
// carries. what names the clause in the failure message.
func (p *parser) stringValue(what string) (string, error) {
	p.acceptPunct("=")
	tok := p.peek()
	if tok.kind != kindString {
		return "", errAt(tok.line, "expected a string after %s, got %s", what, describe(tok))
	}
	p.i++
	return tok.text, nil
}

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// parseTypeName reads a type reference.
//
// The order of the parts is the trap, and it is the mirror image of the
// PostgreSQL sibling's. PostgreSQL writes "timestamp(3) without time zone", so
// its trailing WORDS come after the arguments; MySQL writes
// "decimal(10,2) unsigned", so its trailing ATTRIBUTES do. A parser that
// collects everything before the "(" as the type name reads that as "decimal"
// with no attribute, and one that appends UNSIGNED to the name produces a type
// whose spelling depends on where the parser stopped.
//
// "double precision" is the one MySQL type written as two words. mysqldump
// normalises it to "double", so the two-word form only ever arrives from a
// hand-written file.
func (p *parser) parseTypeName() (myType, error) {
	tok := p.peek()
	line := tok.line
	if tok.kind != kindWord && tok.kind != kindQuotedIdent {
		return myType{}, errAt(line, "expected a type name, got %s", describe(tok))
	}
	name, err := p.identifier()
	if err != nil {
		return myType{}, err
	}

	t := myType{Words: []string{name}, Line: line}
	// The multi-word type names, every one of them a standard SQL spelling
	// MySQL accepts as a synonym of a one-word type. They are read here rather
	// than left to the attribute loop because a type's own words are not
	// attributes: the loop would drop them, and the parenthesis that follows
	// them would end the column list. Measured against MySQL 8.0.46, which
	// accepts all of these and reports them as varchar, char, mediumtext,
	// mediumblob and double.
	switch t.Words[0] {
	case "double":
		p.acceptTypeWord(&t, "precision")
	case "national":
		if !p.acceptTypeWord(&t, "varchar") {
			if p.acceptTypeWord(&t, "character") || p.acceptTypeWord(&t, "char") {
				p.acceptTypeWord(&t, "varying")
			}
		}
	case "character", "char", "nchar":
		p.acceptTypeWord(&t, "varying")
	case "long":
		if !p.acceptTypeWord(&t, "varchar") {
			p.acceptTypeWord(&t, "varbinary")
		}
	}
	if p.peek().isPunct("(") {
		args, err := p.typeArgs()
		if err != nil {
			return myType{}, err
		}
		t.Args = args
	}

	for {
		switch {
		case p.acceptWord("unsigned"):
			t.Unsigned = true
		case p.acceptWord("zerofill"):
			// ZEROFILL implies UNSIGNED at the server, but the inference is
			// deliberately not made here: this representation records what the
			// dump wrote, and mysqldump writes both words when both hold.
			t.Zerofill = true
		case p.acceptWord("collate"), p.acceptWords("character", "set"), p.acceptWord("charset"):
			if _, err := p.identifier(); err != nil {
				return myType{}, err
			}
		default:
			return t, nil
		}
	}
}

// acceptTypeWord consumes w when it is at the cursor and records it as another
// word of the type name being read.
func (p *parser) acceptTypeWord(t *myType, w string) bool {
	if !p.acceptWord(w) {
		return false
	}
	t.Words = append(t.Words, w)
	return true
}

// typeArgs reads the parenthesised argument list of a type. Each argument is
// returned as text so that the length of varchar(255), the two parts of
// decimal(10,2) and the whole value list of enum('a','b') come back in one
// shape.
//
// A value list arrives intact because a string is one token: the comma inside
// enum('a','b,c') is a byte of the second value and never a separator here.
func (p *parser) typeArgs() ([]string, error) {
	line := p.peek().line
	if err := p.expectPunct("("); err != nil {
		return nil, err
	}
	var args []string
	for {
		var parts []string
		for depth := 0; !p.done(); {
			tok := p.peek()
			if depth == 0 && (tok.isPunct(",") || tok.isPunct(")")) {
				break
			}
			switch {
			case tok.isPunct("("):
				depth++
			case tok.isPunct(")"):
				depth--
			}
			parts = append(parts, tok.text)
			p.i++
		}
		args = append(args, strings.Join(parts, " "))
		if p.acceptPunct(",") {
			continue
		}
		if p.acceptPunct(")") {
			return args, nil
		}
		return nil, errAt(line, "unterminated type argument list")
	}
}

// ---------------------------------------------------------------------------
// Table elements: constraints, keys and indexes
// ---------------------------------------------------------------------------

// elementKind says what one table element amounted to.
type elementKind uint8

// Element kinds. elementDropped means the element was recognised, warned about
// and deliberately not represented, which is what a CHECK, a FULLTEXT KEY and a
// SPATIAL KEY all become.
const (
	elementDropped elementKind = iota
	elementConstraint
	elementIndex
)

// tableElement is the result of reading one table element. Kind says which of
// the two payloads is meaningful.
//
// A kind and two values rather than the PostgreSQL sibling's (pgConstraint,
// bool, error), because MySQL writes plain KEY elements inside a CREATE TABLE
// and those are indexes rather than constraints, so there are three outcomes
// here where there were two.
type tableElement struct {
	Kind       elementKind
	Constraint myConstraint
	Index      myIndex
}

// parseTableConstraint reads a constraint, a key or an index written as a table
// element or added by ALTER TABLE.
//
// CHECK, FULLTEXT and SPATIAL are recognised, warned about and dropped, because
// the design format has nowhere to put any of them.
func (p *parser) parseTableConstraint(t qname, d *diagList) (tableElement, error) {
	line := p.peek().line
	name, nameLine := "", line
	if p.acceptWord("constraint") {
		// The symbol is optional: CONSTRAINT PRIMARY KEY (id) is legal.
		if !isConstraintStart(p) {
			nameLine = p.peek().line
			n, err := p.identifier()
			if err != nil {
				return tableElement{}, err
			}
			name = n
		}
	}

	switch {
	case p.acceptWords("primary", "key"):
		return p.finishKeyConstraint(constraintPrimaryKey, name, nameLine, t, line, d)
	case p.acceptWord("unique"):
		p.acceptWord("key")
		p.acceptWord("index")
		n, nl, err := p.optionalIndexName()
		if err != nil {
			return tableElement{}, err
		}
		if n != "" {
			// MySQL treats the index name as the constraint name for a unique
			// key, so it wins over a CONSTRAINT symbol when both are written.
			name, nameLine = n, nl
		}
		return p.finishKeyConstraint(constraintUnique, name, nameLine, t, line, d)
	case p.acceptWords("foreign", "key"):
		c := myConstraint{Kind: constraintForeign, Name: name, NameLine: nameLine, Table: t, Line: line}
		// FOREIGN KEY may carry an index name of its own before the columns.
		// It names the index MySQL creates to back the key, not the key, so it
		// is read and discarded.
		if _, _, err := p.optionalIndexName(); err != nil {
			return tableElement{}, err
		}
		cols, err := p.columnList()
		if err != nil {
			return tableElement{}, err
		}
		c.Columns = cols
		if !p.acceptWord("references") {
			return tableElement{}, errAt(p.peek().line, "expected REFERENCES, got %s", describe(p.peek()))
		}
		if err := p.parseReferences(&c, d); err != nil {
			return tableElement{}, err
		}
		p.skipElement()
		return tableElement{Kind: elementConstraint, Constraint: c}, nil
	case p.acceptWord("key"), p.acceptWord("index"):
		idx := myIndex{Name: name, Table: t, Line: line}
		n, _, err := p.optionalIndexName()
		if err != nil {
			return tableElement{}, err
		}
		if n != "" {
			idx.Name = n
		}
		p.acceptIndexType()
		cols, err := p.indexColumnList()
		if err != nil {
			return tableElement{}, err
		}
		p.skipElement()
		label := "index" + labelOf(idx.Name) + " on table " + t.String()
		if cols.Expression {
			d.warnf(line, "%s: expression index is not imported", label)
			return tableElement{}, nil
		}
		idx.Columns = cols.Names
		reportIndexColumnLosses(d, line, label, cols)
		return tableElement{Kind: elementIndex, Index: idx}, nil
	case p.atWord("fulltext"), p.atWord("spatial"):
		kind := "full-text"
		if p.atWord("spatial") {
			kind = "spatial"
		}
		p.next()
		return p.dropSpecialKey(kind, t, line, d)
	case p.acceptWord("check"):
		c := myConstraint{Kind: constraintCheck, Name: name, NameLine: nameLine, Table: t, Line: line}
		d.warnf(line, "constraint%s on table %s: check constraint is not imported", labelOf(name), t)
		if err := p.skipBalancedParens(); err != nil {
			return tableElement{}, err
		}
		p.skipElement()
		return tableElement{Constraint: c}, nil
	}
	return tableElement{}, errAt(p.peek().line, "expected a key, an index or a constraint, got %s", describe(p.peek()))
}

// finishKeyConstraint reads the column list of a PRIMARY KEY or UNIQUE element
// and everything after it. The two differ only in the kind they record.
func (p *parser) finishKeyConstraint(kind constraintKind, name string, nameLine int, t qname, line int, d *diagList) (tableElement, error) {
	c := myConstraint{Kind: kind, Name: name, NameLine: nameLine, Table: t, Line: line}
	p.acceptIndexType()
	cols, err := p.indexColumnList()
	if err != nil {
		return tableElement{}, err
	}
	p.skipElement()

	what := "unique key"
	if kind == constraintPrimaryKey {
		what = "primary key"
	}
	label := what + labelOf(name) + " on table " + t.String()
	if cols.Expression {
		// Unlike an index, a key the document cannot name in full cannot be
		// narrowed and kept: it would claim a uniqueness over the wrong
		// columns, and a foreign key elsewhere may be resting on it.
		d.warnf(line, "%s: expression key part is not imported; the key is dropped", label)
		return tableElement{}, nil
	}
	c.Columns = cols.Names
	reportIndexColumnLosses(d, line, label, cols)
	return tableElement{Kind: elementConstraint, Constraint: c}, nil
}

// dropSpecialKey reads a FULLTEXT or SPATIAL key far enough to step over it and
// reports what was lost. Neither is an index over the columns the document
// would name: a full-text index answers a MATCH ... AGAINST and a spatial one
// an R-tree query, so importing either as a plain index would describe
// something that does not exist.
func (p *parser) dropSpecialKey(kind string, t qname, line int, d *diagList) (tableElement, error) {
	p.acceptWord("key")
	p.acceptWord("index")
	name, _, err := p.optionalIndexName()
	if err != nil {
		return tableElement{}, err
	}
	if _, err := p.indexColumnList(); err != nil {
		return tableElement{}, err
	}
	p.skipElement()
	d.warnf(line, "index%s on table %s: %s index is not imported", labelOf(name), t, kind)
	return tableElement{}, nil
}

// optionalIndexName reads the name a key or index element may carry before its
// column list, with the line it was written on. It returns "" when the element
// went straight to its columns, which an index name never can be: the lexer
// refuses an empty backtick pair.
func (p *parser) optionalIndexName() (string, int, error) {
	tok := p.peek()
	if tok.isPunct("(") || tok.isWord("using") {
		return "", 0, nil
	}
	if tok.kind != kindWord && tok.kind != kindQuotedIdent {
		return "", 0, nil
	}
	name, err := p.identifier()
	if err != nil {
		return "", 0, err
	}
	return name, tok.line, nil
}

// acceptIndexType consumes an optional USING BTREE or USING HASH.
//
// The method is read and discarded rather than recorded, which is where myIndex
// differs from the PostgreSQL sibling's pgIndex and why there is no warning
// either. InnoDB has only B-trees and accepts USING HASH in silence, so the
// clause is a request the engine need not honour; the one engine that does
// honour it, MEMORY, already warns for not being InnoDB.
func (p *parser) acceptIndexType() {
	if p.acceptWord("using") {
		p.next()
	}
}

// reportIndexColumnLosses warns about what a key or index column list said that
// a design document cannot hold.
//
// A prefix length and a descending order are reported and the object is KEPT,
// because both still describe an index over the columns the document names. An
// expression is not reported here: it makes the caller drop the object whole,
// and the caller says so in its own words.
func reportIndexColumnLosses(d *diagList, line int, label string, cols indexColumns) {
	for _, name := range cols.Prefixed {
		d.warnf(line, "%s: prefix length on column %q is not imported; the whole column is recorded", label, name)
	}
	if cols.Descending {
		d.warnf(line, "%s: descending column order is not imported", label)
	}
}

// parseReferences reads the target of a foreign key and its referential
// actions. The cursor is on the token after REFERENCES.
func (p *parser) parseReferences(c *myConstraint, d *diagList) error {
	ref, err := p.qualifiedName()
	if err != nil {
		return err
	}
	c.RefTable = ref
	if p.peek().isPunct("(") {
		cols, err := p.columnList()
		if err != nil {
			return err
		}
		c.RefColumns = cols
	}
	return p.parseReferentialActions(c, d)
}

// parseReferentialActions reads MATCH, ON DELETE and ON UPDATE clauses in any
// order. The action is stored in the spelling the design format uses, so that
// build.go only has to check membership.
func (p *parser) parseReferentialActions(c *myConstraint, d *diagList) error {
	label := "foreign key" + labelOf(c.Name) + " on table " + c.Table.String()
	for !p.done() {
		line := p.peek().line
		switch {
		case p.acceptWord("match"):
			switch {
			case p.acceptWord("full"):
				d.warnf(line, "%s: MATCH FULL is not imported", label)
			case p.acceptWord("partial"):
				d.warnf(line, "%s: MATCH PARTIAL is not imported", label)
			default:
				p.acceptWord("simple")
			}
		case p.acceptWords("on", "delete"):
			action, err := p.referentialAction()
			if err != nil {
				return err
			}
			c.OnDelete = action
		case p.acceptWords("on", "update"):
			action, err := p.referentialAction()
			if err != nil {
				return err
			}
			c.OnUpdate = action
		default:
			return nil
		}
	}
	return nil
}

// referentialAction reads one referential action.
//
// All five are recorded, SET DEFAULT included. MySQL 8.0.46 accepts the clause,
// stores it, and mysqldump writes it back verbatim; InnoDB simply never carries
// it out. Refusing it here would mean refusing a dump of a database MySQL
// itself created, which is not a statement about the document.
//
// There is no optional column list on SET NULL and SET DEFAULT, which is why
// this has no counterpart to the PostgreSQL sibling's skipActionColumns:
// MySQL's grammar has nowhere to write one.
func (p *parser) referentialAction() (string, error) {
	line := p.peek().line
	switch {
	case p.acceptWord("cascade"):
		return "CASCADE", nil
	case p.acceptWord("restrict"):
		return "RESTRICT", nil
	case p.acceptWords("no", "action"):
		return "NO ACTION", nil
	case p.acceptWords("set", "null"):
		return "SET NULL", nil
	case p.acceptWords("set", "default"):
		return "SET DEFAULT", nil
	}
	return "", errAt(line, "expected a referential action, got %s", describe(p.peek()))
}

// labelOf renders a constraint or index name for a diagnostic, or nothing at all
// when the object was written without one.
func labelOf(name string) string {
	if name == "" {
		return ""
	}
	return " " + name
}

// ---------------------------------------------------------------------------
// Table options
// ---------------------------------------------------------------------------

// parseTableOptions reads the tail of a CREATE TABLE.
//
// Almost everything here is skipped SILENTLY, and that is the point. mysqldump
// writes "ENGINE=InnoDB AUTO_INCREMENT=42 DEFAULT CHARSET=utf8mb4
// COLLATE=utf8mb4_0900_ai_ci ROW_FORMAT=DYNAMIC" on every single table, so
// warning about each dropped option would put four warnings per table in front
// of the warnings that matter - which is the argument skippable already makes
// about SET and LOCK TABLES.
//
// Two options change what the table IS rather than how it is stored, and those
// two warn. A non-InnoDB table silently ignores the foreign keys the document
// will claim it has. A partitioned table is not the table the document would
// describe - although its columns still are, so it is kept and the partitioning
// alone is reported. COMMENT and ENGINE are additionally READ, because the
// first is authored content and the second is what the engine warning is about.
func (p *parser) parseTableOptions(t *myTable, d *diagList) error {
	for !p.done() {
		at := p.peek().line
		switch {
		case p.acceptWords("partition", "by"):
			d.warnf(at, "table %s: partitioning is not imported", t.Name)
			return nil
		case p.acceptWord("comment"):
			text, err := p.stringValue("COMMENT")
			if err != nil {
				return err
			}
			t.Comment = text
		case p.acceptWord("engine"):
			p.acceptPunct("=")
			tok := p.peek()
			switch tok.kind {
			case kindWord, kindQuotedIdent, kindString:
				p.i++
				t.Engine = foldASCII([]byte(tok.text))
			default:
				return errAt(tok.line, "expected an engine name after ENGINE, got %s", describe(tok))
			}
			if t.Engine != "innodb" {
				d.warnf(at, "table %s: storage engine %s is not imported, and only InnoDB enforces the foreign keys a design document declares", t.Name, t.Engine)
			}
		case p.acceptWord("as"), p.acceptWord("select"):
			// A column list followed by a query. The columns that were written
			// down are true and stay imported; the query is not.
			d.warnf(at, "table %s: the query a table is defined from is not imported", t.Name)
			return nil
		default:
			p.next()
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// CREATE INDEX
// ---------------------------------------------------------------------------

// parseCreateIndex reads a standalone index definition.
//
// mysqldump never writes one - it puts every index inside the CREATE TABLE that
// owns it - but jjf's own MySQL writer does, and so does a hand-made file, so
// the form has to be understood for a script this tool wrote to be readable by
// the tool that wrote it.
func (p *parser) parseCreateIndex(out *myDump, d *diagList) error {
	line := p.peek().line
	p.acceptWord("create")
	idx := myIndex{Line: line}
	special := ""
	switch {
	case p.acceptWord("unique"):
		idx.Unique = true
	case p.acceptWord("fulltext"):
		special = "full-text"
	case p.acceptWord("spatial"):
		special = "spatial"
	}
	if !p.acceptWord("index") {
		return errAt(p.peek().line, "expected INDEX, got %s", describe(p.peek()))
	}
	name, err := p.identifier()
	if err != nil {
		return err
	}
	idx.Name = name
	p.acceptIndexType()
	if !p.acceptWord("on") {
		return errAt(p.peek().line, "expected ON in CREATE INDEX, got %s", describe(p.peek()))
	}
	table, err := p.qualifiedName()
	if err != nil {
		return err
	}
	idx.Table = table
	cols, err := p.indexColumnList()
	if err != nil {
		return err
	}

	label := "index" + labelOf(idx.Name) + " on table " + table.String()
	switch {
	case special != "":
		d.warnf(line, "%s: %s index is not imported", label, special)
	case cols.Expression:
		d.warnf(line, "%s: expression index is not imported", label)
	default:
		idx.Columns = cols.Names
		reportIndexColumnLosses(d, line, label, cols)
		out.Indexes = append(out.Indexes, idx)
	}
	return nil
}

// ---------------------------------------------------------------------------
// ALTER TABLE
// ---------------------------------------------------------------------------

// parseAlterTable reads the actions of an ALTER TABLE statement. jjf's own
// MySQL writer emits one action per statement, but the grammar allows a
// comma-separated list and a hand-made or concatenated dump may contain one.
//
// There is no ONLY and no IF EXISTS to step over, which the PostgreSQL sibling
// has to: MySQL's ALTER TABLE has neither.
func (p *parser) parseAlterTable(src []byte, out *myDump, d *diagList) error {
	p.acceptWord("alter")
	p.acceptWord("table")
	table, err := p.qualifiedName()
	if err != nil {
		return err
	}
	for {
		if err := p.parseAlterTableAction(src, table, out, d); err != nil {
			return err
		}
		if !p.acceptPunct(",") {
			return nil
		}
	}
}

// parseAlterTableAction reads one ALTER TABLE action.
//
// Only ADD is mapped. MODIFY and CHANGE restate a column the CREATE TABLE
// already gave, DROP takes one away, and RENAME, ENGINE, CONVERT TO CHARACTER
// SET, ALTER INDEX ... INVISIBLE and the rest say nothing about the design.
// Every one of them, and every action a future MySQL adds, is skipped without
// complaint - which is what keeps a dump from a newer server importable.
func (p *parser) parseAlterTableAction(src []byte, table qname, out *myDump, d *diagList) error {
	line := p.peek().line
	if !p.acceptWord("add") {
		p.skipElement()
		return nil
	}
	if isConstraintStart(p) {
		elem, err := p.parseTableConstraint(table, d)
		if err != nil {
			return err
		}
		switch elem.Kind {
		case elementConstraint:
			out.Constraints = append(out.Constraints, elem.Constraint)
		case elementIndex:
			out.Indexes = append(out.Indexes, elem.Index)
		}
		return nil
	}
	p.acceptWord("column")
	p.acceptWords("if", "not", "exists")
	t := out.table(table)
	if t == nil {
		d.warnf(line, "table %s: ADD COLUMN names a table this dump never created; not imported", table)
		p.skipElement()
		return nil
	}
	d.warnf(line, "table %s: ADD COLUMN is imported at the end of the column list", table)
	return p.parseColumnDefinition(src, t, d)
}
