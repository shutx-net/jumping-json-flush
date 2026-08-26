#!/bin/sh
#
# Regenerate the committed mysqldump fixtures under testdata/dump/.
#
# This is a developer tool. No test runs it: the dumps it produces are committed
# so that the test suite needs neither a database nor a network.
#
# For every file in testdata/source/, it drops and recreates a database of that
# name on the server it was pointed at, feeds the file in, dumps the result with
# "mysqldump --no-data", and writes it into testdata/dump/mysql<series>/, where
# <series> is the two-part release series the dump's own banner reports.
#
# The series is the point. The importer claims to read dumps from a range of
# MySQL servers, and fixture_test.go's TestImportAgreesAcrossMysqldumpSeries
# holds every capture in that range to the same golden document. It is a
# directory per SERIES rather than per MAJOR, which is where this differs from
# its PostgreSQL sibling: 8.0 and 8.4 are different release series of one major
# and their mysqldump output differs, so a directory per major would hide the
# very difference the captures exist to expose.
#
# Usage:
#   sh generate.sh            regenerate every fixture
#   sh generate.sh ecshop     regenerate the ecshop fixture only
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
#
# MYSQL_SOCKET is there because a distribution package normally authenticates
# its root account with auth_socket, which answers a TCP connection with
# "Access denied" no matter what password is offered. A container publishes a
# port and wants the TCP path; a locally installed server wants this one.
#
# The last two exist because mysqldump's output shape depends on the CLIENT
# version, so a capture is only meaningful when the client matches the server it
# dumped. Naming the commands lets CI point them at wrappers that exec into the
# server's own container, which makes client and server the same build by
# construction. Neither this script nor the round-trip script learns what Docker
# is.
#
# GETTING A SERVER. Either a container:
#
#   docker run --rm -d --name jjf-mysql -e MYSQL_ALLOW_EMPTY_PASSWORD=1 \
#           -p 3306:3306 mysql:8.0
#
# or a distribution package - "apt-get install -y mysql-server" on Debian and
# Ubuntu, which starts a server on the unix socket that the client finds by
# default. There is no portable MySQL equivalent of PostgreSQL's initdb plus
# pg_ctl, which is why this script connects to a server it did not start:
# "mysqld --initialize-insecure" differs between packagings and needs a data
# directory layout the script would have to know about.
#
# IT DROPS DATABASES. Every fixture name under testdata/source/ is a database
# this script deletes and recreates on the server it was pointed at. Do not run
# it against a server holding anything you want to keep.
#
# --default-character-set=utf8mb4 IS NOT OPTIONAL, on either command. Without it
# a client may negotiate latin1, and every Japanese logicalName and COMMENT in
# the fixtures is then encoded twice. Such a dump still lexes, still parses,
# still imports and still round-trips, so no test in this repository would ever
# catch it: the only symptom is mojibake in a golden nobody reads closely.
#
# testdata/dump/synthetic/ is NOT generated. Those files are hand-written, say
# so in their own headers, and exist to cover shapes no installed mysqldump
# produces.
#
# The dumps are taken WITHOUT --no-tablespaces and WITHOUT --skip-triggers on
# purpose: the executable comments, the per-table character_set_client dance,
# the doubled view definition and the DELIMITER-wrapped trigger those produce
# are exactly the statements the importer has to step over in silence, so the
# fixtures should contain them.
#
# KNOWN NON-DETERMINISM. --skip-dump-date removes the timestamp trailer, which
# is the one line that would otherwise differ on every single run. Two lines
# still move, and both move only when the SERVER moves:
#
#   -- MySQL dump 10.13  Distrib 8.0.46, for Linux (x86_64)
#   -- Server version    8.0.46-0ubuntu0.24.04.3
#
# The first is the mysqldump client's build, the second the server's, and a
# patch release changes both. Do not strip them: the second is what the importer
# reads to decide whether it was written against a server it knows, and a
# fixture with no banner would leave that path untested. A workflow that diffs
# regenerated fixtures ignores these two lines the way .github/workflows/
# pg-fixtures.yml ignores its four.
#
# Every DEFINER in testdata/source/ is written out as `jjf`@`%` rather than left
# to the server. mysqldump writes the RESOLVED definer of a view or a trigger,
# so a definer the server filled in would make the capture depend on which
# account this script connected as.

set -eu

MYSQL_HOST=${MYSQL_HOST:-127.0.0.1}
MYSQL_PORT=${MYSQL_PORT:-3306}
MYSQL_SOCKET=${MYSQL_SOCKET:-}
MYSQL_USER=${MYSQL_USER:-root}
MYSQL_PASSWORD=${MYSQL_PASSWORD:-}
MYSQL=${MYSQL:-mysql}
MYSQLDUMP=${MYSQLDUMP:-mysqldump}

dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
work=""

# cleanup removes the scratch directory, however this script ends. The
# databases it created are left on the server: they are named after the
# fixtures, they are recreated on the next run, and deleting them here would
# make a failed run harder to look into.
cleanup() {
	[ -n "$work" ] || return 0
	rm -rf "$work"
	work=""
}
trap cleanup EXIT INT TERM

# The password is passed through the environment rather than on the command
# line, because an argument is visible to every process on the machine. An empty
# value is left unset, since MYSQL_PWD="" would still count as "a password was
# given" and fail against a server that wants none.
if [ -n "$MYSQL_PASSWORD" ]; then
	MYSQL_PWD=$MYSQL_PASSWORD
	export MYSQL_PWD
fi

# client and dumper wrap the two commands with the connection arguments, so that
# --default-character-set is written once and cannot be forgotten at a call site.
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

work=$(mktemp -d)

for src in "$dir"/source/*.sql; do
	name=$(basename "$src" .sql)
	if [ $# -gt 0 ] && [ "$1" != "$name" ]; then
		continue
	fi

	client --execute="DROP DATABASE IF EXISTS \`$name\`; CREATE DATABASE \`$name\` DEFAULT CHARACTER SET utf8mb4"
	# The source is fed on stdin rather than passed with --execute or a path, so
	# that the same script works when MYSQL is a wrapper that execs into a
	# container and cannot see this checkout.
	client "$name" < "$src"
	dumper --no-data --skip-dump-date "$name" > "$work/$name.sql"

	# The series comes from the dump itself rather than from a parameter, so
	# that a connection pointing anywhere still lands in the right directory.
	# That is the call internal/importer/postgres/testdata/generate.sh already
	# makes when it reads the major from pg_dump --version.
	series=$(sed -n 's/^-- Server version[[:space:]]*\([0-9][0-9]*\.[0-9][0-9]*\)\..*$/\1/p' "$work/$name.sql" | head -n 1)
	if [ -z "$series" ]; then
		echo "cannot read the server version banner out of the dump of $name" >&2
		exit 1
	fi

	out=$dir/dump/mysql$series
	mkdir -p "$out"
	cp "$work/$name.sql" "$out/$name.sql"
	echo "==> mysql$series $name"
done

cleanup
echo "wrote $dir/dump"
