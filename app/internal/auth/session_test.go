package auth

import "testing"

func TestSessionTokenHashValidation(t *testing.T) {
	token, err := GenerateSessionToken()
	if err != nil {
		t.Fatalf("GenerateSessionToken() error = %v", err)
	}

	hash := HashToken(token)
	if !TokenMatchesHash(token, hash) {
		t.Fatalf("expected token hash match to pass")
	}

	if TokenMatchesHash("different-token", hash) {
		t.Fatalf("expected token hash match to fail for different token")
	}
}
