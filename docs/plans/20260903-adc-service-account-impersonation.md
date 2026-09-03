# Application Default Credentials からの service account impersonation

- Status: `Implemented`
- 作成日: 2026-09-03
- 対象: `internal/provider/provider_config.go` の認証経路選択
- 関連: `docs/plans/20260831-plan-domain-data-source.md`（keyless 経路の threat model）

## 背景

利用側リポジトリは provider を keyless で使う。gcloud のログインユーザーの access token を `GOOGLE_OAUTH_ACCESS_TOKEN` で渡し、provider が `service_account` を IAM Service Account Credentials API の `signJwt` で借り、`impersonated_user_email` を subject にしたドメイン全体の委任（DWD）token で Admin SDK を呼ぶ。この経路は 0.8.0 の `scripts/plan-domain.sh` で検証済みで、JSON key を作らない点が利点である。

一方で運用上の欠点が 2026-09-03 に顕在化した。

- 毎回 `GOOGLE_OAUTH_ACCESS_TOKEN="$(gcloud auth print-access-token)"` を前置しないと動かない。前置を忘れると provider は Application Default Credentials（ADC）を直接使い、user 型 ADC には Admin SDK の scope が無いため、全 user と group の refresh が `403 ACCESS_TOKEN_SCOPE_INSUFFICIENT` で失敗する。失敗の見た目から原因（認証経路の取り違え）に辿り着きにくい。
- GCS backend は ADC で動くのに provider だけが token の前置を要求する非対称があり、gcloud の user 認証と ADC が別々に失効することと相まって、再認証の手順が二重になる。

原因は provider の実装にある。`loadAndValidate` は `access_token` 経路でだけ `service_account` を借りる impersonation を実装しており、ADC 経路は `FindDefaultCredentialsWithParams` に `Subject` を渡すだけで `service_account` を参照しない。`Subject` は service account key 型の credential でしか効かないため、user 型 ADC では黙って無視される。

つまり「元 credential から service account を借りて DWD する」という同じ操作が、元 credential が access token のときだけ実装されている。これを ADC にも広げれば、利用者は `gcloud auth application-default login` と `.envrc` の設定だけで backend と provider の両方を同じ credential で動かせる。token を shell 変数や環境変数で受け渡す工程そのものが無くなる。

## Goal

1. `service_account` と `impersonated_user_email` が設定されていれば、元 credential が `access_token` または private key を持たない ADC のときに同じ impersonation 経路（IAM `signJwt` による DWD）で認証する。service account key 型 ADC は既存どおり、その key による直接 DWD を維持する。
2. ADC が service account key 型ではなく、`impersonated_user_email` が設定されているのに `service_account` が無い場合は、Admin SDK を呼ぶ前に原因と対処を示すエラーで止める。
3. 選択した認証経路を INFO ログに出し、どの主体で動いているかを運用時に確認できるようにする。
4. docs、example、CHANGELOG を更新し、`0.9.0` として release できる実装を完成させる。tag 発行と release 自体は別途承認後に行う。
5. provider-level の `impersonated_user_email` を設定せず、user の access token または user 型 ADC を直接使う Gmail send-as alias の既存認証経路を維持する。

## Non-goals

- schema の変更。attribute の追加・削除・Required 化・DefaultFunc の追加は行わない。既存の `service_account` と `impersonated_user_email` で表現できる。
- Admin SDK の `oauth_scopes` 追加や、新しい API の有効化要求。IAM Service Account Credentials API は `access_token` 経路で既に使っている。ADC から同 API を呼ぶ元 credential には `cloud-platform` scope を要求する。
- `scripts/plan-domain.sh` と `make plan-domain` の変更。access token 経路は互換のまま残す。
- 利用側リポジトリの変更（provider version の更新、runbook、認証方針の見直し）。本 plan の release 後に利用側の別 plan で扱う。
- 実際の GitHub Actions や Workload Identity Federation 環境での end-to-end 検証。WIF を含む private key を持たない ADC の経路選択と fail fast は unit test の対象とする。
- acceptance test、sweeper、実 Workspace に対する CRUD。
- tag 発行、release、利用側リポジトリへの反映。いずれも本 plan の実装完了後に別途承認を得て行う。

## Strategy

### 認証経路の選択

`loadAndValidate` の分岐を次の表に固定する。上から順に評価し、既存の優先順位（`access_token` > `credentials` > ADC）は変えない。

