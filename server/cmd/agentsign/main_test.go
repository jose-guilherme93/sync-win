package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func TestParsePrivateKeyAcceptsSeedAndFullKey(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for _, encoded := range []string{
		base64.StdEncoding.EncodeToString(priv.Seed()),
		base64.StdEncoding.EncodeToString(priv),
	} {
		parsed, err := parsePrivateKey(encoded)
		if err != nil {
			t.Fatalf("parsePrivateKey: %v", err)
		}
		if !parsed.Public().(ed25519.PublicKey).Equal(pub) {
			t.Fatal("derived public key does not match")
		}
	}
}

func TestParsePrivateKeyRejectsBadInput(t *testing.T) {
	if _, err := parsePrivateKey("not-base64!!"); err == nil {
		t.Fatal("expected error for invalid base64")
	}
	if _, err := parsePrivateKey(base64.StdEncoding.EncodeToString([]byte("short"))); err == nil {
		t.Fatal("expected error for wrong key length")
	}
}
