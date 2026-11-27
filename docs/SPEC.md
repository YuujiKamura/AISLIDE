# AISLIDE 仕様（v0）

AISLIDE は Google スライド（ネイティブ）を「画面」とし、AICAD の Control Plane（CP 9876）と同じ作法で
規制図を作図・編集する常駐プロセス。人はブラウザで直接描き、AI は CP に 1 行コマンドを送る。

## 0. 設計原則（AICAD から継承）

1. **常駐 + CP**: `127.0.0.1:9877` TCP、1 行コマンド → 1 行応答（UTF-8 固定、改行は `\n`）。
   応答は `ok ...` / `ng <理由>` で始まる。複数行結果は 1 行に `; ` 区切り、または `list` のみ
   `ok n=<件数>` の後に件数分の行を返し、最後に空行。
2. **1 エージェント = 1 レイヤ**: `claim <layer> agent=<id>` で所有。他人のレイヤには書けない（ng）。
3. **部品を置く**: 生の図形ではなく part（site / sign / cone_line / guard / closure / detour / note）。
   基準値（コーン間隔 3〜5m 等）は部品側に埋め込む。距離は georef により**実 m**で扱う。
4. **編集できるのはエージェント部品だけ**: タグの無い要素（人が描いた線・元からの凡例等）は `base`
   として読み取り専用。取り込むには `adopt <i> <layer> agent=<id>`。
5. **index で指す**: `list` が返す index（または objectId）で対象を指す。**文字検索による指定は無い**。
   一括操作は `clear <layer>`（自レイヤのみ）だけ。
6. **人とAIが同じモデル**: 人がブラウザで部品を動かしたら、同期（`sync` / 定期ポーリング）で
   部品の位置パラメータ（lat/lon）を更新する。
7. **書込み前に revision 確認**: batchUpdate に `writeControl.requiredRevisionId` を付ける。
   不一致なら自動で再読込し `ng stale` を返す（勝手に再試行しない）。
8. **undo**: 全書込みの逆操作を journal に記録し `undo` で戻す。

## 1. タグ（要素の代替テキスト description に保存）

```
kisei:{"layer":"signs","owner":"agent1","kind":"sign","params":{...}}
```
- 背景写真: `kisei:{"layer":"map","kind":"map_bg","georef":{"z":17,"center":[lat,lon],"px":[w,h],"sites":{"1":[lat,lon],...}}}`
- 旧試作（python kisei_live）の `kisei_live:{georef}` も読めること（移行互換）。
- objectId は `kc_<layer>_<8hex>`。mask は `kl_mask_*`（旧）/ `kc_map_mask_*`。
- タグ無し = layer `base`, owner なし, 読み取り専用。

## 2. 座標

- 紙面 mm（A3 横 420x297、原点左上、EMU = mm × 36000）。
- 地理座標 `lat,lon` / `site:N` → georef（WebMercator、背景画像の現在位置・サイズから逆算）で mm へ。
- 引数の点表記: `32.7695,130.7783` | `site:2` | `mm:120.5,88` 。
- 長さ: `40m`（実距離）| `12mm`（紙面）。

## 3. コマンド（v0）

| 分類 | コマンド | 応答 |
|---|---|---|
| 接続 | `open <presId or URL>` | `ok rev=<id> n=<要素数> georef=yes/no` |
| | `state` | `ok pres=<id> rev=<id> layers=<n> pick=<i or ->` |
| | `sync` | 再読込。`ok rev=<id> changed=<人が動かした部品数>` |
| 表示 | `view fit sites=1,2,3 [margin=1.3]` / `view center <pt> width=<len>` | 背景を動かす（map レイヤ専有） |
| 一覧 | `list [layer]` | 行: `<i> <layer> <kind> <objectId> x=<mm> y=<mm> w= h= [@lat,lon] [「text」]` |
| | `layers` / `kinds` | |
| 指差し | `pick <i>` | 共有スライド上に青枠（layer `_cursor`）を置く。人のブラウザにも即見える |
| | `unpick` | 青枠を消す |
| | `pickat <pt> [tol=3mm]` | 最も近い要素の index |
| 作図 | `claim <layer> agent=<id>` | |
| | `part <layer> <kind> agent=<id> k=v ...` | 部品を置く。`ok i=<index> id=<objectId>` |
| 編集 | `move <i> <dx> <dy> [m\|mm]` / `moveto <i> <pt>` / `settext <i> <文字>` / `delete <i>` | 自レイヤのみ（agent= 必須） |
| | `adopt <i> <layer> agent=<id>` | base 要素にタグを付けて取り込む |
| | `clear <layer> agent=<id>` | |
| | `undo [agent=<id>]` | 直近の書込みを戻す |
| 検査 | `capture <abs.png>` | pages.getThumbnail(LARGE) を保存 |
| | `drc` | `[規則ID] i=<index> 事実` を `; ` 区切り。感想禁止 |
| 座標 | `georef image=<i> z=<z> center=<lat,lon> px=<w>x<h> [site1=lat,lon ...]` | |