| 条件 | 経路 | 本 plan での扱い |
|---|---|---|
| `access_token` あり、`service_account` あり、`impersonated_user_email` あり | access token を元 credential に `service_account` を借り、subject を `impersonated_user_email` にした DWD token | 既存。共通 helper に寄せる |
| `access_token` あり、provider-level の `impersonated_user_email` あり、`service_account` なし | 認証設定エラー | 既存の fail fast を維持 |
| `access_token` あり、Gmail sub-client が内部設定した subject あり、`service_account` なし | access token をそのまま使う | **修正**。ADC への silent fallback を止め、既存の直接主体を維持 |
| `access_token` あり、`impersonated_user_email` なし | access token をそのまま使う | 既存 |
| `credentials` あり | service account key の JWT（`Subject` = `impersonated_user_email`）。`service_account` は無視 | 既存 |
| service account key 型 ADC | key による JWT（`Subject` = `impersonated_user_email`）。`service_account` は従来どおり無視 | 既存互換を維持 |
| private key を持たない ADC、`service_account` あり、`impersonated_user_email` あり | ADC を元 credential に `service_account` を借り、DWD token | **追加** |
| private key を持たない ADC、provider-level の `impersonated_user_email` あり、`service_account` なし | 認証設定エラー | **追加**。Admin SDK 呼び出し前に fail fast |
| private key を持たない ADC、Gmail sub-client が内部設定した subject あり、`service_account` なし | ADC を直接使う | 既存の直接主体を維持 |
| ADC、`impersonated_user_email` なし | ADC を直接使う。`service_account` 単独指定は従来どおり認証経路を変えない | 既存 |

ADC の種類は `Credentials.JSON` の `type` で判定する。`service_account` だけを private key による直接 DWD が可能な型として許可する。`authorized_user`、`external_account`、`external_account_authorized_user`、`impersonated_service_account` などその他の JSON 型、および `Credentials.JSON` が空になる metadata server 型は、`impersonated_user_email` を直接適用できない ADC として扱う。未知の型や JSON なしを黙って通さない。

ADC の判定用 credential は一度解決し、impersonation 経路では IAM Service Account Credentials API 用の `cloud-platform` scope で元 credential を取得する。service account key 型 ADC または subject なしの ADC 直接経路では、既存どおり `ClientScopes` と必要な `Subject` を渡して取得する。

### 実装の骨子

impersonation を 1 つの helper に集約し、元 credential を表す client option だけを差し替える。`access_token` 経路にある `context.TODO()` も `ctx` に直す。

ADC 経路では、`google.golang.org/api/transport/http.NewTransport` と `option.WithCredentials(source)` で認証・quota header 付き transport を作る factory を置き、その `http.Client` を `option.WithHTTPClient` で impersonation helper に渡す。production は実 transport、unit test は recording base transport を同じ factory に渡す。これにより test 用 `WithHTTPClient` が production の `WithCredentials` 処理を迂回する問題を避ける。

```go
const cloudPlatformScope = "https://www.googleapis.com/auth/cloud-platform"

// 元 credential で service_account を借り、impersonated_user_email を subject にした
// DWD の TokenSource を返す。access_token 経路と ADC 経路で共用する。
func (c *apiClient) impersonatedTokenSource(ctx context.Context, opts ...option.ClientOption) (oauth2.TokenSource, error) {
	return impersonate.CredentialsTokenSource(ctx, impersonate.CredentialsConfig{
		TargetPrincipal: c.ServiceAccount,
		Scopes:          c.ClientScopes,
		Subject:         c.ImpersonatedUserEmail,
	}, opts...)
}

func adcImpersonationHTTPClient(ctx context.Context, source *googleoauth.Credentials, base http.RoundTripper) (*http.Client, error) {
	transport, err := googlehttptransport.NewTransport(ctx, base, option.WithCredentials(source))
	if err != nil {
		return nil, err
	}
	return &http.Client{Transport: transport}, nil
}
```

`access_token` 経路は `option.WithTokenSource` を helper に直接渡す。ADC 経路は `option.WithCredentials` を上記 factory に渡し、生成した client を `option.WithHTTPClient` で helper に渡す。ADC 全体を factory に渡すことで、元 token source に加えて `quota_project_id` などの credential metadata も IAM Service Account Credentials API の HTTP client に引き継ぐ。

