package cli_test

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"envfacts/internal/cli"
	"envfacts/internal/diff"
	"envfacts/internal/scan"
)

func saveDiffInput(t *testing.T, path string, r scan.Report) {
	t.Helper()
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func decodeDiff(t *testing.T, data []byte) diff.Report {
	t.Helper()
	d := json.NewDecoder(bytes.NewReader(data))
	var result diff.Report
	if err := d.Decode(&result); err != nil {
		t.Fatal(err)
	}
	if err := d.Decode(new(any)); err != io.EOF || result.Command != "diff" || result.SchemaVersion != 1 || result.InvocationStatus != "not_tested" || result.Changes == nil {
		t.Fatal("比較結果のJSON形式が不正です。")
	}
	return result
}

func TestDiffCLIOutputAndExitCodes(t *testing.T) {
	base := setupScan(t)
	var scanOutput, diagnostics bytes.Buffer
	if code := cli.Run([]string{"scan", "--json"}, &scanOutput, &diagnostics); code != 0 {
		t.Fatal("比較用の模擬記録を作成できません。")
	}
	r := decodeReport(t, scanOutput.Bytes())
	before, after := filepath.Join(base, "前の 記録.json"), filepath.Join(base, "後の記録.json")
	saveDiffInput(t, before, r)
	for _, tc := range []struct {
		name, status string
		code         int
		mutate       func(*scan.Report)
	}{
		{"差分なし", "unchanged", 0, func(*scan.Report) {}},
		{"優先順の変更", "changed", 1, func(r *scan.Report) {
			r.Coverage.Extensions[0], r.Coverage.Extensions[1] = r.Coverage.Extensions[1], r.Coverage.Extensions[0]
		}},
		{"差分と不完全", "incomplete", 2, func(r *scan.Report) {
			r.Coverage.Extensions[0], r.Coverage.Extensions[1] = r.Coverage.Extensions[1], r.Coverage.Extensions[0]
			r.Status = "incomplete"
			r.Issues = []scan.Issue{{Code: "fixture-secret", Message: "fixture-secret", Source: "fixture-secret"}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := decodeReport(t, scanOutput.Bytes())
			tc.mutate(&input)
			saveDiffInput(t, after, input)
			for _, jsonOutput := range []bool{false, true} {
				var stdout, stderr bytes.Buffer
				args := []string{"diff", "--after", after, "--before", before}
				if jsonOutput {
					args = append(args, "--json")
				}
				if code := cli.Run(args, &stdout, &stderr); code != tc.code {
					t.Fatalf("終了コード = %d、期待値 = %d、診断 = %s", code, tc.code, stderr.String())
				}
				if jsonOutput {
					result := decodeDiff(t, stdout.Bytes())
					if result.Status != tc.status || tc.code != 0 && len(result.Changes) == 0 {
						t.Fatal("既知の変更または状態が不正です。")
					}
				} else if !strings.Contains(stdout.String(), "比較:") {
					t.Fatal("日本語の比較結果がありません。")
				}
				if (stderr.Len() > 0) != (tc.code == 2) || strings.Contains(stdout.String()+stderr.String(), "fixture-secret") {
					t.Fatal("出力の分離または余分な情報の保護が不正です。")
				}
			}
		})
	}
	saveDiffInput(t, after, r)
	var stderr bytes.Buffer
	if code := cli.Run([]string{"diff", "--before", before, "--after", after, "--json"}, failingWriter{}, &stderr); code != 2 || stderr.Len() == 0 {
		t.Fatal("比較結果の出力失敗を検出していません。")
	}
	if err := os.WriteFile(after, []byte(`{"fixture-secret":`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	stderr.Reset()
	if code := cli.Run([]string{"diff", "--before", before, "--after", after, "--json"}, &stdout, &stderr); code != 2 || decodeDiff(t, stdout.Bytes()).Status != "incomplete" || strings.Contains(stdout.String()+stderr.String(), "fixture-secret") {
		t.Fatal("破損した入力のJSON報告が不正です。")
	}
}

func TestDiffInvalidArguments(t *testing.T) {
	for _, args := range [][]string{
		{"diff"}, {"diff", "--before", "one"}, {"diff", "--after", "one"},
		{"diff", "--before", "--json", "--after", "two"},
		{"diff", "--before", "one", "--after", "two", "--before", "three"},
		{"diff", "--before", "one", "--after", "two", "--after", "three"},
		{"diff", "--before", "one", "--after", "two", "--output", "out"},
		{"diff", "--before", "one", "--after", "two", "--command", "tool"},
		{"diff", "--before", "one", "--after", "two", "fixture-secret"},
	} {
		var stdout, stderr bytes.Buffer
		args = append(args, "--json")
		if code := cli.Run(args, &stdout, &stderr); code != 64 {
			t.Fatalf("終了コード = %d、引数 = %v", code, args)
		}
		r := decodeDiff(t, stdout.Bytes())
		if r.Status != "invalid_arguments" || r.Coverage.Before != nil || r.Coverage.After != nil || stderr.Len() == 0 || strings.Contains(stdout.String()+stderr.String(), "fixture-secret") {
			t.Fatal("不正な引数で比較を始めているか、値を診断に出力しています。")
		}
	}
}
