# domain data source の plan

利用者が行う作業は以下だけです。`terraform apply` は実行しません。

## 1. Google 側の認証を準備する

1. Google Cloud で Admin SDK API を有効にし、ドメイン全体の委任を有効にした service account と JSON key を用意します。
2. Google Admin コンソールの「セキュリティ > アクセスとデータ管理 > API の制御 > ドメイン全体の委任」で、service account の OAuth client ID に次の scope を許可します。

   ```text
   https://www.googleapis.com/auth/admin.directory.domain.readonly
   ```

3. なりすまし先には、ドメインを参照できる管理者権限（該当する Domains 権限または Super Admin）を持つユーザーを選びます。

参考:

- [service account とドメイン全体の委任](https://developers.google.com/workspace/guides/create-credentials#service-account)
- [ドメイン全体の委任を設定する](https://developers.google.com/identity/protocols/oauth2/service-account#delegatingauthority)
- [Domains の管理者権限](https://knowledge.workspace.google.com/admin/users/administrator-privilege-definitions#domains)

## 2. 環境変数を設定する

```bash
export GOOGLEWORKSPACE_CREDENTIALS="/absolute/path/to/service-account-key.json"
export GOOGLEWORKSPACE_CUSTOMER_ID="C01234567"
export GOOGLEWORKSPACE_IMPERSONATED_USER_EMAIL="admin@example.com"
```

`GOOGLEWORKSPACE_CREDENTIALS` には、リポジトリ外に保存した JSON key のファイルパスを指定してください。key の内容をリポジトリへ保存しないでください。

## 3. リポジトリルートで実行する

Go 1.27 以上と Terraform 0.14 以上が必要です。

```bash
cd /path/to/terraform-provider-googleworkspace
make plan-domain DOMAIN=example.com
```

このコマンドはローカルの checkout から provider をビルドし、上記 data source を read-only scope で読み取ります。plan file と Terraform state は保存せず、`apply` も実行しません。

`terraform init` は不要です。上記コマンドだけを実行してください。
