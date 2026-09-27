ALTER TABLE usage_daily
    ADD COLUMN IF NOT EXISTS id uuid DEFAULT gen_random_uuid();

UPDATE usage_daily
SET id = gen_random_uuid()
WHERE id IS NULL;

ALTER TABLE usage_daily
    ALTER COLUMN id SET NOT NULL;

DO $$
DECLARE
    pk_name text;
    pk_definition text;
BEGIN
    SELECT conname, pg_get_constraintdef(oid)
      INTO pk_name, pk_definition
      FROM pg_constraint
     WHERE conrelid = 'usage_daily'::regclass
       AND contype = 'p'
     LIMIT 1;

    IF pk_name IS NOT NULL AND position('(id)' in pk_definition) = 0 THEN
        EXECUTE format('ALTER TABLE usage_daily DROP CONSTRAINT %I', pk_name);
    END IF;
END
$$;

ALTER TABLE usage_daily
    ALTER COLUMN model_id DROP NOT NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
          FROM pg_constraint
         WHERE conrelid = 'usage_daily'::regclass
           AND contype = 'p'
    ) THEN
        ALTER TABLE usage_daily
            ADD CONSTRAINT usage_daily_pkey PRIMARY KEY (id);
    END IF;
END
$$;

CREATE UNIQUE INDEX IF NOT EXISTS ux_usage_daily_dimensions
    ON usage_daily(usage_date, api_key_id, model_id) NULLS NOT DISTINCT;
