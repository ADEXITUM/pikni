package egais

// Документы обмена в упрощённом формате имитатора (docs/design/govsim.md, п. 3.1). Названия
// типов — «по мотивам» ЕГАИС, XML-схемы не воспроизводятся.

const (
	WayBill    = "WayBill"
	WayBillAct = "WayBillAct"
	Ticket     = "Ticket"
	QueryRests = "QueryRests"
	ReplyRests = "ReplyRests"
	// ActWriteOffShop — списание из торгового зала, для пива — с причиной «Реализация».
	ActWriteOffShop = "ActWriteOffShop"
)

// TTN — товарно-транспортная накладная от поставщика.
type TTN struct {
	ID        string        `json:"ttn_id,omitempty"`
	Number    string        `json:"number"`
	Date      string        `json:"date"`
	Shipper   Party         `json:"shipper"`
	Consignee string        `json:"consignee"`
	Positions []TTNPosition `json:"positions"`
	Packages  []TTNPackage  `json:"packages"`
}

type Party struct {
	FSRARID string `json:"fsrar_id"`
	Name    string `json:"name"`
	INN     string `json:"inn"`
	KPP     string `json:"kpp"`
}

type TTNPosition struct {
	Identity int      `json:"identity"`
	Alcocode string   `json:"alcocode"`
	Name     string   `json:"name"`
	Qty      int      `json:"qty"`
	Price    float64  `json:"price"`
	Marked   bool     `json:"marked"`
	Marks    []string `json:"marks,omitempty"`
}

type TTNPackage struct {
	Code     string   `json:"code"`
	Identity int      `json:"identity"`
	Marks    []string `json:"marks,omitempty"`
	Qty      int      `json:"qty"`
}

// Act — акт подтверждения или расхождения: сколько и какие марки приняты по каждой позиции.
type Act struct {
	TTNID     string        `json:"ttn_id"`
	Positions []ActPosition `json:"positions"`
}

type ActPosition struct {
	Identity    int      `json:"identity"`
	QtyAccepted int      `json:"qty_accepted"`
	Marks       []string `json:"marks,omitempty"`
}

// TicketReply — квитанция ЕГАИС по отправленному документу.
type TicketReply struct {
	ReplyID string `json:"reply_id"`
	DocType string `json:"doc_type"`
	Result  string `json:"result"`
	Reason  string `json:"reason,omitempty"`
}
