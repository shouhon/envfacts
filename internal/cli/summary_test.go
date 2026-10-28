package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/shouhon/envfacts/internal/cli"
	"github.com/shouhon/envfacts/internal/scan"
)

type summaryResult struct {
	SchemaVersion int    `json:"schema_version"`
	OutputFormat  string `json:"output_format"`
	Status        string `json:"status"`
	Coverage      struct {
		ScoopRoots        []scan.Root `json:"scoop_roots"`
		CommandSelection  string      `json:"command_selection"`
		RequestedCommands []string    `json:"requested_commands"`
		SearchPathCounts  struct {
			Complete   int `json:"complete"`
			Absent     int `json:"absent"`
			Incomplete int `json:"incomplete"`
		} `json:"search_path_counts"`
		Extensions []string `json:"extensions"`
		Excluded   []string `json:"excluded"`
	} `json:"coverage"`
	PackageCount     int            `json:"package_count"`
	Commands         []scan.Command `json:"commands"`
	InvocationStatus string         `json:"invocation_status"`
	IssueCount       int            `json:"issue_count"`
	Issues           []struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Count   int    `json:"count"`
	} `json:"issues"`
	DetailsHint string `json:"details_hint"`
}

func decodeSummary(t *testing.T, data []byte) summaryResult {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	var s summaryResult
	if err := decoder.Decode(&s); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		t.Fatal("要約がJSONオブジェクト1件ではありません。")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if _, exists := raw["packages"]; exists {
		t.Fatal("要約にパッケージの全件一覧があります。")
	}
	if s.SchemaVersion != 1 || s.OutputFormat != "summary" || s.InvocationStatus != "not_tested" || !strings.Contains(s.DetailsHint, "--json") || !strings.Contains(s.DetailsHint, "--output") {
		t.Fatal("要約形式、起動未確認または詳細への案内が不正です。")
	}
	return s
}

