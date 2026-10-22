package scan

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func environment(t *testing.T) (string, string, string) {
	t.Helper()
	base := t.TempDir()
	user, global, path := filepath.Join(base, "利用者 Scoop"), filepath.Join(base, "共通 Scoop"), filepath.Join(base, "外部 CLI")
	for _, dir := range []string{filepath.Join(user, "apps"), filepath.Join(global, "apps"), path} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for key, value := range map[string]string{"USERPROFILE": base, "XDG_CONFIG_HOME": filepath.Join(base, "設定"), "SCOOP": user, "SCOOP_GLOBAL": global, "PATH": path, "PATHEXT": ".EXE;.CMD;.BAT"} {
		t.Setenv(key, value)
	}
	return user, global, path
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func install(t *testing.T, root, name, manifest string) string {
	t.Helper()
	current := filepath.Join(root, "apps", name, "current")
	write(t, filepath.Join(current, "scoop-manifest.json"), manifest)
	return current
}

func addShim(t *testing.T, root, name, target string) {
	t.Helper()
	write(t, filepath.Join(root, "shims", name+".exe"), "実行しない模擬のshim")
	write(t, filepath.Join(root, "shims", name+".shim"), "path = \""+target+"\"\r\nargs = --token fixture-secret-do-not-report\r\n")
}

func command(t *testing.T, r Report, name string) Command {
	t.Helper()
	for _, c := range r.Commands {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("コマンド %s の記録がありません。", name)
	return Command{}
}

func hasIssue(r Report, code string) bool {
	return slices.ContainsFunc(r.Issues, func(i Issue) bool { return i.Code == code })
}

func TestScoopPackagesAndShadowedCandidates(t *testing.T) {
	user, global, external := environment(t)
	manifest, err := os.ReadFile("testdata/multi-manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	u := install(t, user, "engine-package", string(manifest))
	g := install(t, global, "engine-package", `{"version":"2.0","bin":[["tool.exe","alpha"]]}`)
	for _, target := range []string{filepath.Join(u, "bin", "engine.exe"), filepath.Join(u, "helper.exe"), filepath.Join(g, "tool.exe")} {
		write(t, target, "実行禁止")
	}
	addShim(t, user, "alpha", filepath.Join(u, "bin", "engine.exe"))
	addShim(t, user, "beta", filepath.Join(u, "bin", "engine.exe"))
	addShim(t, user, "helper", filepath.Join(u, "helper.exe"))
	addShim(t, global, "alpha", filepath.Join(g, "tool.exe"))
	install(t, user, "画面アプリ", `{"version":"4","shortcuts":[["view.exe","表示"]]}`)
	install(t, user, "データ", `{"version":"5"}`)
	write(t, filepath.Join(external, "alpha.cmd"), "@echo 実行禁止")
	t.Setenv("PATH", strings.Join([]string{filepath.Join(user, "shims"), external, filepath.Join(global, "shims")}, ";"))
	before, envBefore := snapshot(t, filepath.Dir(user)), os.Environ()
	r := Scan(nil)
	if r.Status != "complete" || r.ExitCode() != 0 {
		t.Fatalf("調査状態 = %s、問題 = %v", r.Status, r.Issues)
	}
	if len(r.Packages) != 4 || len(r.Commands) != 3 {
		t.Fatalf("パッケージ数 = %d、コマンド数 = %d", len(r.Packages), len(r.Commands))
	}
	c := command(t, r, "alpha")
	if len(c.Candidates) != 3 || len(c.PackageIDs) != 2 || c.Version == nil || *c.Version != "1.2.3" {
		t.Fatal("同名の導入情報・候補・優先版が区別されていません。")
	}
	if c.Candidates[0].Resolution != "scoop_shim" || len(c.Candidates[0].PackageIDs) != 1 || len(c.Candidates[1].PackageIDs) != 0 {
		t.Fatal("候補とパッケージの関連付けが不正です。")
	}
	if c.ActualPath == nil || !samePath(*c.ActualPath, filepath.Join(u, "bin", "engine.exe")) {
		t.Fatal("shimの参照先が不正です。")
	}
	kinds := map[string]string{}
	for _, p := range r.Packages {
		kinds[p.Name] = p.Kind
	}
	if kinds["画面アプリ"] != "gui" || kinds["データ"] != "no_commands" {
		t.Fatal("公開コマンドのないパッケージの区別が不正です。")
	}
	data, _ := json.Marshal(r)
	if strings.Contains(string(data), "fixture-secret") || strings.Contains(string(data), "example.invalid") || strings.Contains(string(data), "実行禁止") {
		t.Fatal("不要なメタデータが出力されています。")
	}
	if !reflect.DeepEqual(before, snapshot(t, filepath.Dir(user))) || !reflect.DeepEqual(envBefore, os.Environ()) {
		t.Fatal("調査対象または環境変数を変更しています。")
	}
	second := Scan(nil)
	r.CapturedAt, second.CapturedAt = "", ""
	if !reflect.DeepEqual(r, second) {
		t.Fatal("同じ情報から作る記録の順序が安定していません。")
	}
}

func snapshot(t *testing.T, root string) map[string][32]byte {
	t.Helper()
	result := map[string][32]byte{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			result[path] = [32]byte{}
			return nil
		}
		data, err := os.ReadFile(path)
		if err == nil {
			result[path] = sha256.Sum256(data)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestExplicitSelectionDistinguishesMissingAndUnknown(t *testing.T) {
	user, _, path := environment(t)
	install(t, user, "導入済み", `{"version":"3","bin":[["tool.exe","installed"]]}`)
	write(t, filepath.Join(path, "outside.exe"), "実行禁止")
	r := Scan([]string{"OUTSIDE", "missing", "installed", "outside"})
	if r.Status != "missing" || r.ExitCode() != 1 || len(r.Commands) != 3 {
		t.Fatalf("調査状態 = %s", r.Status)
	}
	if c := command(t, r, "outside"); c.Status != "resolved" || c.Version != nil || c.VersionReason == "" || len(c.PackageIDs) != 0 {
		t.Fatal("外部コマンドの版を推測しています。")
	}
	if c := command(t, r, "installed"); c.Status != "not_found" || len(c.PackageIDs) != 1 {
		t.Fatal("導入情報とPATHの不存在を混同しています。")
	}
	if c := command(t, r, "missing"); c.Status != "not_found" || len(c.PackageIDs) != 0 {
		t.Fatal("未導入コマンドの記録が不正です。")
	}
	if len(Scan([]string{"outside"}).Commands) != 1 {
		t.Fatal("明示していないコマンドも探索しています。")
	}
}

func TestPowerShellPathAndExtensionOrder(t *testing.T) {
	_, _, first := environment(t)
	second := filepath.Join(filepath.Dir(first), "後順位")
	for _, path := range []string{filepath.Join(first, "tool.CMD"), filepath.Join(first, "tool.exe"), filepath.Join(first, "tool.ps1"), filepath.Join(second, "tool.exe")} {
		write(t, path, "実行禁止")
	}
	t.Setenv("PATH", first+";;"+second+";"+first)
	t.Setenv("PATHEXT", ".CMD;.EXE;.cmd")
	r := Scan([]string{"tool"})
	c := command(t, r, "tool")
	if r.ExitCode() != 0 || len(c.Candidates) != 4 {
		t.Fatal("探索候補が不正です。")
	}
	for i, suffix := range []string{"tool.ps1", "tool.CMD", "tool.exe", "tool.exe"} {
		if !strings.HasSuffix(c.Candidates[i].Path, suffix) || c.Candidates[i].Priority != i+1 {
			t.Fatal("PATHまたは拡張子の優先順が不正です。")
		}
	}
	if got := command(t, Scan([]string{"tool.exe"}), "tool.exe"); len(got.Candidates) != 2 || strings.HasSuffix(*got.PreferredPath, ".ps1") {
		t.Fatal("拡張子を指定した探索が不正です。")
	}
	t.Setenv("PATH", `~\外部 CLI`)
	if got := command(t, Scan([]string{"tool"}), "tool"); len(got.Candidates) != 3 {
		t.Fatal("PATHの利用者フォルダー指定を解決していません。")
	}
}

func TestPartialFailuresPreserveFacts(t *testing.T) {
	for _, failure := range []string{"manifest", "root", "path", "shim"} {
		t.Run(failure, func(t *testing.T) {
			user, global, path := environment(t)
			current := install(t, user, "good", `{"version":"1","bin":"tool.exe"}`)
			write(t, filepath.Join(current, "tool.exe"), "実行禁止")
			addShim(t, user, "tool", filepath.Join(current, "tool.exe"))
			t.Setenv("PATH", filepath.Join(user, "shims")+";"+path)
			s := scanner{report: NewReport("scan"), readFile: os.ReadFile, readDir: os.ReadDir}
			if failure == "manifest" || failure == "shim" {
				s.readFile = func(name string) ([]byte, error) {
					if failure == "manifest" && strings.HasSuffix(name, "scoop-manifest.json") || failure == "shim" && strings.HasSuffix(name, ".shim") {
						return nil, fs.ErrPermission
					}
					return os.ReadFile(name)
				}
			} else {
				s.readDir = func(name string) ([]os.DirEntry, error) {
					if failure == "root" && samePath(name, filepath.Join(global, "apps")) || failure == "path" && samePath(name, path) {
						return nil, fs.ErrPermission
					}
					return os.ReadDir(name)
				}
			}
			r := s.scan([]string{"tool", "missing"})
			if r.Status != "incomplete" || r.ExitCode() != 2 || command(t, r, "tool").Status != "resolved" || len(r.Issues) == 0 {
				t.Fatal("読取失敗を成功や不存在として扱っています。")
			}
		})
	}
}

func TestUnsupportedAndBrokenMetadata(t *testing.T) {
	for _, tc := range []struct{ name, content, code string }{
		{"破損", `{"version":`, "invalid_manifest"},
		{"null", `null`, "invalid_manifest"},
		{"版不明", `{"bin":"tool.exe"}`, "unknown_package_version"},
		{"未対応bin", `{"version":"1","bin":{"file":"tool.exe"}}`, "unsupported_bin"},
		{"導入時の構成不明", `{"version":"1","architecture":{"64bit":{"bin":"tool.exe"}}}`, "unknown_architecture"},
		{"追加PATHの列挙は未対応", `{"version":"1","env_add_path":"bin"}`, "unsupported_env_add_path"},
		{"nightlyの実体不明", `{"version":"nightly"}`, "unknown_package_version"},
		{"未対応の版", `{"version":"https://fixture:fixture-secret-do-not-report@example.invalid"}`, "unknown_package_version"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			user, _, _ := environment(t)
			install(t, user, "broken", tc.content)
			r := Scan([]string{"missing"})
			if r.ExitCode() != 2 || len(r.Packages) != 1 || !hasIssue(r, tc.code) {
				t.Fatalf("失敗の分類が不正です: %v", r.Issues)
			}
			data, _ := json.Marshal(r)
			if strings.Contains(string(data), "fixture-secret") {
				t.Fatal("不正な版の値を出力しています。")
			}
		})
	}
}

func TestLegacyAndArchitectureSpecificMetadata(t *testing.T) {
	user, _, _ := environment(t)
	current := filepath.Join(user, "apps", "legacy", "current")
	write(t, filepath.Join(current, "manifest.json"), "\ufeff"+`{"version":"7","bin":"other.exe","architecture":{"64bit":{"bin":[["tool.exe","alias"]]}}}`)
	write(t, filepath.Join(current, "install.json"), `{"architecture":"64bit","url":"fixture-secret-do-not-report"}`)
	write(t, filepath.Join(current, "tool.exe"), "実行禁止")
	t.Setenv("PATH", current)
	r := Scan(nil)
	if r.ExitCode() != 0 || len(r.Commands) != 1 || command(t, r, "alias").Status != "not_found" || r.Packages[0].Commands[0] != "alias" {
		t.Fatalf("導入時の構成が反映されていません: %v", r.Issues)
	}
	r = Scan([]string{"alias.exe"})
	if command(t, r, "alias.exe").Version != nil {
		t.Fatal("存在しない別名に版を推測しています。")
	}
}

func TestUnknownShimNeverUsesInstalledVersion(t *testing.T) {
	for _, tc := range []struct{ extension, metadata, code string }{
		{".ps1", "", "unsupported_shim"},
		{".exe", `path = "relative.exe"`, "unsupported_shim"},
		{".exe", "", "unsupported_shim"},
		{".exe", `path = "C:\missing\tool.exe"`, "unavailable_command_target"},
	} {
		t.Run(tc.code+tc.extension+tc.metadata, func(t *testing.T) {
			user, _, _ := environment(t)
			install(t, user, "pkg", `{"version":"8","bin":"tool.exe"}`)
			write(t, filepath.Join(user, "shims", "tool"+tc.extension), "throw '実行禁止'")
			write(t, filepath.Join(user, "shims", "tool.shim"), tc.metadata)
			t.Setenv("PATH", filepath.Join(user, "shims"))
			r := Scan(nil)
			c := command(t, r, "tool")
			if r.ExitCode() != 2 || !hasIssue(r, tc.code) || c.Status != "resolved" || c.Version != nil || c.ActualPath != nil {
				t.Fatal("未確認のshimを確認済みにしています。")
			}
		})
	}
}

func TestRootConfigurationAndMissingCurrent(t *testing.T) {
	user, global, _ := environment(t)
	t.Setenv("SCOOP", "")
	t.Setenv("SCOOP_GLOBAL", "")
	config := map[string]any{"root_path": user, "global_path": global, "irrelevant_secret": "fixture-secret-do-not-report"}
	data, _ := json.Marshal(config)
	write(t, filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "scoop", "config.json"), string(data))
	write(t, filepath.Join(user, "apps", "incomplete", "9", "manifest.json"), `{"version":"9"}`)
	r := Scan(nil)
	if !samePath(r.Coverage.ScoopRoots[0].Path, user) || !samePath(r.Coverage.ScoopRoots[1].Path, global) || !hasIssue(r, "unavailable_current") || r.Packages[0].Version != nil {
		t.Fatal("導入先またはcurrentがない場合の扱いが不正です。")
	}
	write(t, filepath.Join(user, "config.json"), `{"no_junction":true}`)
	if r = Scan(nil); !hasIssue(r, "unsupported_scoop_configuration") {
		t.Fatal("未対応の設定を成功としています。")
	}
}

func TestInvalidConfigurationDoesNotApplyPartialValues(t *testing.T) {
	_, _, _ = environment(t)
	write(t, filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "scoop", "config.json"), `{"root_path":"fixture-secret-do-not-report","no_junction":"invalid"}`)
	r := Scan(nil)
	if r.ExitCode() != 2 || !hasIssue(r, "invalid_scoop_config") {
		t.Fatal("不正な設定を成功としています。")
	}
	for _, root := range r.Coverage.ScoopRoots {
		if strings.Contains(root.Path, "fixture-secret") {
			t.Fatal("不正な設定の一部を採用しています。")
		}
	}
}

func TestAbsentRootsAndCommandValidation(t *testing.T) {
	user, global, _ := environment(t)
	t.Setenv("SCOOP", filepath.Join(user, "存在しない"))
	t.Setenv("SCOOP_GLOBAL", filepath.Join(global, "存在しない"))
	r := Scan(nil)
	if r.ExitCode() != 0 || r.Coverage.ScoopRoots[0].Status != "absent" {
		t.Fatal("存在しない導入先を区別していません。")
	}
	for _, name := range []string{"", "..", "../tool", `C:\tool`, "*", "--json", "tool\nvalue", " tool"} {
		if ValidCommandName(name) {
			t.Errorf("不正な名前を許可しています: %q", name)
		}
	}
	if !ValidCommandName("日本語-tool.exe") {
		t.Fatal("有効な名前を拒否しています。")
	}
	if _, err := shimTarget([]byte("unknown = hidden")); !errors.Is(err, fs.ErrInvalid) {
		t.Fatal("未対応のshimを許可しています。")
	}
}

func TestWindowsJunctionTargetsMatchInstalledVersion(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windowsのjunctionを使う検証です。")
	}
	pwsh, err := exec.LookPath("pwsh")
	if err != nil {
		t.Fatal("検証に必要なPowerShell 7が見つかりません。")
	}
	user, _, _ := environment(t)
	version := filepath.Join(user, "apps", "engine-package", "1.2.3")
	write(t, filepath.Join(version, "scoop-manifest.json"), `{"version":"1.2.3","bin":[["bin/engine.exe","alpha"]]}`)
	write(t, filepath.Join(version, "bin", "engine.exe"), "実行禁止")
	current := filepath.Join(user, "apps", "engine-package", "current")
	script := filepath.Join(filepath.Dir(user), "junction.ps1")
	write(t, script, "param([string]$Link, [string]$Target)\n$ErrorActionPreference = 'Stop'\nNew-Item -ItemType Junction -Path $Link -Target $Target | Out-Null\n")
	if output, err := exec.Command(pwsh, "-NoProfile", "-File", script, current, version).CombinedOutput(); err != nil {
		t.Fatalf("模擬のjunctionを作成できません: %v、%s", err, output)
	}
	addShim(t, user, "alpha", filepath.Join(current, "bin", "engine.exe"))
	t.Setenv("PATH", filepath.Join(user, "shims"))
	r := Scan(nil)
	c := command(t, r, "alpha")
	if r.ExitCode() != 0 || c.Version == nil || *c.Version != "1.2.3" || c.ActualPath == nil || !samePath(*c.ActualPath, filepath.Join(version, "bin", "engine.exe")) {
		t.Fatalf("junctionを通した導入版の照合に失敗しました: %v", r.Issues)
	}
	t.Setenv("PATH", filepath.Join(current, "bin"))
	r = Scan([]string{"engine"})
	if r.ExitCode() != 0 || command(t, r, "engine").ActualPath == nil {
		t.Fatal("PATHがjunctionの配下の場合に実体を確認できません。")
	}
}
