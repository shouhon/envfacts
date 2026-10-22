package cli

import (
	"fmt"
	"io"
)

const usage = `envfacts - ローカル環境のCLIの導入情報と名前解決結果を報告するツール

使い方:
  envfacts --help       日本語の使い方と調査範囲を表示する
  envfacts --version    版番号を表示する

この開発初期版では、使い方と版番号の表示を利用できます。
scan と diff は未実装です。

実装予定のコマンド:
  envfacts scan [--command <名前> ...] [--json] [--output <ファイル>]
  envfacts diff --before <記録> --after <記録> [--json]

調査範囲:
  Windows 11・PowerShellを対象に、Scoopの利用者単位・全利用者共通の
  導入情報と、現在のプロセスのPATHを読み取ります。
  対象CLIの実行、認証確認、ネットワーク接続、環境設定の変更は行いません。
  PowerShellの関数・エイリアス、WSL、コンテナーは対象外です。
`

// Run は引数を処理し、指定された出力先へ書き込み、終了コードを返す。
func Run(args []string, stdout, stderr io.Writer) int {
	var output string
	switch {
	case len(args) == 0:
		output = usage
	case len(args) == 1 && (args[0] == "--help" || args[0] == "-h"):
		output = usage
	case len(args) == 1 && args[0] == "--version":
		output = "envfacts " + Version + "\n"
	default:
		fmt.Fprintln(stderr, "この開発初期版で利用できるのは --help と --version です。使い方: envfacts --help")
		return 64
	}

	if _, err := io.WriteString(stdout, output); err != nil {
		fmt.Fprintln(stderr, "標準出力へ書き込めませんでした。")
		return 2
	}
	return 0
}
