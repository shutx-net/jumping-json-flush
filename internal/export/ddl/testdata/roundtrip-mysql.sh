#!/bin/sh
#
# Take each MySQL design document twice around the DDL round trip, against one
# live MySQL server.
#
#   document.json -> jjf export ddl -> MySQL -> mysqldump --no-data
#                 -> jjf import mysql -> document.json  (pass 1)
#                 -> jjf export ddl -> MySQL -> mysqldump --no-data
#                 -> jjf import mysql -> document.json  (pass 2)
#
# This is a developer tool. No test runs it: "go test ./..." needs neither a
# database nor a network, which is the same arrangement
# internal/importer/mysql/testdata/generate.sh describes in its own header.
# .github/workflows/mysql-fixtures.yml is what runs this with nobody watching,
# one captured server series per matrix leg.
#
# It is roundtrip.sh's sibling and not a mode of it. The two duplicate the
# two-pass loop and the three diffs rather than sharing a harness, because every
# step that touches a server - obtaining one, creating a database, applying a
# script, taking a dump, naming the dialect on the import - is a step that
# differs, and those are most of the script. One script taking a dialect
# argument would have to explain two servers, two clients, two ways of getting a
# database and two sets of expected drift in one header, and neither half would
# be readable. That is the trade .github/workflows/pg-fixtures.yml already makes
# twice about its own duplicated blocks.
#
# It exists because golden files cannot say what matters most.
# testdata/golden/mysql/ proves that the generator emits what it emitted;
# nothing in it says the SQL executes, and nothing says the importer reads back
# what the generator wrote. A database is the only oracle for either.
#
# THE ASSERTION is that pass 2 equals pass 1, byte for byte, as documents. It is
# deliberately NOT that pass 1 equals the input document, because MySQL rewrites
# parts of a design on the way in and mysqldump then renders what it stored, not
# what was written:
#
#   * a column written BOOLEAN comes back as TINYINT with a length of 1. MySQL
#     has no boolean type: BOOLEAN is a spelling of TINYINT(1), and that is what
#     it stores and what mysqldump writes
#   * an index appears behind every foreign key whose columns are not already
#     the leading columns of a key the table has, because InnoDB requires one
#     and creates it, named after its first column
#   * a named PRIMARY KEY loses its name. MySQL calls every primary key PRIMARY
#     and keeps no other, so nothing is left for the importer to record
#   * an unnamed UNIQUE key comes back carrying the name MySQL gave it, which is
#     its first column's, and a unique INDEX comes back as a unique KEY: MySQL
#     does not distinguish the two, so one of the fixtures' unique indexes lands
#     in the table rather than in the index phase
#   * an unnamed FOREIGN KEY comes back as <table>_ibfk_<n>, InnoDB's own name
#     for one
#   * ON UPDATE NO ACTION and ON DELETE NO ACTION disappear, exactly as they do
#     on the PostgreSQL side and for the same reason: NO ACTION is the default,
#     so nothing is stored and nothing is dumped
#   * DEFAULT NULL disappears, because MySQL treats it as no default at all
#   * a numeric default comes back quoted - DEFAULT 0 as DEFAULT '0' - an
#     arithmetic one comes back doubly parenthesised - DEFAULT (1 + 2) as
#     DEFAULT ((1 + 2)) - and a function name in one comes back lowercased,
#     because MySQL stores the expression and renders its own parse of it
#   * a type is rewritten to what the server stores and jjf then spells it its
#     own way: DECIMAL(8) comes back DECIMAL(8,0), FLOAT(24) comes back FLOAT,
#     INT comes back INTEGER
#   * the tables come back in the server's order rather than the document's, and
#     database.logicalName is gone, because no CREATE DATABASE is generated for
#     a comment to attach to
#
# Table options are the one thing on that list which does NOT reach the
# document: the dump carries ENGINE, the default character set, the collation
# and the row format, the design format has nowhere to hold any of them, and the
# importer drops them. They are a limitation rather than a divergence, which is
# why they show up in <name>.pass1.dump.sql and in nothing this script compares.
#
# All of that is MySQL's behaviour and not jjf's, it has all happened by the end
# of pass 1, and freezing it would make a MySQL release a jjf failure with no
# jjf change to make. Anything that moves between pass 1 and pass 2 is something
# else entirely: the generator and the importer disagreeing with each other,
# which is a defect and is what this script is for. The document-against-pass-1
# comparison is still written out, as a report - see <name>.input.diff below -
# because it is the one place where "what does this MySQL series do to a design
# document" is visible at all.
#
# The comparison is at the document level, never at the SQL level. Two dumps of
# the same schema do match byte for byte here, unlike pg_dump's, but comparing
# them would gate on mysqldump's rendering rather than on what jjf makes of it,
# and the banner lines move with every patch release of the server.
#
# Usage:
#   sh roundtrip-mysql.sh              round trip full.json, edge.json and minimal.json
#   sh roundtrip-mysql.sh edge.json    round trip the named documents only
#
# A bare name is resolved in mysql/ beside this script, which is where the MySQL
# fixtures live - testdata/ itself holds the PostgreSQL ones. A name containing
# a "/" is used as given, so a document from anywhere can be round tripped.
#
# Environment:
#   MYSQL_HOST      server to connect to (default: 127.0.0.1)
#   MYSQL_PORT      port to connect to (default: 3306)
#   MYSQL_SOCKET    unix socket to connect through instead of the host and port
#                   (default: empty, meaning connect over TCP)
#   MYSQL_USER      account to connect as (default: root)
#   MYSQL_PASSWORD  its password (default: empty, which is what a throwaway
#                   container is normally started with)
#   MYSQL           the client command (default: mysql)
#   MYSQLDUMP       the dump command (default: mysqldump)
#   OUTDIR          where the working files and the diffs go (default: a fresh
#                   directory under /tmp, printed as the first line of output).
#                   Created if it does not exist, and NOT deleted afterwards:
#                   the diffs are the output
#   JJF             a built jjf binary. Unset, one is built into OUTDIR. A built
#                   binary and never "go run", because go run reports its own
#                   exit status and this script reads jjf's
#
# MYSQL_SOCKET is there because a distribution package normally authenticates
# its root account with auth_socket, which answers a TCP connection with
# "Access denied" no matter what password is offered. A container publishes a
# port and wants the TCP path; a locally installed server wants this one. MYSQL
# and MYSQLDUMP exist because mysqldump's output shape depends on the CLIENT
# version, so a round trip is only meaningful when the client matches the server
# it dumped; naming the commands lets CI point them at wrappers that exec into
# the server's own container. This script does not learn what Docker is, and
# neither does generate.sh, whose header carries the same two paragraphs.
#
# --default-character-set=utf8mb4 IS NOT OPTIONAL, on either command, and this
# script is the reason the flag cannot be left to a call site. Without it a
# client may negotiate latin1 and every Japanese logicalName and COMMENT is then
# encoded twice - and because that corruption is STABLE, pass 2 would compare
# mojibake against mojibake and this script would go green on a round trip that
# destroyed the text. The assertion cannot catch it. The flag is the only
# defence, which is why it lives in the two wrapper functions below and nowhere
# else.
#
# THE SERVER IS NOT STARTED BY THIS SCRIPT. It connects to one it was pointed
# at, because there is no portable MySQL equivalent of PostgreSQL's initdb plus
# pg_ctl: "mysqld --initialize-insecure" differs between packagings and needs a
# data directory layout the script would have to know about, and in CI the
# server is a container anyway. Getting one is the same two commands
# generate.sh's header lists:
#
#   docker run --rm -d --name jjf-mysql -e MYSQL_ALLOW_EMPTY_PASSWORD=1 \
#           -p 3306:3306 mysql:8.0
#
# or "apt-get install -y mysql-server", whose root account wants MYSQL_SOCKET.
#
# IT DROPS DATABASES ON A SERVER IT DOES NOT OWN. Each document gets two of
# them, <name>_pass1 and <name>_pass2, each dropped if it exists, created empty,
# and dropped again however this script ends. Do not point it at a server
# holding anything you want to keep.
#
# THE DATABASE NAME IS SUPPLIED, NOT PRESERVED. The generator emits no CREATE
# DATABASE and no USE, so the name is not in the DDL at all, and the dump of
# <name>_pass1 announces <name>_pass1 in its own header banner, which is where
# "jjf import mysql" reads a name from when it is given none. This script
# therefore passes -database on both imports, with the value read out of the
# ORIGINAL document. Without it the two passes would differ in database.name
# alone and the gate would fail on something the harness itself invented.
#
# NO PRELUDE IS NEEDED, and none should be added. roundtrip.sh applies
# edge.prelude.sql because its edge.json names a PostgreSQL user-defined type
# that no generated statement creates. MySQL has no CREATE TYPE, so the
# equivalent gap - an ENUM or a SET, whose value list the design format has
# nowhere to put - cannot be closed by a prelude at all. It is closed by the
# fixture not using one, and saying so here is what stops anyone building a
# prelude mechanism for a document that could never be helped by it.
#
# CAVEATS:
#   * jq is required: it is what reads database.name out of a document. It is in
#     the nix dev shell and on the CI runner
#   * no path involved may contain a space, and no document name may, because
#     the name becomes both a database name and a filename prefix
#   * the two databases per document are dropped however this script ends.
#     OUTDIR is not

