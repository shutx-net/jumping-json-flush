--
-- PostgreSQL database dump
--

\restrict RaGbDiHsfHqfdCZATAAaB1J5Q0jbR6OUfbYdXZwgN2LQhSK4yvF4YOPSKHkxUCT

-- Dumped from database version 14.24 (Ubuntu 14.24-1.pgdg24.04+2)
-- Dumped by pg_dump version 14.24 (Ubuntu 14.24-1.pgdg24.04+2)

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

--
-- Name: audit; Type: SCHEMA; Schema: -; Owner: jjf
--

CREATE SCHEMA audit;


ALTER SCHEMA audit OWNER TO jjf;

--
-- Name: btree_gist; Type: EXTENSION; Schema: -; Owner: -
--

CREATE EXTENSION IF NOT EXISTS btree_gist WITH SCHEMA public;


--
-- Name: EXTENSION btree_gist; Type: COMMENT; Schema: -; Owner: 
--

COMMENT ON EXTENSION btree_gist IS 'support for indexing common datatypes in GiST';


SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: events; Type: TABLE; Schema: audit; Owner: jjf
--

CREATE TABLE audit.events (
    id bigint NOT NULL,
    happened_at timestamp with time zone DEFAULT now() NOT NULL,
    payload jsonb
);


ALTER TABLE audit.events OWNER TO jjf;

--
-- Name: events_id_seq; Type: SEQUENCE; Schema: audit; Owner: jjf
--

ALTER TABLE audit.events ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME audit.events_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);


--
-- Name: items; Type: TABLE; Schema: public; Owner: jjf
--

CREATE TABLE public.items (
    id integer NOT NULL,
    event_id bigint,
    quantity integer,
    price numeric(10,2) NOT NULL,
    total numeric(10,2) GENERATED ALWAYS AS ((price * (quantity)::numeric)) STORED,
    note text,
    doc jsonb,
    span interval day to second(6),
    grid integer[],
    flags bit(8),
    mask bit varying(64),
    cost money,
    origin inet,
    deleted_at timestamp with time zone,
    valid_from timestamp with time zone NOT NULL,
    valid_to timestamp with time zone NOT NULL,
    CONSTRAINT items_price_check CHECK ((price >= (0)::numeric)),
    CONSTRAINT items_quantity_check CHECK ((quantity > 0))
);


ALTER TABLE public.items OWNER TO jjf;

--
-- Name: no_key; Type: TABLE; Schema: public; Owner: jjf
--

CREATE TABLE public.no_key (
    label text NOT NULL,
    item_id integer
);


ALTER TABLE public.no_key OWNER TO jjf;

--
-- Name: events events_pkey; Type: CONSTRAINT; Schema: audit; Owner: jjf
--

ALTER TABLE ONLY audit.events
    ADD CONSTRAINT events_pkey PRIMARY KEY (id);


--
-- Name: items items_no_overlap; Type: CONSTRAINT; Schema: public; Owner: jjf
--

ALTER TABLE ONLY public.items
    ADD CONSTRAINT items_no_overlap EXCLUDE USING gist (id WITH =, tstzrange(valid_from, valid_to) WITH &&);


--
-- Name: items items_pkey; Type: CONSTRAINT; Schema: public; Owner: jjf
--

ALTER TABLE ONLY public.items
    ADD CONSTRAINT items_pkey PRIMARY KEY (id);


--
-- Name: items_doc_idx; Type: INDEX; Schema: public; Owner: jjf
--

CREATE INDEX items_doc_idx ON public.items USING gin (doc);


--
-- Name: items_live_idx; Type: INDEX; Schema: public; Owner: jjf
--

CREATE INDEX items_live_idx ON public.items USING btree (id) WHERE (deleted_at IS NULL);


--
-- Name: items_note_idx; Type: INDEX; Schema: public; Owner: jjf
--

CREATE INDEX items_note_idx ON public.items USING btree (lower(note));


--
-- Name: items_ordered_idx; Type: INDEX; Schema: public; Owner: jjf
--

CREATE INDEX items_ordered_idx ON public.items USING btree (price DESC NULLS LAST);


--
-- Name: items_price_incl_idx; Type: INDEX; Schema: public; Owner: jjf
--

CREATE INDEX items_price_incl_idx ON public.items USING btree (price) INCLUDE (note);


--
-- Name: items items_event_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: jjf
--

ALTER TABLE ONLY public.items
    ADD CONSTRAINT items_event_id_fkey FOREIGN KEY (event_id) REFERENCES audit.events(id);


--
-- Name: no_key no_key_item_fkey; Type: FK CONSTRAINT; Schema: public; Owner: jjf
--

ALTER TABLE ONLY public.no_key
    ADD CONSTRAINT no_key_item_fkey FOREIGN KEY (item_id) REFERENCES public.items(id) DEFERRABLE INITIALLY DEFERRED;


--
-- Name: no_key no_key_item_match_fkey; Type: FK CONSTRAINT; Schema: public; Owner: jjf
--

ALTER TABLE ONLY public.no_key
    ADD CONSTRAINT no_key_item_match_fkey FOREIGN KEY (item_id) REFERENCES public.items(id) MATCH FULL;


--
-- PostgreSQL database dump complete
--

\unrestrict RaGbDiHsfHqfdCZATAAaB1J5Q0jbR6OUfbYdXZwgN2LQhSK4yvF4YOPSKHkxUCT

