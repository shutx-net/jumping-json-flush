--
-- PostgreSQL database dump
--

-- Dumped by pg_dump version 16.13 (Ubuntu 16.13-0ubuntu0.24.04.1)

SET default_table_access_method = heap;

CREATE TABLE public.users (
    id integer NOT NULL,
    email character varying(255) NOT NULL,
    doc jsonb,
    deleted_at timestamp with time zone,
    CONSTRAINT users_email_check CHECK ((length((email)::text) > 3))
);

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);

CREATE UNIQUE INDEX users_email_live_idx ON public.users USING btree (email) WHERE (deleted_at IS NULL);

CREATE INDEX users_doc_idx ON public.users USING gin (doc);
