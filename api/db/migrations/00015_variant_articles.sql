-- +goose Up
-- Артикул переходит с позиции на вариант (решение 0031): на полке стоит, пробивается на кассе
-- и имеет свою цену и свой алкокод конкретная бутылка, а не позиция «во всех объёмах».
create sequence variant_article_seq;

alter table variants add column article text;

-- Уже заведённые варианты нумеруются по порядку прежних артикулов позиций, внутри позиции —
-- по объёму, таре и упаковке, как в карточке.
update variants v
set article = 'ЧК-' || lpad(n.num::text, 6, '0')
from (select x.id,
             row_number() over (order by p.article, x.volume_ml nulls first, x.container nulls first,
                                         x.gift_pack) as num
      from variants x
      join products p on p.id = x.product_id) n
where n.id = v.id;

select setval('variant_article_seq', greatest(count(*), 1), count(*) > 0) from variants;

alter table variants
    alter column article set default 'ЧК-' || lpad(nextval('variant_article_seq')::text, 6, '0'),
    alter column article set not null,
    add constraint variants_article_key unique (article);

alter sequence variant_article_seq owned by variants.article;

drop view product_cards;

-- Последовательность прежних артикулов принадлежит колонке и удаляется вместе с ней.
alter table products drop column article;

create view product_cards as
select p.id, p.name, p.category_id, c.name as category_name, p.kind_code,
       k.egais_accounting, p.abv, p.crpt_marked, p.producer_id, pr.name as producer_name,
       p.brand_id, b.name as brand_name, p.product_line_id, l.name as line_name, p.variety,
       p.country_code, co.name as country_name, p.origin_region_id, r.name as region_name,
       p.attributes, p.is_active, p.version
from products p
join categories c on c.id = p.category_id
join producers pr on pr.id = p.producer_id
join countries co on co.code = p.country_code
left join product_kinds k on k.code = p.kind_code
left join brands b on b.id = p.brand_id
left join product_lines l on l.id = p.product_line_id
left join origin_regions r on r.id = p.origin_region_id;
