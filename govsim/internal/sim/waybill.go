package sim

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// wayBill — ТТН, которую отправляет точка сети другой точке (перемещение, СО-4). Марки должны
// числиться на балансе отправителя, количество продукции без марок — на его регистре 1 (склад).
// Принятая ТТН списывает продукцию с отправителя, марки уходят «в пути», а ТТН попадает во
// «входящие» получателя. Идентификатор ТТН — идентификатор этого документа: по нему получатель
// пришлёт акт.
func wayBill(ctx context.Context, tx pgx.Tx, fsrar string, doc uuid.UUID, payload []byte) error {
	var ttn TTN
	if err := json.Unmarshal(payload, &ttn); err != nil {
		return reject("ТТН не разобрана: %v", err)
	}
	if ttn.Consignee == "" || ttn.Consignee == fsrar {
		return reject("Не указан получатель ТТН")
	}
	var shipper Party
	err := tx.QueryRow(ctx, `select fsrar_id, name, inn, kpp from participants where fsrar_id = $1`, fsrar).
		Scan(&shipper.FSRARID, &shipper.Name, &shipper.INN, &shipper.KPP)
	if err != nil {
		return err
	}
	var exists bool
	if err := tx.QueryRow(ctx, `select exists (select 1 from participants where fsrar_id = $1)`, ttn.Consignee).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return reject("Получатель %s не зарегистрирован в ЕГАИС", ttn.Consignee)
	}
	if len(ttn.Positions) == 0 {
		return reject("В ТТН нет позиций")
	}
	for _, p := range ttn.Positions {
		if p.Qty <= 0 {
			return reject("Позиция %d: количество должно быть больше нуля", p.Identity)
		}
		if !p.Marked {
			n, err := tx.Exec(ctx, `update rests set qty = qty - $3
				where fsrar_id = $1 and alcocode = $2 and register = 1 and qty >= $3`, fsrar, p.Alcocode, p.Qty)
			if err != nil {
				return err
			}
			if n.RowsAffected() == 0 {
				return reject("Позиция %d (%s): на складе отправителя меньше %d", p.Identity, p.Alcocode, p.Qty)
			}
			continue
		}
		if len(p.Marks) != p.Qty {
			return reject("Позиция %d: %d шт., а марок %d", p.Identity, p.Qty, len(p.Marks))
		}
		var ok int
		if err := tx.QueryRow(ctx, `select count(*) from marks where code = any($1) and owner_fsrar = $2
			and status = 'on_balance' and not blocked and alcocode = $3`, p.Marks, fsrar, p.Alcocode).Scan(&ok); err != nil {
			return err
		}
		if ok != len(p.Marks) {
			return reject("Позиция %d: не все марки на балансе отправителя (%d из %d)", p.Identity, ok, len(p.Marks))
		}
		if _, err := tx.Exec(ctx, `update marks set status = 'in_transit', ttn_id = $2, updated_at = now()
			where code = any($1)`, p.Marks, doc); err != nil {
			return err
		}
		if err := markEvent(ctx, tx, p.Marks, "Отгружена по ТТН "+ttn.Number, fsrar, doc); err != nil {
			return err
		}
	}
	ttn.ID, ttn.Shipper = doc, shipper
	data, err := json.Marshal(ttn)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `update documents set fsrar_to = $2, payload = $3 where id = $1`, doc, ttn.Consignee, data); err != nil {
		return err
	}
	return put(ctx, tx, ttn.Consignee, "WayBill", ttn, &doc)
}

// returnToShipper возвращает отправителю-точке непринятое количество продукции без марок:
// по ТТН оно уже списано с его регистра. Поставщику имитатор остатки не ведёт.
func returnToShipper(ctx context.Context, tx pgx.Tx, shipper, alcocode string, qty int) error {
	if qty <= 0 {
		return nil
	}
	var kind string
	err := tx.QueryRow(ctx, `select kind from participants where fsrar_id = $1`, shipper).Scan(&kind)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && kind == "supplier" {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `insert into rests (fsrar_id, alcocode, register, qty) values ($1, $2, 1, $3)
		on conflict (fsrar_id, alcocode, register) do update set qty = rests.qty + excluded.qty`, shipper, alcocode, qty)
	return err
}
