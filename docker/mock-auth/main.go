package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"golang.org/x/crypto/scrypt"
)

const (
	keyID = "mock-auth-key-1"

	scryptN      = 1 << 14
	scryptR      = 8
	scryptP      = 1
	scryptKeyLen = 64
	saltLen      = 16
)

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func parseTTL(s string) time.Duration {
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 3600 * time.Second
	}
	return time.Duration(n) * time.Second
}

type user struct {
	PlayerID     string
	Email        string
	PasswordHash string
	CreatedAt    time.Time
}

type signupRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginResponse struct {
	IDToken     string `json:"id_token"`
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

var (
	port     = getenv("PORT", "9229")
	issuer   = getenv("MOCK_AUTH_ISSUER", "http://localhost:9229")
	clientID = getenv("MOCK_AUTH_CLIENT_ID", "raidync-local-client")
	ttl      = parseTTL(getenv("MOCK_AUTH_TOKEN_TTL", "3600"))

	privateKey *rsa.PrivateKey
	jwks       jose.JSONWebKeySet
	signer     jose.Signer

	usersMu sync.RWMutex
	users   = make(map[string]*user)
)

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeJSONError(w http.ResponseWriter, status int, errCode, message string) {
	writeJSON(w, status, map[string]string{
		"error":   errCode,
		"message": message,
	})
}

func hashPassword(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash, err := scrypt.Key([]byte(password), salt, scryptN, scryptR, scryptP, scryptKeyLen)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(salt) + ":" + hex.EncodeToString(hash), nil
}

func verifyPassword(password, stored string) bool {
	parts := strings.SplitN(stored, ":", 2)
	if len(parts) != 2 {
		return false
	}
	salt, err := hex.DecodeString(parts[0])
	if err != nil {
		return false
	}
	expected, err := hex.DecodeString(parts[1])
	if err != nil {
		return false
	}
	candidate, err := scrypt.Key([]byte(password), salt, scryptN, scryptR, scryptP, scryptKeyLen)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(candidate, expected) == 1
}

func issueTokens(u *user) (string, string, error) {
	now := time.Now()
	exp := now.Add(ttl)

	idClaims := map[string]any{
		"sub":              u.PlayerID,
		"aud":              clientID,
		"iss":              issuer,
		"iat":              now.Unix(),
		"exp":              exp.Unix(),
		"email":            u.Email,
		"email_verified":   true,
		"cognito:username": u.PlayerID,
		"token_use":        "id",
		"auth_time":        now.Unix(),
	}

	idToken, err := jwt.Signed(signer).Claims(idClaims).Serialize()
	if err != nil {
		return "", "", err
	}

	accessClaims := map[string]any{
		"sub":       u.PlayerID,
		"iss":       issuer,
		"iat":       now.Unix(),
		"exp":       exp.Unix(),
		"username":  u.PlayerID,
		"client_id": clientID,
		"scope":     "openid",
		"token_use": "access",
		"auth_time": now.Unix(),
	}

	accessToken, err := jwt.Signed(signer).Claims(accessClaims).Serialize()
	if err != nil {
		return "", "", err
	}

	return idToken, accessToken, nil
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func handleJWKS(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, jwks)
}

func handleSignup(w http.ResponseWriter, r *http.Request) {
	var req signupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "InvalidParameterException", "invalid request body")
		return
	}

	if !strings.Contains(req.Email, "@") {
		writeJSONError(w, http.StatusBadRequest, "InvalidParameterException", "email is required and must be valid")
		return
	}
	if len(req.Password) < 8 {
		writeJSONError(w, http.StatusBadRequest, "InvalidParameterException", "password must be at least 8 characters")
		return
	}

	usersMu.Lock()
	defer usersMu.Unlock()

	if _, exists := users[req.Email]; exists {
		writeJSONError(w, http.StatusConflict, "UsernameExistsException", "email already registered")
		return
	}

	passwordHash, err := hashPassword(req.Password)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "InternalError", "failed to hash password")
		return
	}

	playerID := uuid.NewString()
	users[req.Email] = &user{
		PlayerID:     playerID,
		Email:        req.Email,
		PasswordHash: passwordHash,
		CreatedAt:    time.Now(),
	}

	log.Printf("[mock-auth] signup: %s (playerId=%s)", req.Email, playerID)

	writeJSON(w, http.StatusCreated, map[string]string{
		"playerId": playerID,
		"email":    req.Email,
	})
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "InvalidParameterException", "invalid request body")
		return
	}
	if req.Email == "" || req.Password == "" {
		writeJSONError(w, http.StatusBadRequest, "InvalidParameterException", "email and password are required")
		return
	}

	usersMu.RLock()
	u, exists := users[req.Email]
	usersMu.RUnlock()

	if !exists || !verifyPassword(req.Password, u.PasswordHash) {
		writeJSONError(w, http.StatusUnauthorized, "NotAuthorizedException", "Incorrect username or password")
		return
	}

	idToken, accessToken, err := issueTokens(u)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "InternalError", "failed to issue tokens")
		return
	}

	log.Printf("[mock-auth] login: %s (playerId=%s)", u.Email, u.PlayerID)

	writeJSON(w, http.StatusOK, loginResponse{
		IDToken:     idToken,
		AccessToken: accessToken,
		TokenType:   "Bearer",
		ExpiresIn:   int(ttl.Seconds()),
	})
}

func main() {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		log.Fatalf("[mock-auth] key generation failed: %v", err)
	}
	privateKey = key

	publicJWK := jose.JSONWebKey{
		Key:       &key.PublicKey,
		KeyID:     keyID,
		Algorithm: string(jose.RS256),
		Use:       "sig",
	}
	jwks = jose.JSONWebKeySet{Keys: []jose.JSONWebKey{publicJWK}}

	signer, err = jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: privateKey},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", keyID),
	)
	if err != nil {
		log.Fatalf("[mock-auth] signer init failed: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handleHealth)
	mux.HandleFunc("GET /.well-known/jwks.json", handleJWKS)
	mux.HandleFunc("POST /signup", handleSignup)
	mux.HandleFunc("POST /login", handleLogin)

	addr := ":" + port
	log.Printf("[mock-auth] listening on port %s", port)
	log.Printf("[mock-auth] issuer: %s", issuer)
	log.Printf("[mock-auth] JWKS available at /.well-known/jwks.json")
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("[mock-auth] server error: %v", err)
	}
}
