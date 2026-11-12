package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/shouhon/envfacts/internal/cli"
	"github.com/shouhon/envfacts/internal/scan"
)

func TestPathOnlyOutputAndDetailedSave(t *testing.T) {
	base := setupSummaryScan(t)
	addSummaryPackage(t, base, "無関係な破損", `{"version":`)
	for _, format := range []string{"", "--json", "--json-summary"} {
		t.Run("表示形式"+format, func(t *testing.T) {
			output := filepath.Join(base, "path-only"+format+".json")
			args := []string{"scan", "--path-only", "--command", "TOOL", "--command", "tool", "--output", output}
			if format != "" {
				args = append(args, format)
			}
			var stdout, stderr bytes.Buffer
			if code := cli.Run(args, &stdout, &stderr); code != 0 || stderr.Len() != 0 {
				t.Fatalf("限定調査の終了コード = %d、診断 = %s", code, stderr.String())
			}
			saved, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			var report scan.Report
			if err := json.Unmarshal(saved, &report); err != nil {
				t.Fatal(err)
			}
			if report.SchemaVersion != 2 || !report.Coverage.PathOnly() || len(report.Coverage.ScoopRoots) != 0 || len(report.Packages) != 0 || len(report.Commands) != 1 || len(report.Commands[0].Candidates) != 2 || report.Commands[0].Version != nil {
				t.Fatal("保存記録がPATH限定の範囲と全候補を表していません。")
			}
			switch format {
			case "":
				for _, text := range []string{"指定名のPATH確認のみ", "対象外:", "導入元・導入版", "環境全体の調査は行っていません", "未確認"} {
					if !strings.Contains(stdout.String(), text) {
						t.Fatalf("通常表示に範囲と未確認の案内がありません: %s", text)
					}
				}
			case "--json":
				if !bytes.Equal(saved, stdout.Bytes()) {
					t.Fatal("詳細保存と表示が一致しません。")
				}
			case "--json-summary":
				var summary struct {
					SchemaVersion int            `json:"schema_version"`
					OutputFormat  string         `json:"output_format"`
					Coverage      scan.Coverage  `json:"coverage"`
					Commands      []scan.Command `json:"commands"`
					PackageCount  int            `json:"package_count"`
				}
				if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil || summary.SchemaVersion != 2 || summary.OutputFormat != "summary" || !summary.Coverage.PathOnly() || summary.PackageCount != 0 || !reflect.DeepEqual(summary.Commands, report.Commands) {
					t.Fatal("要約が限定範囲または候補を失っています。")
				}
			}
			stdout.Reset()
			if code := cli.Run([]string{"diff", "--before", output, "--after", output, "--json"}, &stdout, &stderr); code != 0 || decodeDiff(t, stdout.Bytes()).Status != "unchanged" {
				t.Fatal("限定記録どうしの比較が導入版未調査を失敗としています。")
			}
		})
	}
}

func TestPathOnlyRequiresExplicitNames(t *testing.T) {
	for _, args := range [][]string{
		{"scan", "--path-only", "--json"},
		{"scan", "--path-only", "--json-summary"},
		{"scan", "--path-only", "--path-only", "--command", "tool", "--json"},
		{"diff", "--path-only", "--json"},
	} {
		var stdout, stderr bytes.Buffer
		if code := cli.Run(args, &stdout, &stderr); code != 64 || stderr.Len() == 0 {
			t.Fatalf("不正な限定指定の終了コード = %d", code)
		}
		if strings.Contains(strings.Join(args, " "), "--json-summary") {
			if decodeSummary(t, stdout.Bytes()).Status != "invalid_arguments" {
				t.Fatal("引数エラーの要約が不正です。")
			}
		} else if decodeReport(t, stdout.Bytes()).Status != "invalid_arguments" {
			t.Fatal("引数エラーで調査しています。")
		}
	}
}
