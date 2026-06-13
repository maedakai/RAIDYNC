// authorizer Lambda: API Gateway TOKEN 型カスタムオーソライザ。
// Authorization: Bearer <jwt> を検証し、成功時は sub を Authorizer Context にセットする。
// 業務 Lambda 側は apigw.PlayerID 経由で req.RequestContext.Authorizer["sub"] を読み取る。
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"

	"github.com/raidync/server/internal/auth"
)

type handler struct {
	verifier *auth.Verifier
}

func (h *handler) handle(_ context.Context, event events.APIGatewayCustomAuthorizerRequest) (events.APIGatewayCustomAuthorizerResponse, error) {
	token, err := extractBearer(event.AuthorizationToken)
	if err != nil {
		return deny(""), nil
	}

	claims, err := h.verifier.Verify(token)
	if err != nil {
		log.Printf("authorizer: verify failed: %v", err)
		return deny(""), nil
	}

	return allow(claims.PlayerID, event.MethodArn, map[string]interface{}{
		"sub":       claims.PlayerID,
		"token_use": claims.TokenUse,
		"email":     claims.Email,
	}), nil
}

func extractBearer(raw string) (string, error) {
	parts := strings.SplitN(raw, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", errors.New("malformed authorization header")
	}
	return parts[1], nil
}

func allow(principalID, methodArn string, ctx map[string]interface{}) events.APIGatewayCustomAuthorizerResponse {
	return policy(principalID, "Allow", methodArn, ctx)
}

func deny(principalID string) events.APIGatewayCustomAuthorizerResponse {
	if principalID == "" {
		principalID = "anonymous"
	}
	return policy(principalID, "Deny", "*", nil)
}

func policy(principalID, effect, resource string, ctx map[string]interface{}) events.APIGatewayCustomAuthorizerResponse {
	return events.APIGatewayCustomAuthorizerResponse{
		PrincipalID: principalID,
		PolicyDocument: events.APIGatewayCustomAuthorizerPolicy{
			Version: "2012-10-17",
			Statement: []events.IAMPolicyStatement{{
				Action:   []string{"execute-api:Invoke"},
				Effect:   effect,
				Resource: []string{resource},
			}},
		},
		Context: ctx,
	}
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("authorizer: %s is required", key)
	}
	return v
}

func main() {
	ctx := context.Background()
	cfg := auth.Config{
		JWKSURL:  mustEnv("AUTH_JWKS_URL"),
		Issuer:   mustEnv("AUTH_ISSUER"),
		ClientID: os.Getenv("AUTH_CLIENT_ID"), // 任意
	}
	v, err := auth.New(ctx, cfg)
	if err != nil {
		log.Fatalf("authorizer: init verifier: %v", fmt.Errorf("%w", err))
	}
	h := &handler{verifier: v}
	lambda.Start(h.handle)
}
