# Authorizer Lambda シーケンス図（関数レベル）

`server/cmd/lambda/authorizer/main.go` をエントリポイントとする
API Gateway TOKEN 型カスタムオーソライザの実行シーケンス。

- 関連パッケージ: `server/internal/auth/verifier.go`
- 役割: `Authorization: Bearer <jwt>` を検証し、Allow/Deny の IAM ポリシーを返す門番 Lambda

---

## ① 起動時（コールドスタート: `main` が一度だけ実行）

```mermaid
sequenceDiagram
    autonumber
    participant RT as "Lambda Runtime"
    participant main as "main()"
    participant env as "mustEnv()"
    participant New as "auth.New()"
    participant kf as "keyfunc.NewDefaultCtx()"
    participant JWKS as "JWKSエンドポイント (mock-auth / Cognito)"

    RT->>main: コンテナ起動
    main->>env: mustEnv("AUTH_JWKS_URL")
    env-->>main: 値 (空ならlog.Fatalfで終了)
    main->>env: mustEnv("AUTH_ISSUER")
    env-->>main: 値
    main->>main: os.Getenv("AUTH_CLIENT_ID") ※任意
    main->>New: New(ctx, cfg)
    New->>kf: NewDefaultCtx(ctx, [JWKSURL])
    kf->>JWKS: GET /.well-known/jwks.json
    JWKS-->>kf: 公開鍵セット(キャッシュ, 既定1h更新)
    kf-->>New: keyfunc
    New-->>main: *Verifier (失敗時 log.Fatalf)
    main->>main: handler{verifier: v} 生成
    main->>RT: lambda.Start(h.handle)
    Note over RT: 以降リクエスト受付ループへ
```

---

## ② リクエストごと（`handle` が毎回実行）

```mermaid
sequenceDiagram
    autonumber
    participant APIGW as "API Gateway"
    participant h as "handle()"
    participant ext as "extractBearer()"
    participant V as "verifier.Verify()"
    participant jwt as "jwt.Parse()"
    participant kf as "keyfunc (検証鍵提供)"
    participant chk as "checkAudience()"
    participant pol as "allow/deny → policy"

    APIGW->>h: APIGatewayCustomAuthorizerRequest<br/>(AuthorizationToken, MethodArn)

    h->>ext: extractBearer(AuthorizationToken)
    alt 形式不正 ("Bearer x" でない / 空)
        ext-->>h: error
        h->>pol: deny("")
        pol-->>h: Deny ポリシー (Resource:"*")
        h-->>APIGW: response, nil
    else 正常
        ext-->>h: token文字列

        h->>V: Verify(token)
        V->>jwt: Parse(token, keyfn, opts)<br/>[RS256/iss/exp必須]
        jwt->>kf: 署名検証用の公開鍵を要求
        kf-->>jwt: 公開鍵
        jwt-->>V: token (署名/iss/exp 検証済)

        alt parse失敗 or token.Valid=false
            V-->>h: error
            h->>h: log.Printf(verify failed)
            h->>pol: deny("")
            pol-->>h: Deny ポリシー
            h-->>APIGW: response, nil
        else 検証OK
            V->>V: claims取得 / sub空チェック
            opt clientID設定あり
                V->>chk: checkAudience(claims, token_use)
                Note right of chk: access→client_id照合<br/>id→audienceContains(aud)
                chk-->>V: ok / error
            end
            V-->>h: *Claims{PlayerID, TokenUse, Email}
            h->>pol: allow(PlayerID, MethodArn, ctx)
            Note right of pol: ctx={sub, token_use, email}
            pol-->>h: Allow ポリシー (Resource:MethodArn)
            h-->>APIGW: response, nil
        end
    end

    Note over APIGW: Allow→業務Lambda呼び出し<br/>(Authorizer["sub"]を引き継ぐ)<br/>Deny→403で遮断
```

---

## 補足ポイント

- **鍵取得は①で1回だけ**。②では `keyfunc` がキャッシュ済みの鍵を返すため、リクエストごとに JWKS を叩かない（コンテナ再利用時）。
- **②はどの分岐でも `error` を返さず `nil`** を返す。拒否は「Deny ポリシー」という正常応答で表現し、API Gateway 側の 500 化を避ける設計。
- `Verify` 内の検証は `jwt.Parse` 呼び出しで **署名・`iss`・`exp` がまとめて** 行われ、その後にコード側で `sub` 必須チェックと（任意の）`checkAudience` が追加で走る。
