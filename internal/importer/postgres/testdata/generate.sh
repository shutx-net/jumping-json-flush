#!/bin/sh
#
# Regenerate the committed pg_dump fixtures under testdata/dump/.
#
# This is a developer tool. No test runs it: the dumps it produces are committed
# so that the test suite needs neither a database nor a network.
#
# For every PostgreSQL major it finds installed, it starts a throwaway cluster in
# a scratch directory under /tmp, loads one file from testdata/source/, dumps the
# result with "pg_dump --schema-only" into testdata/dump/pg<major>/, and deletes
# the cluster again. The cluster is never committed, and nothing outside the
# scratch directory and testdata/dump/ is written.
#
# The majors are the point: the importer claims to read dumps from a range of
# pg_dump versions, and TestImportAgreesAcrossPgDumpMajors holds every capture in
# that range to the same golden document.
#
# Usage:
#   sh generate.sh            regenerate every fixture, for every major found
#   sh generate.sh ecshop     regenerate the ecshop fixture only, same majors
#
# Environment:
#   PGBIN   directory holding initdb / pg_ctl / pg_dump. Set it to regenerate a
#           single major; unset, every /usr/lib/postgresql/*/bin is used
#   PGPORT  port for the scratch cluster (default: 5433, so that a developer's
#           own PostgreSQL on 5432 is never touched)
#
# INSTALLING THE OTHER MAJORS: a distribution ships one. The rest come from the
# PostgreSQL Global Development Group apt repository at apt.postgresql.org, which
# carries every supported major side by side under /usr/lib/postgresql/<major>:
#
#   install -d /usr/share/postgresql-common/pgdg
#   curl -fsSL -o /usr/share/postgresql-common/pgdg/apt.postgresql.org.asc \
#           https://www.postgresql.org/media/keys/ACCC4CF8.asc
#   . /etc/os-release
#   echo "deb [signed-by=/usr/share/postgresql-common/pgdg/apt.postgresql.org.asc] \
#           https://apt.postgresql.org/pub/repos/apt $VERSION_CODENAME-pgdg main" \
#           > /etc/apt/sources.list.d/pgdg.list
#   apt-get update
#   apt-get install -y postgresql-13 postgresql-14 postgresql-15 postgresql-17 \
#           postgresql-18
#
# testdata/dump/synthetic/ is NOT generated. Those files are hand-written, say so
# in their own headers, and exist to cover shapes no installed pg_dump produces.
#
# The dumps are taken WITHOUT --no-owner and --no-privileges on purpose: the
# OWNER TO and GRANT statements that produces are exactly the kind of statement
# the importer has to skip in silence, so the fixtures should contain them.
#
# KNOWN NON-DETERMINISM: pg_dump wraps its output in "\restrict <token>" and
# "\unrestrict <token>" psql meta-commands, and the token is different on every
# run. Regenerating a fixture therefore always shows a two-line diff even when
# nothing about the schema changed. Do not strip those lines: they are part of
# what a real dump looks like, and the importer must survive them. Every major
# from 13 up writes them; the wrapper was backported that far.

set -eu

PGPORT=${PGPORT:-5433}

dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
base=""
bin=""

# bins is the list of PostgreSQL bin directories to dump with: the PGBIN
# override alone, or every major installed.
if [ -n "${PGBIN:-}" ]; then
	bins=$PGBIN
else
	bins=""
	for candidate in /usr/lib/postgresql/*/bin; do
		[ -x "$candidate/pg_dump" ] || continue
		bins="$bins $candidate"
	done
fi

if [ -z "$bins" ]; then
	echo "no PostgreSQL binaries found under /usr/lib/postgresql/*/bin; set PGBIN" >&2
	exit 1
fi

# cleanup stops the server and removes the scratch cluster, however this script
# ends. It runs between majors too, so only one cluster ever exists at a time.
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

for bin in $bins; do
	if [ ! -x "$bin/pg_dump" ]; then
		echo "PostgreSQL binaries not found in $bin; set PGBIN" >&2
		exit 1
	fi

	# The major comes from pg_dump itself rather than from the path, so that a
	# PGBIN pointing anywhere still lands in the right directory.
	major=$("$bin/pg_dump" --version | sed -n 's/^pg_dump ([^)]*) \([0-9][0-9]*\).*/\1/p')
	if [ -z "$major" ]; then
		echo "cannot read the major version of $bin/pg_dump" >&2
		exit 1
	fi
	out=$dir/dump/pg$major

	# The scratch directory has to live somewhere the postgres user can
	# traverse, which is why it is under /tmp and not under the repository.
	base=$(mktemp -d /tmp/jjf-pgfixture.XXXXXX)
	chown postgres:postgres "$base" 2>/dev/null || true
	chmod 700 "$base"

	# --no-locale keeps collation names from varying between machines; -A trust
	# and a unix socket mean no password is needed and nothing listens on TCP.
	as_postgres "$bin/initdb -D $base/data -U jjf --no-locale --encoding=UTF8 -A trust" >/dev/null
	as_postgres "$bin/pg_ctl -D $base/data -o \"-k $base -h '' -p $PGPORT\" -l $base/server.log start -w" >/dev/null

	mkdir -p "$out"

	for src in "$dir"/source/*.sql; do
		name=$(basename "$src" .sql)
		if [ $# -gt 0 ] && [ "$1" != "$name" ]; then
			continue
		fi

		echo "==> pg$major $name"
		as_postgres "$bin/createdb -h $base -p $PGPORT -U jjf $name" >/dev/null
		as_postgres "$bin/psql -h $base -p $PGPORT -U jjf -d $name -v ON_ERROR_STOP=1 -q -f $src" >/dev/null
		as_postgres "$bin/pg_dump -h $base -p $PGPORT -U jjf --schema-only $name" > "$out/$name.sql"
	done

	cleanup
done

echo "wrote $dir/dump"
