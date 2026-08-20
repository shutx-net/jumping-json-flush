--
-- PostgreSQL database dump
--

-- Dumped by pg_dump version 16.13 (Ubuntu 16.13-0ubuntu0.24.04.1)

CREATE SCHEMA app;

CREATE FUNCTION public.touch_updated_at() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$;

CREATE VIEW public.active_users AS
 SELECT id,
    email
   FROM public.users
  WHERE (deleted_at IS NULL);
