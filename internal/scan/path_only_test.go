package scan

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPathOnlySkipsScoopAndPreservesCandidates(t *testing.T) {
	user, _, first := environment(t)
	install(t, user, "無関係", `{"version":`)
	write(t, filepath.Join(user, "config.json"), `{"root_path":`)
	second := filepath.Join(user, "shims")
	for _, path := range []string{filepath.Join(first, "tool.ps1"), filepath.Join(first, "tool.cmd"), filepath.Join(second, "tool.exe")} {
		write(t, path, "実行禁止")
	}
	write(t, filepath.Join(second, "tool.shim"), "解釈しない参照情報")
	t.Setenv("PATH", first+";"+second)
	before, envBefore := snapshot(t, filepath.Dir(user)), os.Environ()
	r := Scan([]string{"TOOL", "tool"}, true)
	if r.ExitCode() != 0 || r.SchemaVersion != 2 || !r.Coverage.PathOnly() || len(r.Packages) != 0 || len(r.Coverage.ScoopRoots) != 0 || len(r.Commands) != 1 {
		t.Fatalf("PATHの限定範囲が不正です: %+v", r)
	}
	c := command(t, r, "tool")
	if c.Status != "resolved" || len(c.Candidates) != 3 || c.Version != nil || c.VersionSource != nil || c.ActualPath != nil || len(c.PackageIDs) != 0 || !strings.Contains(c.VersionReason, "導入台帳") {
		t.Fatal("PATHでの発見と導入・実体の確認を混同しています。")
	}
	for i, candidate := range c.Candidates {
		if candidate.Priority != i+1 || candidate.Resolution != "not_checked" || candidate.ActualPath != nil || len(candidate.PackageIDs) != 0 {
			t.Fatal("候補の優先順位または実体の未確認が不正です。")
		}
	}
	if !strings.HasSuffix(c.Candidates[0].Path, "tool.ps1") || !strings.HasSuffix(c.Candidates[1].Path, "tool.cmd") || !samePath(*c.PreferredPath, c.Candidates[0].Path) {
		t.Fatal("PATHと拡張子の優先順位が不正です。")
	}
	if !reflect.DeepEqual(before, snapshot(t, filepath.Dir(user))) || !reflect.DeepEqual(envBefore, os.Environ()) {
		t.Fatal("限定調査がファイルまたは環境設定を変更しています。")
	}
}

func TestPathOnlyReadsOnlySearchDirectories(t *testing.T) {
	for _, unreadable := range []bool{false, true} {
		t.Run(map[bool]string{false: "名前不足", true: "読取不能と名前不足"}[unreadable], func(t *testing.T) {
			_, _, first := environment(t)
			second := filepath.Join(filepath.Dir(first), "後順位")
			write(t, filepath.Join(first, "tool.exe"), "実行禁止")
			write(t, filepath.Join(second, "tool.cmd"), "実行禁止")
			t.Setenv("PATH", first+";"+second)
			r := NewReport("scan")
			r.SchemaVersion, r.Coverage.ScanMode = 2, "path_only"
			s := scanner{
				report: r,
				readFile: func(string) ([]byte, error) {
					t.Fatal("PATH限定調査で設定・導入台帳・shim本文を読み取っています。")
					return nil, fs.ErrPermission
				},
				readDir: func(path string) ([]os.DirEntry, error) {
					if !samePath(path, first) && !samePath(path, second) {
						t.Fatal("PATH以外のディレクトリを列挙しています。")
					}
					if unreadable && samePath(path, second) {
						return nil, fs.ErrPermission
					}
					return os.ReadDir(path)
				},
			}
			r = s.scan([]string{"tool", "missing"})
			if command(t, r, "tool").Status != "resolved" || command(t, r, "missing").Status != "not_found" {
				t.Fatal("読めた候補または名前不足を失っています。")
			}
			if unreadable {
				if r.ExitCode() != 2 || !hasIssue(r, "unreadable_search_path") || len(command(t, r, "tool").Candidates) != 1 {
					t.Fatal("PATHの未読範囲を保持していません。")
				}
			} else if r.ExitCode() != 1 || len(command(t, r, "tool").Candidates) != 2 {
				t.Fatal("名前不足または全候補を保持していません。")
			}
		})
	}
}
