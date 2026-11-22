// Package auth は JWT 検証ロジックを提供する。
// mock-auth（および本番 Cognito User Pool）が発行する RS256 トークンを JWKS 経由で検証する。
package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
)

// Claims は検証済みトークンから取り出した最小情報。
type Claims struct {
	PlayerID string // sub
	TokenUse string // "id" or "access"
	Email    string // id token のみ
}

// Verifier は JWKS を用いて RS256 JWT を検証する。
type Verifier struct {
	keyfn    jwt.Keyfunc
	issuer   string
	clientID string // "" なら audience/client_id チェックを行わない
}

// Config は Verifier の構築パラメータ。
type Config struct {
	JWKSURL  string // 例: http://mock-auth:9229/.well-known/jwks.json
	Issuer   string // 例: http://localhost:9229
	ClientID string // 例: raidync-local-client（空なら未チェック）
}

var ErrInvalidToken = errors.New("auth: invalid token")

// New は JWKS を取得して Verifier を構築する。
// Refresh は keyfunc 既定（1 時間）のままで運用する想定。
func New(ctx context.Context, cfg Config) (*Verifier, error) {
	if cfg.JWKSURL == "" {
		return nil, errors.New("auth: JWKSURL is required")
	}
	k, err := keyfunc.NewDefaultCtx(ctx, []string{cfg.JWKSURL})
	if err != nil {
		return nil, fmt.Errorf("auth: fetch JWKS: %w", err)
	}
	return &Verifier{
		keyfn:    k.Keyfunc,
		issuer:   cfg.Issuer,
		clientID: cfg.ClientID,
	}, nil
}

// Verify はトークン文字列を検証し、Claims を返す。
// 検証項目: 署名 (RS256) / 発行者 (iss) / 有効期限 (exp) / 必要に応じ aud or client_id。
func (v *Verifier) Verify(tokenString string) (*Claims, error) {
	parserOpts := []jwt.ParserOption{
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithIssuer(v.issuer),
		jwt.WithExpirationRequired(),
	}

	token, err := jwt.Parse(tokenString, v.keyfn, parserOpts...)
	if err != nil {
		if !errors.Is(err, jwt.ErrTokenExpired) &&
			!errors.Is(err, jwt.ErrTokenMalformed) &&
			!errors.Is(err, jwt.ErrTokenSignatureInvalid) &&
			!errors.Is(err, jwt.ErrTokenInvalidIssuer) {
			return nil, fmt.Errorf("auth: parse: %w", err)
		}

		return nil, fmt.Errorf("auth: parse: %w: %w", ErrInvalidToken, err)
	}
	if !token.Valid {
		return nil, fmt.Errorf("auth: token invalid: %w", ErrInvalidToken)
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("auth: auth claims: %w", ErrInvalidToken)
	}

	sub, _ := claims["sub"].(string)
	if sub == "" {
		return nil, fmt.Errorf("auth: missing sub: %w", ErrInvalidToken)
	}

	tokenUse, _ := claims["token_use"].(string)

	// aud / client_id チェック（Cognito 流儀: id token=aud / access token=client_id）。
	if v.clientID != "" {
		if err := v.checkAudience(claims, tokenUse); err != nil {
			return nil, fmt.Errorf("auth: %w: %w", ErrInvalidToken, err)
		}
	}

	email, _ := claims["email"].(string)
	return &Claims{
		PlayerID: sub,
		TokenUse: tokenUse,
		Email:    email,
	}, nil
}

func (v *Verifier) checkAudience(claims jwt.MapClaims, tokenUse string) error {
	switch tokenUse {
	case "access":
		if cid, _ := claims["client_id"].(string); cid != v.clientID {
			return fmt.Errorf("auth: client_id mismatch: got %q", cid)
		}
	case "id", "":
		// jwt.MapClaims の aud は string or []string のどちらもありうる
		if !audienceContains(claims["aud"], v.clientID) {
			return errors.New("auth: aud mismatch")
		}
	}
	return nil
}

func audienceContains(raw any, want string) bool {
	switch v := raw.(type) {
	case string:
		return v == want
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok && s == want {
				return true
			}
		}
	case []string:
		for _, s := range v {
			if s == want {
				return true
			}
		}
	}
	return false
}
