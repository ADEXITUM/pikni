-- +goose Up
-- Окна розничной продажи алкоголя в местном времени точки. subject_code null — федеральное
-- правило, оно действует во всех регионах вместе с региональным. Новая редакция закона —
-- новая строка с датой начала; на дату продажи действует последняя наступившая.
create table alcohol_sale_rules (
    id           uuid primary key default uuidv7(),
    subject_code text references ru_subjects (code),
    scope        text not null check (scope in ('all', 'strong')),
    valid_from   date not null,
    sale_from    time not null,
    sale_to      time not null,
    source       text not null,
    created_at   timestamptz not null default now(),
    check (sale_from < sale_to)
);

create unique index alcohol_sale_rules_edition_key
    on alcohol_sale_rules (coalesce(subject_code, ''), scope, valid_from);

-- Дни полного запрета: ежегодные по дате (month_day «ММ-ДД») или разовые (date).
create table alcohol_sale_bans (
    id           uuid primary key default uuidv7(),
    subject_code text not null references ru_subjects (code),
    name         text not null,
    scope        text not null check (scope in ('all', 'strong')),
    date         date,
    month_day    text check (month_day ~ '^(0[1-9]|1[0-2])-(0[1-9]|[12][0-9]|3[01])$'),
    source       text not null,
    created_at   timestamptz not null default now(),
    check ((date is null) <> (month_day is null))
);

-- Действующие редакции на 23.10.2026 по обзорам КонсультантПлюс (на 01.10.2026) и Контур.Норматива.
-- Даты начала прежних редакций — по сообщениям СМИ. Дни, даты которых закон не задаёт
-- (последний звонок, выпускные, День молодёжи в Амурской области), вносятся по решениям муниципалитетов.
insert into alcohol_sale_rules (subject_code, scope, valid_from, sale_from, sale_to, source) values
    (null, 'all', '2013-01-01', '08:00', '23:00',
     '171-ФЗ, ст. 16, п. 9, абз. 1'),
    ('28', 'all', '2014-10-01', '11:00', '19:00',
     'Закон Амурской области от 25.09.2014 № 403-ОЗ, ст. 1 (первоначальная редакция)'),
    ('28', 'all', '2016-04-08', '11:00', '21:00',
     'Закон Амурской области от 25.09.2014 № 403-ОЗ, ст. 1, ч. 1, п. 1 (ред. закона от 28.03.2016 № 659-ОЗ)'),
    ('27', 'all', '2016-03-02', '10:00', '22:00',
     'Закон Хабаровского края от 25.11.2015 № 143, ст. 3'),
    ('25', 'all', '2023-03-01', '10:00', '22:00',
     'Закон Приморского края от 02.12.2009 № 536-КЗ, ст. 6, ч. 1 (ред. закона от 05.12.2022 № 245-КЗ)');

insert into alcohol_sale_bans (subject_code, name, scope, month_day, date, source) values
    ('28', 'Международный день защиты детей', 'all', '06-01', null, 'Закон Амурской области № 403-ОЗ, ст. 1'),
    ('28', 'День знаний', 'all', '09-01', null, 'Закон Амурской области № 403-ОЗ, ст. 1'),
    ('27', 'Международный день защиты детей', 'all', '06-01', null, 'Закон Хабаровского края № 143, ст. 4'),
    ('27', 'День молодёжи (последняя суббота июня)', 'all', null, '2026-06-27', 'Закон Хабаровского края № 143, ст. 4'),
    ('27', 'День молодёжи (последняя суббота июня)', 'all', null, '2027-06-26', 'Закон Хабаровского края № 143, ст. 4'),
    ('27', 'День знаний (если воскресенье — 2 сентября)', 'all', '09-01', null, 'Закон Хабаровского края № 143, ст. 4'),
    ('25', 'Международный день защиты детей', 'all', '06-01', null, 'Закон Приморского края № 536-КЗ, ст. 6'),
    ('25', 'День знаний (если выходной — следующий рабочий день)', 'all', '09-01', null, 'Закон Приморского края № 536-КЗ, ст. 6');
