package catalog

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var (
	ErrInvalidBarcode  = errors.New("invalid barcode")
	ErrInvalidGTIN     = errors.New("invalid GTIN")
	ErrInvalidAlcocode = errors.New("alcocode must be 19 digits")
)

var (
	digitsOnly      = regexp.MustCompile(`^[0-9]+$`)
	alcocodePattern = regexp.MustCompile(`^[0-9]{19}$`)
)

// gs1CheckDigit считает контрольную цифру GS1 (EAN, UPC, GTIN): цифры справа налево
// умножаются попеременно на 3 и 1, контрольная дополняет сумму до кратной 10.
func gs1CheckDigit(payload string) byte {
	sum := 0
	for i := len(payload) - 1; i >= 0; i-- {
		d := int(payload[i] - '0')
		if (len(payload)-1-i)%2 == 0 {
			d *= 3
		}
		sum += d
	}
	return byte('0' + (10-sum%10)%10)
}

func gs1Valid(code string) bool {
	return len(code) > 1 && digitsOnly.MatchString(code) && gs1CheckDigit(code[:len(code)-1]) == code[len(code)-1]
}

// barcodeKind определяет вид штрихкода. Цифровые EAN-8, UPC-A и EAN-13 проверяются по
// контрольной цифре; EAN-13 с префиксом 2 — внутренний. Остальное считается Code 128.
func barcodeKind(code string) (string, error) {
	if code == "" || len(code) > 48 {
		return "", ErrInvalidBarcode
	}
	if digitsOnly.MatchString(code) {
		kind := map[int]string{8: "ean8", 12: "upca", 13: "ean13"}[len(code)]
		if kind != "" {
			if !gs1Valid(code) {
				return "", ErrInvalidBarcode
			}
			if kind == "ean13" && code[0] == '2' {
				return "internal", nil
			}
			return kind, nil
		}
	}
	for _, r := range code {
		if r < ' ' || r > '~' {
			return "", ErrInvalidBarcode
		}
	}
	return "code128", nil
}

// internalBarcode — внутренний EAN-13: префикс 20, номер из последовательности и контрольная цифра.
func internalBarcode(n int64) string {
	payload := fmt.Sprintf("20%010d", n)
	return payload + string(gs1CheckDigit(payload))
}

// normalizeGTIN принимает GTIN-8, -12, -13 или -14 и возвращает 14 цифр, как в коде «Честного знака».
func normalizeGTIN(s *string) (*string, error) {
	v := optional(s)
	if v == nil {
		return nil, nil
	}
	switch len(*v) {
	case 8, 12, 13, 14:
	default:
		return nil, ErrInvalidGTIN
	}
	if !gs1Valid(*v) {
		return nil, ErrInvalidGTIN
	}
	gtin := strings.Repeat("0", 14-len(*v)) + *v
	return &gtin, nil
}

func normalizeAlcocode(s *string) (*string, error) {
	v := optional(s)
	if v != nil && !alcocodePattern.MatchString(*v) {
		return nil, ErrInvalidAlcocode
	}
	return v, nil
}
