# 記録比較

製品仕様の正本は [要件定義書](requirements.md)、入力の取り決めは [収集方式と記録形式](scan-format.md) です。`diff` は `schema_version: 1` の保存済み `scan` 記録2件だけを読み、導入先・PATH・対象CLIを調べ直しません。入力ファイルは変更しません。

```powershell
.\bin\envfacts.exe diff --before .\before.json --after .\after.json
.\bin\envfacts.exe diff --before .\before.json --after .\after.json --json
```

`--before` と `--after` はそれぞれ必須で、重複指定できません。`--command` と `--output` は `scan` 専用です。比較結果を保存する場合は標準出力をリダイレクトします。

## 比較する事実

- パッケージは `id`、コマンドは `name`、候補はコマンド名とWindowsの大文字小文字を区別しないパスで照合します。導入先の異なる同名パッケージを混同しません。
- 記録日時、JSONのキー順、パッケージ・コマンド・候補・情報源・集合の配列順だけでは変更にしません。候補は数値の `priority` で並べます。PATHは `priority`、拡張子は `coverage.extensions` の順が探索規則です。
- パッケージ・自動選択されたコマンド・候補の追加と削除、確認済みの導入版、導入先や候補の実体、優先候補と候補間の優先順を比較します。後順位で隠れた候補の実体変更も対象です。
- 分類、公開コマンド、関連パッケージ、候補の解決方式の変更は `updated` とします。根拠のファイル名や診断の文章の変化だけでは変更にしません。
- 両方が `not_found` のコマンドは不存在の一致として扱います。解決済みコマンドや導入パッケージの版が片側でも不明なら、`unknown_version` と `incomplete` を返し、同じ版とも版変更とも断定しません。候補の実体が未確認なら `unknown_target` とします。

## 共通範囲と不完全な比較

`coverage.before` と `coverage.after` は、正常に読み取れた入力の調査範囲を示します。記録自体が不正な側は `null` です。`coverage.common` は次の内容を持ちます。

| 項目 | 意味 |
| --- | --- |
| `scoop_roots` | 両方にある同じ `scope`・導入先。`complete` は両側でパッケージの列挙が完了したかを示す |
| `commands` | 両方の記録にあるコマンド名の集合 |
| `search_paths` | 両方で読み取れた、または不存在を確認したPATHのディレクトリ |
| `extensions` | 共通の拡張子の集合。ここでの並びは優先順位ではない |
| `package_inventory_comparable` | すべての導入先が同じで、両側の列挙が完了している |
| `command_inventory_comparable` | 明示した名前の集合が同じ、または両側がScoopの自動選択で導入先全体を比較できる |
| `resolution_comparable` | PATHのディレクトリ集合・拡張子集合が同じで、両側のPATH探索に未読範囲がない |

導入先、コマンドの選択方法・明示名、PATHの集合、拡張子の集合、対象外の範囲が異なれば `coverage_mismatch` として不完全にします。PATHや拡張子の優先順だけの違いは、範囲の違いにせず変更として扱います。

範囲が違っても、共通の導入先の既知のパッケージ、共通の名前、共通の探索先の候補は比較します。片側だけで探索した場所の候補を追加・削除と断定せず、探索先が変わった場合のコマンド全体の解決結果も変更と断定しません。共通候補どうしの実体や相対的な優先順は比較できます。

元の調査が不完全なら `incomplete_record` を返します。未読の導入先の欠落を削除と扱わず、マニフェストを読めず `kind: unknown` になっただけの分類・公開コマンドも定義変更としません。確認できた導入版などの変更は残します。

## 出力と入力検査

出力は `schema_version: 1`、`tool: envfacts`、`command: diff` です。記録日時は比較した時点のUTCです。`packages` と `commands` は空配列とし、比較で確認した事実を `changes`、比較範囲を `coverage` に出します。元の調査結果全体を再出力しません。`invocation_status` は常に `not_tested` です。

`changes` の各要素は `kind`、`entity`（`package` / `command` / `candidate` / `coverage`）、`id`、`field`、`before`、`after` を持ちます。候補の変更には対象のコマンドを示す `command` も付きます。`kind` は `added` / `removed` / `version_changed` / `actual_path_changed` / `priority_changed` / `updated` です。追加前・削除後は `null` です。1回の更新で複数の事実が変われば、それぞれを記録します。変更は対象・識別子・コマンド・項目・種類の順、診断はコード・情報源・説明の順に安定させます。

入力はUTF-8（先頭のBOMは許容）のJSONオブジェクト1件に限ります。必須項目、調査記録であること、状態値、パッケージ・コマンド・候補の識別子、関連パッケージの参照、探索先と候補の優先順位、最優先候補との整合性を確認します。JSONキーの重複は大文字小文字だけの違いも含めて拒否します。壊れたJSONや不整合は `invalid_record`、未対応の形式の版は `unsupported_schema_version`、読取不能や通常の記録ファイル以外は `unreadable_record` です。`diff` の結果や引数エラーの記録を `scan` の代わりに読みません。

未知の項目は比較せず、出力へ転記しません。元の `issues` の本文・値やJSONの解析例外も出力せず、必要な診断コードと日本語の説明に置き換えます。`--json` の標準出力はオブジェクト1件、診断は標準エラーです。

終了コードは、比較完了で差分なしが `0`、差分ありが `1`、比較が不完全・記録が不正・読取不能・出力失敗が `2`、引数の誤りが `64` です。既知の変更があっても、不完全なら `2` を優先します。
