// Package password хеширует пароли (Argon2id), проверяет их и задаёт правила.
package password

import (
	"crypto/rand"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/alexedwards/argon2id"
)

// Рекомендация OWASP для Argon2id: 19 МиБ памяти, 2 прохода, 1 поток.
var params = &argon2id.Params{Memory: 19 * 1024, Iterations: 2, Parallelism: 1, SaltLength: 16, KeyLength: 32}

var (
	ErrTooShort    = errors.New("password is shorter than 8 characters")
	ErrTooLong     = errors.New("password is longer than 128 characters")
	ErrSameAsLogin = errors.New("password equals the login")
)

func Hash(plain string) (string, error) {
	return argon2id.CreateHash(plain, params)
}

func Check(plain, hash string) (bool, error) {
	return argon2id.ComparePasswordAndHash(plain, hash)
}

func Validate(login, plain string) error {
	switch n := utf8.RuneCountInString(plain); {
	case n < 8:
		return ErrTooShort
	case n > 128:
		return ErrTooLong
	case strings.EqualFold(plain, login):
		return ErrSameAsLogin
	}
	return nil
}

// Без похожих друг на друга l, o, 0 и 1. Ровно 32 символа: остаток от деления байта
// на 32 распределён равномерно, и все символы выпадают одинаково часто.
const alphabet = "abcdefghijkmnpqrstuvwxyz23456789"

func Temporary() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b)
}
