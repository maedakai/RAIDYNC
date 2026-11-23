# AWS IAM ユーザー作成 手順（Runbook）

G4（GameLift Anywhere 実接続）以降の AWS 操作を、ルートユーザーではなく **最小権限の IAM ユーザー**で行うためのセットアップ手順。Windows / PowerShell 前提。

> **なぜ IAM ユーザーか**：ルートユーザーは請求・アカウント設定など「取り返しのつかない操作」ができる最上位権限。日常の CLI 操作に使うのは AWS のベストプラクティス違反。用途を絞った IAM ユーザー＋最小権限ポリシーで操作する。

---

## 前提

- AWS アカウントを保有（現状はルートユーザーのみ）。
- 初回のみ**ルートでコンソールにサインイン**して IAM ユーザーを作る（以降はルートを封印）。
- AWS CLI v2 がインストール済み（`aws --version`）。

---

## 事前（推奨）: ルートユーザーの保護

IAM ユーザーを作る前に、ルートで最低限これだけ済ませておく。

- **ルートに MFA を有効化**（IAM コンソール上部の警告から設定）。
- 以後の作業は作成する IAM ユーザーで行い、**ルートは日常利用しない**。

---

## ステップ1: 権限ポリシーを作成

> **AWS dev 環境（`terraform/environments/dev`）をデプロイする場合**は、下のポリシーではなく [`terraform/bootstrap/raidync-cli-deploy-policy.json`](../terraform/bootstrap/raidync-cli-deploy-policy.json) を貼り付ける。中の `<ACCOUNT_ID>` は自分の AWS アカウント ID に置き換える。下のポリシーは GameLift Anywhere の検証だけに絞った、最初の版。

ポートフォリオとしては**最小権限**が好印象なので、`gamelift:*` ではなくスコープしたカスタムポリシーを作る。