### part kinds（v0）
- `site n=<番号> at=<pt>` 赤丸白数字（中は数字だけ。丸囲み文字は使わない）
- `sign key=<凡例番号 ①..> at=<pt> [w=12mm]` 凡例の看板画像（legend の image）を複製し、右肩に番号バッジ
- `cone_line from=<pt> to=<pt> [pitch=4m]` pitch は 3〜5m に制限（範囲外は ng）
- `guard k=A|B at=<pt>` 誘導員（丸+文字）
- `closure path=<pt>;<pt>;...` 通行止め範囲（赤実線 4pt）
- `detour path=<pt>;<pt>;...` 迂回路（青破線）
- `note at=<pt> t=<文字>` 引出し注記（短文のみ、20 文字超は ng）

## 4. パッケージ境界（並行作業の契約）

```
internal/geo      WebMercator, Georef{Z,Center,PxW,PxH}, ToMM/ToLatLon（画像 box から逆算）。純関数
internal/model    Deck（要素一覧・タグ・index・所有権）。Slides JSON から構築。純データ
internal/parts    Build(kind, args, ctx) -> []Op（抽象操作）。基準値はここ。純関数
internal/slides   Slides/Drive API アダプタ。Load / Apply([]Op, requiredRev) / Thumbnail
internal/journal  逆操作の記録と undo
internal/cp       TCP サーバ + コマンド dispatch（model+parts+slides を束ねる）
cmd/aislide       main（daemon 起動）と `aislide send "<cmd>"` クライアント
```

`Op`（抽象操作、parts → slides の受け渡し型）は `internal/model/op.go` に定義済み。
slides アダプタは Op を batchUpdate リクエストへ変換する唯一の場所。

## 5. 認証

サービスアカウント鍵: 環境変数 `KISEI_SA_KEY`、なければ `G:\マイドライブ\_credentials\visionapi-*.json`。
スコープ presentations + drive.readonly。スライドは SA に編集者共有されている前提。

## 6. テスト

- `testdata/akitsu_presentation.json`: 秋津町秋田（43）の実スライド（presentations.get 生 JSON）。
  背景 p1_i4 に旧形式 georef、kl_mark_* 3 個、**人が描いた線 4 本（h1b5d0a27ea77b246_1_*）**を含む。
  → 人の線が base（読み取り専用）として列挙され、agent からの move/delete が ng になることをテストする。
- 実 API を叩くテストは `-tags live` のみ。

## 7. 追補（2026-11-27 ユーザー指摘）

### 7.1 層の分離：動かさない物はレイアウトへ
スライド上に大きな背景部品（航空写真・白マスク・凡例の枠・表題）があると、人がブラウザで看板等を
掴もうとしてそれらを掴んでしまう。Slides にロック機能は無いので、**固定物はスライドのレイアウト
（layout ページ）に置く**。レイアウト上の要素は通常の編集画面では選択されない。
- layout 層: map_bg（航空写真）, mask, legend_frame, title。`view` はレイアウト上の map_bg を動かす。
- slide 層: 人と agent が掴む部品のみ（看板・ラベル・線・目印・注記）。
- 移行コマンド `pin <i>`: 要素をレイアウトへ移す（画像は contentUrl から createImage で再作成し元を削除）。
  ※ レイアウトへの書込み可否・contentUrl からの再作成可否は試験用コピーで要検証。

### 7.2 取り消し線（下書き）と清書
- `strike <i> agent=<id>`: 対象に赤の斜線（layer `review`）を重ねる。対象自体は消さない。
- `finalize agent=<id>`: strike された対象と斜線をまとめて削除（journal に記録、undo 可）。

### 7.3 現場ルール参照
看板セットの要否は construction-skills `traffic-control-db` の FR-10 等に従う（例: 田園部小規模の
車両通行止では 100m先・徐行は不要、予告は各方向に 50m先を 1 枚）。
