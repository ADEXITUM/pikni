package web

import (
	"bytes"
	"encoding/base64"
	"html/template"
	"image/png"

	"github.com/boombuler/barcode"
	"github.com/boombuler/barcode/code128"
	"github.com/boombuler/barcode/datamatrix"
	"github.com/boombuler/barcode/ean"
)

// Штрихкоды печатной страницы — картинки PNG прямо в HTML: страницу можно распечатать или
// сканировать камерой с экрана.
func image(code barcode.Barcode, err error, w, h int) template.URL {
	if err != nil {
		return ""
	}
	scaled, err := barcode.Scale(code, w, h)
	if err != nil {
		return ""
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, scaled); err != nil {
		return ""
	}
	return template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()))
}

func dataMatrix(s string) template.URL {
	c, err := datamatrix.Encode(s)
	return image(c, err, 120, 120)
}

func ean13(s string) template.URL {
	c, err := ean.Encode(s)
	return image(c, err, 220, 70)
}

// sscc печатает номер короба кодом 128 с идентификатором применения (00), как GS1-128.
func sscc(s string) template.URL {
	c, err := code128.Encode("00" + s)
	return image(c, err, 320, 70)
}