```go
if c.ServiceAccount != "" && c.ImpersonatedUserEmail != "" {
	source, err := googleoauth.FindDefaultCredentials(ctx, cloudPlatformScope)
	if err != nil {
		return diag.FromErr(err)
	}
	log.Printf("[INFO] Authenticating using Application Default Credentials, impersonating service account %q as %q", c.ServiceAccount, c.ImpersonatedUserEmail)
	sourceClient, err := adcImpersonationHTTPClient(ctx, source, http.DefaultTransport)
	if err != nil {
		return diag.FromErr(err)
	}
	tokenSource, err := c.impersonatedTokenSource(ctx, option.WithHTTPClient(sourceClient))
	if err != nil {
		return diag.FromErr(err)
	}
	return c.SetupClient(ctx, &googleoauth.Credentials{TokenSource: tokenSource})
}
```

元 credential に要求する scope は `cloud-platform` の 1 件とする。IAM Service Account Credentials API の呼び出しに必要で、user 型 ADC では scope 指定が無視され、service account key 型や metadata server 型の ADC ではこの scope の token が使われる。

### fail fast

ADC を直接使う分岐で、`impersonated_user_email` が設定されているのに `service_account` が無く、ADC が service account key 型ではない場合はエラーで止める。`Credentials.JSON` が空の metadata server 型も subject を適用できないためエラー対象にする。

この fail-fast は、Admin SDK scope を持つ user ADC が `impersonated_user_email` を無視してログインユーザー本人として成功していた既存構成をエラーに変える。誤った主体での実行を防ぐための意図的な互換性変更として採用する。移行方法は目的別に次のとおりとする。

- service account を主体に DWD する場合: `service_account` を設定する。
- ログインユーザー本人として Admin SDK を呼ぶ場合: 適切な Admin SDK scope を持つ ADC を使い、適用されていなかった `impersonated_user_email` を削除する。
- service account key による DWD を継続する場合: `credentials` または service account key 型 ADC を使う。

エラー文言は原因と上記の対処を含める。例: `impersonated_user_email cannot be applied directly to these Application Default Credentials. Set service_account for domain-wide delegation, remove impersonated_user_email when intentionally authenticating as the ADC principal, or use service account key credentials.`

### Gmail sub-client の元 credential 引き継ぎ

`NewGmailService` は send-as alias の対象ユーザーを subject にした sub-client を作るが、現状は `Credentials` しか引き継がず、`access_token` と `service_account` を落とす。このままでは ADC impersonation の sub-client が fail fast し、Goal 1 を Gmail send-as alias では満たせない。

`AccessToken` と `ServiceAccount` も引き継ぎ、「元 credential と service account は固定、subject だけがサービスごとに変わる」形にする。Gmail send-as alias を Goal 1 の対象に含め、既存 access token 経路の silent ADC fallback も同時に修正する。

ただし、Gmail sub-client が内部的に設定する `userId` は、provider-level の `impersonated_user_email` と同じ意味ではない。親 client で `impersonated_user_email` が未設定の場合、`NewGmailService` は child client に非公開の `allowDirectPrincipal` flag を設定する。route selector はこの flag を次のように扱う。

- `service_account` がある場合、または元 credential が service account key 型の場合は、従来どおり `userId` を subject に DWD する。
- user の `access_token` または private key を持たない ADC で `service_account` が無い場合は、`userId` を credential の subject に渡さず、元 credential の主体を直接使う。provider-level の impersonation 要求ではないため fail-fast の対象外とする。
- 親 client で `impersonated_user_email` が設定されている場合は flag を設定せず、通常の fail-fast と DWD 経路を適用する。

`allowDirectPrincipal` は provider schema に追加せず、`NewGmailService` が作る child client だけで使う内部状態とする。これにより、通常の provider 設定で `impersonated_user_email` が黙って無視される問題を再導入しない。

### quota project

ADC impersonation では共有 transport factory 内で `option.WithCredentials(source)` を使い、ADC JSON の `quota_project_id` と `GOOGLE_CLOUD_QUOTA_PROJECT` の既存設定を IAM Service Account Credentials API の呼び出しへ引き継ぐ。設定済み quota project を意図的に落とす fallback は設けない。

必要な権限主体を次のように区別する。

- 元 credential の主体には、対象 service account 上の `roles/iam.serviceAccountTokenCreator` が必要である。
- quota project を設定・利用する元 credential の主体には、その quota project 上の `serviceusage.services.use` が必要である。
- pinned `google.golang.org/api/impersonate` の契約に従い、元 credential が `authorized_user` の場合、または quota project を伴う場合は、対象 service account にも quota project 上の `serviceusage.services.use` を含む role（例: `roles/serviceusage.serviceUsageConsumer`）を付与する。

