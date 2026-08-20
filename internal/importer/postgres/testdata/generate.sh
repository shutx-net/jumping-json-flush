#!/bin/sh
#
# Regenerate the committed pg_dump fixtures under testdata/dump/.
#
# This is a developer tool. No test runs it: the dumps it produces are committed
# so that the test suite needs neither a database nor a network.
#
# It starts a throwaway PostgreSQL cluster in a scratch directory under /tmp,
# loads one file from testdata/source/, dumps the result with
# "pg_dump --schema-only", and deletes the cluster again. The cluster is never
# committed, and nothing outside the scratch directory and testdata/dump/ is
# written.
#
# Usage:
#   sh generate.sh            regenerate every fixture in testdata/source/
#   sh generate.sh ecshop     regenerate testdata/dump/ecshop.sql only
#
# Environment:
#   PGBIN   directory holding initdb / pg_ctl / pg_dump (default: PostgreSQL 16)
#   PGPORT  port for the scratch cluster (default: 5433, so that a developer's
#           own PostgreSQL on 5432 is never touched)
#
# The dumps are taken WITHOUT --no-owner and --no-privileges on purpose: the
# OWNER TO and GRANT statements that produces are exactly the kind of statement
# the importer has to skip in silence, so the fixtures should contain them.
#
# KNOWN NON-DETERMINISM: pg_dump 16.13 wraps its output in "\restrict <token>"
# and "\unrestrict <token>" psql meta-commands, and the token is different on
# every run. Regenerating a fixture therefore always shows a two-line diff even
# when nothing about the schema changed. Do not strip those lines: they are part
# of what a real dump looks like, and the importer must survive them.

set -eu

PGBIN=${PGBIN:-/usr/lib/postgresql/16/bin}
PGPORT=${PGPORT:-5433}

dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
base=""

if [ ! -x "$PGBIN/pg_dump" ]; then
	echo "PostgreSQL binaries not found in $PGBIN; set PGBIN" >&2
	exit 1
fi

# cleanup stops the server and removes the scratch cluster, however this script
# ends.
cleanup() {
	[ -n "$base" ] || return 0
	if [ -d "$base/data" ]; then
		as_postgres "$PGBIN/pg_ctl -D $base/data stop -m immediate" >/dev/null 2>&1 || true
	fi
	rm -rf "$base"
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

# The scratch directory has to live somewhere the postgres user can traverse,
# which is why it is under /tmp and not under the repository.
base=$(mktemp -d /tmp/jjf-pgfixture.XXXXXX)
chown postgres:postgres "$base" 2>/dev/null || true
chmod 700 "$base"

# --no-locale keeps collation names from varying between machines; -A trust and
# a unix socket mean no password is needed and nothing listens on TCP.
as_postgres "$PGBIN/initdb -D $base/data -U jjf --no-locale --encoding=UTF8 -A trust" >/dev/null
as_postgres "$PGBIN/pg_ctl -D $base/data -o \"-k $base -h '' -p $PGPORT\" -l $base/server.log start -w" >/dev/null

mkdir -p "$dir/dump"

for src in "$dir"/source/*.sql; do
	name=$(basename "$src" .sql)
	if [ $# -gt 0 ] && [ "$1" != "$name" ]; then
		continue
	fi

	echo "==> $name"
	as_postgres "$PGBIN/createdb -h $base -p $PGPORT -U jjf $name" >/dev/null
	as_postgres "$PGBIN/psql -h $base -p $PGPORT -U jjf -d $name -v ON_ERROR_STOP=1 -q -f $src" >/dev/null
	as_postgres "$PGBIN/pg_dump -h $base -p $PGPORT -U jjf --schema-only $name" > "$dir/dump/$name.sql"
done

echo "wrote $dir/dump"
