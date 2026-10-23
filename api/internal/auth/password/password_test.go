package password

import (
	"errors"
	"strings"
	"testing"
)

func TestHashAndCheck(t *testing.T) {
	hash, err := Hash("кедровая-шишка")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Errorf("hash = %q, want PHC string with OWASP params", hash)
	}

	for plain, want := range map[string]bool{"кедровая-шишка": true, "Кедровая-шишка": false, "": false} {
		ok, err := Check(plain, hash)
		if err != nil || ok != want {
			t.Errorf("Check(%q) = %v, %v; want %v", plain, ok, err, want)
		}
	}
}

func TestHashUsesRandomSalt(t *testing.T) {
	a, _ := Hash("одинаковый")
	b, _ := Hash("одинаковый")
	if a == b {
		t.Error("two hashes of the same password are equal")
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		login, plain string
		want         error
	}{
		{"petrova", "1234567", ErrTooShort},
		{"petrova", "пароль12", nil},
		{"petrova", strings.Repeat("я", 128), nil},
		{"petrova", strings.Repeat("я", 129), ErrTooLong},
		{"petrova123", "Petrova123", ErrSameAsLogin},
	}
	for _, tt := range tests {
		if err := Validate(tt.login, tt.plain); !errors.Is(err, tt.want) {
			t.Errorf("Validate(%q, %q) = %v, want %v", tt.login, tt.plain, err, tt.want)
		}
	}
}

func TestTemporary(t *testing.T) {
	a, b := Temporary(), Temporary()
	if len(a) != 12 || strings.Trim(a, alphabet) != "" {
		t.Errorf("Temporary() = %q, want 12 chars from alphabet", a)
	}
	if a == b {
		t.Error("two temporary passwords are equal")
	}
	if err := Validate("admin", a); err != nil {
		t.Errorf("temporary password breaks the rules: %v", err)
	}
}
