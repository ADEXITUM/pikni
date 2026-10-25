-- +goose Up
-- Отчёты по точкам (остатки на дату, оборотная ведомость) выбирают движения точки: без индекса по
-- точке это полный просмотр журнала движений всей сети (решение 0064).
create index stock_moves_location_date_idx on stock_moves (location_id, doc_date);
