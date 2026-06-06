package secret

import "testing"

func TestEncryptDecryptRoundTrip(t *testing.T) {
	codec, err := NewCodec("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}

	ciphertext, err := codec.Encrypt("sk-test-secret")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if ciphertext == "" || ciphertext == "sk-test-secret" {
		t.Fatalf("Encrypt() returned unsafe ciphertext: %q", ciphertext)
	}

	plaintext, err := codec.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if plaintext != "sk-test-secret" {
		t.Fatalf("Decrypt() = %q, want %q", plaintext, "sk-test-secret")
	}
}

func TestDecryptWithWrongSecretFails(t *testing.T) {
	codec, err := NewCodec("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	ciphertext, err := codec.Encrypt("sk-test-secret")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	wrongCodec, err := NewCodec("abcdef0123456789abcdef0123456789")
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	if _, err := wrongCodec.Decrypt(ciphertext); err == nil {
		t.Fatal("Decrypt() with wrong secret succeeded, want error")
	}
}

func TestMaskAPIKey(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want string
	}{
		{name: "empty", key: "", want: ""},
		{name: "short", key: "abc", want: "****"},
		{name: "normal", key: "sk-1234567890abcdef", want: "sk-****cdef"},
		{name: "token plan", key: "tp-abcdef123456", want: "tp-****3456"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MaskAPIKey(tt.key); got != tt.want {
				t.Fatalf("MaskAPIKey(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}