set -eu

MYSQL_HOST=${MYSQL_HOST:-127.0.0.1}
MYSQL_PORT=${MYSQL_PORT:-3306}
MYSQL_SOCKET=${MYSQL_SOCKET:-}
MYSQL_USER=${MYSQL_USER:-root}
MYSQL_PASSWORD=${MYSQL_PASSWORD:-}
MYSQL=${MYSQL:-mysql}
MYSQLDUMP=${MYSQLDUMP:-mysqldump}

dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
root=$(CDPATH= cd -- "$dir/../../../.." && pwd)
created=""

if ! command -v jq >/dev/null 2>&1; then
	echo "jq is required: it reads database.name out of each document" >&2
	exit 1
fi

# Both commands are checked here rather than left to the connection probe below,
# which would report a missing client as a server that cannot be reached. That
# distinction matters most where the two are wrapper scripts pointing into a
# container, which is what CI does: a wrapper that is not executable and a
# container that is not running fail in the same place otherwise.
for prog in "$MYSQL" "$MYSQLDUMP"; do
	if ! command -v "$prog" >/dev/null 2>&1; then
		echo "$prog is not executable; set MYSQL and MYSQLDUMP to the client and the dumper" >&2
		exit 1
	fi
done

# The password is passed through the environment rather than on the command
# line, because an argument is visible to every process on the machine. An empty
# value is left unset, since MYSQL_PWD="" would still count as "a password was
# given" and fail against a server that wants none. generate.sh says the same
# thing beside the same three lines.
if [ -n "$MYSQL_PASSWORD" ]; then
	MYSQL_PWD=$MYSQL_PASSWORD
	export MYSQL_PWD
