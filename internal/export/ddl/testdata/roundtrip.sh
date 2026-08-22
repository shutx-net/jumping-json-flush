#!/bin/sh
#
# Take each design document twice around the DDL round trip, against one live
# PostgreSQL server.
#
#   document.json -> jjf export ddl -> PostgreSQL -> pg_dump --schema-only
#                 -> jjf import -> document.json  (pass 1)
#                 -> jjf export ddl -> PostgreSQL -> pg_dump --schema-only
#                 -> jjf import -> document.json  (pass 2)
#
# This is a developer tool. No test runs it: "go test ./..." needs neither a
# database nor a network, which is the same arrangement
# internal/importer/postgres/testdata/generate.sh describes in its own header.
# .github/workflows/pg-fixtures.yml is what runs this with nobody watching, one
# PostgreSQL major per matrix leg.
#
# It exists because golden files cannot say what matters most. testdata/golden/
# proves that the generator emits what it emitted; nothing in it says the SQL
# executes, and nothing says the importer reads back what the generator wrote.
# A database is the only oracle for either.
#
# THE ASSERTION is that pass 2 equals pass 1, byte for byte, as documents. It is
# deliberately NOT that pass 1 equals the input document, because PostgreSQL
# rewrites parts of a design on the way in and pg_dump then renders what it
# stored, not what was written:
#
#   * a hand-written quoted literal acquires an explicit cast - 'pending'
#     becomes 'pending'::text, 1e3 becomes '1000'::numeric, INTERVAL '1 day'
#     becomes '1 day'::interval, E'a\b' has its escape resolved
#   * DEFAULT NULL disappears, because PostgreSQL treats it as no default
#   * ON UPDATE NO ACTION and ON DELETE NO ACTION disappear, because they are
#     PostgreSQL's own default - docs/usage.md says so already
#   * a nullable primary key column comes back as NOT NULL
#
# All of that is PostgreSQL's behaviour and not jjf's, it has all happened by
# the end of pass 1, and freezing it would make a PostgreSQL release a jjf
# failure with no jjf change to make. Anything that moves between pass 1 and
# pass 2 is something else entirely: the generator and the importer disagreeing
# with each other, which is a defect and is what this script is for. The
# document-against-pass-1 comparison is still written out, as a report - see
# <name>.input.diff below - because it is the one place where "what does this
# PostgreSQL major do to a design document" is visible at all.
#
# The comparison is at the document level, never at the SQL level: pg_dump
# writes a random token into its "\restrict" / "\unrestrict" lines, so two dumps
# of the same schema never match byte for byte.
#
# Usage:
#   sh roundtrip.sh              round trip full.json, edge.json and minimal.json
#   sh roundtrip.sh edge.json    round trip the named documents only
#
# A bare name is resolved beside this script; a name containing a "/" is used as
# given, so a document from anywhere can be round tripped.
#
# Environment:
#   PGBIN   directory holding initdb / pg_ctl / createdb / psql / pg_dump. Unset,
#           the single /usr/lib/postgresql/*/bin that exists is used, and having
#           more than one installed is an error naming them: one run means one
#           server, and running every major is the CI matrix's job
#   PGPORT  port for the scratch cluster (default: 5434, so that a developer's
#           own PostgreSQL on 5432 is never touched and running this beside
#           generate.sh on 5433 needs no thought)
#   OUTDIR  where the working files and the diffs go (default: a fresh directory
#           under /tmp, printed as the first line of output). Created if it does
#           not exist, and NOT deleted afterwards: the diffs are the output
#   JJF     a built jjf binary. Unset, one is built into OUTDIR. A built binary
#           and never "go run", because go run reports its own exit status and
#           this script reads jjf's
#
# What lands in OUTDIR, per document:
#   <name>.pass0.sql        DDL generated from the input document
#   <name>.pass1.dump.sql   pg_dump of the database that DDL built
#   <name>.pass1.json       the document imported from it
#   <name>.pass1.warn       the importer's warnings, recorded and never compared
#   <name>.pass1.sql        DDL generated from the pass 1 document
#   <name>.pass2.dump.sql   pg_dump of the database THAT built
#   <name>.pass2.json       the document imported from it
#   <name>.pass2.warn
#   <name>.fixedpoint.diff  pass 1 against pass 2 - THE GATE, empty when it holds
#   <name>.input.diff       the input document against pass 1 - a report
#   <name>.ddl.diff         the DDL of pass 0 against the DDL of pass 1 - a report
#   result.txt              one "ok <name>" or "FAIL <name>" line per document
#
# The exit status is 0 when every document came round twice unchanged and every
# command on the way succeeded, and 1 otherwise. A failed document does not stop
# the run: one bad document must not hide the state of the others, which is the
# same call "fail-fast: false" makes in the workflow's matrix.
#
# THE DATABASE NAME IS SUPPLIED, NOT PRESERVED. The generator emits no CREATE
# DATABASE and no CREATE SCHEMA, so the name is not in the DDL at all, and
# "jjf import" falls back to the name of the file it read. This script therefore
# passes -database on both imports, with the value read out of the ORIGINAL
# document. Without it the two passes would differ in database.name alone and
# the gate would fail on something the harness itself invented.
#
# A DOCUMENT MAY NEED A PRELUDE. Column types are opaque strings, so a document
# naming a user-defined type produces DDL that references a type no statement in
# it creates - a limitation of the format that design/ddl-export.md states
# plainly and does not intend to close. Where <name>.prelude.sql exists beside
# the document, it is applied to the empty database first, and this script says
# so in its log every time. Only edge.json has one.
#
# CAVEATS:
#   * jq is required: it is what reads database.name out of a document. It is in
#     the nix dev shell and on the CI runner
#   * the PostgreSQL server refuses to run as root, so as root every server
#     command goes through "su postgres", which then has to be able to read
#     OUTDIR and traverse the path above the checkout. As any ordinary user they
#     run directly and the question never comes up, which is what CI does -
#     the same caveat generate.sh carries
#   * no path involved may contain a space, for the same reason: the command is
#     handed to "su postgres -c" as one string
#   * the scratch cluster is deleted however this script ends. OUTDIR is not

