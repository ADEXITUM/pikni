-- +goose Up
-- Фасовка сопутствующих товаров — масса нетто: у снеков нет объёма, у них граммы. У варианта
-- либо объём, либо масса, либо ни того ни другого (сигаретная пачка).
alter table variants
    add column net_weight_g integer check (net_weight_g between 1 and 100000),
    add constraint variants_volume_or_weight_check check (volume_ml is null or net_weight_g is null),
    drop constraint variants_product_id_volume_ml_container_gift_pack_key,
    add constraint variants_packaging_key
        unique nulls not distinct (product_id, volume_ml, net_weight_g, container, gift_pack);

-- Признак «алкогольная продукция» задаётся категории верхнего уровня и наследуется вниз: от
-- него зависит, есть ли у позиции вид продукции ЕГАИС, крепость, алкокод и МРЦ. Так те же
-- справочники годятся и для сети без алкоголя (решение 0032).
alter table categories add column alcohol boolean not null default false;

-- +goose StatementBegin
create or replace function set_category_depth() returns trigger
language plpgsql as $$
declare
    parent categories%rowtype;
begin
    select * into parent from categories where id = new.parent_id;
    new.depth := 1 + coalesce(parent.depth, 0);
    if new.parent_id is not null then
        new.alcohol := parent.alcohol;
    end if;
    return new;
end;
$$;
-- +goose StatementEnd

update categories set alcohol = true
where id in (select id from categories where parent_id is null and name <> 'Безалкогольное и снеки');
update categories c set alcohol = p.alcohol from categories p where c.parent_id = p.id and c.depth = 2;
update categories c set alcohol = p.alcohol from categories p where c.parent_id = p.id and c.depth = 3;

-- Табак в алкомаркетах продаётся рядом с алкоголем и маркируется «Честным знаком».
insert into categories (name, sort) values ('Табачная продукция', 60);
insert into categories (parent_id, name, sort)
select id, 'Сигареты', 10 from categories where name = 'Табачная продукция' and parent_id is null;

-- Настройки сети. Префикс артикула — первая из них: при смене сети меняется одной строкой.
create table app_settings (
    key   text primary key,
    value text not null
);

insert into app_settings (key, value) values ('article_prefix', 'ЧК-');

-- +goose StatementBegin
create function next_article() returns text
language sql volatile as $$
    select coalesce((select value from app_settings where key = 'article_prefix'), '')
           || lpad(nextval('variant_article_seq')::text, 6, '0');
$$;
-- +goose StatementEnd

alter table variants alter column article set default next_article();
