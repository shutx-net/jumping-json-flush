package postgres

import "strings"

// ---------------------------------------------------------------------------
// Dispatch
// ---------------------------------------------------------------------------

// stmtForm is the statement shape jjf recognised, if any.
type stmtForm uint8

// Statement forms. formSkip is the default on purpose: a dump contains an
// open-ended set of statements - SET, SELECT pg_catalog.set_config, CREATE
// SCHEMA / TYPE / DOMAIN / FUNCTION / TRIGGER / VIEW / EXTENSION / POLICY,
// GRANT, REVOKE, COPY, ANALYZE and more, and more again in the next PostgreSQL
// release - so what jjf maps is enumerated and everything else is skipped,
// never the other way round.
const (
	formSkip stmtForm = iota
	formCreateTable
	formCreateIndex
	formCreateSequence
	formAlterTable
	formAlterSequence
	formCommentOn
)

// isTableQualifier reports whether w is one of the words that may sit between
// CREATE and TABLE. It is a switch rather than a package-level slice so that
// nothing in the process can mutate it.
func isTableQualifier(w string) bool {
	switch w {
	case "unlogged", "global", "local", "temp", "temporary":
		return true
	}
	return false
}

// classify identifies a statement from its leading words alone, consuming
// nothing.
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
		case p.wordAt(1) == "index", p.wordAt(1) == "unique" && p.wordAt(2) == "index":
			return formCreateIndex
		case p.wordAt(1) == "sequence":
			return formCreateSequence
		}
	case "alter":
		switch p.wordAt(1) {
		case "table":
			return formAlterTable
		case "sequence":
			return formAlterSequence
		}
	case "comment":
		if p.wordAt(1) == "on" {
			return formCommentOn
		}
	}
	return formSkip
}

// skippable reports whether a statement is one jjf does not map. Skipping is
// silent: a warning for every SET and GRANT in a dump would bury the warnings
// that matter.
func skippable(toks []token) bool { return classify(&parser{toks: toks}) == formSkip }

// dispatch routes one statement to its handler.
func (p *parser) dispatch(src []byte, out *pgDump, d *diagList) error {
	switch classify(p) {
	case formCreateTable:
		return p.parseCreateTable(src, out, d)
	case formCreateIndex:
		return p.parseCreateIndex(out, d)
	case formCreateSequence:
		return p.parseCreateSequence(out)
	case formAlterTable:
		return p.parseAlterTable(src, out, d)
	case formAlterSequence:
		return p.parseAlterSequence(out)
	case formCommentOn:
		return p.parseCommentOn(out, d)
	}
	return nil
}

// ---------------------------------------------------------------------------
// CREATE TABLE
// ---------------------------------------------------------------------------

