-- Corrige a descrição do item 27 e mantém a tradução no catálogo.
update data.content_translation
set translated_text = 'Pelo quente usado para fazer casacos.',
    status = 'machine_translated'
where locale = 'pt-BR'
  and source_text = 'Lông thú ấm áp dùng may áo khoác';

update data.data_tbl_item_template
set description = 'Pelo quente usado para fazer casacos.'
where id = 27;
