--
-- HAND-WRITTEN FIXTURE: a dump whose second column list is never closed.
--
-- The first table is valid so that the reported line number is not 1.
--

CREATE TABLE public.authors (
    id integer NOT NULL,
    name text NOT NULL
);

CREATE TABLE public.books (
    id integer NOT NULL,
    title text NOT NULL
