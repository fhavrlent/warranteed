package store_test

import (
	"context"
	"testing"
)

func TestSealedTokens(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	u := user(t, s, "a")
	st, err := s.Settings(ctx, u)
	if err != nil {
		t.Fatal(err)
	}
	// Stored before tokens were encrypted.
	st.PaperlessToken, st.HomeBoxToken = "pl-token", "hb-token"
	if err := s.SaveSettings(ctx, u, st); err != nil {
		t.Fatal(err)
	}

	key := make([]byte, 32)
	if err := s.SetKey(key); err != nil {
		t.Fatal(err)
	}
	if err := s.SealTokens(ctx); err != nil {
		t.Fatal(err)
	}
	conn, err := s.SaveWherehouseConn(ctx, u, 1, "Home", "wh-token")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetWherehouseHookSecret(ctx, conn, "hook-secret"); err != nil {
		t.Fatal(err)
	}
	st, err = s.Settings(ctx, u)
	if err != nil {
		t.Fatal(err)
	}
	if st.PaperlessToken != "pl-token" || st.HomeBoxToken != "hb-token" {
		t.Errorf("tokens read back: %q %q", st.PaperlessToken, st.HomeBoxToken)
	}
	if conns, err := s.WherehouseConns(ctx, u); err != nil || len(conns) != 1 || conns[0].Token != "wh-token" {
		t.Errorf("wherehouse connections = %+v, %v", conns, err)
	}
	if hooks, err := s.WherehouseHooks(ctx); err != nil || len(hooks) != 1 || hooks[0].Secret != "hook-secret" {
		t.Errorf("hooks = %+v, %v", hooks, err)
	}

	// They're stored encrypted: another key can't read them, nor no key.
	other := make([]byte, 32)
	other[0] = 1
	s.SetKey(other)
	if _, err := s.Settings(ctx, u); err == nil {
		t.Error("tokens read with another key")
	}
	if _, err := s.WherehouseConns(ctx, u); err == nil {
		t.Error("wherehouse token read with another key")
	}
	if _, err := s.WherehouseHooks(ctx); err == nil {
		t.Error("ping secret read with another key")
	}
	s.SetKey(nil)
	if _, err := s.Settings(ctx, u); err == nil {
		t.Error("tokens read without the key")
	}
}
