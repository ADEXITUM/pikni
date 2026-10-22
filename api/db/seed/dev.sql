-- Точки учебной сети «Чекушка» для локальной разработки. Повторный запуск ничего не дублирует.
insert into locations (code, name, kind, city, timezone) values
    ('СК-01', 'Центральный склад',                  'warehouse', 'Благовещенск', 'Asia/Yakutsk'),
    ('СК-02', 'Региональный склад',                 'warehouse', 'Хабаровск',    'Asia/Vladivostok'),
    ('МГ-01', 'Магазин на ул. Ленина',              'store',     'Благовещенск', 'Asia/Yakutsk'),
    ('МГ-02', 'Магазин на ул. 50 лет Октября',      'store',     'Благовещенск', 'Asia/Yakutsk'),
    ('МГ-03', 'Магазин на ул. Муравьёва-Амурского', 'store',     'Хабаровск',    'Asia/Vladivostok'),
    ('МГ-04', 'Магазин на ул. Светланской',         'store',     'Владивосток',  'Asia/Vladivostok')
on conflict (code) do nothing;
