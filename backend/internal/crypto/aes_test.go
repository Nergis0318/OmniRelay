package crypto

import (
	"encoding/hex"
	"strings"
	"testing"
)

// 32 bytes = 64 hex chars
const testKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestEncryptDecryptRoundTrip(t *testing.T) {
	for _, plaintext := range []string{
		"",
		"hello world",
		"sk-proj-Abc123...",
		"한국어 secret with unicode 🔒",
		strings.Repeat("x", 4096),
	} {
		t.Run(plaintext, func(t *testing.T) {
			ct, err := Encrypt(plaintext, testKey)
			if err != nil {
				t.Fatalf("Encrypt: %v", err)
			}
			pt, err := Decrypt(ct, testKey)
			if err != nil {
				t.Fatalf("Decrypt: %v", err)
			}
			if pt != plaintext {
				t.Fatalf("round-trip mismatch: got %q, want %q", pt, plaintext)
			}
		})
	}
}

func TestEncryptProducesDifferentCiphertextEachCall(t *testing.T) {
	// Random nonce should make repeated Encrypt of the same plaintext produce different ciphertexts.
	a, err := Encrypt("same plaintext", testKey)
	if err != nil {
		t.Fatalf("first Encrypt: %v", err)
	}
	b, err := Encrypt("same plaintext", testKey)
	if err != nil {
		t.Fatalf("second Encrypt: %v", err)
	}
	if a == b {
		t.Fatalf("Encrypt should use a fresh nonce; got identical ciphertexts: %s", a)
	}
}

func TestEncryptRejectsBadKeyLength(t *testing.T) {
	// 31 bytes (62 hex chars) - too short
	if _, err := Encrypt("x", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcd"); err == nil {
		t.Fatal("expected error for short key")
	}
	// Not hex at all
	if _, err := Encrypt("x", "not-hex-at-all-not-hex-at-all-not-hex-at-all-not-hex-at-all-zzzz"); err == nil {
		t.Fatal("expected error for non-hex key")
	}
}

func TestDecryptRejectsTamperedCiphertext(t *testing.T) {
	ct, err := Encrypt("secret", testKey)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	// Flip a byte in the ciphertext (after the nonce) and expect Open to fail.
	raw, _ := hex.DecodeString(ct)
	raw[len(raw)-1] ^= 0xFF
	tampered := hex.EncodeToString(raw)
	if _, err := Decrypt(tampered, testKey); err == nil {
		t.Fatal("Decrypt should reject tampered ciphertext (GCM auth tag mismatch)")
	}
}

func TestDecryptRejectsTooShortCiphertext(t *testing.T) {
	// 4 bytes = 8 hex chars; well below nonceSize (12).
	if _, err := Decrypt("deadbeef", testKey); err == nil {
		t.Fatal("Decrypt should reject ciphertext shorter than nonce size")
	}
}

func TestDecryptRejectsNonHexCiphertext(t *testing.T) {
	if _, err := Decrypt("nothex!!", testKey); err == nil {
		t.Fatal("Decrypt should reject non-hex ciphertext")
	}
}

func TestDecryptRejectsWrongKey(t *testing.T) {
	ct, err := Encrypt("secret", testKey)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	wrongKey := "1111111111111111111111111111111111111111111111111111111111111111"
	if _, err := Decrypt(ct, wrongKey); err == nil {
		t.Fatal("Decrypt should fail with wrong key (GCM auth tag mismatch)")
	}
}
