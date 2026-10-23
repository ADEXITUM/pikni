-- +goose Up
-- Вариант — то, что стоит на полке и пробивается на кассе: объём, тара, подарочная упаковка.
-- Объём, тара и упаковка задают вариант и после создания не меняются: на вариант будут
-- ссылаться остатки и документы. У снеков объёма и тары может не быть.
create table variants (
    id         uuid primary key default uuidv7(),
    product_id uuid not null references products (id),
    volume_ml  integer references volumes (ml),
    container  text check (container in ('glass', 'pet', 'can')),
    gift_pack  boolean not null default false,
    alcocode   text unique check (alcocode ~ '^[0-9]{19}$'),
    gtin       text unique check (gtin ~ '^[0-9]{14}$'),
    is_active  boolean not null default true,
    created_at timestamptz not null default now(),
    unique nulls not distinct (product_id, volume_ml, container, gift_pack)
);

create index variants_product_idx on variants (product_id);

-- Штрихкод принадлежит одному варианту; у варианта их может быть несколько.
create table barcodes (
    code       text primary key,
    variant_id uuid not null references variants (id),
    kind       text not null check (kind in ('ean13', 'ean8', 'upca', 'code128', 'internal')),
    created_at timestamptz not null default now()
);

create index barcodes_variant_idx on barcodes (variant_id);

-- Номера внутренних EAN-13 (ЭТ-1): префикс 20 из диапазона для внутреннего применения.
create sequence internal_barcode_seq;