権限不足の場合は credential / IAM 設定の問題としてエラーを返す。必要に応じて利用者が、元 credential の主体と対象 service account の両方に必要な権限がある project を `gcloud auth application-default set-quota-project PROJECT_ID` で選ぶ。

### threat model の差分

`docs/plans/20260831-plan-domain-data-source.md` の finding は、shell で取得した token を環境変数で provider process に渡す工程に対するものだった（ambient な override、`xtrace`、`TF_REATTACH_PROVIDERS`、debug log による漏洩）。ADC 経路では token が shell、環境変数、log を経由せず、provider が Google の認証ライブラリ経由で取得する。残る ambient な面は ADC の解決順（`GOOGLE_APPLICATION_CREDENTIALS`、ADC ファイル、metadata server）だが、これは Google の標準挙動で、`env` で確認でき、借りる先の `service_account` と subject は HCL と `GOOGLEWORKSPACE_*` で明示される。元 credential の主体が対象 service account への `roles/iam.serviceAccountTokenCreator` を持たなければ token は発行されないため、影響範囲は IAM で閉じる。access token 経路は互換のまま残すため、keyless script と CI からの利用は従来どおり可能である。

### 検討して採用しない案

- `.envrc` で token を export する、または wrapper script で前置を隠す。前置を隠すだけで token が環境変数を経由する構造は変わらず、`.envrc` の値は 1 時間で失効したまま残る。
- `gcloud auth application-default login --scopes=<Admin SDK scope>` で利用者自身の権限を ADC に載せる（upstream の docs が案内する方法）。コード変更は不要だが、service account を主体にする利用側の設計を捨てることになり、利用者ごとに Workspace 管理者権限が必要になる。再認証のたびに scope 指定を思い出す必要があり、忘れると今回と同じ 403 が再発する。
- service account の JSON key を `credentials` で渡す。現行実装で動くが、長期鍵をディスクに置くことになり keyless の方針に反する。
- `service_account` に `GOOGLEWORKSPACE_SERVICE_ACCOUNT_EMAIL` の DefaultFunc を足す。借りる先の主体は HCL に明示する方が監査しやすく、環境変数解決はテナント識別子（customer ID、impersonated user）に留める。

## Implementation

1. `internal/provider/provider_config.go`
   - `impersonatedTokenSource` helper を追加し、`access_token` 経路の impersonation をこれに置き換える。`context.TODO()` を `ctx` にする。
   - ADC の `Credentials.JSON` から type を読む小さな判定関数と、認証経路を決める純粋な route selector を追加する。
   - service account key 型 ADC は既存の直接 DWD を維持する。それ以外の ADC には `service_account` + `impersonated_user_email` の impersonation 経路を追加する。
   - ADC 直接分岐に、service account key 型以外の ADC と `impersonated_user_email` の不成立組み合わせを拒否する fail fast を追加する。未知の JSON type と JSON なしも fail closed にする。
   - ADC impersonation の元 credential は `cloud-platform` scope で取得する。`transport/http.NewTransport` と `option.WithCredentials` を使う共有 factory で認証・quota header 付き client を作り、helper へ渡す。
   - 各経路の INFO ログを揃える。token や JSON の内容は出さない。
   - `NewGmailService` に `AccessToken` と `ServiceAccount` の引き継ぎを追加する。親 client の `ImpersonatedUserEmail` が空の場合だけ child client の内部 `allowDirectPrincipal` flag を有効にする。
   - route selector は `allowDirectPrincipal` を入力に含め、Gmail 内部の subject と provider-level の impersonation 要求を区別する。flag が有効でも service account key の直接 DWD と、`service_account` を借りる DWD は優先する。
2. `internal/provider/provider.go`
   - `service_account` の `Description` を「`impersonated_user_email` を設定し、元 credential が `access_token` または private key を持たない ADC のときに IAM Service Account Credentials API で借りる service account。元 credential の主体に `roles/iam.serviceAccountTokenCreator` が必要。`credentials` または service account key 型 ADC の使用時は無視」の趣旨に改める。
   - `credentials` の `Description` の末尾「If not provided, the application default credentials will be used.」に、`service_account` があれば ADC からその service account を借りる旨を添える。