func addSummaryPackage(t *testing.T, base, name, manifest string) string {
	t.Helper()
	current := filepath.Join(base, "user", "apps", name, "current")
	if err := os.MkdirAll(current, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(current, "scoop-manifest.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	return current
}

func setupSummaryScan(t *testing.T) string {
	t.Helper()
	base := setupScan(t)
	current := addSummaryPackage(t, base, "engine-package", `{"version":"1.2.3","bin":[["engine.exe","tool","fixture-secret-do-not-report"]]}`)
	shims := filepath.Join(base, "user", "shims")
	if err := os.MkdirAll(shims, 0o700); err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{
		filepath.Join(current, "engine.exe"):             "実行禁止",
		filepath.Join(shims, "tool.exe"):                 "実行禁止",
		filepath.Join(shims, "tool.shim"):                "path = \"" + filepath.Join(current, "engine.exe") + "\"\nargs = fixture-secret-do-not-report",
		filepath.Join(os.Getenv("PATH"), "outsider.exe"): "実行禁止",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", shims+";"+os.Getenv("PATH")+";"+filepath.Join(base, "存在しない探索先"))
	return base
}

func TestScanSummaryMatchesDetailedResults(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		names        []string
		incomplete   bool
		code         int
	}{
		{name: "導入版と競合候補", names: []string{"tool"}, status: "complete"},
		{name: "版不明", names: []string{"outsider"}, status: "complete"},
		{name: "名前不足", names: []string{"missing"}, status: "missing", code: 1},
		{name: "不完全と名前不足の併存", names: []string{"missing", "tool"}, incomplete: true, status: "incomplete", code: 2},
		{name: "Scoopの公開名", status: "complete"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := setupSummaryScan(t)
			if tc.incomplete {
				addSummaryPackage(t, base, "unrelated-a", `{"version":"1.0","env_add_path":"bin"}`)
				addSummaryPackage(t, base, "unrelated-b", `{"version":"1.0","env_add_path":"bin"}`)
				addSummaryPackage(t, base, "broken", `{`)
			}
			args := []string{"scan"}
			for _, name := range tc.names {
				args = append(args, "--command", name)
			}
			var fullOut, fullErr, summaryOut, summaryErr bytes.Buffer
			fullCode := cli.Run(append(append([]string{}, args...), "--json"), &fullOut, &fullErr)
			summaryCode := cli.Run(append(append([]string{}, args...), "--json-summary"), &summaryOut, &summaryErr)
			r := decodeReport(t, fullOut.Bytes())
			s := decodeSummary(t, summaryOut.Bytes())
			if fullCode != tc.code || summaryCode != tc.code || s.Status != tc.status || s.Status != r.Status {
				t.Fatalf("状態または終了コードが一致しません: 詳細=%d、要約=%d、状態=%s", fullCode, summaryCode, s.Status)
			}
			if !reflect.DeepEqual(s.Commands, r.Commands) || s.PackageCount != len(r.Packages) || s.IssueCount != len(r.Issues) || !reflect.DeepEqual(s.Coverage.ScoopRoots, r.Coverage.ScoopRoots) || !reflect.DeepEqual(s.Coverage.RequestedCommands, r.Coverage.RequestedCommands) || s.Coverage.CommandSelection != r.Coverage.CommandSelection || !reflect.DeepEqual(s.Coverage.Extensions, r.Coverage.Extensions) || !reflect.DeepEqual(s.Coverage.Excluded, r.Coverage.Excluded) {
				t.Fatal("要約でコマンドの事実、調査範囲または診断を失っています。")
			}
			if s.Coverage.SearchPathCounts.Complete != 2 || s.Coverage.SearchPathCounts.Absent != 1 || s.Coverage.SearchPathCounts.Incomplete != 0 {
				t.Fatal("PATHの調査範囲の集計が不正です。")
			}
			counts := map[[2]string]int{}
			for _, issue := range r.Issues {
				counts[[2]string{issue.Code, issue.Message}]++
			}
			if len(s.Issues) != len(counts) {
				t.Fatal("診断の集約件数が不正です。")
			}
			for _, issue := range s.Issues {
				if issue.Count != counts[[2]string{issue.Code, issue.Message}] || strings.Count(summaryErr.String(), issue.Code) != 1 {
					t.Fatal("診断の意味や件数を失ったか、標準エラーで重複しています。")
				}
			}
			if (summaryErr.Len() > 0) != tc.incomplete || strings.Contains(summaryOut.String()+summaryErr.String(), "fixture-secret") {
				t.Fatal("診断の出力先または余分な値の保護が不正です。")
			}
			for _, c := range s.Commands {
				if c.Name == "tool" && (c.Version == nil || *c.Version != "1.2.3" || c.VersionSource == nil || len(c.Candidates) != 2 || c.Candidates[0].PathPriority != 1 || c.Candidates[1].PathPriority != 2 || c.Candidates[1].Priority != 2) {
					t.Fatal("既知の導入版、版の根拠または候補の優先順位が不正です。")
				}
				if c.Name == "outsider" && (c.Status != "resolved" || c.Version != nil || c.VersionReason == "" || c.InvocationStatus != "not_tested") {
					t.Fatal("版不明と起動未確認の区別が不正です。")
				}
			}
		})
	}
}

func TestScanSummarySizeDoesNotGrowWithUnrelatedPackages(t *testing.T) {
	base := setupScan(t)
	addSummaryPackage(t, base, "unrelated-first", `{"version":"1.0","env_add_path":"bin"}`)
	var beforeOut, beforeErr, afterOut, afterErr bytes.Buffer
	args := []string{"scan", "--command", "tool", "--json-summary"}
	if code := cli.Run(args, &beforeOut, &beforeErr); code != 2 {
		t.Fatalf("追加前の終了コード = %d", code)
	}
	for i := range 100 {
		addSummaryPackage(t, base, fmt.Sprintf("unrelated-%03d", i), `{"version":"1.0","env_add_path":"bin"}`)
	}
	if code := cli.Run(args, &afterOut, &afterErr); code != 2 {
		t.Fatalf("追加後の終了コード = %d", code)
	}
	before, after := decodeSummary(t, beforeOut.Bytes()), decodeSummary(t, afterOut.Bytes())
	if after.PackageCount != 101 || after.IssueCount != 101 || len(after.Issues) != 1 || after.Issues[0].Count != 101 || !reflect.DeepEqual(before.Commands, after.Commands) {
		t.Fatal("パッケージ追加後の集計またはコマンドの結果が不正です。")
	}
	if afterOut.Len() > beforeOut.Len()+128 || afterErr.Len() > beforeErr.Len()+32 || strings.Contains(afterOut.String()+afterErr.String(), "unrelated-") {
		t.Fatalf("無関係なパッケージ数に応じて出力が増えています: 標準出力%d→%d、標準エラー%d→%dバイト", beforeOut.Len(), afterOut.Len(), beforeErr.Len(), afterErr.Len())
	}
}

func TestScanSummaryArgumentsAndOutputFailure(t *testing.T) {
	for _, args := range [][]string{
		{"scan", "--json", "--json-summary"}, {"scan", "--json-summary", "--json"},
		{"scan", "--command", "--json-summary"}, {"scan", "--command", "../fixture-secret", "--json-summary"},
		{"scan", "--unknown", "fixture-secret", "--json-summary"},
	} {
		var stdout, stderr bytes.Buffer
		if code := cli.Run(args, &stdout, &stderr); code != 64 {
			t.Fatalf("引数エラーの終了コード = %d", code)
		}
		s := decodeSummary(t, stdout.Bytes())
		if s.Status != "invalid_arguments" || len(s.Coverage.ScoopRoots) != 0 || s.IssueCount != 1 || s.Issues[0].Code != "invalid_arguments" || stderr.Len() == 0 || strings.Contains(stdout.String()+stderr.String(), "fixture-secret") {
			t.Fatal("引数エラーで調査を始めたか、診断または値の保護が不正です。")
		}
	}
	setupScan(t)
	var stderr bytes.Buffer
	if code := cli.Run([]string{"scan", "--json-summary"}, failingWriter{}, &stderr); code != 2 || stderr.Len() == 0 {
		t.Fatal("要約の出力失敗を検出していません。")
	}
}

func TestScanSummarySavesDetailedRecord(t *testing.T) {
	base := setupSummaryScan(t)
	output := filepath.Join(base, "詳細 記録.json")
	args := []string{"scan", "--command", "tool", "--json-summary", "--output", output}
	var stdout, stderr bytes.Buffer
	if code := cli.Run(args, &stdout, &stderr); code != 0 {
		t.Fatalf("要約と詳細保存の終了コード = %d", code)
	}
	s := decodeSummary(t, stdout.Bytes())
	saved, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	r := decodeReport(t, saved)
	if len(r.Packages) != s.PackageCount || !reflect.DeepEqual(r.Commands, s.Commands) {
		t.Fatal("要約モードで詳細記録を保存していません。")
	}
	var diffOut, diffErr bytes.Buffer
	if code := cli.Run([]string{"diff", "--before", output, "--after", output, "--json"}, &diffOut, &diffErr); code != 0 {
		t.Fatalf("保存した詳細記録を比較できません: 終了コード=%d、診断=%s", code, &diffErr)
	}
	summaryPath := filepath.Join(base, "表示用要約.json")
	if err := os.WriteFile(summaryPath, stdout.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	diffOut.Reset()
	diffErr.Reset()
	if code := cli.Run([]string{"diff", "--before", summaryPath, "--after", summaryPath, "--json"}, &diffOut, &diffErr); code != 2 || !strings.Contains(diffErr.String(), "invalid_record") {
		t.Fatal("表示用要約を比較用の記録として受け入れています。")
	}
	stdout.Reset()
	stderr.Reset()
	if code := cli.Run(args, &stdout, &stderr); code != 2 {
		t.Fatalf("要約モードの上書き拒否の終了コード = %d", code)
	}
	s = decodeSummary(t, stdout.Bytes())
	if s.Status != "incomplete" || s.IssueCount != 1 || s.Issues[0].Code != "save_failed" || !reflect.DeepEqual(s.Commands, r.Commands) {
		t.Fatal("保存失敗で判明した事実または診断を失っています。")
	}
	after, err := os.ReadFile(output)
	if err != nil || !bytes.Equal(saved, after) {
		t.Fatal("既存の詳細記録を変更しています。")
	}
}
