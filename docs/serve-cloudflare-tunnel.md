# serve を Cloudflare Tunnel + Access でインターネット公開する

`agent-tasks serve` ([docs/details.md](details.md) の「インターネット公開」節も参照) を、外出先・
スマホからも見られるように **Cloudflare Tunnel** で公開し、**認証は serve 側に足さず
Cloudflare Access (Zero Trust)** に任せる手順。serve は無認証・`127.0.0.1` バインドのまま保ち、
公開経路を Tunnel だけに一本化することで「Cloudflare を迂回した直アクセス」を塞ぐ。

> このページのホスト名・メール等はすべて例 (`tasks.example.com` / `you@example.com`)。自分の値に
> 読み替えること。

## 構成

```
スマホ ──HTTPS──▶ Cloudflare エッジ (Access で認証) ──Tunnel──▶ cloudflared ──▶ 127.0.0.1:8080 (serve)
```

- serve は無認証のままでよい。手前の Cloudflare Access が SSO (Google / GitHub / One-time PIN) で
  認証し、通過した人だけを serve へ通す。
- **serve をポート開放しない** (`--addr :8080` などで LAN/全公開しない)。cloudflared は同じマシンの
  `127.0.0.1:8080` に繋ぐので、serve は既定バインドのままでよい。ローカル限定を保つと、公開経路が
  Tunnel だけになり、Access を迂回する直アクセスが構造的に不可能になる。
- **クイックトンネル (`trycloudflare.com`) には Access を貼れない**。認証をかけるには
  **named tunnel + Cloudflare 管理下の独自ゾーン** が必須 (以下はこれ前提)。

## 前提

- Cloudflare アカウントと、そのアカウントで管理しているゾーン (例 `example.com`) があること。
- `cloudflared` がインストール済みであること (入手方法は
  [Cloudflare のダウンロードページ](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/downloads/)
  を参照)。

## セットアップ (一度きり)

### 1. Cloudflare にログイン

```sh
cloudflared tunnel login    # ブラウザが開き、対象ゾーンを認可 → ~/.cloudflared/cert.pem が作られる
```

### 2. named tunnel 作成 + DNS 割当

```sh
cloudflared tunnel create agent-tasks                          # 資格情報 ~/.cloudflared/<UUID>.json 生成
cloudflared tunnel route dns agent-tasks tasks.example.com     # 公開ホスト名の CNAME をトンネルへ
cloudflared tunnel list                                        # UUID を確認
```

`route dns` が作る CNAME は `<UUID>.cfargotunnel.com` を指す **プロキシ固定 (グレー雲にできない)**
レコード。だから「うっかり DNS only にして Cloudflare を迂回」という事故が起きない。

### 3. 専用設定ファイルを作る

複数トンネルを使う環境でグローバルな `~/.cloudflared/config.yml` を上書きしないよう、**トンネル専用の
設定ファイル** (`~/.cloudflared/agent-tasks.yml`) にして、起動時に `--config` で明示する。

```yaml
# ~/.cloudflared/agent-tasks.yml
tunnel: agent-tasks
credentials-file: /home/USER/.cloudflared/<UUID>.json

ingress:
  - hostname: tasks.example.com
    service: http://127.0.0.1:8080   # serve の既定バインド先へ中継
  - service: http_status:404         # catch-all (最後に必須)
```

UUID を手で書き写す代わりに、`tunnel list` から引いて生成すると間違いがない:

```sh
TUNNEL=agent-tasks
HOST=tasks.example.com
UUID=$(cloudflared tunnel list | awk -v n="$TUNNEL" '$2==n{print $1}')
cat > ~/.cloudflared/$TUNNEL.yml <<EOF
tunnel: $TUNNEL
credentials-file: $HOME/.cloudflared/$UUID.json

ingress:
  - hostname: $HOST
    service: http://127.0.0.1:8080
  - service: http_status:404
EOF
cloudflared tunnel --config ~/.cloudflared/$TUNNEL.yml ingress validate   # OK が出れば妥当
```

### 4. Cloudflare Access で認証をかける (公開前に先に)

