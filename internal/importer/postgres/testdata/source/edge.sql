-- Source DDL for the fixture that exercises every warning path.
--
-- Everything here is something the jjf design format cannot hold. Each one must
-- produce a warning and still leave a usable document behind.

CREATE EXTENSION IF NOT EXISTS btree_gist;

CREATE SCHEMA audit;

CREATE TABLE audit.events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    happened_at timestamp with time zone DEFAULT now() NOT NULL,
    payload jsonb
);

CREATE TABLE public.items (
    id integer NOT NULL PRIMARY KEY,
    event_id bigint REFERENCES audit.events (id),
    quantity integer CHECK (quantity > 0),
    price numeric(10,2) NOT NULL,
    total numeric(10,2) GENERATED ALWAYS AS (price * quantity) STORED,
    note text,
    doc jsonb,
    span interval day to second(6),
    grid integer[][],
    flags bit(8),
    mask bit varying(64),
    cost money,
    origin inet,
    deleted_at timestamp with time zone,
    valid_from timestamp with time zone NOT NULL,
    valid_to timestamp with time zone NOT NULL,
    CONSTRAINT items_price_check CHECK ((price >= (0)::numeric)),
    CONSTRAINT items_no_overlap EXCLUDE USING gist (id WITH =, tstzrange(valid_from, valid_to) WITH &&)
);

CREATE TABLE public.no_key (
    label text NOT NULL,
    item_id integer
);

ALTER TABLE public.no_key
    ADD CONSTRAINT no_key_item_fkey FOREIGN KEY (item_id) REFERENCES public.items (id)
    DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE public.no_key
    ADD CONSTRAINT no_key_item_match_fkey FOREIGN KEY (item_id) REFERENCES public.items (id) MATCH FULL;

CREATE INDEX items_live_idx ON public.items (id) WHERE (deleted_at IS NULL);
CREATE INDEX items_note_idx ON public.items (lower(note));
CREATE INDEX items_price_incl_idx ON public.items (price) INCLUDE (note);
CREATE INDEX items_doc_idx ON public.items USING gin (doc);
CREATE INDEX items_ordered_idx ON public.items (price DESC NULLS LAST);
