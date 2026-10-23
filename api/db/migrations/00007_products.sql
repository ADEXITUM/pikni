-- +goose Up
-- Составные ключи нужны для внешних ключей ниже: база сама проверяет, что бренд принадлежит
-- производителю позиции, линейка — её бренду, а регион — её стране.
alter table brands add constraint brands_id_producer_key unique (id, producer_id);
alter table origin_regions add constraint origin_regions_id_country_key unique (id, country_code);

-- Линейка — позиции одного бренда, которые различаются разновидностью: «Тайга классическая»
-- и «Тайга на пшенице».
create table product_lines (
    id        uuid primary key default uuidv7(),
    brand_id  uuid not null references brands (id),
    name      text not null,
    is_active boolean not null default true,
    unique (brand_id, name),
    constraint product_lines_id_brand_key unique (id, brand_id)
);

create sequence product_article_seq;

-- Без вида продукции позиция не учитывается в ЕГАИС (снеки, безалкогольное). Способ учёта
-- не хранится: его задаёт вид продукции.
create table products (
    id               uuid primary key default uuidv7(),
    article          text not null unique default 'ЧК-' || lpad(nextval('product_article_seq')::text, 6, '0'),
    name             text not null,
    category_id      uuid not null references categories (id),
    kind_code        text references product_kinds (code),
    abv              numeric(4, 1) check (abv between 0 and 100),
    crpt_marked      boolean not null default false,
    producer_id      uuid not null references producers (id),
    brand_id         uuid,
    product_line_id  uuid,
    variety          text,
    country_code     text not null references countries (code),
    origin_region_id uuid,
    attributes       jsonb not null default '{}',
    is_active        boolean not null default true,
    version          integer not null default 1,
    created_at       timestamptz not null default now(),
    updated_at       timestamptz not null default now(),
    constraint products_brand_fkey foreign key (brand_id, producer_id) references brands (id, producer_id),
    constraint products_line_fkey foreign key (product_line_id, brand_id) references product_lines (id, brand_id),
    constraint products_region_fkey foreign key (origin_region_id, country_code) references origin_regions (id, country_code),
    constraint products_line_needs_brand check (product_line_id is null or brand_id is not null),
    constraint products_alcohol_abv check (kind_code is null or abv is not null)
);

alter sequence product_article_seq owned by products.article;

create index products_name_idx on products (name, id);
create index products_category_idx on products (category_id);
create index products_brand_idx on products (brand_id);
create index products_attributes_idx on products using gin (attributes);

-- Карточка позиции с названиями связанных записей: для экрана и журнала действий.
create view product_cards as
select p.id, p.article, p.name, p.category_id, c.name as category_name, p.kind_code,
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
