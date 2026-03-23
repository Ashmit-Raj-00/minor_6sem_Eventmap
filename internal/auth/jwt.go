package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var (
	ErrTokenMalformed = errors.New("token malformed")
	ErrTokenInvalid   = errors.New("token invalid")
	ErrTokenExpired   = errors.New("token expired")
)

type Claims struct {
	Sub   string `json:"sub"`
	Role  string `json:"role"`
	Iat   int64  `json:"iat"`
	Exp   int64  `json:"exp"`
}

func SignHS256(secret []byte, claims Claims) (string, error) {
	headerJSON, err := json.Marshal(map[string]string{
		"alg": "HS256",
		"typ": "JWT",
	})
	if err != nil {
		return "", err
	}

	payloadJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}

	enc := base64.RawURLEncoding
	header := enc.EncodeToString(headerJSON)
	payload := enc.EncodeToString(payloadJSON)
	unsigned := header + "." + payload
	sig := signHS256(secret, unsigned)
	return unsigned + "." + enc.EncodeToString(sig), nil
}

func VerifyHS256(secret []byte, token string, now time.Time) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, ErrTokenMalformed
	}
	enc := base64.RawURLEncoding

	headerJSON, err := enc.DecodeString(parts[0])
	if err != nil {
		return Claims{}, ErrTokenMalformed
	}
	var header map[string]any
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return Claims{}, ErrTokenMalformed
	}
	if header["alg"] != "HS256" {
		return Claims{}, ErrTokenInvalid
	}
	if typ, ok := header["typ"]; ok && typ != "JWT" {
		return Claims{}, ErrTokenInvalid
	}

	payloadJSON, err := enc.DecodeString(parts[1])
	if err != nil {
		return Claims{}, ErrTokenMalformed
	}
	var claims Claims
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		return Claims{}, ErrTokenMalformed
	}

	sig, err := enc.DecodeString(parts[2])
	if err != nil {
		return Claims{}, ErrTokenMalformed
	}

	unsigned := parts[0] + "." + parts[1]
	expected := signHS256(secret, unsigned)
	if !hmac.Equal(sig, expected) {
		return Claims{}, ErrTokenInvalid
	}
	if claims.Exp > 0 && now.Unix() >= claims.Exp {
		return Claims{}, ErrTokenExpired
	}
	return claims, nil
}

func signHS256(secret []byte, msg string) []byte {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(msg))
	return mac.Sum(nil)
}
