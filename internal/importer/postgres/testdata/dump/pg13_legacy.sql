--
-- HAND-WRITTEN FIXTURE. This file was NOT produced by generate.sh.
--
-- Only PostgreSQL 16 is installed in the development container, so this dump is
-- written by hand in the shape older pg_dump majors produce, from real-world
-- pg_dump 13 output. It differs from the generated fixtures in the ways the
-- majors really differ:
--
--   * "SET search_path = public, pg_catalog;" instead of the
--     "SELECT pg_catalog.set_config('search_path', '', false);" form, so every
--     name in the file is UNQUALIFIED;
--   * "WITH (oids = false)" after the column list;
--   * serial columns rather than identity columns;
--   * no \restrict wrapper.
--
-- It is the fixture that proves an unqualified name is treated as belonging to
-- the schema being imported.
--

-- Dumped from database version 13.14
-- Dumped by pg_dump version 13.14

SET statement_timeout = 0;
SET lock_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SET check_function_bodies = false;
SET client_min_messages = warning;
SET row_security = off;

SET search_path = public, pg_catalog;

SET default_tablespace = '';

SET default_with_oids = false;

--
-- Name: authors; Type: TABLE; Schema: public; Owner: jjf
--

CREATE TABLE authors (
    id integer NOT NULL,
    name character varying(120) NOT NULL,
    bio text,
    born_on date
)
WITH (oids = false);


ALTER TABLE authors OWNER TO jjf;

--
-- Name: TABLE authors; Type: COMMENT; Schema: public; Owner: jjf
--

COMMENT ON TABLE authors IS '著者
本を書いた人。';


--
-- Name: authors_id_seq; Type: SEQUENCE; Schema: public; Owner: jjf
--

CREATE SEQUENCE authors_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


ALTER TABLE authors_id_seq OWNER TO jjf;

--
-- Name: authors_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: jjf
--

ALTER SEQUENCE authors_id_seq OWNED BY authors.id;


--
-- Name: books; Type: TABLE; Schema: public; Owner: jjf
--

CREATE TABLE books (
    id integer NOT NULL,
    author_id integer NOT NULL,
    isbn character(13) NOT NULL,
    title text NOT NULL,
    price numeric(8,2) DEFAULT 0 NOT NULL,
    published_at timestamp without time zone
)
WITH (oids = false);


ALTER TABLE books OWNER TO jjf;

--
-- Name: TABLE books; Type: COMMENT; Schema: public; Owner: jjf
--

COMMENT ON TABLE books IS '書籍';


--
-- Name: COLUMN books.isbn; Type: COMMENT; Schema: public; Owner: jjf
--

COMMENT ON COLUMN books.isbn IS 'ISBN';


--
-- Name: books_id_seq; Type: SEQUENCE; Schema: public; Owner: jjf
--

CREATE SEQUENCE books_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


ALTER TABLE books_id_seq OWNER TO jjf;

--
-- Name: books_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: jjf
--

ALTER SEQUENCE books_id_seq OWNED BY books.id;


--
-- Name: authors id; Type: DEFAULT; Schema: public; Owner: jjf
--

ALTER TABLE ONLY authors ALTER COLUMN id SET DEFAULT nextval('authors_id_seq'::regclass);


--
-- Name: books id; Type: DEFAULT; Schema: public; Owner: jjf
--

ALTER TABLE ONLY books ALTER COLUMN id SET DEFAULT nextval('books_id_seq'::regclass);


--
-- Name: authors authors_pkey; Type: CONSTRAINT; Schema: public; Owner: jjf
--

ALTER TABLE ONLY authors
    ADD CONSTRAINT authors_pkey PRIMARY KEY (id);


--
-- Name: books books_isbn_key; Type: CONSTRAINT; Schema: public; Owner: jjf
--

ALTER TABLE ONLY books
    ADD CONSTRAINT books_isbn_key UNIQUE (isbn);


--
-- Name: books books_pkey; Type: CONSTRAINT; Schema: public; Owner: jjf
--

ALTER TABLE ONLY books
    ADD CONSTRAINT books_pkey PRIMARY KEY (id);


--
-- Name: books_author_id_idx; Type: INDEX; Schema: public; Owner: jjf
--

CREATE INDEX books_author_id_idx ON books USING btree (author_id);


--
-- Name: books books_author_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: jjf
--

ALTER TABLE ONLY books
    ADD CONSTRAINT books_author_id_fkey FOREIGN KEY (author_id) REFERENCES authors(id) ON DELETE CASCADE;


--
-- PostgreSQL database dump complete
--
