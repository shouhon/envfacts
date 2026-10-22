package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"envfacts/internal/diff"
)

func parseDiff(args []string) (string, string, bool) {
	before, after := "", ""
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--json":
		case "--before", "--after":
			option := args[i]
			if i+1 == len(args) || args[i+1] == "" || strings.HasPrefix(args[i+1], "--") {
				return "", "", false
			}
			i++
			if option == "--before" && before == "" {
				before = args[i]
			} else if option == "--after" && after == "" {
				after = args[i]
			} else {
				return "", "", false
			}
		default:
			return "", "", false
		}
	}
	return before, after, before != "" && after != ""
}

func writeDiffReport(r diff.Report, jsonOutput bool, stdout, stderr io.Writer) int {
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
		status := map[string]string{"unchanged": "差分なし", "changed": "差分あり", "incomplete": "不完全"}[r.Status]
		fmt.Fprintf(&text, "比較: %s\n共通の導入先: %d件、共通のコマンド: %d件、確認した変更: %d件\n", status, len(r.Coverage.Common.ScoopRoots), len(r.Coverage.Common.Commands), len(r.Changes))
		kinds := map[string]string{"added": "追加", "removed": "削除", "version_changed": "導入版変更", "actual_path_changed": "実体変更", "priority_changed": "優先順変更", "updated": "定義変更"}
		entities := map[string]string{"package": "パッケージ", "command": "コマンド", "candidate": "候補", "coverage": "探索規則"}
		for _, c := range r.Changes {
			name := c.ID
			if c.Command != "" {
				name = c.Command + ": " + name
			}
			before, _ := json.Marshal(c.Before)
			after, _ := json.Marshal(c.After)
			fmt.Fprintf(&text, "%s %s %s: %s → %s\n", kinds[c.Kind], entities[c.Entity], name, before, after)
		}
		text.WriteString("保存済み記録の共通範囲だけを比較しました。起動・認証・ネットワーク接続は未確認です。詳細は --json で確認できます。\n")
		_, err = io.WriteString(stdout, text.String())
	}
	if err != nil {
		fmt.Fprintln(stderr, "標準出力へ書き込めませんでした。")
		return 2
	}
	return r.ExitCode()
}