set -eu

PGPORT=${PGPORT:-5434}

dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
root=$(CDPATH= cd -- "$dir/../../../.." && pwd)
base=""

if ! command -v jq >/dev/null 2>&1; then
	echo "jq is required: it reads database.name out of each document" >&2
	exit 1
fi

# bin is the one PostgreSQL installation this run speaks to. generate.sh loops
# over every major it finds; this does not, because one run round trips against
# one server and covering the majors is what the workflow matrix is for.
if [ -n "${PGBIN:-}" ]; then
	bin=$PGBIN
else
	bin=""
	found=""
	count=0
	for candidate in /usr/lib/postgresql/*/bin; do
		[ -x "$candidate/initdb" ] || continue
		count=$((count + 1))
		bin=$candidate
		found="$found $candidate"
	done
	if [ "$count" -eq 0 ]; then
		echo "no PostgreSQL binaries found under /usr/lib/postgresql/*/bin; set PGBIN" >&2
		exit 1
	fi
	if [ "$count" -gt 1 ]; then
		echo "more than one PostgreSQL installation found:$found" >&2
		echo "set PGBIN to the one to round trip against" >&2
		exit 1
	fi
fi

for prog in initdb pg_ctl createdb psql pg_dump; do
	if [ ! -x "$bin/$prog" ]; then
		echo "$bin/$prog is not executable; set PGBIN to a directory holding initdb, pg_ctl, createdb, psql and pg_dump" >&2
		exit 1
	fi
done

if [ -n "${OUTDIR:-}" ]; then
	mkdir -p "$OUTDIR"
	OUTDIR=$(CDPATH= cd -- "$OUTDIR" && pwd)
else
	OUTDIR=$(mktemp -d /tmp/jjf-roundtrip.XXXXXX)
fi
# mktemp makes a directory only its owner can enter. As root that owner is root
# and the server user is postgres, which is then asked to read the very files
# this script generates, so the door has to be opened. As an ordinary user the
# files are already the invoking user's own and nothing here applies.
if [ "$(id -u)" = 0 ]; then
	chmod a+rx "$OUTDIR"