fi

# client and dumper wrap the two commands with the connection arguments, so that
# --default-character-set is written once and cannot be forgotten at a call
# site. See the header: forgetting it is the one failure this script cannot
# detect.
#
# The branch is written out in both rather than collected into one variable that
# is then split on whitespace: a socket path may contain a space, and a
# developer script that mangles one would be a puzzle to debug.
client() {
	if [ -n "$MYSQL_SOCKET" ]; then
		"$MYSQL" --socket="$MYSQL_SOCKET" --user="$MYSQL_USER" \
			--default-character-set=utf8mb4 "$@"
		return
	fi
	"$MYSQL" --host="$MYSQL_HOST" --port="$MYSQL_PORT" --user="$MYSQL_USER" \
		--protocol=TCP --default-character-set=utf8mb4 "$@"
}

dumper() {
	if [ -n "$MYSQL_SOCKET" ]; then
		"$MYSQLDUMP" --socket="$MYSQL_SOCKET" --user="$MYSQL_USER" \
			--default-character-set=utf8mb4 "$@"
		return
	fi
	"$MYSQLDUMP" --host="$MYSQL_HOST" --port="$MYSQL_PORT" --user="$MYSQL_USER" \
		--protocol=TCP --default-character-set=utf8mb4 "$@"
}

# where names the server for a failure message.
where=$MYSQL_HOST:$MYSQL_PORT
if [ -n "$MYSQL_SOCKET" ]; then
	where=$MYSQL_SOCKET
