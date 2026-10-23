-- +goose Up
-- Дерево категорий для витрины и отчётов, не больше трёх уровней. Глубину считает триггер
-- от родителя, поэтому четвёртый уровень отклоняет ограничение check. Родитель после
-- создания не меняется.
create table categories (
    id        uuid primary key default uuidv7(),
    parent_id uuid references categories (id),
    name      text not null,
    depth     smallint not null check (depth between 1 and 3),
    sort      integer not null default 0,
    is_active boolean not null default true,
    unique nulls not distinct (parent_id, name)
);

-- +goose StatementBegin
create function set_category_depth() returns trigger
language plpgsql as $$
begin
    new.depth := 1 + coalesce((select depth from categories where id = new.parent_id), 0);
    return new;
end;
$$;
-- +goose StatementEnd

create trigger categories_set_depth
    before insert on categories
    for each row execute function set_category_depth();

-- Схема характеристик категории; подкатегории наследуют характеристики предков. Код и тип
-- после создания не меняются. options — допустимые значения списка: их можно добавлять
-- и отправлять в архив, но не удалять, чтобы значения у позиций не потеряли смысл.
create table category_attributes (
    id               uuid primary key default uuidv7(),
    category_id      uuid not null references categories (id),
    code             text not null check (code ~ '^[a-z][a-z0-9_]{1,31}$'),
    name             text not null,
    type             text not null check (type in ('enum', 'number', 'text', 'bool')),
    unit             text,
    options          jsonb not null default '[]',
    filterable       boolean not null default false,
    used_for_analogs boolean not null default false,
    sort             integer not null default 0,
    is_active        boolean not null default true,
    unique (category_id, code)
);

-- Начальное дерево и характеристики по ТЗ (НСИ-1, НСИ-3).
-- +goose StatementBegin
do $$
declare
    strong uuid; wine uuid; beer uuid; other uuid; c uuid;
    opts jsonb;
begin
    insert into categories (name, sort) values ('Крепкий алкоголь', 10) returning id into strong;
    insert into categories (name, sort) values ('Вино', 20) returning id into wine;
    insert into categories (name, sort) values ('Пиво и сидр', 30) returning id into beer;
    insert into categories (name, sort) values ('Слабоалкогольные напитки', 40) returning id into c;
    insert into category_attributes (category_id, code, name, type) values (c, 'flavor', 'Вкус', 'text');
    insert into categories (name, sort) values ('Безалкогольное и снеки', 50) returning id into other;

    insert into categories (parent_id, name, sort) values (strong, 'Водка', 10) returning id into c;
    opts := '[{"value": "пшеница"}, {"value": "рожь"}, {"value": "смесь зерновых"}, {"value": "картофель"}]';
    insert into category_attributes (category_id, code, name, type, options, filterable, used_for_analogs, sort) values
        (c, 'raw_material', 'Сырьё', 'enum', opts, true, true, 10);
    opts := '[{"value": "угольная"}, {"value": "серебряная"}, {"value": "берёзовая"}, {"value": "многоступенчатая"}]';
    insert into category_attributes (category_id, code, name, type, options, filterable, sort) values
        (c, 'filtration', 'Фильтрация', 'enum', opts, true, 20);

    insert into categories (parent_id, name, sort) values (strong, 'Коньяк и бренди', 20) returning id into c;
    insert into category_attributes (category_id, code, name, type, unit, filterable, used_for_analogs) values
        (c, 'aging', 'Выдержка', 'number', 'лет', true, true);

    insert into categories (parent_id, name, sort) values (strong, 'Виски', 30) returning id into c;
    insert into category_attributes (category_id, code, name, type, unit, filterable, used_for_analogs, sort) values
        (c, 'aging', 'Выдержка', 'number', 'лет', true, true, 10);
    opts := '[{"value": "односолодовый"}, {"value": "купажированный"}, {"value": "зерновой"}, {"value": "бурбон"}]';
    insert into category_attributes (category_id, code, name, type, options, filterable, used_for_analogs, sort) values
        (c, 'whisky_type', 'Тип', 'enum', opts, true, true, 20);

    insert into categories (parent_id, name, sort) values (strong, 'Ром', 40);
    insert into categories (parent_id, name, sort) values (strong, 'Джин', 50);
    insert into categories (parent_id, name, sort) values (strong, 'Текила', 60);
    insert into categories (parent_id, name, sort) values (strong, 'Ликёры и настойки', 70) returning id into c;
    insert into category_attributes (category_id, code, name, type) values (c, 'flavor', 'Вкус', 'text');

    opts := '[{"value": "красное"}, {"value": "белое"}, {"value": "розовое"}, {"value": "оранжевое"}]';
    insert into category_attributes (category_id, code, name, type, options, filterable, used_for_analogs, sort) values
        (wine, 'color', 'Цвет', 'enum', opts, true, true, 10);
    opts := '[{"value": "брют натюр"}, {"value": "экстра брют"}, {"value": "брют"}, {"value": "сухое"},
              {"value": "полусухое"}, {"value": "полусладкое"}, {"value": "сладкое"}]';
    insert into category_attributes (category_id, code, name, type, options, filterable, used_for_analogs, sort) values
        (wine, 'sugar', 'Содержание сахара', 'enum', opts, true, true, 20);
    insert into category_attributes (category_id, code, name, type, filterable, sort) values
        (wine, 'grape', 'Сорт винограда', 'text', true, 30);
    insert into category_attributes (category_id, code, name, type, sort) values
        (wine, 'vintage', 'Год урожая', 'number', 40);
    insert into categories (parent_id, name, sort) values (wine, 'Тихое', 10), (wine, 'Игристое', 20), (wine, 'Креплёное', 30);

    insert into categories (parent_id, name, sort) values (beer, 'Пиво', 10) returning id into c;
    opts := '[{"value": "светлое"}, {"value": "тёмное"}, {"value": "пшеничное"}, {"value": "эль"},
              {"value": "IPA"}, {"value": "стаут"}, {"value": "портер"}]';
    insert into category_attributes (category_id, code, name, type, options, filterable, used_for_analogs, sort) values
        (c, 'style', 'Стиль', 'enum', opts, true, true, 10);
    opts := '[{"value": "фильтрованное"}, {"value": "нефильтрованное"}]';
    insert into category_attributes (category_id, code, name, type, options, filterable, used_for_analogs, sort) values
        (c, 'filtration', 'Фильтрация', 'enum', opts, true, true, 20);
    insert into categories (parent_id, name, sort) values (beer, 'Сидр и пуаре', 20) returning id into c;
    insert into category_attributes (category_id, code, name, type) values (c, 'flavor', 'Вкус', 'text');

    insert into categories (parent_id, name, sort) values (other, 'Напитки', 10) returning id into c;
    insert into category_attributes (category_id, code, name, type) values (c, 'flavor', 'Вкус', 'text');
    insert into categories (parent_id, name, sort) values (other, 'Снеки', 20) returning id into c;
    insert into category_attributes (category_id, code, name, type) values (c, 'flavor', 'Вкус', 'text');
end;
$$;
-- +goose StatementEnd

update category_attributes
set options = (select jsonb_agg(o || '{"archived": false}') from jsonb_array_elements(options) o)
where type = 'enum';
