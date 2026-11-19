-- Corrige o nome do material exibido no tooltip do item 23.
UPDATE data.content_translation
SET translated_text = 'Cristal',
    status = 'machine_translated'
WHERE locale = 'pt-BR'
  AND source_text = 'Pha Lê';

UPDATE data.data_tbl_item_template
SET name = 'Cristal'
WHERE name = 'Pha Lê';
