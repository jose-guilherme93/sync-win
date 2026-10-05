// Command agentsign generates the Ed25519 keypair that authenticates agent
// auto-updates and signs a built agent binary with it.
//
// Key management: keep the private key out of the repository and pass it to the
// Docker build as a BuildKit secret. The matching public key is injected into
// the agent binary at build time (see docker/Dockerfile.server).
//
//	agentsign -genkey
//	agentsign -key "$PRIVATE" -derive-public
//	agentsign -key "$PRIVATE" -in lem-agent -out lem-agent.sig
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
)

func main() {
	genkey := flag.Bool("genkey", false, "generate a new keypair (prints base64 private and public keys)")
	derivePublic := flag.Bool("derive-public", false, "print the base64 public key for -key")
	key := flag.String("key", "", "base64 Ed25519 private key (32-byte seed or 64-byte key)")
	in := flag.String("in", "", "input file to sign")
	out := flag.String("out", "", "output file for the base64 signature")
	flag.Parse()

	if *genkey {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			fail(err)
		}
		fmt.Printf("private:%s\n", base64.StdEncoding.EncodeToString(priv))
		fmt.Printf("public:%s\n", base64.StdEncoding.EncodeToString(pub))
		return
	}

	priv, err := parsePrivateKey(*key)
	if err != nil {
		fail(err)
	}

	if *derivePublic {
		fmt.Println(base64.StdEncoding.EncodeToString(priv.Public().(ed25519.PublicKey)))
		return
	}

	if *in == "" || *out == "" {
		fail(fmt.Errorf("-in and -out are required"))
	}
	data, err := os.ReadFile(*in)
	if err != nil {
		fail(err)
	}
	signature := ed25519.Sign(priv, data)
	if err := os.WriteFile(*out, []byte(base64.StdEncoding.EncodeToString(signature)), 0o644); err != nil {
		fail(err)
	}
}

func parsePrivateKey(encoded string) (ed25519.PrivateKey, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode key: %w", err)
	}
	switch len(raw) {
	case ed25519.SeedSize:
		return ed25519.NewKeyFromSeed(raw), nil
	case ed25519.PrivateKeySize:
		return ed25519.PrivateKey(raw), nil
	default:
		return nil, fmt.Errorf("key must be a %d-byte seed or %d-byte private key", ed25519.SeedSize, ed25519.PrivateKeySize)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "agentsign:", err)
	os.Exit(1)
}
