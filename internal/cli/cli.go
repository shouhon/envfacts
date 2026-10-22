package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"envfacts/internal/scan"
)

const usage = `envfacts - ローカル環境のCLIの導入情報と名前解決結果を報告するツール

使い方:
  envfacts scan [--command <名前> ...] [--json] [--output <ファイル>]
  envfacts --help       日本語の使い方と調査範囲を表示する
  envfacts --version    版番号を表示する

scanのオプション:
  --command <名前>     指定した名前だけをPATHから探索する。繰り返し指定できる
  --json               標準出力にJSON記録を1件表示する
  --output <ファイル>  JSON記録を保存する。既存ファイルは上書きしない
  名前の指定がなければ、Scoopの導入情報で公開されたコマンドを探索する。
  diffは未実装です。

調査範囲:
  Windows 11・PowerShellを対象に、Scoopの利用者単位・全利用者共通の
  導入情報と、現在のプロセスのPATH・PATHEXTを読み取ります。
  対象CLIの実行、認証確認、ネットワーク接続、環境設定の変更は行いません。
  PowerShellの関数・エイリアス・実行ポリシー、WSL、コンテナーは対象外です。
  Scoopのスクリプト型shim、env_add_pathのコマンド列挙、no_junction設定は
  未対応として報告します。

終了コード:
  0 調査完了、1 明示した名前の不足、2 調査・保存・出力が不完全、64 引数の誤り
`

// Run は引数を処理し、指定された出力先へ書き込み、終了コードを返す。
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || len(args) == 1 && (args[0] == "--help" || args[0] == "-h" || args[0] == "--version") {
		output := usage
		if len(args) == 1 && args[0] == "--version" {
			output = "envfacts " + Version + "\n"
		}
		if _, err := io.WriteString(stdout, output); err != nil {
			fmt.Fprintln(stderr, "標準出力へ書き込めませんでした。")
			return 2
		}
		return 0
	}
	jsonOutput := slices.Contains(args, "--json")
	command := ""
	if args[0] == "scan" || args[0] == "diff" {
		command = args[0]
	}
	names, outputPath, valid := parseScan(args)
	if !valid {
		r := scan.NewReport(command)
		r.Status = "invalid_arguments"
		r.Issues = append(r.Issues, scan.Issue{Code: "invalid_arguments", Message: "引数が不正、または未実装のコマンドです。使い方: envfacts --help", Source: "arguments"})
		return writeReport(r, jsonOutput, stdout, stderr)
	}
	r := scan.Scan(names)
	if outputPath != "" {
		if err := saveReport(outputPath, r); err != nil {
			r.AddIssue("save_failed", "JSON記録を保存できませんでした。既存ファイルの上書きや保存先ディレクトリの作成は行いません。", outputPath)
		}
	}
	return writeReport(r, jsonOutput, stdout, stderr)
}

func parseScan(args []string) ([]string, string, bool) {
	if len(args) == 0 || args[0] != "scan" {
		return nil, "", false
	}
	var names []string
	outputPath := ""
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--json":
		case "--command", "--output":
			option := args[i]
			if i+1 == len(args) || args[i+1] == "" || strings.HasPrefix(args[i+1], "--") {
				return nil, "", false
			}
			i++
			if option == "--command" {
				if !scan.ValidCommandName(args[i]) {
					return nil, "", false
				}
				names = append(names, args[i])
			} else {
				if outputPath != "" {
					return nil, "", false
				}
				outputPath = args[i]
			}
		default:
			return nil, "", false
		}
	}
	return names, outputPath, true
}

func saveReport(path string, report scan.Report) error {
	if !filepath.IsLocal(filepath.Base(path)) {
		return fs.ErrInvalid
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(append(data, '\n'))
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		// 今回新規作成した不完全な記録だけを除去する。
		os.Remove(path)
		if writeErr != nil {
			return writeErr
		}
		return closeErr
	}
	return nil
}

func writeReport(r scan.Report, jsonOutput bool, stdout, stderr io.Writer) int {
	for _, issue := range r.Issues {
		fmt.Fprintf(stderr, "%s: %s\n", issue.Code, issue.Message)
	}
	var err error
	if jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		err = encoder.Encode(r)
	} else if r.Status != "invalid_arguments" {
		var text strings.Builder
		status := map[string]string{"complete": "完了", "missing": "指定した名前に不足あり", "incomplete": "不完全"}[r.Status]
		fmt.Fprintf(&text, "調査: %s\n導入パッケージ: %d件、対象コマンド: %d件\n", status, len(r.Packages), len(r.Commands))
		for _, c := range r.Commands {
			if c.PreferredPath == nil {
				fmt.Fprintf(&text, "%s: PATHから未検出\n", c.Name)
			} else {
				version := "版不明"
				if c.Version != nil {
					version = *c.Version
				}
				fmt.Fprintf(&text, "%s: %s（%s、候補%d件）\n", c.Name, *c.PreferredPath, version, len(c.Candidates))
			}
		}
		text.WriteString("起動・認証・ネットワーク接続は未確認です。詳細は --json で確認できます。\n")
		_, err = io.WriteString(stdout, text.String())
	}
	if err != nil {
		fmt.Fprintln(stderr, "標準出力へ書き込めませんでした。")
		return 2
	}
	return r.ExitCode()
}
