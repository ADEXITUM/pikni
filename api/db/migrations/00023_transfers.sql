-- +goose Up
-- Перемещение между точками (СО-4). Документ ведёт точка-отправитель (location_id, zone_id — её
-- склад), получатель — dest_location_id и его склад dest_zone_id. Жизненный цикл:
-- requested (заявка получателя) → draft (согласовано, сборка) | rejected → in_transit (отгружено,
-- ТТН в ЕГАИС) → posted (получатель провёл поступление по этой ТТН). Поступление ссылается на
-- перемещение через transfer_id.

alter table documents drop constraint documents_kind_check;
alter table documents add constraint documents_kind_check
    check (kind in ('opening', 'floor_transfer', 'receipt', 'sale', 'transfer'));

alter table documents drop constraint documents_status_check;
alter table documents add constraint documents_status_check
    check (status in ('requested', 'rejected', 'draft', 'in_transit', 'awaiting_egais', 'posted',
                      'egais_rejected', 'cancelled'));

alter table documents drop constraint documents_check;
alter table documents add constraint documents_dest_zone_check
    check ((kind in ('floor_transfer', 'transfer')) = (dest_zone_id is not null));

alter table documents
    add column dest_location_id uuid references locations (id),
    add column transfer_id      uuid references documents (id);

alter table documents add constraint documents_transfer_check
    check ((kind = 'transfer') = (dest_location_id is not null) and dest_location_id is distinct from location_id);

create index documents_dest_location_idx on documents (dest_location_id, created_at desc)
    where dest_location_id is not null;
create index documents_transfer_idx on documents (transfer_id) where transfer_id is not null;

-- Событие о документе получают и экраны точки-получателя.
-- +goose StatementBegin
create or replace function notify_document() returns trigger language plpgsql as $$
begin
    perform pikni_event('document', new.id, array_remove(array[
        'doc:' || new.id, 'location:' || new.location_id, 'location:' || new.dest_location_id], null));
    return null;
end;
$$;
-- +goose StatementEnd
