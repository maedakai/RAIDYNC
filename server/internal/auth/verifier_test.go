package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testIssuer   = "https://test-issuer.example.com"
	testClientID = "test-client"
	testKeyID    = "test-key-1"
)

// jwksFixture は RSA 鍵ペア + JWKS を提供する httptest サーバを束ねた検証用ヘルパー。
type jwksFixture struct {
	key    *rsa.PrivateKey
	server *httptest.Server
}

func newJWKSFixture(t *testing.T) *jwksFixture {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	jwks := map[string]any{
		"keys": []map[string]any{
			{
				"kty": "RSA",
				"use": "sig",
				"alg": "RS256",
				"kid": testKeyID,
				"n":   base64URLUint(key.PublicKey.N),
				"e":   base64URLUint(big.NewInt(int64(key.PublicKey.E))),
			},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jwks)
	}))
	t.Cleanup(srv.Close)
	return &jwksFixture{key: key, server: srv}
}

// sign は与えた claims から RS256 JWT を発行する。
func (f *jwksFixture) sign(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = testKeyID
	signed, err := token.SignedString(f.key)
	require.NoError(t, err)
	return signed
}

func base64URLUint(n *big.Int) string {
	return base64.RawURLEncoding.EncodeToString(n.Bytes())
}

func newTestVerifier(t *testing.T, f *jwksFixture, clientID string) *Verifier {
	t.Helper()
	v, err := New(context.Background(), Config{
		JWKSURL:  f.server.URL,
		Issuer:   testIssuer,
		ClientID: clientID,
	})
	require.NoError(t, err)
	return v
}

func TestVerify_IDTokenSuccess(t *testing.T) {
	f := newJWKSFixture(t)
	v := newTestVerifier(t, f, testClientID)

	tok := f.sign(t, jwt.MapClaims{
		"sub":       "player-42",
		"iss":       testIssuer,
		"aud":       testClientID,
		"exp":       time.Now().Add(time.Hour).Unix(),
		"iat":       time.Now().Unix(),
		"token_use": "id",
		"email":     "p42@example.com",
	})

	claims, err := v.Verify(tok)
	require.NoError(t, err)
	assert.Equal(t, "player-42", claims.PlayerID)
	assert.Equal(t, "id", claims.TokenUse)
	assert.Equal(t, "p42@example.com", claims.Email)
}

func TestVerify_AccessTokenSuccess(t *testing.T) {
	f := newJWKSFixture(t)
	v := newTestVerifier(t, f, testClientID)

	tok := f.sign(t, jwt.MapClaims{
		"sub":       "player-7",
		"iss":       testIssuer,
		"exp":       time.Now().Add(time.Hour).Unix(),
		"iat":       time.Now().Unix(),
		"token_use": "access",
		"client_id": testClientID,
	})

	claims, err := v.Verify(tok)
	require.NoError(t, err)
	assert.Equal(t, "player-7", claims.PlayerID)
	assert.Equal(t, "access", claims.TokenUse)
}

func TestVerify_RejectsExpired(t *testing.T) {
	f := newJWKSFixture(t)
	v := newTestVerifier(t, f, "")

	tok := f.sign(t, jwt.MapClaims{
		"sub": "player-1",
		"iss": testIssuer,
		"exp": time.Now().Add(-time.Minute).Unix(),
		"iat": time.Now().Add(-time.Hour).Unix(),
	})

	_, err := v.Verify(tok)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse")
}

func TestVerify_RejectsWrongIssuer(t *testing.T) {
	f := newJWKSFixture(t)
	v := newTestVerifier(t, f, "")

	tok := f.sign(t, jwt.MapClaims{
		"sub": "player-1",
		"iss": "https://attacker.example.com",
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	_, err := v.Verify(tok)
	require.Error(t, err)
}

func TestVerify_RejectsWrongAudience(t *testing.T) {
	f := newJWKSFixture(t)
	v := newTestVerifier(t, f, testClientID)

	tok := f.sign(t, jwt.MapClaims{
		"sub":       "player-1",
		"iss":       testIssuer,
		"aud":       "another-client",
		"exp":       time.Now().Add(time.Hour).Unix(),
		"token_use": "id",
	})

	_, err := v.Verify(tok)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "aud")
}

func TestVerify_RejectsWrongClientIDOnAccessToken(t *testing.T) {
	f := newJWKSFixture(t)
	v := newTestVerifier(t, f, testClientID)

	tok := f.sign(t, jwt.MapClaims{
		"sub":       "player-1",
		"iss":       testIssuer,
		"exp":       time.Now().Add(time.Hour).Unix(),
		"token_use": "access",
		"client_id": "another-client",
	})

	_, err := v.Verify(tok)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "client_id")
}

func TestVerify_RejectsTamperedSignature(t *testing.T) {
	f := newJWKSFixture(t)
	v := newTestVerifier(t, f, "")

	tok := f.sign(t, jwt.MapClaims{
		"sub": "player-1",
		"iss": testIssuer,
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	// 末尾文字を 1 つ書き換えて改ざんをシミュレート
	tampered := tok[:len(tok)-1] + flipChar(tok[len(tok)-1])

	_, err := v.Verify(tampered)
	require.Error(t, err)
}

func TestVerify_RejectsMissingSub(t *testing.T) {
	f := newJWKSFixture(t)
	v := newTestVerifier(t, f, "")

	tok := f.sign(t, jwt.MapClaims{
		"iss": testIssuer,
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	_, err := v.Verify(tok)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sub")
}

func TestVerify_AcceptsAudienceArray(t *testing.T) {
	f := newJWKSFixture(t)
	v := newTestVerifier(t, f, testClientID)

	tok := f.sign(t, jwt.MapClaims{
		"sub":       "player-9",
		"iss":       testIssuer,
		"aud":       []string{"other-client", testClientID},
		"exp":       time.Now().Add(time.Hour).Unix(),
		"token_use": "id",
	})

	claims, err := v.Verify(tok)
	require.NoError(t, err)
	assert.Equal(t, "player-9", claims.PlayerID)
}

// flipChar は文字を 1 つ別の文字に置き換える（base64 文字集合内で）。
func flipChar(c byte) string {
	if c == 'A' {
		return "B"
	}
	return "A"
}