Zero Trust ダッシュボード (https://one.dash.cloudflare.com/) で公開ホスト名を保護する。**トンネルを
起動する前にここを作れば、無防備な瞬間が生まれない。**

> UI は 2026 時点。名称は変わりうるので、対応する項目を読み替えること
> (旧 UI では「Access → Applications → Add an application → Self-hosted」だった)。

1. 左メニュー **Access controls → Applications → Add an application**
2. タイプは **Self-hosted and private** タブ → サブタブ **Public DNS** を選択 → **Continue**
   - 公開ホスト名 (Tunnel 経由でも公開 DNS 名) を守るので **Public DNS**。
     Private destinations は WARP 前提の内部ネットワーク用なので使わない。
3. **Application details → Destinations → Public hostnames**:
   Subdomain `tasks` / Domain `example.com` / Path 空 (= `tasks.example.com`)。
   RDP/SSH/VNC のトグルは Off でよい。
4. **Access policies** (Builder) でポリシーを 1 つ作る:
   - Policy Name 例 `me` / Action **Allow**
   - Include: セレクタ **Emails** / 値 = 許可するメール (`you@example.com`。複数可)
   - **「Save policy」を必ず押す** (押さないと Preview が「No policies added」のまま)
5. **Authentication**: identity providers は「Select all」でよい (通れるのはポリシーの Emails だけ)。
   認証手段が無ければ既定の **One-time PIN** (メールにコード) が使える。Google/GitHub 連携も可。
6. **Details**: Name / Session Duration (例 `24 hours`) を設定。
7. 最下部 **Create**。確定前に Preview に POLICIES と DESTINATIONS が反映されているか確認する。

これで `tasks.example.com` にアクセスすると Cloudflare のログインが挟まり、許可メールで認証した
セッションだけが serve に到達する。

## 日々の起動 (公開する / やめる)

serve と tunnel の 2 プロセスが動いている間だけ公開される。両方止めれば外から届かなくなる。

```sh
# 端末 A: serve (127.0.0.1:8080 のみ。--addr は付けない = LAN 非公開)
agent-tasks serve

# 端末 B: トンネル (専用 config を明示)
cloudflared tunnel --config ~/.cloudflared/agent-tasks.yml run agent-tasks
```

- **必要なときだけ公開**するなら都度起動し、終わったら Ctrl-C で止める。
- **常時公開**するなら常駐化する (下記)。

### 検証 (迂回不可の確認)

```sh
# serve が localhost 限定か (これだけが見えるのが正しい。0.0.0.0/*:8080 が出たら LAN 公開になっている)
ss -ltnp | rg ':8080'                        # → LISTEN 127.0.0.1:8080 のみ

# 公開 URL は未認証だと Access のログインへ 302 (serve の中身は出ない)
curl -s -o /dev/null -w '%{http_code}\n' https://tasks.example.com/    # → 302

# LAN の実 IP:8080 への直アクセスは繋がらない (迂回路が無い)
curl -s -o /dev/null -w '%{http_code}\n' --max-time 3 http://<このマシンのLAN_IP>:8080/  # → 000 (不可)
```

## 常時公開したい場合 (任意)

- cloudflared を常駐化する。グローバル config を使う `sudo cloudflared service install` もあるが、
  専用 config で常駐させたいなら systemd user unit に
  `cloudflared tunnel --config %h/.cloudflared/agent-tasks.yml run agent-tasks` を書くのが無難。
- serve 側も systemd user unit などで自動起動しておく (この repo はプロセス管理まではしない)。

## トラブルシュート / メンテ

- **502 / つながらない**: serve が起動しているか、`ingress` の service が `127.0.0.1:8080` か確認。
- **未認証でも中身が見える (Access が効かない)**: Application の Public hostname がホスト名と完全一致
  しているか、ポリシーが保存 (Create) 済みか確認。
- **トンネル情報**: `cloudflared tunnel info <name>` / 削除は `cloudflared tunnel delete <name>`
  (資格情報 JSON も無効化。併せて DNS レコードと Access Application も消す)。
- **「Migrate ... locally configured tunnel」の案内**: ローカル管理 (設定ファイル) の ingress を
  ダッシュボード管理へ移す一方通行の機能。**不可逆**で、本手順のローカル config 運用とは前提が変わる
  (トークン起動になる)。単一ホストの個人用途では **移行不要**。

## なぜ serve に認証を足さないか

認証は枯れて監査もできる Cloudflare Access に寄せた方が安全で、serve は「ローカルの読み取り専用
ダッシュボード」という単純さを保てる。直アクセスの遮断は「localhost バインド + 公開経路を Tunnel に
一本化」で担保する (serve をポート開放した瞬間にこの前提が崩れるので、`--addr` での公開はしない)。