3. `internal/provider/provider_config_test.go`
   - route selector の table-driven test で `service_account`、`authorized_user`、`external_account`、`external_account_authorized_user`、`impersonated_service_account`、未知の type、JSON なしを網羅する。
   - `GOOGLE_APPLICATION_CREDENTIALS` に既存の fake service account key（`test-data/fake-creds.json`）を `t.Setenv` で指定し、`ServiceAccount` と `ImpersonatedUserEmail` が同時に設定されても直接 DWD 経路を選び、IAM impersonation に切り替わらないことを確認する。TokenSource は遅延評価でネットワークに触れない。
   - `t.TempDir()` に `authorized_user` 型の fake ADC を書き、`ServiceAccount` と `ImpersonatedUserEmail` を設定した `loadAndValidate` が impersonation 経路を選び、エラーなく client を構築することを確認する。
   - 同じ fake ADC で `ImpersonatedUserEmail` のみ設定した `loadAndValidate` が `service_account` を促すエラーになり、`ImpersonatedUserEmail` なしなら従来どおり通ることを確認する。
   - fake `authorized_user` ADC の token endpoint と、IAM `signJwt`、OAuth token exchange を処理する recording base transport を用意する。production と同じ `adcImpersonationHTTPClient` factory に fake ADC と recording transport を渡し、生成した client を helper に渡す hermetic request-level test を追加する。元 tokenが `signJwt` request の Bearer token に使われること、`X-Goog-User-Project`、JWT claim の `iss`、`sub`、`scope`、target service account、最終 DWD token を検証する。外部 network と実 credential は使わない。
   - 上記 test は ADC JSON の `quota_project_id` を検証し、別 case で `GOOGLE_CLOUD_QUOTA_PROJECT` が JSON より優先されることも確認する。
   - `NewGmailService` の sub-client が `AccessToken` と `ServiceAccount` を持つことを確認する。
   - provider-level の `ImpersonatedUserEmail` が空で、user access token または `authorized_user` ADC を直接使う Gmail sub-client は `userId` を内部設定しても fail-fast せず、元 credential の主体を直接使うことを確認する。access token case では ADC に fallback しないことも検証する。
   - provider-level の `ImpersonatedUserEmail` がある場合、および `ServiceAccount` がある場合は、Gmail sub-client が `userId` を subject にした DWD 経路を選ぶことを確認する。service account key 型では、親の impersonation 設定が無くても既存どおり `userId` を subject に直接 DWD することを確認する。
4. `templates/index.md.tmpl` と `examples/provider/`
   - 「Using Domain-Wide Delegation」に、key を作らず ADC または access token から service account を借りる小節を追加する。必要な IAM（元 credential の主体への `roles/iam.serviceAccountTokenCreator`）、gcloud の場合は `gcloud auth application-default login`、WIF など private key を持たない ADC でも同じ設定を使うことを書く。
   - ADC の quota project が IAM Service Account Credentials API に引き継がれ、設定されている場合は元 credential の主体に `serviceusage.services.use` が必要であることを書く。project は `PROJECT_ID` placeholder で示す。
   - pinned impersonation library の要件として、元 credential が user 型の場合、または quota project を伴う場合は、対象 service account にも quota project 上の `serviceusage.services.use` が必要であることを書く。
   - `examples/provider/provider-adc-impersonation.tf` を追加し、`customer_id` / `service_account` / `impersonated_user_email` / `oauth_scopes` だけの provider block を示す。
   - `make generate` で `docs/index.md` を再生成し、差分をコミットに含める。
5. `CHANGELOG.md` の `## 0.9.0 (Unreleased)` に記録する。
   - BREAKING CHANGES: private key を持たない ADC と `impersonated_user_email` を `service_account` なしで使う構成はエラーになる。ログインユーザー本人として実行する既存構成は `impersonated_user_email` を削除し、DWD する構成は `service_account` を設定する。
   - IMPROVEMENTS: provider: `service_account` と `impersonated_user_email` が Application Default Credentials からも IAM Service Account Credentials API 経由で impersonation するようになり、keyless の DWD に `access_token` が不要になった。
   - IMPROVEMENTS: provider: private key を持たない ADC と `impersonated_user_email` の組み合わせを `service_account` なしで使った場合に明確なエラーで止まるようになった。
   - BUG FIXES: gmail send-as alias: ユーザーごとの Gmail client が設定済みの `access_token` / `service_account` を引き継ぐようになり、ADC へ黙って fallback しなくなった。provider-level の impersonation が未設定の user access token / user ADC は、従来どおり元 credential の主体を直接使う。
6. 実装 PR を作成し、CI（unit test と lint）の結果を確認する。merge、tag 発行、release は本 plan の scope 外とし、別途承認後に行う。

