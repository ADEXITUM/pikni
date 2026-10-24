-- Очередь сообщений ЕГАИС (transactional outbox).

-- name: EnqueueMessage :one
insert into egais_messages (location_id, direction, doc_type, document_id, payload)
values (@location_id, 'out', @doc_type, @document_id, @payload)
returning id;
