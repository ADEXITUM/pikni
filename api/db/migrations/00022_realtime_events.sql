-- +goose Up
-- События реального времени (УВ-1). Изменения остатков, документов, сканов, обмена с ЕГАИС и
-- чеков объявляются триггерами в канал pikni_events: ни один путь изменения не забудет событие,
-- а доставляется оно только после фиксации транзакции. Событие — тип, идентификатор и темы
-- подписки; данные клиент перезапрашивает обычным запросом, где действуют права.

-- +goose StatementBegin
create function pikni_event(kind text, id uuid, topics text[]) returns void
language sql as $$
    select pg_notify('pikni_events', json_build_object('type', kind, 'id', id, 'topics', topics)::text);
$$;
-- +goose StatementEnd

-- +goose StatementBegin
create function notify_document() returns trigger language plpgsql as $$
begin
    perform pikni_event('document', new.id, array['doc:' || new.id, 'location:' || new.location_id]);
    return null;
end;
$$;
-- +goose StatementEnd

create trigger documents_events after insert or update on documents
    for each row execute function notify_document();

-- +goose StatementBegin
create function notify_scan() returns trigger language plpgsql as $$
begin
    perform pikni_event('scan', new.document_id, array['doc:' || new.document_id]);
    return null;
end;
$$;
-- +goose StatementEnd

create trigger scans_events after insert or update on scans
    for each row execute function notify_scan();

-- +goose StatementBegin
create function notify_stock() returns trigger language plpgsql as $$
begin
    perform pikni_event('stock', new.variant_id, array[
        'location:' || new.location_id,
        'product:' || (select v.product_id from variants v where v.id = new.variant_id)]);
    return null;
end;
$$;
-- +goose StatementEnd

create trigger stock_balances_events after insert or update on stock_balances
    for each row execute function notify_stock();

-- +goose StatementBegin
create function notify_exchange() returns trigger language plpgsql as $$
begin
    if new.document_id is not null then
        perform pikni_event('exchange', new.document_id, array['doc:' || new.document_id]);
    end if;
    return null;
end;
$$;
-- +goose StatementEnd

create trigger egais_messages_events after insert or update on egais_messages
    for each row execute function notify_exchange();

-- +goose StatementBegin
create function notify_receipt() returns trigger language plpgsql as $$
begin
    perform pikni_event('receipt', new.id, array['shift:' || new.shift_id]);
    return null;
end;
$$;
-- +goose StatementEnd

create trigger pos_receipts_events after insert on pos_receipts
    for each row execute function notify_receipt();