fi

if ! client --execute="SELECT 1" >/dev/null 2>&1; then
	echo "cannot reach a MySQL server at $where as $MYSQL_USER; see the header of $0" >&2
	exit 1
fi

if [ -n "${OUTDIR:-}" ]; then
	mkdir -p "$OUTDIR"
	OUTDIR=$(CDPATH= cd -- "$OUTDIR" && pwd)
else
	OUTDIR=$(mktemp -d /tmp/jjf-roundtrip-mysql.XXXXXX)
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
# contradicts. refused.json is not in it and never can be: it exists to be
# refused, so there is no DDL to apply.
if [ $# -eq 0 ]; then
	set -- full.json edge.json minimal.json
fi

# cleanup drops every database this run created, however this script ends. It is
# the one thing left on a machine the script does not own, so it is removed even
# after a failure; OUTDIR survives on purpose, because a non-zero exit with
# nothing left to read would be useless.
#
# The names are collected in a whitespace-separated list and split here on
# purpose - see the caveat about spaces in the header. Each drop is allowed to
# fail: a run that died before creating its second database must still drop its
# first.
cleanup() {
	[ -n "$created" ] || return 0
	for db in $created; do
		client --execute="DROP DATABASE IF EXISTS \`$db\`" >/dev/null 2>&1 || true
	done
	created=""
}
trap cleanup EXIT INT TERM

# apply creates an empty database and runs one generated script into it.
#
# The mysql client in batch mode stops at the first error and exits non-zero,
# which is what makes "the DDL applies at all" an assertion rather than a
# half-built schema noticed three steps later. There is no ON_ERROR_STOP to pass
# and none is needed: --force is what would turn the assertion off.
#
# A second, empty database for the second pass, never the first one reused: the
# generated DDL creates a schema from nothing, and applying it over a schema
# that already exists is explicitly out of scope for the generator, so reusing
# one database would test something the tool does not claim.
#
# The SQL is fed on stdin rather than passed as a path, so that the same script
# works when MYSQL is a wrapper that execs into a container and cannot see this
# checkout.
apply() {
	ap_db=$1
	ap_sql=$2

	created="$created $ap_db"
	client --execute="DROP DATABASE IF EXISTS \`$ap_db\`; CREATE DATABASE \`$ap_db\` DEFAULT CHARACTER SET utf8mb4" || return 1
	client "$ap_db" < "$ap_sql" || return 1
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

	rt_out=$OUTDIR/$rt_name

	# The dumps are taken WITHOUT --no-tablespaces and WITHOUT --skip-triggers,
	# the same call generate.sh makes and for the same reason: the executable
	# comments and the per-table character_set_client dance those suppress are
	# exactly what the importer has to step over in silence, so the round trip
	# should contain them. --skip-dump-date removes the one line that would
	# otherwise differ between the two passes for no reason at all.
	"$JJF" export ddl "$rt_path" -o "$rt_out.pass0.sql" || return 1
	apply "${rt_name}_pass1" "$rt_out.pass0.sql" || return 1
	dumper --no-data --skip-dump-date "${rt_name}_pass1" > "$rt_out.pass1.dump.sql" || return 1
	# No -strict, and the warnings are recorded rather than compared: they carry
	# line numbers, which move with the dump header.
	"$JJF" import mysql "$rt_out.pass1.dump.sql" -database "$rt_db" -o "$rt_out.pass1.json" \
		2> "$rt_out.pass1.warn" || return 1

	"$JJF" export ddl "$rt_out.pass1.json" -o "$rt_out.pass1.sql" || return 1
	apply "${rt_name}_pass2" "$rt_out.pass1.sql" || return 1
	dumper --no-data --skip-dump-date "${rt_name}_pass2" > "$rt_out.pass2.dump.sql" || return 1
	"$JJF" import mysql "$rt_out.pass2.dump.sql" -database "$rt_db" -o "$rt_out.pass2.json" \
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

: > "$OUTDIR/result.txt"
failed=0

for fixture in "$@"; do
	case $fixture in
	*/*) path=$fixture ;;
	*) path=$dir/mysql/$fixture ;;
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
