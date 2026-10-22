package cli_test

import (
	"bytes"
	"errors"
	"testing"

	"envfacts/internal/cli"
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
		{name: "未実装の調査", args: []string{"scan", "--json"}},
		{name: "未実装の比較", args: []string{"diff"}},
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