fi
echo "output: $OUTDIR"

if [ -z "${JJF:-}" ]; then
	if ! command -v go >/dev/null 2>&1; then
		echo "go is required to build jjf; set JJF to a built binary instead" >&2
		exit 1
	fi
	JJF=$OUTDIR/jjf
	(cd "$root" && go build -o "$JJF" ./cmd/jjf)
fi

# The default list is the one internal/export/ddl/golden_test.go calls fixtures,
# in its order, so that a round trip failure lands next to the golden file it
# contradicts. Its comment says why those three: between them they hold every
# shape the generator has to survive.
if [ $# -eq 0 ]; then
	set -- full.json edge.json minimal.json
fi

# cleanup stops the server and removes the scratch cluster, however this script
# ends. OUTDIR survives on purpose: a non-zero exit with nothing left to read
# would be useless.
cleanup() {
	[ -n "$base" ] || return 0
	if [ -d "$base/data" ]; then
		as_postgres "$bin/pg_ctl -D $base/data stop -m immediate" >/dev/null 2>&1 || true
	fi
	rm -rf "$base"
	base=""
}
trap cleanup EXIT INT TERM

# as_postgres runs a command as the postgres system user when this script runs
# as root, because the PostgreSQL server refuses to run as root.
as_postgres() {
	if [ "$(id -u)" = 0 ]; then
		su postgres -c "$1"
	else
		sh -c "$1"
	fi
}

# apply creates an empty database and runs one generated script into it.
# ON_ERROR_STOP is what makes "the DDL applies at all" an assertion rather than
# a half-built schema noticed three steps later.
#
# A second, empty database for the second pass, never the first one reused: the
# generated DDL creates a schema from nothing, and applying it over a schema
# that already exists is explicitly out of scope for the generator, so reusing
# one database would test something the tool does not claim.
apply() {
	ap_db=$1
	ap_sql=$2
	ap_prelude=$3

	as_postgres "$bin/createdb -h $base -p $PGPORT -U jjf $ap_db" || return 1
	if [ -f "$ap_prelude" ]; then
		# Named every time, so that nobody can conclude from a green run that
		# the generator emits the type definition itself. It does not, and
		# design/ddl-export.md says it never will.
		echo "    prelude: $ap_prelude"
		as_postgres "$bin/psql -h $base -p $PGPORT -U jjf -d $ap_db -v ON_ERROR_STOP=1 -q -f $ap_prelude" || return 1
	fi
	as_postgres "$bin/psql -h $base -p $PGPORT -U jjf -d $ap_db -v ON_ERROR_STOP=1 -q -f $ap_sql" || return 1
}

# roundtrip takes one document round twice and returns non-zero when any step
# failed or the two passes disagree. It is called from a "|| status=$?" list,
# where set -e does not apply, so every step carries its own check.
roundtrip() {
	rt_name=$1
	rt_path=$2

	if [ ! -f "$rt_path" ]; then
		echo "    no such document: $rt_path" >&2
		return 1
	fi

	# The name the document gives itself, passed to both imports. See THE
	# DATABASE NAME IS SUPPLIED, NOT PRESERVED in the header.
	rt_db=$(jq -r '.database.name // empty' "$rt_path")
	if [ -z "$rt_db" ]; then
		echo "    $rt_path names no database.name; -database has nothing to carry" >&2
		return 1
	fi
	echo "    document $rt_path, database $rt_db"

	rt_prelude=$dir/$rt_name.prelude.sql
	rt_out=$OUTDIR/$rt_name

	# The dumps are taken WITHOUT --no-owner and --no-privileges on purpose,
	# the same call generate.sh makes and for the same reason: the OWNER TO and
	# GRANT statements are exactly the kind of statement the importer has to
	# skip in silence, so the round trip should contain them.
	"$JJF" export ddl "$rt_path" -o "$rt_out.pass0.sql" || return 1
	apply "${rt_name}_pass1" "$rt_out.pass0.sql" "$rt_prelude" || return 1
	as_postgres "$bin/pg_dump -h $base -p $PGPORT -U jjf --schema-only ${rt_name}_pass1" \
		> "$rt_out.pass1.dump.sql" || return 1
	# No -strict, and the warnings are recorded rather than compared: they carry
	# line numbers, which move with the dump header.
	"$JJF" import postgres "$rt_out.pass1.dump.sql" -database "$rt_db" -o "$rt_out.pass1.json" \
		2> "$rt_out.pass1.warn" || return 1

	"$JJF" export ddl "$rt_out.pass1.json" -o "$rt_out.pass1.sql" || return 1
	apply "${rt_name}_pass2" "$rt_out.pass1.sql" "$rt_prelude" || return 1
	as_postgres "$bin/pg_dump -h $base -p $PGPORT -U jjf --schema-only ${rt_name}_pass2" \
		> "$rt_out.pass2.dump.sql" || return 1
	"$JJF" import postgres "$rt_out.pass2.dump.sql" -database "$rt_db" -o "$rt_out.pass2.json" \
		2> "$rt_out.pass2.warn" || return 1

	# diff exits 1 for "the files differ" and 2 for trouble, so its status is
	# read rather than left to abort the script - the same "|| status=$?" shape
	# ci.yml's expect() uses and explains.
	#
	# The two reports first, so that they exist even when the gate below fails:
	# a divergence is usually read next to what the database did to the
	# document in the first place.
	diff -u --label "$rt_name.json (the document)" --label "pass 1 (what the database made of it)" \
		"$rt_path" "$rt_out.pass1.json" > "$rt_out.input.diff" || true
	diff -u --label "DDL from the document" --label "DDL from pass 1" \
		"$rt_out.pass0.sql" "$rt_out.pass1.sql" > "$rt_out.ddl.diff" || true

	rt_status=0
	diff -u --label "pass 1" --label "pass 2" \
		"$rt_out.pass1.json" "$rt_out.pass2.json" > "$rt_out.fixedpoint.diff" || rt_status=$?
	if [ "$rt_status" -ne 0 ]; then
		cat "$rt_out.fixedpoint.diff"
		return 1
	fi
	return 0
}

# The scratch directory has to live somewhere the postgres user can traverse,
# which is why it is under /tmp and not under the repository. The flags are
# generate.sh's, verbatim, because the point is to be the same environment:
# --no-locale keeps collation names from varying between machines; -A trust and
# a unix socket mean no password is needed; "-h ''" means the cluster listens on
# no TCP port at all, so neither a developer's own PostgreSQL nor the cluster a
# package manager created can collide with it.
#
# One cluster for the whole run and two databases per document. generate.sh
# cannot be asked for its cluster instead: it stops the server between majors
# and at the end, so by the time anything else runs there is none left.
base=$(mktemp -d /tmp/jjf-roundtrip-cluster.XXXXXX)
chown postgres:postgres "$base" 2>/dev/null || true
chmod 700 "$base"

as_postgres "$bin/initdb -D $base/data -U jjf --no-locale --encoding=UTF8 -A trust" >/dev/null
as_postgres "$bin/pg_ctl -D $base/data -o \"-k $base -h '' -p $PGPORT\" -l $base/server.log start -w" >/dev/null

: > "$OUTDIR/result.txt"
failed=0

for fixture in "$@"; do
	case $fixture in
	*/*) path=$fixture ;;
	*) path=$dir/$fixture ;;
	esac
	name=$(basename "$path" .json)

	echo "==> $name"
	status=0
	roundtrip "$name" "$path" || status=$?
	if [ "$status" -eq 0 ]; then
		echo "ok  $name reaches the same document twice"
		echo "ok $name" >> "$OUTDIR/result.txt"
	else
		echo "FAIL  $name" >&2
		echo "FAIL $name" >> "$OUTDIR/result.txt"
		failed=$((failed + 1))
	fi
done

echo "the working files and the diffs are in $OUTDIR"
if [ "$failed" -ne 0 ]; then
	echo "$failed of $# document(s) did not survive the round trip" >&2
	exit 1
fi
