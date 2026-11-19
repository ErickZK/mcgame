-- Aplica o catálogo pt-BR às tabelas de dados consumidas pelo cliente Flash.
-- A operação é idempotente e preserva IDs, relações e valores sem tradução.
DO $$
DECLARE
  r record;
BEGIN
  FOR r IN
    SELECT c.table_name, c.column_name
    FROM information_schema.columns c
    WHERE c.table_schema = 'data'
      AND c.table_name LIKE 'data_tbl_%'
      AND c.data_type IN ('text', 'character varying', 'character')
  LOOP
    EXECUTE format(
      'UPDATE data.%I AS t
          SET %I = ct.translated_text
        FROM data.content_translation AS ct
       WHERE ct.locale = ''pt-BR''
         AND ct.source_file LIKE %L
         AND ct.source_text IS NOT NULL
         AND t.%I = ct.source_text
         AND t.%I IS DISTINCT FROM ct.translated_text',
      r.table_name,
      r.column_name,
      '%' || r.table_name || '.sql',
      r.column_name,
      r.column_name
    );
  END LOOP;
END $$;