## 主な touched files

- `docs/plans/20260903-adc-service-account-impersonation.md`
- `internal/provider/provider_config.go`
- `internal/provider/provider_config_test.go`
- `internal/provider/provider.go`（`Description` のみ。schema の型・必須性は変えない）
- `templates/index.md.tmpl`
- `examples/provider/provider-adc-impersonation.tf`（新規）
- `docs/index.md`（`make generate` による生成物）
- `CHANGELOG.md`

## User-owned Actions

ADC impersonation を利用するには、IAM Service Account Credentials API が有効であり、元 credential の主体が対象 service account の `roles/iam.serviceAccountTokenCreator` を持つ必要がある。quota project を使う場合は、元 credential の主体がその project の `serviceusage.services.use` を持つ必要がある。加えて、元 credential が user 型の場合または quota project を伴う場合は、pinned impersonation library の要件として対象 service account にも quota project 上の `serviceusage.services.use` を含む role が必要である。具体的な project、service account、credential の値はリポジトリに記録しない。

1. 必要に応じて、元 credential の主体と対象 service account の両方に必要な権限がある quota project を `gcloud auth application-default set-quota-project PROJECT_ID` で選ぶ。
2. 任意の実環境 read-only 検証を行う場合、検証者が ADC を有効にし、自身で `terraform plan` を実行する。credential は agent に渡さない。この検証は hermetic test の代替ではなく、本 plan の実装完了条件にも含めない。
3. merge、tag 発行、release は、実装完了後に別途承認する。

## Question classification

### Default Decisions

2026-09-03 の会話で人間が選択済み。

- wrapper script や service account key ではなく、provider の ADC 経路に service account impersonation を実装する。
- Gmail send-as alias を Goal 1 の対象に含め、`NewGmailService` に `AccessToken` と `ServiceAccount` を引き継ぐ。
- Gmail が内部設定する対象ユーザーは provider-level の impersonation 要求と区別し、user access token / user ADC を直接使う既存構成は fail-fast の対象外として維持する。
- service account key 型 ADC で `service_account` も設定されている場合は、既存どおり key による直接 DWD を維持し、`service_account` を無視する。
- private key を持たない ADC と `impersonated_user_email` を `service_account` なしで使う構成は fail-fast にする。移行手順を docs と CHANGELOG に記載する。
- ADC impersonation では既存の quota project 設定を引き継ぐ。元 credential の主体と、pinned library が要求する場合の対象 service account に必要な `serviceusage.services.use` を要求し、quota project を破棄する fallback は設けない。

### Human Decisions Required

なし。

### Agent-resolvable Decisions

- helper と判定関数の名前、ログとエラーの文言。
- route selector の表現、テストの分割、fake ADC の生成方法（`t.TempDir()` に書く）。
- docs の小節名と example file 名、CHANGELOG の文言。
- `access_token` 経路の既存ログ文言との揃え方。

## Stop condition

`.agents/docs/planning-review.md` の標準 stop condition に加え、次で停止する。

- `go.mod` に新しい依存が必要になった場合。`impersonate` と `golang.org/x/oauth2/google` は既に依存にあり、追加なしで実装できる想定である。
- ADC の種類を安全に判定できず、未知の種類を fail closed にできない場合。
- `option.WithCredentials` で ADC の quota project を引き継げないことが判明した場合。
- 実 credential または実 Workspace を使う検証が必要になった場合。

## Verifier

- `go mod verify`、`go vet ./...`、`make build`、`make test`。
- lint は pinned version で実行する。ローカルの `golangci-lint` が Go の version 不一致で止まる場合は `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2 run ./internal/provider` を使う。
- `make generate` の後に `git diff docs/` を確認し、`docs/index.md` の差分が `templates/` と `Description` の変更に対応していること。
- unit test で次が通ること。
  - service account key 型 ADC + `service_account` + `impersonated_user_email` は既存の直接 DWD 経路を維持する。
  - `authorized_user`、`external_account`、`external_account_authorized_user`、`impersonated_service_account`、未知の JSON type、JSON なしの各 ADC と `impersonated_user_email`（`service_account` なし）は、Admin SDK 呼び出し前に `service_account` を促すエラーになる。
  - user 型 ADC + `service_account` + `impersonated_user_email` は ADC impersonation 経路を選び、client 構築に成功する。
  - user 型 ADC で `impersonated_user_email` なしなら従来どおり成功する。
  - production と同じ transport factory と recording base transport により、user ADC の token refresh → Bearer 認証・`X-Goog-User-Project` 付き IAM `signJwt` → OAuth token exchange の一連の request が行われ、target、subject、scope を含む期待した DWD token になる。
  - ADC JSON の `quota_project_id` が header に付与され、`GOOGLE_CLOUD_QUOTA_PROJECT` が設定された場合は環境変数の値が優先される。
  - Gmail sub-client は設定済みの `access_token` / `service_account` を引き継ぐ。provider-level の impersonation が無い user access token / user ADC は直接主体を維持し、provider-level の impersonation、`service_account`、service account key の各 DWD 経路は `userId` を subject にする。
  - 既存の `access_token` と `credentials` のテストが変更なしで通る。
