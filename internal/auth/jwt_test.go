package auth

import (
	"testing"
	"time"
)

func TestJWT(t *testing.T) {
	secret := []byte("super_secret_key")
	now := time.Now()

	claims := Claims{
		Sub:  "user-123",
		Role: "admin",
		Iat:  now.Unix(),
		Exp:  now.Add(1 * time.Hour).Unix(),
	}

	// Test Signing
	token, err := SignHS256(secret, claims)
	if err != nil {
		t.Fatalf("Failed to sign token: %v", err)
	}
	if token == "" {
		t.Fatal("Expected a non-empty token")
	}

	// Test Verification
	parsedClaims, err := VerifyHS256(secret, token, now)
	if err != nil {
		t.Fatalf("Failed to verify valid token: %v", err)
	}
	if parsedClaims.Sub != claims.Sub || parsedClaims.Role != claims.Role {
		t.Errorf("Claims mismatch. Expected %+v, got %+v", claims, parsedClaims)
	}

	// Test Expiration
	expiredNow := now.Add(2 * time.Hour)
	_, err = VerifyHS256(secret, token, expiredNow)
	if err != ErrTokenExpired {
		t.Errorf("Expected ErrTokenExpired, got: %v", err)
	}
}
