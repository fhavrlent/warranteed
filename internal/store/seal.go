package store

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
)

// sealed marks a value encrypted with the server's key. Values without it
// are from before tokens were encrypted and are read as they are.
const sealed = "enc1:"

// SetKey sets the key that tokens for other apps are encrypted with in the
// database: Paperless, HomeBox and Wherehouse tokens and Wherehouse's ping
// secret. Without one, as in tests, they're stored as they are.
func (s *Store) SetKey(key []byte) error {
	if key == nil {
		s.aead = nil
		return nil
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	s.aead, err = cipher.NewGCM(block)
	return err
}

func (s *Store) seal(plain string) (string, error) {
	if plain == "" || s.aead == nil {
		return plain, nil
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return sealed + base64.RawStdEncoding.EncodeToString(s.aead.Seal(nonce, nonce, []byte(plain), nil)), nil
}

func (s *Store) open(value string) (string, error) {
	rest, ok := strings.CutPrefix(value, sealed)
	if !ok {
		return value, nil
	}
	if s.aead == nil {
		return "", errors.New("a token is encrypted, but no key is set")
	}
	raw, err := base64.RawStdEncoding.DecodeString(rest)
	if err != nil || len(raw) < s.aead.NonceSize() {
		return "", errors.New("a stored token is damaged")
	}
	plain, err := s.aead.Open(nil, raw[:s.aead.NonceSize()], raw[s.aead.NonceSize():], nil)
	if err != nil {
		return "", errors.New("a stored token can't be read with this key; was WARRANTEED_SECRET or secret.key changed?")
	}
	return string(plain), nil
}

// sealedColumns hold tokens for other apps.
var sealedColumns = []string{"paperless_token", "homebox_token", "wherehouse_token", "wherehouse_hook_secret"}

// SealTokens encrypts tokens stored before they were encrypted.
func (s *Store) SealTokens(ctx context.Context) error {
	if s.aead == nil {
		return nil
	}
	for _, col := range sealedColumns {
		rows, err := s.db.QueryContext(ctx, `SELECT user_id, `+col+` FROM user_settings WHERE `+col+` != '' AND `+col+` NOT LIKE 'enc1:%'`)
		if err != nil {
			return err
		}
		type plain struct {
			user  int64
			value string
		}
		var todo []plain
		for rows.Next() {
			var p plain
			if err := rows.Scan(&p.user, &p.value); err != nil {
				rows.Close()
				return err
			}
			todo = append(todo, p)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, p := range todo {
			v, err := s.seal(p.value)
			if err != nil {
				return err
			}
			if _, err := s.db.ExecContext(ctx, `UPDATE user_settings SET `+col+` = ? WHERE user_id = ?`, v, p.user); err != nil {
				return err
			}
		}
	}
	return nil
}
