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

// sealedColumns hold tokens for other apps, by table and its key column.
var sealedColumns = []struct{ table, key, col string }{
	{"user_settings", "user_id", "paperless_token"},
	{"user_settings", "user_id", "homebox_token"},
	{"wherehouse_connections", "id", "token"},
	{"wherehouse_connections", "id", "hook_secret"},
}

// SealTokens encrypts tokens stored before they were encrypted.
func (s *Store) SealTokens(ctx context.Context) error {
	if s.aead == nil {
		return nil
	}
	for _, c := range sealedColumns {
		rows, err := s.db.QueryContext(ctx, `SELECT `+c.key+`, `+c.col+` FROM `+c.table+` WHERE `+c.col+` != ''
			AND `+c.col+` NOT LIKE 'enc1:%'`)
		if err != nil {
			return err
		}
		type plain struct {
			key   int64
			value string
		}
		var todo []plain
		for rows.Next() {
			var p plain
			if err := rows.Scan(&p.key, &p.value); err != nil {
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
			if _, err := s.db.ExecContext(ctx, `UPDATE `+c.table+` SET `+c.col+` = ? WHERE `+c.key+` = ?`, v, p.key); err != nil {
				return err
			}
		}
	}
	return nil
}
