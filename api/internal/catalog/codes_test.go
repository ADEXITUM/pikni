package catalog

import (
	"errors"
	"testing"
)

func TestBarcodeKind(t *testing.T) {
	t.Parallel()
	tests := []struct {
		code, kind string
		err        error
	}{
		{"4006381333931", "ean13", nil},
		{"4006381333932", "", ErrInvalidBarcode},
		{"73513537", "ean8", nil},
		{"036000291452", "upca", nil},
		{"2000000000015", "internal", nil},
		{"PK-BOX-0001", "code128", nil},
		{"12345", "code128", nil},
		{"штрихкод", "", ErrInvalidBarcode},
		{"", "", ErrInvalidBarcode},
	}
	for _, tt := range tests {
		kind, err := barcodeKind(tt.code)
		if kind != tt.kind || !errors.Is(err, tt.err) {
			t.Errorf("barcodeKind(%q) = %q, %v; want %q, %v", tt.code, kind, err, tt.kind, tt.err)
		}
	}
}

func TestInternalBarcode(t *testing.T) {
	t.Parallel()
	for n, want := range map[int64]string{1: "2000000000015", 2: "2000000000022", 3: "2000000000039"} {
		if got := internalBarcode(n); got != want || !gs1Valid(got) {
			t.Errorf("internalBarcode(%d) = %s, want %s", n, got, want)
		}
	}
}

func TestNormalizeGTIN(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{"4006381333931": "04006381333931", "04006381333931": "04006381333931", "73513537": "00000073513537"} {
		got, err := normalizeGTIN(&in)
		if err != nil || *got != want {
			t.Errorf("normalizeGTIN(%s) = %v, %v; want %s", in, got, err, want)
		}
	}
	for _, in := range []string{"4006381333932", "123", "40063813339310"} {
		if _, err := normalizeGTIN(&in); !errors.Is(err, ErrInvalidGTIN) {
			t.Errorf("normalizeGTIN(%s) error = %v", in, err)
		}
	}
}
