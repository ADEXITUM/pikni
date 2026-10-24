-- Очередь сообщений ЕГАИС (transactional outbox).

-- name: EnqueueMessage :one
insert into egais_messages (location_id, direction, doc_type, document_id, payload)
values (@location_id, 'out', @doc_type, @document_id, @payload)
returning id;

-- name: ClaimQueued :many
-- Отправка очереди: несколько экземпляров сервера берут разные сообщения (skip locked).
select m.id, m.location_id, l.fsrar_id, m.doc_type, m.payload, m.attempts
from egais_messages m
join locations l on l.id = m.location_id
where m.direction = 'out' and m.status = 'queued' and m.next_attempt_at <= now()
order by m.created_at
limit 20
for update of m skip locked;

-- name: MarkSent :exec
update egais_messages
set status = 'sent', external_id = @external_id, attempts = attempts + 1, last_error = null, updated_at = now()
where id = @id;

-- name: MarkRetry :exec
update egais_messages
set attempts = attempts + 1, last_error = @last_error, next_attempt_at = @next_attempt_at, updated_at = now()
where id = @id;

-- name: ExchangeLocations :many
select id, fsrar_id::text as fsrar_id
from locations
where is_active and fsrar_id is not null;

-- name: InsertInbound :one
insert into egais_messages (location_id, direction, doc_type, external_id, payload, status)
values (@location_id, 'in', @doc_type, @external_id, @payload, 'processed')
on conflict (location_id, direction, external_id) where external_id is not null do nothing
returning id;

-- name: LinkInbound :exec
update egais_messages set document_id = @document_id where id = @id;

-- name: FindSent :one
select id, document_id, doc_type
from egais_messages
where location_id = @location_id and direction = 'out' and external_id = @external_id
for update;

-- name: SetTicket :exec
update egais_messages set status = @status, ticket = @ticket, updated_at = now() where id = @id;

-- name: DocumentPending :one
-- Сколько исходящих сообщений документа ещё без квитанции и сколько отклонено.
select count(*) filter (where status in ('queued', 'sent'))::integer as pending,
       count(*) filter (where status = 'rejected')::integer as rejected
from egais_messages
where document_id = @document_id and direction = 'out';

-- name: DocumentExchange :many
select id, direction, doc_type, status, attempts, last_error, next_attempt_at, created_at, updated_at,
       coalesce(ticket ->> 'reason', '')::text as reason
from egais_messages
where document_id = @document_id
order by created_at, id;

-- name: InsertRestsSnapshot :exec
insert into egais_rests_snapshots (location_id, payload) values (@location_id, @payload);