// parseCreateTable reads a table definition with its columns and inline
// constraints.
func (p *parser) parseCreateTable(src []byte, out *pgDump, d *diagList) error {
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
	// A typed table (CREATE TABLE t OF type) and a partition (... PARTITION OF
	// parent) have no column list of their own, so there is nothing to import.
	if p.atWord("of") {
		d.warnf(line, "table %s: typed table is not imported", name)
		return nil
	}
	if p.acceptWords("partition", "of") {
		d.warnf(line, "table %s: table partition is not imported", name)
		return nil
	}

	t := pgTable{Name: name, Line: line}
	if err := p.expectPunct("("); err != nil {
		return err
	}
	if !p.acceptPunct(")") { // CREATE TABLE t () is legal and has no elements
		for {
			// LIKE copies another table's definition into this one, so the
			// columns this table really has are not written here at all.
			// Checked inside the loop and not once at the head, because
			// PostgreSQL allows the element beside ordinary columns - which
			// MySQL, whose importer answers the same SQL the same way, does
			// not. Dropped rather than guessed: a table built from what IS
			// written here describes something the database does not have,
			// and the parser used to build one whose single column was called
			// "like".
			//
			// The word alone decides, and that was measured rather than
			// assumed: pg_get_keywords() calls LIKE reserved, and 16.13 reads
			// "CREATE TABLE t (like text)" as an element copying a table
			// called text rather than as a column. A column honestly called
			// "like" is therefore quoted, and a quoted name is a
			// kindQuotedIdent, which atWord never returns.
			if p.atWord("like") {
				d.warnf(line, "table %s: LIKE copies a definition this dump does not state; not imported", name)
				return nil
			}
			if isConstraintStart(p) {
				c, ok, err := p.parseTableConstraint(name, d)
				if err != nil {
					return err
				}
				if ok {
					t.Constraints = append(t.Constraints, c)
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

	// What follows the column list is storage and inheritance detail. Only the
	// two forms that change the SHAPE of the table are worth a warning.
tail:
	for !p.done() {
		switch {
		case p.acceptWord("inherits"):
			d.warnf(line, "table %s: inherited columns are not imported", name)
			if err := p.skipBalancedParens(); err != nil {
				return err
			}
		case p.acceptWords("partition", "by"):
			d.warnf(line, "table %s: partitioning is not imported", name)
			break tail
		default:
			p.next()
		}
	}
	out.Tables = append(out.Tables, t)
	return nil
}

// isConstraintStart reports whether the cursor is on a table constraint rather
// than a column definition.
//
// Every leading word here is reserved in PostgreSQL except EXCLUDE, so a column
// can only be called one of them when it is quoted - and a quoted name is a
// kindQuotedIdent, which wordAt never returns. EXCLUDE is checked against what
// follows it, so a column honestly called "exclude" still parses.
func isConstraintStart(p *parser) bool {
	switch p.wordAt(0) {
	case "constraint", "primary", "unique", "foreign", "check":
		return true
	case "exclude":
		return p.wordAt(1) == "using" || p.punctAt(1, "(")
	}
	return false
}

// parseColumnDefinition reads one column and the constraints written on it.
// Inline PRIMARY KEY, UNIQUE and REFERENCES clauses become ordinary constraints
// on the table, so that build.go has only one shape to resolve.
func (p *parser) parseColumnDefinition(src []byte, t *pgTable, d *diagList) error {
	line := p.peek().line
	name, err := p.identifier()
	if err != nil {
		return err
	}
	typ, err := p.parseTypeName(d)
	if err != nil {
		return err
	}

	col := pgColumn{Name: name, Line: line, Type: typ}
	label := "column " + t.Name.String() + "." + name
	// A CONSTRAINT clause names whichever constraint follows it.
	pending := ""
	warnedDeferrable := false

	for !p.done() && !p.peek().isPunct(",") && !p.peek().isPunct(")") {
		at := p.peek().line
		switch {
		case p.acceptWord("constraint"):
			n, err := p.identifier()
			if err != nil {
				return err
			}
			pending = n
		case p.acceptWords("not", "null"):
			col.NotNull = true
		case p.acceptWord("null"):
			// The default already; stated explicitly by some tools.
		case p.acceptWord("default"):
			col.HasDefault = true
			col.Default = p.exprText(src, stopAtColumnConstraint(p.i))
		case p.acceptWords("primary", "key"):
			t.Constraints = append(t.Constraints, pgConstraint{
				Kind: constraintPrimaryKey, Name: pending, NameLine: at,
				Table: t.Name, Columns: []string{name}, Line: at,
			})
			pending = ""
		case p.acceptWord("unique"):
			if p.acceptWords("nulls", "not", "distinct") {
				d.warnf(at, "%s: NULLS NOT DISTINCT is not imported", label)
			}
			p.acceptWords("nulls", "distinct")
			t.Constraints = append(t.Constraints, pgConstraint{
				Kind: constraintUnique, Name: pending, NameLine: at,
				Table: t.Name, Columns: []string{name}, Line: at,
			})
			pending = ""
		case p.acceptWord("references"):
			c := pgConstraint{
				Kind: constraintForeign, Name: pending, NameLine: at,
				Table: t.Name, Columns: []string{name}, Line: at,
			}
			if err := p.parseReferences(&c, d); err != nil {
				return err
			}
			t.Constraints = append(t.Constraints, c)
			pending = ""
		case p.acceptWord("check"):
			d.warnf(at, "%s: check constraint is not imported", label)
			if err := p.skipBalancedParens(); err != nil {
				return err
			}
			p.acceptWords("no", "inherit")
		case p.acceptWords("generated", "always", "as", "identity"),
			p.acceptWords("generated", "by", "default", "as", "identity"):
			col.Identity = true
			if p.peek().isPunct("(") {
				if err := p.skipBalancedParens(); err != nil {
					return err
				}
			}
		case p.acceptWords("generated", "always", "as"):
			d.warnf(at, "%s: generated column is imported as an ordinary column", label)
			col.Generated = true
			if err := p.skipBalancedParens(); err != nil {
				return err
			}
			p.acceptWord("stored")
		case p.acceptWord("collate"):
			if _, err := p.qualifiedName(); err != nil {
				return err
			}
		case p.acceptWord("deferrable"),
			p.acceptWords("not", "deferrable"),
			p.acceptWords("initially", "deferred"),
			p.acceptWords("initially", "immediate"):
			if !warnedDeferrable {
				d.warnf(at, "%s: deferrable constraint is imported as an ordinary constraint", label)
				warnedDeferrable = true
			}
		case p.acceptWords("no", "inherit"):
			d.warnf(at, "%s: NO INHERIT is not imported", label)
		default:
			// An unrecognised column option is skipped rather than rejected:
			// tolerance here is what keeps a dump from a newer PostgreSQL
			// working.
			//
			// Whatever it carries in parentheses is skipped WITH it. One token
			// at a time would walk onto the ")" that closes those parentheses,
			// which the loop condition above reads as the end of the column
			// list - and every column, constraint and key written after this
			// one would then be lost without a word.
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

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// isIntervalField reports whether w is a field qualifier an INTERVAL type may
// carry.
func isIntervalField(w string) bool {
	switch w {
	case "year", "month", "day", "hour", "minute", "second":
		return true
	}
	return false
}

// parseTypeName reads a type reference.
//
// The order of the parts is the trap: PostgreSQL writes "timestamp(3) without
// time zone", so the parenthesised argument arrives BEFORE the trailing words.
// A parser that collects all the words first and then looks for a "(" reads
// that type as "timestamp" with no precision.
func (p *parser) parseTypeName(d *diagList) (pgType, error) {
	tok := p.peek()
	line := tok.line
	if tok.kind != kindWord && tok.kind != kindQuotedIdent {
		return pgType{}, errAt(line, "expected a type name, got %s", describe(tok))
	}
	name, err := p.qualifiedName()
	if err != nil {
		return pgType{}, err
	}

	t := pgType{Words: []string{name.String()}, Line: line}
	switch t.Words[0] {
	case "double":
		if p.acceptWord("precision") {
			t.Words = append(t.Words, "precision")
		}
	case "character", "char", "bit":
		if p.acceptWord("varying") {
			t.Words = append(t.Words, "varying")
		}
	case "national":
		// NATIONAL CHARACTER and NATIONAL CHAR, either of which may be
		// followed by VARYING. PostgreSQL 16.13 accepts all three and reports
		// them as character / character varying; they are read here rather
		// than left to the attribute loop because a type's own words are not
		// column options - the loop would drop them, and the parenthesis
		// after them would end the column list.
		if p.acceptWord("character") {
			t.Words = append(t.Words, "character")
		} else if p.acceptWord("char") {
			t.Words = append(t.Words, "char")
		}
		if len(t.Words) > 1 && p.acceptWord("varying") {
			t.Words = append(t.Words, "varying")
		}
	case "interval":
		// INTERVAL puts its field qualifier before the precision:
		// "interval day to second(6)".
		if f := p.wordAt(0); isIntervalField(f) {
			p.next()
			t.Words = append(t.Words, f)
			if p.acceptWord("to") {
				unit, err := p.identifier()
				if err != nil {
					return pgType{}, err
				}
				t.Words = append(t.Words, "to", unit)
			}
		}
	}

	if p.peek().isPunct("(") {
		args, err := p.typeArgs()
		if err != nil {
			return pgType{}, err
		}
		t.Args = args
	}

	switch t.Words[0] {
	case "timestamp", "time":
		if p.acceptWords("without", "time", "zone") {
			t.Words = append(t.Words, "without", "time", "zone")
		} else if p.acceptWords("with", "time", "zone") {
			t.Words = append(t.Words, "with", "time", "zone")
		}
	}

	for p.acceptPunct("[") {
		if p.peek().kind == kindNumber {
			// A dimension bound carries no meaning in PostgreSQL, which is why
			// dropping it is safe and worth saying once.
			d.warnf(p.peek().line, "type %s: array dimension bound is not imported", t)
			p.next()
		}
		if err := p.expectPunct("]"); err != nil {
			return pgType{}, err
		}
		t.ArrayDims++
	}
	return t, nil
}

// typeArgs reads the parenthesised argument list of a type. Each argument is
// returned as text so that both the length of varchar(255) and the two parts of
// numeric(10,2) come back in one shape.
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
// Constraints
// ---------------------------------------------------------------------------

// parseTableConstraint reads a constraint written as a table element or added by
// ALTER TABLE. The bool reports whether a constraint was produced: CHECK and
// EXCLUDE are recognised, warned about and dropped, because the design format
// has nowhere to put them.
func (p *parser) parseTableConstraint(t qname, d *diagList) (pgConstraint, bool, error) {
	line := p.peek().line
	c := pgConstraint{Table: t, Line: line, NameLine: line}
	if p.acceptWord("constraint") {
		c.NameLine = p.peek().line
		name, err := p.identifier()
		if err != nil {
			return c, false, err
		}
		c.Name = name
	}
	label := "constraint" + labelOf(c.Name) + " on table " + t.String()

	kept := true
	switch {
	case p.acceptWords("primary", "key"):
		c.Kind = constraintPrimaryKey
		cols, err := p.columnList()
		if err != nil {
			return c, false, err
		}
		c.Columns = cols
	case p.acceptWord("unique"):
		c.Kind = constraintUnique
		if p.acceptWords("nulls", "not", "distinct") {
			d.warnf(line, "%s: NULLS NOT DISTINCT is not imported", label)
		}
		p.acceptWords("nulls", "distinct")
		cols, err := p.columnList()
		if err != nil {
			return c, false, err
		}
		c.Columns = cols
	case p.acceptWords("foreign", "key"):
		c.Kind = constraintForeign
		cols, err := p.columnList()
		if err != nil {
			return c, false, err
		}
		c.Columns = cols
		if !p.acceptWord("references") {
			return c, false, errAt(p.peek().line, "expected REFERENCES, got %s", describe(p.peek()))
		}
		if err := p.parseReferences(&c, d); err != nil {
			return c, false, err
		}
	case p.acceptWord("check"):
		c.Kind = constraintCheck
		d.warnf(line, "%s: check constraint is not imported", label)
		if err := p.skipBalancedParens(); err != nil {
			return c, false, err
		}
		kept = false
	case p.acceptWord("exclude"):
		c.Kind = constraintExclude
		d.warnf(line, "%s: exclusion constraint is not imported", label)
		p.skipElement()
		kept = false
	default:
		return c, false, errAt(p.peek().line, "expected a constraint, got %s", describe(p.peek()))
	}

	// Trailing flags apply to every constraint kind and none of them can be
	// written to a design document.
	warned := false
	for {
		if p.acceptWords("not", "valid") ||
			p.acceptWord("deferrable") ||
			p.acceptWords("not", "deferrable") ||
			p.acceptWords("initially", "deferred") ||
			p.acceptWords("initially", "immediate") ||
			p.acceptWords("no", "inherit") {
			if !warned && kept {
				d.warnf(line, "%s: DEFERRABLE and NOT VALID flags are not imported", label)
				warned = true
			}
			continue
		}
		break
	}
	return c, kept, nil
}

// parseReferences reads the target of a foreign key and its referential
// actions. The cursor is on the token after REFERENCES.
func (p *parser) parseReferences(c *pgConstraint, d *diagList) error {
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
func (p *parser) parseReferentialActions(c *pgConstraint, d *diagList) error {
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
			action, err := p.referentialAction(d, label)
			if err != nil {
				return err
			}
			c.OnDelete = action
		case p.acceptWords("on", "update"):
			action, err := p.referentialAction(d, label)
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
func (p *parser) referentialAction(d *diagList, label string) (string, error) {
	line := p.peek().line
	switch {
	case p.acceptWord("cascade"):
		return "CASCADE", nil
	case p.acceptWord("restrict"):
		return "RESTRICT", nil
	case p.acceptWords("no", "action"):
		return "NO ACTION", nil
	case p.acceptWords("set", "null"):
		return "SET NULL", p.skipActionColumns(d, label, "SET NULL")
	case p.acceptWords("set", "default"):
		return "SET DEFAULT", p.skipActionColumns(d, label, "SET DEFAULT")
	}
	return "", errAt(line, "expected a referential action, got %s", describe(p.peek()))
}

// skipActionColumns consumes the optional column list of SET NULL / SET DEFAULT,
// which narrows the action to some of the key's columns and has no place in a
// design document.
func (p *parser) skipActionColumns(d *diagList, label, action string) error {
	if !p.peek().isPunct("(") {
		return nil
	}
	d.warnf(p.peek().line, "%s: column list on %s is not imported", label, action)
	return p.skipBalancedParens()
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
// CREATE INDEX
// ---------------------------------------------------------------------------

// parseCreateIndex reads an index definition.
//
// An index over an expression is dropped whole rather than in part: importing
// only the plain columns of "USING btree (a, lower(b))" would describe an index
// that does not exist.
func (p *parser) parseCreateIndex(out *pgDump, d *diagList) error {
	line := p.peek().line
	p.acceptWord("create")
	idx := pgIndex{Unique: p.acceptWord("unique"), Method: "btree", Line: line}
	if !p.acceptWord("index") {
		return errAt(p.peek().line, "expected INDEX, got %s", describe(p.peek()))
	}
	p.acceptWord("concurrently")
	p.acceptWords("if", "not", "exists")
	if !p.atWord("on") {
		name, err := p.identifier()
		if err != nil {
			return err
		}
		idx.Name = name
	}
	if !p.acceptWord("on") {
		return errAt(p.peek().line, "expected ON in CREATE INDEX, got %s", describe(p.peek()))
	}
	p.acceptWord("only")
	table, err := p.qualifiedName()
	if err != nil {
		return err
	}
	idx.Table = table
	if p.acceptWord("using") {
		method, err := p.identifier()
		if err != nil {
			return err
		}
		idx.Method = method
	}
	label := "index" + labelOf(idx.Name) + " on table " + table.String()

	if err := p.expectPunct("("); err != nil {
		return err
	}
	expression, ordered := false, false
	for {
		col, ok, err := p.parseIndexElement(&ordered)
		if err != nil {
			return err
		}
		if ok {
			idx.Columns = append(idx.Columns, col)
		} else {
			expression = true
		}
		if p.acceptPunct(",") {
			continue
		}
		if err := p.expectPunct(")"); err != nil {
			return err
		}
		break
	}
	if ordered {
		d.warnf(line, "%s: column ordering options are not imported", label)
	}

	// Everything after the column list is either storage detail or a narrowing
	// of what the index covers. The second kind has to be reported.
	for !p.done() {
		at := p.peek().line
		switch {
		case p.acceptWord("include"):
			d.warnf(at, "%s: INCLUDE columns are not imported", label)
			if err := p.skipBalancedParens(); err != nil {
				return err
			}
		case p.acceptWord("where"):
			d.warnf(at, "%s: partial index predicate is not imported", label)
			return finishIndex(out, d, idx, expression, label)
		// NULLS NOT DISTINCT before NULLS DISTINCT, or the shorter phrase
		// would take the NULLS of the longer one. The constraint path already
		// reports this clause; pg_dump writes both spellings for one table, so
		// reporting it in one place and not the other made the same fact
		// visible or not depending on which spelling it chose.
		case p.acceptWords("nulls", "not", "distinct"):
			d.warnf(at, "%s: NULLS NOT DISTINCT is not imported", label)
		case p.acceptWords("nulls", "distinct"):
			// PostgreSQL's default. Writing it says what the document already
			// says, so there is nothing to report.
		case p.acceptWord("with"):
			if err := p.skipBalancedParens(); err != nil {
				return err
			}
		default:
			p.next()
		}
	}
	return finishIndex(out, d, idx, expression, label)
}

// finishIndex records an index, or explains why it was dropped.
func finishIndex(out *pgDump, d *diagList, idx pgIndex, expression bool, label string) error {
	if expression {
		d.warnf(idx.Line, "%s: expression index is not imported", label)
		return nil
	}
	if idx.Method != "btree" {
		d.warnf(idx.Line, "%s: access method %s is not imported; recorded as a plain index", label, idx.Method)
	}
	out.Indexes = append(out.Indexes, idx)
	return nil
}

// parseIndexElement reads one index element. ok is false when the element is an
// expression rather than a bare column.
func (p *parser) parseIndexElement(ordered *bool) (string, bool, error) {
	tok := p.peek()
	if tok.kind != kindWord && tok.kind != kindQuotedIdent {
		p.skipElement()
		return "", false, nil
	}
	// A name followed by "(" is a function call, and one followed by "." or "::"
	// belongs to a larger expression.
	if p.punctAt(1, "(") || p.punctAt(1, ".") || p.punctAt(1, "::") {
		p.skipElement()
		return "", false, nil
	}
	name, err := p.identifier()
	if err != nil {
		return "", false, err
	}

	for !p.done() && !p.peek().isPunct(",") && !p.peek().isPunct(")") {
		switch {
		case p.acceptWord("asc"):
			// Already the default order.
		case p.acceptWord("desc"):
			*ordered = true
		case p.acceptWords("nulls", "first"), p.acceptWords("nulls", "last"):
			*ordered = true
		case p.acceptWord("collate"):
			*ordered = true
			if _, err := p.qualifiedName(); err != nil {
				return "", false, err
			}
		case p.peek().kind == kindWord || p.peek().kind == kindQuotedIdent:
			// An operator class name. It changes what the index can answer, so
			// it counts as an option that was dropped.
			*ordered = true
			p.next()
		default:
			// Anything else means this was not a plain column after all.
			p.skipElement()
			return "", false, nil
		}
	}
	return name, true, nil
}

// ---------------------------------------------------------------------------
// ALTER TABLE
// ---------------------------------------------------------------------------

// parseAlterTable reads the actions of an ALTER TABLE statement. pg_dump writes
// one action per statement, but the grammar allows a comma-separated list and a
// concatenated dump may contain one.
func (p *parser) parseAlterTable(src []byte, out *pgDump, d *diagList) error {
	p.acceptWord("alter")
	p.acceptWord("table")
	for p.acceptWord("only") || p.acceptWords("if", "exists") {
	}
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
// An action this package does not map is skipped without complaint - OWNER TO,
// ENABLE TRIGGER, CLUSTER ON, REPLICA IDENTITY and the rest say nothing about
// the design - and so is an action it has never heard of, which is what keeps a
// dump from a future PostgreSQL importable.
func (p *parser) parseAlterTableAction(src []byte, table qname, out *pgDump, d *diagList) error {
	line := p.peek().line
	switch {
	case p.acceptWord("add"):
		if isConstraintStart(p) {
			c, ok, err := p.parseTableConstraint(table, d)
			if err != nil {
				return err
			}
			if ok {
				out.Constraints = append(out.Constraints, c)
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

	case p.acceptWord("alter"):
		p.acceptWord("column")
		column, err := p.identifier()
		if err != nil {
			return err
		}
		return p.parseAlterColumnAction(src, table, column, line, out)

	default:
		p.skipElement()
		return nil
	}
}

// parseAlterColumnAction reads one ALTER COLUMN action. Only the four that
// change what a design document would say are recorded.
func (p *parser) parseAlterColumnAction(src []byte, table qname, column string, line int, out *pgDump) error {
	switch {
	case p.acceptWords("set", "default"):
		out.Defaults = append(out.Defaults, pgDefault{
			Table: table, Column: column, Expr: p.exprText(src, stopAtComma), Line: line,
		})
	case p.acceptWords("set", "not", "null"):
		out.NotNulls = append(out.NotNulls, pgNotNull{Table: table, Column: column, Line: line})
	case p.acceptWords("drop", "not", "null"):
		out.NotNulls = append(out.NotNulls, pgNotNull{Table: table, Column: column, Drop: true, Line: line})
	case p.acceptWords("add", "generated"):
		if !p.acceptWord("always") {
			p.acceptWords("by", "default")
		}
		p.acceptWords("as", "identity")
		if p.peek().isPunct("(") {
			if err := p.skipBalancedParens(); err != nil {
				return err
			}
		}
		out.Identities = append(out.Identities, pgIdentity{Table: table, Column: column, Line: line})
	default:
		p.skipElement()
	}
	return nil
}

// ---------------------------------------------------------------------------
// COMMENT ON
// ---------------------------------------------------------------------------

// parseCommentOn reads a comment on a table or a column. Comments on anything
// else - a schema, a function, an index, a constraint - are skipped silently,
// because a design document has nowhere to put them.
func (p *parser) parseCommentOn(out *pgDump, d *diagList) error {
	line := p.peek().line
	p.acceptWord("comment")
	p.acceptWord("on")

	switch {
	case p.acceptWord("table"):
		table, err := p.qualifiedName()
		if err != nil {
			return err
		}
		text, ok, err := p.commentText()
		if err != nil || !ok {
			return err
		}
		out.Comments = append(out.Comments, pgComment{
			Target: commentTable, Table: table, Text: text, Line: line,
		})
	case p.acceptWord("column"):
		parts, at, err := p.dottedName()
		if err != nil {
			return err
		}
		if len(parts) < 2 || len(parts) > 3 {
			return errAt(at, "expected [schema.]table.column after COMMENT ON COLUMN, got %s", strings.Join(parts, "."))
		}
		table := qname{Name: parts[len(parts)-2], Line: at}
		if len(parts) == 3 {
			table.Schema = parts[0]
		}
		text, ok, err := p.commentText()
		if err != nil || !ok {
			return err
		}
		out.Comments = append(out.Comments, pgComment{
			Target: commentColumn, Table: table, Column: parts[len(parts)-1], Text: text, Line: line,
		})
	}
	return nil
}

// commentText reads "IS <string>". ok is false for "IS NULL", which removes a
// comment rather than setting one.
func (p *parser) commentText() (string, bool, error) {
	if !p.acceptWord("is") {
		return "", false, errAt(p.peek().line, "expected IS in COMMENT ON, got %s", describe(p.peek()))
	}
	if p.acceptWord("null") {
		return "", false, nil
	}
	tok := p.peek()
	if tok.kind != kindString {
		return "", false, errAt(tok.line, "expected a string after IS, got %s", describe(tok))
	}
	p.i++
	return tok.text, true, nil
}

// ---------------------------------------------------------------------------
// Sequences
// ---------------------------------------------------------------------------

// parseCreateSequence records a sequence by name. Its options say nothing a
// design document can hold; the sequence matters only because a column whose
// default is nextval() of it auto-increments.
func (p *parser) parseCreateSequence(out *pgDump) error {
	line := p.peek().line
	p.acceptWord("create")
	p.acceptWord("sequence")
	p.acceptWords("if", "not", "exists")
	name, err := p.qualifiedName()
	if err != nil {
		return err
	}
	out.Sequences = append(out.Sequences, pgSequence{Name: name, Line: line})
	return nil
}

// parseAlterSequence records OWNED BY, which is how pg_dump ties the sequence of
// a serial column back to that column. Every other action is skipped.
func (p *parser) parseAlterSequence(out *pgDump) error {
	line := p.peek().line
	p.acceptWord("alter")
	p.acceptWord("sequence")
	p.acceptWords("if", "exists")
	name, err := p.qualifiedName()
	if err != nil {
		return err
	}

	for !p.done() {
		if !p.acceptWords("owned", "by") {
			p.next()
			continue
		}
		if p.acceptWord("none") {
			return nil
		}
		parts, at, err := p.dottedName()
		if err != nil {
			return err
		}
		if len(parts) < 2 || len(parts) > 3 {
			return errAt(at, "expected [schema.]table.column after OWNED BY, got %s", strings.Join(parts, "."))
		}
		owner := qname{Name: parts[len(parts)-2], Line: at}
		if len(parts) == 3 {
			owner.Schema = parts[0]
		}
		for i := range out.Sequences {
			if out.Sequences[i].Name.same(name) {
				out.Sequences[i].OwnedTable = owner
				out.Sequences[i].OwnedColumn = parts[len(parts)-1]
				return nil
			}
		}
		// A dump that alters a sequence it never created is still usable.
		out.Sequences = append(out.Sequences, pgSequence{
			Name: name, OwnedTable: owner, OwnedColumn: parts[len(parts)-1], Line: line,
		})
		return nil
	}
	return nil
}