- v0.8.0 の access token 経路を互換性 baseline とし、既存 test が同じ結果で通ること。公開版 v0.8.0 に ADC-only 成功を要求しない。
- 任意の利用者実施による read-only 検証では、v0.8.0 の access token 経路と dev build の ADC impersonation 経路で同じ plan 差分（または no-change）になり、後者で `403 ACCESS_TOKEN_SCOPE_INSUFFICIENT` が出ないことを比較する。
- `codex` の code review で P0 / P1 finding がないこと。

## 利用側リポジトリの追従（別 plan）

`v0.9.0` の release 後、利用側リポジトリで次を扱う別 plan を作る。本 plan の scope 外。

- provider の version constraint を `~> 0.9` に上げ、`terraform init -upgrade` で lock file を更新する。
- 利用側の設計 plan にある「User ADC を使わない」決定を見直す。token が環境変数を経由しないこと、backend と provider が同じ ADC で動くこと、前置忘れの失敗モードが消えることを理由に、ADC を元 credential とする方針へ更新する。
- provider 設定のコメントと runbook から `GOOGLE_OAUTH_ACCESS_TOKEN` の前置を外す。再認証は `gcloud auth application-default login` の 1 種類になる。
- `GOOGLEWORKSPACE_CUSTOMER_ID` と `GOOGLEWORKSPACE_IMPERSONATED_USER_EMAIL` の環境変数はそのまま provider が読む。

## AI critical review 履歴

### 2026-09-03 初回（codex exec / gpt-5.6-sol）

次の P1 finding を検出した。

| # | Finding | 対応 |
|---|---|---|
| 1 | Gmail sub-client が `ServiceAccount` を落とすため、ADC impersonation が Gmail send-as alias で成立しない | 引き継ぎを必須変更にするか Gmail を Goal から除外する二択に固定し、Human Decisions Required に残した |
| 2 | `authorized_user` だけの fail fast では、WIF、impersonated service account、metadata server などで subject が黙って無視される | service account key 型以外と JSON なしを fail closed にし、ADC type の route table と網羅 test を追加した |
| 3 | service account key 型 ADC でも無条件に IAM `signJwt` へ切り替えると既存構成に新しい API・IAM 要件を課す | key による直接 DWD を維持する互換性案へ変更し、Human Decisions Required に確認事項として残した |
| 4 | 中心となる user ADC → `signJwt` → DWD 経路を必須 verifier が実行しない | fake user ADC の refresh から `signJwt` と token exchange までを通す hermetic request-level test を追加し、v0.8.0 baseline は access token 経路と明記した |
| 5 | quota project を捨てる案が未確定で、user ADC の quota・IAM 要件と矛盾する | `option.WithCredentials` で既存 quota project を引き継ぎ、`serviceusage.services.use` 要件を docs と User-owned Actions に明記する案へ変更し、Human Decisions Required に確認事項として残した |

初回 review は、status が `Human Review Pending` で Human Decisions Required が残るため implementation-ready ではないと判定した。

### 2026-09-03 再レビュー（fresh codex exec / gpt-5.6-sol）

初回 finding への対応を確認したうえで、次の P1 finding を追加で検出した。

| # | Finding | 対応 |
|---|---|---|
| 1 | fail-fast は、Admin SDK scope 付き user ADC が `impersonated_user_email` を無視してログインユーザー本人として成功していた既存構成をエラーにする未承認の互換性変更 | Human Decisions Required に追加し、本人として使う場合は `impersonated_user_email` を削除、DWD する場合は `service_account` を設定する移行手順と CHANGELOG 項目を追加した |
| 2 | quota project の `serviceusage.services.use` を元 credential の主体だけに要求する記述は、pinned impersonation library が対象 service account に要求する契約を満たさない | 元 credential の主体と対象 service account の要件を分け、対象 service account にも必要な場合と付与先 quota project を Strategy、docs、User-owned Actions に明記した |