1. IAM コンソール → 左メニュー **ポリシー** → **ポリシーの作成**
2. **JSON** タブに切り替えて以下を貼り付け

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "RaidyncGameLiftAnywhere",
      "Effect": "Allow",
      "Action": [
        "gamelift:CreateLocation",
        "gamelift:CreateFleet",
        "gamelift:DescribeFleetAttributes",
        "gamelift:RegisterCompute",
        "gamelift:DeregisterCompute",
        "gamelift:GetComputeAuthToken",
        "gamelift:CreateGameSession",
        "gamelift:DescribeGameSessions",
        "gamelift:CreatePlayerSessions",
        "gamelift:DescribePlayerSessions",
        "gamelift:UpdateFleetCapacity",
        "gamelift:DeleteFleet",
        "gamelift:DeleteLocation",
        "gamelift:ListFleets",
        "gamelift:ListLocations",
        "gamelift:ListCompute"
      ],
      "Resource": "*"
    },
    {
      "Sid": "CallerIdentity",
      "Effect": "Allow",
      "Action": "sts:GetCallerIdentity",
      "Resource": "*"
    }
  ]
}
```

3. 名前を `RaidyncGameLiftAnywhere` にして **作成**

> **急ぐ場合**: 上記 `Action` を `"gamelift:*"` 1 行に置き換えても動く（権限は広くなる）。最小権限を優先するなら上記のスコープ版を使う。
>
> **将来の IaC 化**: このポリシーは G3（Terraform 化）で `aws_iam_policy` リソースに落とせる。動いた構成をそのまま移植する。

---

## ステップ2: IAM ユーザーを作成

1. IAM → **ユーザー** → **ユーザーの作成**
2. ユーザー名: `raidync-cli`（CLI 用途と分かる名前）
3. 「**AWS マネジメントコンソールへのアクセスを提供**」は**チェックしない**（CLI 専用ならコンソールログイン不要）
4. 次へ → 許可のオプションで「**ポリシーを直接アタッチする**」→ ステップ1 の `RaidyncGameLiftAnywhere` を検索してチェック
5. 次へ → **ユーザーの作成**

---

## ステップ3: アクセスキーを発行（CLI 用）

1. 作成したユーザーを開く → **セキュリティ認証情報** タブ
2. **アクセスキーを作成**
3. ユースケースで「**コマンドラインインターフェイス (CLI)**」を選択 → 下部の確認にチェック → 次へ
4. **アクセスキー**と**シークレットアクセスキー**が表示される → **`.csv` をダウンロード**

> ⚠️ シークレットアクセスキーはこの画面でしか表示されない（再表示不可）。必ず控える。

---

## ステップ4: CLI に設定

```powershell
aws configure
```

プロンプトに順に入力:

| プロンプト | 入力値 |
|---|---|
| `AWS Access Key ID` | 発行した `AKIA...` |
| `AWS Secret Access Key` | 発行したシークレット |
| `Default region name` | `ap-northeast-1` |
| `Default output format` | `json` |

設定は `%USERPROFILE%\.aws\credentials` と `config` に保存される。

---

## ステップ5: 確認

```powershell
aws sts get-caller-identity --region ap-northeast-1
```

`Arn` が `arn:aws:iam::<account>:user/raidync-cli` になっていれば、ルートではなく IAM ユーザーとして操作できている状態。これで GameLift Anywhere Runbook（`docs/phase4-anywhere-runbook.md`）のステップ1 に着手できる。

---

## GUI 用 IAM ユーザー（`raidync-admin`）の作成

`raidync-cli` は**コンソールアクセス無し（CLI 専用）**。マネジメントコンソール（GUI）を人間が操作するには、コンソールログインできる別ユーザーが要る。ルートは封印方針のため、**GUI 操作専用の人間管理者ユーザー**を作る。責務分離の構成:

| ID | 用途 | 資格情報 | 権限 |
|---|---|---|---|
| ルート | 封印（請求・アカウント設定などルート専用のみ） | — | 最上位（使わない） |
| `raidync-admin` | 人間の **GUI 操作**用 | コンソール+パスワード+**MFA**、**長期キー無し** | 広め（AdministratorAccess） |
| `raidync-cli` | 自動化 / **CLI** 用 | アクセスキーのみ | 最小権限スコープ |

> **なぜ CLI でなく GUI で作るか**：ユーザー作成には IAM 書き込み権限が要るが、`raidync-cli` は IAM 権限を持たない。CLI で作るとルートのアクセスキーが必要になり「ルート封印」に反する。よって**初回だけルートでコンソールにサインインし GUI で作成**するのが筋（作成後ルートは再封印）。

### 手順（ルートでコンソールにサインイン）

1. IAM → **ユーザー** → **ユーザーの作成**
2. ユーザー名: `raidync-admin`
3. 「**AWS マネジメントコンソールへのアクセスを提供**」→ **チェックする**
   - 「**IAM ユーザーを作成します**」を選択
   - コンソールパスワード: 自分で使うので任意設定
   - 「ユーザーは次回サインイン時に新しいパスワードを作成」→ 自分専用なら**外してよい**
4. 許可: 「**ポリシーを直接アタッチする**」→ **`AdministratorAccess`**（推奨）
   - ポートフォリオの語り：「**人間管理者は広め＋MFA 保護／サービス実行・IaC はロールで最小権限**」という設計思想。より絞るなら `PowerUserAccess` + `IAMFullAccess`。当面 `RaidyncGameLiftAnywhere` + 各種 ReadOnly でも GameLift GUI は概ね動くが、フリート作成でサービスリンクロール作成が要るため権限不足だと詰まりやすい。
5. **ユーザーの作成** → **サインイン URL**（`https://<account-id>.signin.aws.amazon.com/console`）を控える
6. **MFA を有効化（必須）**：作成したユーザー → **セキュリティ認証情報** → 「**MFA デバイスの割り当て**」→ 認証アプリ（TOTP; Google Authenticator 等）で登録
7. **アクセスキーは発行しない**（GUI 専用。CLI 操作は `raidync-cli` を使う）

以降、GUI 操作は上記サインイン URL から `raidync-admin` でログインする。

> **対応する CLI（参考・通常は使わない）**：以下は IAM 書き込み権限＝ルート資格情報が要るため**非推奨**（上記のとおり GUI で実施する）。
> ```powershell
> aws iam create-user --user-name raidync-admin
> aws iam create-login-profile --user-name raidync-admin --password '<pw>' --password-reset-required
> aws iam attach-user-policy --user-name raidync-admin --policy-arn arn:aws:iam::aws:policy/AdministratorAccess
> # MFA は QR を扱うため GUI 推奨（create-virtual-mfa-device + enable-mfa-device）
> ```

---

## セキュリティ / 運用注意

- ダウンロードした `.csv`／シークレットは **リポジトリにコミットしない**（`.aws/` はリポジトリ外なので `aws configure` 経由なら安全）。
- IAM ユーザーにも **MFA** を付けるとなお良い（コンソールログインを使わないなら必須ではない）。
- アクセスキーを使わなくなったら IAM で **無効化 / 削除**。定期的なローテーションも推奨。
- **runtime（gameserver 実行）は IAM 資格情報を使わない**。SDK 接続は `GAMELIFT_SDK_AUTH_TOKEN`（Anywhere Runbook ステップ4 で取得の短命トークン）で行う。IAM 資格情報が要るのは `aws gamelift ...` を叩く CLI 側だけ。
