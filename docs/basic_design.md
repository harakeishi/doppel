# シャドーイング基盤 基本設計書（必要最小限）

## 1. 概要
本システムは、既存システム（API サーバー A: Ruby on Rails）から新規システム（API サーバー B: Go）への移行を安全に判定するための最小限のシャドーイング基盤である。Shadow Proxy と Shadow Server を中心に、HTTP リクエストの複製・記録、DB クエリの録画/再生、レスポンス比較を行う。

主目的は以下のとおり。

- 本番 DB（MySQL）を新システム B が直接参照せずに、A と同等の結果を返せるか検証する
- A と B の HTTP レスポンスや DB クエリ結果の差分を記録・可視化する
- 本番トラフィックを用いた移行可否の判断材料を最短経路で得る

## 2. システム構成概要
### 2.1 構成図（概念）
```
[Client]
   ↓
[Shadow Proxy] ———→ [API Server A (Ruby)] → [Production MySQL]
         └——→ [API Server B (Go)] —→（DBアクセスは Shadow Server 経由）

[Shadow Server]
  ├ HTTPリクエスト/レスポンス記録
  ├ DBクエリ録画API (/record)
  ├ DBクエリ再生API (/replay)
  ├ HTTPレスポンス比較
  └ Web UI 表示
```

### 2.2 コンポーネント概要
| コンポーネント | 役割 |
| --- | --- |
| Shadow Proxy | クライアントリクエストの複製、Trace ID 付与、A/B 双方への転送、結果記録 |
| API Server A | 既存の Rails アプリケーション。ActiveRecord から DB クエリ録画を Shadow Server に送信 |
| API Server B | 新規 Go アプリケーション。DB への直接アクセスを禁止し、Shadow Server 経由で録画結果を参照 |
| Shadow Server | DB 録画/再生 API、レスポンス比較、データ永続化、Web UI の提供 |
| Production MySQL | 本番データベース。Shadow 環境では A のみが直接参照 |

### 2.3 ネットワーク要件（最小）
- Shadow Proxy と Shadow Server は低遅延で通信できる内部ネットワークに置く。
- `/record`, `/replay` は内部向けとし、単純な共有シークレットでの署名認証または mTLS のいずれか一方を採用する。
- Trace ID を含むヘッダが途中で除去されないよう、リバースプロキシ設定を確認する。

## 3. 要求仕様
### 3.1 機能一覧
| 機能 | 説明 |
| --- | --- |
| HTTPプロキシ | Shadow Proxy が A/B に同時にリクエスト転送し、トレースIDを発行する |
| トレースID管理 | HTTP リクエスト単位の UUID を発行し、DBクエリ録画・再生に利用 |
| DBクエリ録画 | A が MySQL に投げた SQL / バインド値 / 結果セットを録画 |
| DBモック | B が投げるクエリは録画済みデータを返却（本番DBを参照しない） |
| HTTPレスポンス比較 | A/B の HTTPレスポンスを保存・比較し差分情報を記録 |
| Web UI | トレース一覧、詳細、DBクエリ一覧、レスポンス比較を表示 |

### 3.2 非機能要件（必要最小限）
- **可用性**: 記録失敗や Trace ID 衝突が発生した場合は B 側への転送のみ停止し、A へのレスポンスを優先して返す。
- **性能**: A への遅延増加を小さく保つため、Proxy と Shadow Server 間の処理は非同期キューでバッファリング可能とする。
- **セキュリティ**: 記録データは PII を含む可能性があるため、保存期間の上限（例: 7 日）を設定し、保存先の暗号化を推奨とする。

## 4. トレースとデータフロー
### 4.1 1 リクエストの基本フロー
1. クライアント → Shadow Proxy
2. Proxy で trace_id を発行し、リクエストを記録
3. Proxy から A/B に同一リクエストを転送
4. A のレスポンス → クライアントへ返却
5. B のレスポンス → 保存のみ
6. Rails（A）側で ActiveRecord をフックし、trace_id + sequence（クエリ順）付きで DB 結果を `/record` に送信
7. B 側はモック Driver で `/replay` に問い合わせて録画結果を取得
8. Shadow Server が A/B のレスポンスを格納し比較
9. Web UI で確認可能

### 4.2 データフロー詳細
- **前処理**: Proxy は毎リクエストで新規 `X-Trace-ID` を発行する（既存のトレースヘッダは保持しない）。
- **並列実行**: A と B への転送は goroutine で並列化し、A のレスポンスを優先的に返す。
- **エラー時**: `/record` が失敗した場合は B の DB 再生を停止し、A のレスポンスのみ返す。`/replay` で結果が無い場合は 404 を返し、B 側は上位でエラーをハンドリングする。

## 5. トレースID 仕様
- UUID v4 を採用。
- Proxy が必ず発行し、リクエストヘッダ `X-Trace-ID` にセット。
- 下流の Rails / Go / Shadow Server 間で伝播。
- DB クエリ録画 API は trace_id を必須パラメータとし、sequence も必須。
- Web UI では Trace ID をキーとして検索・フィルタリングを提供する。

## 6. データモデル（DB スキーマ案）
> SQLite を初期 DB とし、必要に応じて PostgreSQL / MySQL に移行可能な前提。