この 2 件と既存の Human Decisions Required を除き、追加の critical technical gap はないとの判定だった。status は引き続き `Human Review Pending` であり、人間判断の反映後に `AI Critical Review Pending` へ進めて最終レビューする。

### 2026-09-03 2回目の再レビュー（fresh codex exec / gpt-5.6-sol）

次の P1 finding を検出した。

| # | Finding | 対応 |
|---|---|---|
| 1 | helper に `option.WithHTTPClient` で mock client を直接注入する test は、`WithHTTPClient` の優先順位により production の `option.WithCredentials` が付ける Bearer token と quota header を迂回する | `transport/http.NewTransport` と `option.WithCredentials` で認証・quota header 付き client を作る factory を production/test で共有し、recording base transport で `Authorization` と `X-Goog-User-Project` を検証する設計へ変更した。ADC JSON と環境変数の quota project 優先順位も verifier に追加した |

この finding 以外に P0 / P1 technical issue はなく、4件の Human Decisions Required と status だけが別の blocker と判定された。

### 2026-09-03 3回目の再レビュー（fresh codex exec / gpt-5.6-sol）

前回 finding への対応を pinned `google.golang.org/api` の transport 実装と照合した。production と test で共有する transport factory が元 credential の Bearer token と quota project header を recording base transport まで適用し、request-level verifier が実経路を検証できることを確認した。

追加の P0 / P1 technical issue はないとの判定だった。未解決なのは4件の Human Decisions Required と `Human Review Pending` の status だけであり、人間判断の反映後に `AI Critical Review Pending` へ進めて最終レビューする。

### 2026-09-03 人間レビュー後の最終レビュー（fresh codex exec / gpt-5.6-sol）

次の P1 finding を検出した。

| # | Finding | 対応 |
|---|---|---|
| 1 | Gmail sub-client は provider-level の設定が無くても `userId` を `ImpersonatedUserEmail` に設定するため、一般の fail-fast をそのまま適用すると direct user ADC の既存 Gmail 経路が壊れ、`impersonated_user_email` を削除する移行方法も使えない | Gmail 内部の subject を provider-level の impersonation 要求と区別する非公開 flag を route selector に追加し、user access token / user ADC の直接主体を維持する設計と behavioral test を追加した。`service_account` と service account key の DWD は引き続き `userId` を subject にする |

### 2026-09-03 最終再レビュー（fresh codex exec / gpt-5.6-sol）

上記 Gmail finding の対応と、承認済みの認証経路、互換性、quota project、IAM 要件、verifier、stop condition を repository code および pinned dependency と照合した。追加の P0 / P1 finding と implementation readiness blocker はなく、`Ready for Implementation` と判定した。`go mod verify` も成功した。

## 更新履歴

### 2026-09-03 実装完了

ADC と access token の service account impersonation を共通化し、ADC type による経路選択、private key を持たない ADC の fail-fast、quota project を保持する transport、Gmail sub-client の認証設定引き継ぎを実装した。schema の型・必須性は変更していない。unit test、docs、example、CHANGELOG を更新し、全 verifier と fresh-context code review（P0 / P1 finding なし）を通過した。

### 2026-09-03 初版

利用側リポジトリで `GOOGLE_OAUTH_ACCESS_TOKEN` の前置なしに実行した際の `403 ACCESS_TOKEN_SCOPE_INSUFFICIENT` を受け、provider の ADC 経路に service account impersonation が無いことを原因として特定した。wrapper、ADC の scope 追加、service account key の各案と比較し、provider 側で ADC からも同じ impersonation 経路を通す方針を人間が選択した。planning 規約のフォーマットで plan を作成し、Human Review Pending とした。

### 2026-09-03 人間レビュー反映

Human Decisions Required の推奨4案が承認された。Gmail send-as alias の認証情報引き継ぎ、service account key 型 ADC の直接 DWD 維持、不成立構成の fail-fast、quota project の引き継ぎと必要な IAM 要件を Default Decisions に確定し、status を `AI Critical Review Pending` に進めた。

### 2026-09-03 AI critical review 完了

人間レビュー後の review で Gmail の直接主体と内部 subject の競合を検出し、既存互換を維持する設計と verifier を追加した。fresh review で critical finding と blocker が残っていないことを確認し、status を `Ready for Implementation` に進めた。
