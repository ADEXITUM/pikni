-- +goose Up
-- Продажи за период и «залежавшийся» товар на годе истории (1,2 млн чеков) читали все чеки: дата
-- чека по часовому поясу точки индексом не ищется. Запросы добавляют условие по времени чека с
-- запасом в сутки, его и ищет индекс; строки чеков — по товару (решение 0064).
create index pos_receipts_created_idx on pos_receipts (created_on_device_at);
create index pos_receipt_lines_variant_idx on pos_receipt_lines (variant_id, receipt_id);
