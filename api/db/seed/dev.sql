-- Точки учебной сети «Чекушка» для локальной разработки. Повторный запуск обновляет их, не дублируя.
-- ФСРАР ID, КПП и лицензии вымышленные.
insert into locations (code, name, kind, city, timezone, subject_code, address, lat, lon,
                       fsrar_id, kpp, license_no, license_valid_to, opening_hours, is_public) values
    ('СК-01', 'Центральный склад', 'warehouse', 'Благовещенск', 'Asia/Yakutsk', '28',
     'ул. Мухина, 150', 50.296100, 127.505300, '030000100001', '280101001', '28РПО0000101', '2029-03-01',
     '{"mon": {"open": "08:00", "close": "20:00"}, "tue": {"open": "08:00", "close": "20:00"},
       "wed": {"open": "08:00", "close": "20:00"}, "thu": {"open": "08:00", "close": "20:00"},
       "fri": {"open": "08:00", "close": "20:00"}}', false),
    ('СК-02', 'Региональный склад', 'warehouse', 'Хабаровск', 'Asia/Vladivostok', '27',
     'ул. Промышленная, 21', 48.452300, 135.103800, '030000100002', '272401001', '27РПО0000102', '2028-11-15',
     '{"mon": {"open": "08:00", "close": "20:00"}, "tue": {"open": "08:00", "close": "20:00"},
       "wed": {"open": "08:00", "close": "20:00"}, "thu": {"open": "08:00", "close": "20:00"},
       "fri": {"open": "08:00", "close": "20:00"}}', false),
    ('МГ-01', 'Магазин на ул. Ленина', 'store', 'Благовещенск', 'Asia/Yakutsk', '28',
     'ул. Ленина, 112', 50.257600, 127.535500, '030000100011', '280101002', '28РПО0000111', '2028-06-30',
     '{"mon": {"open": "09:00", "close": "23:00"}, "tue": {"open": "09:00", "close": "23:00"},
       "wed": {"open": "09:00", "close": "23:00"}, "thu": {"open": "09:00", "close": "23:00"},
       "fri": {"open": "09:00", "close": "23:00"}, "sat": {"open": "10:00", "close": "23:00"},
       "sun": {"open": "10:00", "close": "22:00"}}', true),
    ('МГ-02', 'Магазин на ул. 50 лет Октября', 'store', 'Благовещенск', 'Asia/Yakutsk', '28',
     'ул. 50 лет Октября, 65', 50.268400, 127.523900, '030000100012', '280101003', '28РПО0000112', '2027-12-31',
     '{"mon": {"open": "10:00", "close": "22:00"}, "tue": {"open": "10:00", "close": "22:00"},
       "wed": {"open": "10:00", "close": "22:00"}, "thu": {"open": "10:00", "close": "22:00"},
       "fri": {"open": "10:00", "close": "22:00"}, "sat": {"open": "10:00", "close": "22:00"},
       "sun": {"open": "10:00", "close": "22:00"}}', true),
    ('МГ-03', 'Магазин на ул. Муравьёва-Амурского', 'store', 'Хабаровск', 'Asia/Vladivostok', '27',
     'ул. Муравьёва-Амурского, 18', 48.480200, 135.071700, '030000100013', '272401002', '27РПО0000113', '2028-04-20',
     '{"mon": {"open": "08:00", "close": "23:00"}, "tue": {"open": "08:00", "close": "23:00"},
       "wed": {"open": "08:00", "close": "23:00"}, "thu": {"open": "08:00", "close": "23:00"},
       "fri": {"open": "08:00", "close": "23:00"}, "sat": {"open": "08:00", "close": "23:00"},
       "sun": {"open": "08:00", "close": "23:00"}}', true),
    ('МГ-04', 'Магазин на ул. Светланской', 'store', 'Владивосток', 'Asia/Vladivostok', '25',
     'ул. Светланская, 33', 43.116300, 131.885600, '030000100014', '254001001', '25РПО0000114', '2029-01-10',
     '{"mon": {"open": "10:00", "close": "22:00"}, "tue": {"open": "10:00", "close": "22:00"},
       "wed": {"open": "10:00", "close": "22:00"}, "thu": {"open": "10:00", "close": "22:00"},
       "fri": {"open": "10:00", "close": "22:00"}, "sat": {"open": "10:00", "close": "22:00"},
       "sun": {"open": "11:00", "close": "21:00"}}', true)
on conflict (code) do update set
    name = excluded.name, city = excluded.city, timezone = excluded.timezone,
    subject_code = excluded.subject_code, address = excluded.address, lat = excluded.lat, lon = excluded.lon,
    fsrar_id = excluded.fsrar_id, kpp = excluded.kpp, license_no = excluded.license_no,
    license_valid_to = excluded.license_valid_to, opening_hours = excluded.opening_hours,
    is_public = excluded.is_public;

-- Регионы происхождения для демо-каталога (catalog.csv ссылается на них по названию).
insert into origin_regions (country_code, name) values
    ('250', 'Бордо'), ('250', 'Коньяк'), ('250', 'Шампань'), ('268', 'Кахетия'), ('724', 'Риоха'),
    ('826', 'Шотландия'), ('840', 'Кентукки'), ('643', 'Крым'), ('643', 'Кубань')
on conflict (country_code, name) do nothing;

-- Поставщики учебной сети; ИНН, КПП и ФСРАР ID вымышленные, контрольные цифры ИНН сходятся.
insert into suppliers (name, inn, kpp, fsrar_id, license_no, license_valid_to, default_receiving_mode, contacts) values
    ('ООО «Амур-Дистрибуция»', '2801001056', '280101004', '030000200001', '28ЗАП0000201', '2028-12-31', 'sample',
     '{"person": "Иванова Мария, менеджер", "phone": "+7 4162 00-11-22", "email": "sales@amur-distr.example"}'),
    ('ООО «Хабаровский винный дом»', '2724002077', '272401003', '030000200002', '27ЗАП0000202', '2027-09-30', 'boxes',
     '{"person": "Ким Сергей", "phone": "+7 4212 00-33-44"}'),
    ('ООО «Приморская пивная компания»', '2540003095', '254001002', '030000200003', '25ЗАП0000203', '2029-03-31', 'document',
     '{"phone": "+7 423 200-55-66", "email": "order@primbeer.example"}')
on conflict (inn, kpp) do nothing;