### 6.1 traces（リクエスト単位）
| カラム | 型 | 説明 |
| --- | --- | --- |
| trace_id(PK) | TEXT | UUID v4 |
| method | TEXT | GET/POST... |
| path | TEXT | リクエストパス |
| created_at | DATETIME | 記録時刻 |

### 6.2 http_requests
| カラム | 型 | 説明 |
| --- | --- | --- |
| trace_id | TEXT | traces への FK |
| headers | JSON | 送信ヘッダ |
| body | BLOB/TEXT | 送信ボディ（上限サイズ設定） |

### 6.3 http_responses
| カラム | 型 | 説明 |
| --- | --- | --- |
| trace_id | TEXT | traces への FK |
| target | TEXT | 'A' or 'B' |
| status | INTEGER | HTTP ステータスコード |
| headers | JSON | 受信ヘッダ |
| body | BLOB/TEXT | レスポンスボディ |
| duration_ms | INTEGER | Proxy からの計測時間 |

### 6.4 http_diffs
| カラム | 型 | 説明 |
| --- | --- | --- |
| trace_id | TEXT | PK |
| status_equal | BOOL | ステータスが一致するか |
| headers_equal | BOOL | ヘッダが一致するか（無視リスト適用後） |
| body_equal | BOOL | ボディが一致するか |
| body_diff_json | JSON | 差分結果（JSON パッチ形式など） |

### 6.5 db_queries
| カラム | 型 | 説明 |
| --- | --- | --- |
| trace_id | TEXT | traces への FK |
| sequence | INT | 1,2,3...（trace 内で昇順インデックス） |
| sql | TEXT | 実行 SQL |
| bindings | JSON | バインド値（型情報含む） |
| result | JSON | columns/rows を含む結果セット |
| duration_ms | INTEGER | A 側での実行時間 |

### 6.6 インデックス・運用
- traces.created_at, traces.path, http_diffs.body_equal にインデックスを付与し、UI での検索を高速化。
- db_queries(trace_id, sequence) にユニーク制約を付与し、再生の整合性を担保。
- 保存期間超過データのクリーンアップジョブを cron などで実行する。

## 7. 各機能詳細
### 7.1 HTTP プロキシ機能
- Go の `httputil.ReverseProxy` を利用。
- Proxy での処理:
  - trace_id を付与
  - リクエスト内容（パス・ヘッダ・ボディ）を DB に保存
  - A/B に同一リクエストを転送
  - A のレスポンスをクライアントに返却
  - A/B 双方のレスポンスを保存・比較
- 並列実行: A と B の HTTP 転送は goroutine で並列実行し、B 側の遅延がクライアントに影響しないようにする。
- 無視ヘッダ例: `Date`, `Server`, `X-Request-ID`, `Set-Cookie`（値が動的なもの）。

### 7.2 DB クエリ録画（`POST /record`）
- Rails 側の処理概要:
  - ActiveRecord の `sql.active_record` を subscribe
  - SQL / バインド値 / 結果セットを取得
  - trace_id は Thread-local 等に保持
  - `/record` に JSON を POST
- Shadow Server 側の処理:
  - JSON を db_queries テーブルに保存
  - sequence はトレース毎に単調増加
  - スキーマ検証エラー時は 400 を返却、重複登録時は 409 を返却

### 7.3 DB モック（`GET /replay` + カスタム Driver）
- B 側（Go）
  - database/sql の Custom Driver を作成
  - `QueryContext` が呼ばれたら:
    - ctx から trace_id を取得
    - sequence をインクリメント
    - `/replay?trace_id=xxx&sequence=N` を呼び出す
    - 結果セット JSON を driver.Rows に変換して返却
  - 未録画時の挙動: `/replay` が 404 の場合はエラーを返し、上位層でフォールバックレスポンスを生成
- Shadow Server 側
  - db_queries からトレース + sequence で結果を返す
  - 署名検証やレートリミットで B 以外のアクセスを拒否

### 7.4 HTTP レスポンス比較
- 比較対象
  - ステータスコード
  - ヘッダ（無視リストあり）
  - ボディ（JSON は構造比較、それ以外はバイト比較）
- 差分結果は http_diffs に記録。
- UI では JSON 差分をハイライトし、非 JSON はハッシュ一致/不一致のみ表示する。

### 7.5 Web UI
- 主な画面
  - **トレース一覧**: TraceID, Method, Path, Status(A/B), Body差分, Date をテーブル表示し、TraceID/Path/日付範囲で検索。
  - **トレース詳細**: リクエスト / レスンスの実内容表示、A/B のレスポンス比較、JSON 差分ハイライト。
  - **DB クエリ一覧**: 時系列にクエリを表示し、sequence で識別。結果セットの差分を表示。
- UI 技術要件（最小）
  - シンプルな SSR もしくは軽量フロントエンドで構築し、API は `/api/traces`, `/api/traces/{id}`, `/api/traces/{id}/queries` のみ提供。
  - テーブル表示はページネーションと単純な検索（TraceID・Path・日付）を備える。

## 8. 拡張方針（将来）
- モックサーバーを MySQL ワイヤプロトコル対応に拡張し、他言語の MySQL クライアントでも利用可能に。
- 差分レポートの自動生成・通知。
- 保存データのマスキング・サニタイズ機構の追加。
