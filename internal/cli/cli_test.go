package cli_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/shouhon/envfacts/internal/cli"
	"github.com/shouhon/envfacts/internal/scan"
)

func TestVersionWritesOnlyToStdout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"--version"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("終了コード = %d、期待値 = 0", code)
	}
	if got, want := stdout.String(), "envfacts "+cli.Version+"\n"; got != want {
		t.Errorf("標準出力 = %q、期待値 = %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Errorf("標準エラーに余分な出力があります: %q", stderr.String())
	}
}

func TestUnsupportedArgumentsDoNotReportSuccess(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{name: "未知のオプション", args: []string{"--unknown"}},
		{name: "余分な引数", args: []string{"--version", "extra"}},
		{name: "調査の不正な引数", args: []string{"scan", "--unknown"}},
		{name: "比較の記録指定なし", args: []string{"diff"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := cli.Run(tc.args, &stdout, &stderr); code != 64 {
				t.Errorf("終了コード = %d、期待値 = 64", code)
			}
			if stdout.Len() != 0 || stderr.Len() == 0 {
				t.Errorf("出力先が不正です: 標準出力 = %q、標準エラー = %q", stdout.String(), stderr.String())
			}
		})
	}
}

func TestOutputFailureIsReported(t *testing.T) {
	var stderr bytes.Buffer
	if code := cli.Run([]string{"--version"}, failingWriter{}, &stderr); code != 2 {
		t.Errorf("終了コード = %d、期待値 = 2", code)
	}
	if stderr.Len() == 0 {
		t.Error("出力失敗の説明がありません。")
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("検証用の書き込み失敗")
}

func setupScan(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	path := filepath.Join(base, "日本語 CLI")
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "tool.cmd"), []byte("@echo 実行禁止"), 0o600); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{"USERPROFILE": base, "XDG_CONFIG_HOME": filepath.Join(base, "設定"), "SCOOP": filepath.Join(base, "user"), "SCOOP_GLOBAL": filepath.Join(base, "global"), "PATH": path, "PATHEXT": ".CMD;.EXE"} {
		t.Setenv(key, value)
	}
	return base
}

func decodeReport(t *testing.T, data []byte) scan.Report {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	var r scan.Report
	if err := decoder.Decode(&r); err != nil {
		t.Fatalf("JSONを読めません: %v", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		t.Fatal("標準出力がJSONオブジェクト1件ではありません。")
	}
	if r.SchemaVersion != 1 || r.Tool != "envfacts" || r.InvocationStatus != "not_tested" {
		t.Fatal("記録形式が不正です。")
	}
	return r
}

func TestScanJSONAndExitCodes(t *testing.T) {
	setupScan(t)
	for _, tc := range []struct {
		name, status string
		code         int
	}{{"tool", "complete", 0}, {"missing", "missing", 1}} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := cli.Run([]string{"scan", "--command", tc.name, "--json"}, &stdout, &stderr); code != tc.code {
				t.Fatalf("終了コード = %d", code)
			}
			r := decodeReport(t, stdout.Bytes())
			if r.Status != tc.status || len(r.Commands) != 1 || stderr.Len() != 0 {
				t.Fatal("調査状態または出力先が不正です。")
			}
		})
	}
	var stderr bytes.Buffer
	if code := cli.Run([]string{"scan", "--command", "tool", "--json"}, failingWriter{}, &stderr); code != 2 || stderr.Len() == 0 {
		t.Fatal("JSON出力の失敗を検出していません。")
	}
}

func TestInvalidArgumentsStillProduceJSON(t *testing.T) {
	for _, args := range [][]string{
		{"scan", "--command", "--json"}, {"scan", "--output", "--json"}, {"scan", "--command", "../tool", "--json"},
		{"scan", "--output", "one", "--output", "two", "--json"}, {"scan", "--unknown", "fixture-secret-do-not-report", "--json"},
		{"diff", "--json"}, {"--version", "--json"}, {"scan", "extra", "--json"},
	} {
		var stdout, stderr bytes.Buffer
		if code := cli.Run(args, &stdout, &stderr); code != 64 {
			t.Fatalf("終了コード = %d", code)
		}
		if r := decodeReport(t, stdout.Bytes()); r.Status != "invalid_arguments" || len(r.Coverage.ScoopRoots) != 0 {
			t.Fatal("不正な引数で調査を始めています。")
		}
		if stderr.Len() == 0 || strings.Contains(stdout.String()+stderr.String(), "fixture-secret") {
			t.Fatal("診断または値の保護が不正です。")
		}
	}
}

func TestSaveReportIsExclusiveAndPreservesKnownFacts(t *testing.T) {
	base := setupScan(t)
	output := filepath.Join(base, "記録 空白.json")
	args := []string{"scan", "--command", "tool", "--json", "--output", output}
	var stdout, stderr bytes.Buffer
	if code := cli.Run(args, &stdout, &stderr); code != 0 {
		t.Fatalf("保存の終了コード = %d", code)
	}
	saved, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	decodeReport(t, saved)
	if !bytes.Equal(saved, stdout.Bytes()) {
		t.Fatal("保存した記録とJSON表示が異なります。")
	}
	stdout.Reset()
	stderr.Reset()
	if code := cli.Run(args, &stdout, &stderr); code != 2 {
		t.Fatalf("上書き拒否の終了コード = %d", code)
	}
	r := decodeReport(t, stdout.Bytes())
	if r.Status != "incomplete" || len(r.Commands) != 1 || r.Commands[0].Status != "resolved" || stderr.Len() == 0 {
		t.Fatal("保存失敗で判明した情報を失っています。")
	}
	after, _ := os.ReadFile(output)
	if !bytes.Equal(saved, after) {
		t.Fatal("既存ファイルを変更しています。")
	}
	stdout.Reset()
	stderr.Reset()
	if code := cli.Run([]string{"scan", "--command", "missing", "--output", output, "--json"}, &stdout, &stderr); code != 2 {
		t.Fatal("不足より保存失敗を優先していません。")
	}
	stdout.Reset()
	stderr.Reset()
	if code := cli.Run([]string{"scan", "--output", filepath.Join(base, "未作成", "out.json"), "--json"}, &stdout, &stderr); code != 2 {
		t.Fatal("保存先の不存在を成功としています。")
	}
	if _, err := os.Stat(filepath.Join(base, "未作成")); !os.IsNotExist(err) {
		t.Fatal("保存先ディレクトリを作成しています。")
	}
}

func TestHumanOutputAndNoImplicitFile(t *testing.T) {
	base := setupScan(t)
	before, err := os.ReadDir(base)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{"scan", "--command", "tool"}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "版不明") || stderr.Len() != 0 {
		t.Fatal("通常表示が不正です。")
	}
	after, _ := os.ReadDir(base)
	if len(before) != len(after) {
		t.Fatal("保存指定なしでファイルを作成しています。")
	}
}

func TestSaveRejectsWindowsDevicesAndStreams(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windowsの予約名を検証します。")
	}
	base := setupScan(t)
	for _, name := range []string{"NUL", "CON", "record.json:stream"} {
		var stdout, stderr bytes.Buffer
		if code := cli.Run([]string{"scan", "--command", "tool", "--json", "--output", filepath.Join(base, name)}, &stdout, &stderr); code != 2 {
			t.Fatal("通常の記録ファイルでない保存先を受け入れています。")
		}
	}
}
