package diff_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/shouhon/envfacts/internal/diff"
	"github.com/shouhon/envfacts/internal/scan"
)

func ptr(s string) *string { return &s }

// 実体が存在しない記録を使い、比較時の再調査に結果が依存しないことも確認する。
func fixture(t *testing.T) scan.Report {
	t.Helper()
	r := scan.NewReport("scan")
	r.Coverage.CommandSelection = "scoop"
	base := t.TempDir()
	r.Coverage.Extensions = []string{".ps1", ".exe", ".cmd"}
	for i, scope := range []string{"user", "global"} {
		root := filepath.Join(base, scope+" 日本語 Scoop")
		r.Coverage.ScoopRoots = append(r.Coverage.ScoopRoots, scan.Root{Scope: scope, Path: root, Source: "default", Status: "complete"})
		r.Coverage.SearchPaths = append(r.Coverage.SearchPaths, scan.SearchPath{Path: filepath.Join(root, "shims"), Priority: i + 1, Status: "complete"})
		install := filepath.Join(root, "apps", "engine", "1.0")
		source := filepath.Join(root, "apps", "engine", "current", "scoop-manifest.json")
		p := scan.Package{ID: "scoop:" + scope + ":" + strings.ToLower(filepath.Join(root, "apps", "engine")), Name: "engine", Manager: "scoop", Scope: scope, InstallPath: install,
			Kind: "cli", Commands: []string{"alpha", "beta"}, Version: ptr("1.0"), VersionSource: &source, Sources: []string{source, filepath.Join(install, "scoop-install.json")}}
		r.Packages = append(r.Packages, p)
	}
	for _, name := range []string{"alpha", "beta"} {
		c := scan.Command{Name: name, Status: "resolved", Candidates: []scan.Candidate{}, PackageIDs: []string{}, Version: ptr("1.0"), VersionSource: r.Packages[0].VersionSource, InvocationStatus: "not_tested"}
		for i, p := range r.Packages {
			path := filepath.Join(r.Coverage.SearchPaths[i].Path, name+".exe")
			target := filepath.Join(p.InstallPath, name+".exe")
			c.Candidates = append(c.Candidates, scan.Candidate{Path: path, Priority: i + 1, PathPriority: i + 1, ExtensionPriority: 2, ActualPath: &target, Resolution: "scoop_shim", PackageIDs: []string{p.ID}})
			c.PackageIDs = append(c.PackageIDs, p.ID)
		}
		c.PreferredPath, c.ActualPath = &c.Candidates[0].Path, c.Candidates[0].ActualPath
		r.Commands = append(r.Commands, c)
	}
	return r
}

func clone(t *testing.T, r scan.Report) scan.Report {
	t.Helper()
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var result scan.Report
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func save(t *testing.T, directory, name string, r scan.Report) string {
	t.Helper()
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func compare(t *testing.T, before, after scan.Report) diff.Report {
	t.Helper()
	dir := t.TempDir()
	a, b := save(t, dir, "前の記録.json", before), save(t, dir, "後の 記録.json", after)
	return diff.Files(a, b)
}

func issue(r diff.Report, code string) bool {
	return slices.ContainsFunc(r.Issues, func(i scan.Issue) bool { return i.Code == code })
}

func change(r diff.Report, kind, entity, field string) bool {
	return slices.ContainsFunc(r.Changes, func(c diff.Change) bool { return c.Kind == kind && c.Entity == entity && c.Field == field })
}

func TestArrayOrderAndTimeAreNotChanges(t *testing.T) {
	a := fixture(t)
	b := clone(t, a)
	b.CapturedAt = "2025-01-01T00:00:00Z"
	slices.Reverse(b.Packages)
	slices.Reverse(b.Commands)
	slices.Reverse(b.Coverage.ScoopRoots)
	slices.Reverse(b.Coverage.SearchPaths)
	slices.Reverse(b.Coverage.Excluded)
	for i := range b.Packages {
		slices.Reverse(b.Packages[i].Commands)
		slices.Reverse(b.Packages[i].Sources)
	}
	for i := range b.Commands {
		slices.Reverse(b.Commands[i].Candidates)
		slices.Reverse(b.Commands[i].PackageIDs)
	}
	r := compare(t, a, b)
	if r.Status != "unchanged" || r.ExitCode() != 0 || len(r.Changes) != 0 || len(r.Issues) != 0 {
		t.Fatalf("順序だけの違いを変更としています: %+v", r)
	}
	if !r.Coverage.Common.PackageInventoryComparable || !r.Coverage.Common.ResolutionComparable || len(r.Coverage.Common.Commands) != 2 {
		t.Fatal("比較した範囲が不正です。")
	}
}

func TestMeaningfulChanges(t *testing.T) {
	for _, tc := range []struct {
		name, kind, entity, field string
		mutate                    func(*scan.Report)
	}{
		{"導入版", "version_changed", "package", "version", func(r *scan.Report) { r.Packages[0].Version = ptr("2.0") }},
		{"コマンドの版", "version_changed", "command", "version", func(r *scan.Report) { r.Commands[0].Version = ptr("2.0") }},
		{"導入先の実体", "actual_path_changed", "package", "install_path", func(r *scan.Report) { r.Packages[0].InstallPath += "-new" }},
		{"最優先の実体", "actual_path_changed", "command", "actual_path", func(r *scan.Report) {
			r.Commands[0].Candidates[0].ActualPath = ptr(*r.Commands[0].Candidates[0].ActualPath + "-new")
			r.Commands[0].ActualPath = r.Commands[0].Candidates[0].ActualPath
		}},
		{"隠れた実体", "actual_path_changed", "candidate", "actual_path", func(r *scan.Report) {
			r.Commands[0].Candidates[1].ActualPath = ptr(*r.Commands[0].Candidates[1].ActualPath + "-new")
		}},
		{"PATHの優先順", "priority_changed", "command", "candidate_order", func(r *scan.Report) {
			r.Coverage.SearchPaths[0].Priority, r.Coverage.SearchPaths[1].Priority = 2, 1
			for i := range r.Commands {
				c := &r.Commands[i]
				c.Candidates[0].Priority, c.Candidates[0].PathPriority = 2, 2
				c.Candidates[1].Priority, c.Candidates[1].PathPriority = 1, 1
				c.PreferredPath, c.ActualPath = &c.Candidates[1].Path, c.Candidates[1].ActualPath
				c.VersionSource = r.Packages[1].VersionSource
			}
		}},
		{"拡張子の優先順", "priority_changed", "coverage", "order", func(r *scan.Report) {
			r.Coverage.Extensions = []string{".exe", ".ps1", ".cmd"}
			for i := range r.Commands {
				for j := range r.Commands[i].Candidates {
					r.Commands[i].Candidates[j].ExtensionPriority = 1
				}
			}
		}},
		{"公開コマンドの削除", "removed", "command", "", func(r *scan.Report) {
			for i := range r.Packages {
				r.Packages[i].Commands = []string{"alpha"}
			}
			r.Commands = r.Commands[:1]
		}},
		{"候補の削除", "removed", "candidate", "", func(r *scan.Report) { r.Commands[0].Candidates = r.Commands[0].Candidates[:1] }},
		{"パッケージの追加", "added", "package", "", func(r *scan.Report) {
			root := r.Coverage.ScoopRoots[0]
			p := r.Packages[0]
			p.Name, p.ID = "viewer", "scoop:user:"+strings.ToLower(filepath.Join(root.Path, "apps", "viewer"))
			p.Kind, p.Commands = "gui", []string{}
			p.InstallPath = filepath.Join(root.Path, "apps", "viewer", "1.0")
			r.Packages = append(r.Packages, p)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := fixture(t)
			b := clone(t, a)
			tc.mutate(&b)
			r := compare(t, a, b)
			if r.Status != "changed" || r.ExitCode() != 1 || !change(r, tc.kind, tc.entity, tc.field) {
				t.Fatalf("変更を検出できません: %+v", r)
			}
			if tc.kind == "added" || tc.kind == "removed" {
				reverse := compare(t, b, a)
				opposite := map[string]string{"added": "removed", "removed": "added"}[tc.kind]
				if reverse.ExitCode() != 1 || !change(reverse, opposite, tc.entity, tc.field) {
					t.Fatalf("逆方向の変更を検出できません: %+v", reverse)
				}
			}
		})
	}
}

func TestUnknownVersionsAreNotConfirmedEqualOrChanged(t *testing.T) {
	a := fixture(t)
	a.Packages[0].Version, a.Packages[0].VersionSource, a.Packages[0].VersionReason = nil, nil, "未確認です。"
	a.Commands[0].Version, a.Commands[0].VersionSource, a.Commands[0].VersionReason = nil, nil, "未確認です。"
	for _, knownAfter := range []bool{false, true} {
		b := clone(t, a)
		if knownAfter {
			b.Packages[0].Version, b.Packages[0].VersionSource, b.Packages[0].VersionReason = ptr("2.0"), &b.Packages[0].Sources[0], ""
			b.Commands[0].Version, b.Commands[0].VersionSource, b.Commands[0].VersionReason = ptr("2.0"), b.Packages[0].VersionSource, ""
		}
		r := compare(t, a, b)
		if r.Status != "incomplete" || r.ExitCode() != 2 || !issue(r, "unknown_version") || change(r, "version_changed", "package", "version") || len(r.Changes) != 0 {
			t.Fatalf("版不明を一致または版変更として扱っています: %+v", r)
		}
	}
}

func TestDifferentSelectionsOnlyCompareSharedCommands(t *testing.T) {
	a := fixture(t)
	a.Coverage.CommandSelection, a.Coverage.RequestedCommands = "explicit", []string{"alpha", "beta"}
	b := clone(t, a)
	b.Coverage.RequestedCommands = []string{"beta", "missing"}
	b.Commands = []scan.Command{b.Commands[1], {Name: "missing", Status: "not_found", Candidates: []scan.Candidate{}, PackageIDs: []string{}, VersionReason: "PATHから未検出です。", InvocationStatus: "not_tested"}}
	b.Packages[0].Version = ptr("2.0")
	r := compare(t, a, b)
	if r.ExitCode() != 2 || !issue(r, "coverage_mismatch") || !slices.Equal(r.Coverage.Common.Commands, []string{"beta"}) || !change(r, "version_changed", "package", "version") {
		t.Fatalf("異なる範囲の共通部分が不正です: %+v", r)
	}
	if change(r, "added", "command", "") || change(r, "removed", "command", "") {
		t.Fatal("調査対象の違いをコマンドの追加・削除としています。")
	}
}

func TestNewSearchScopeDoesNotImplyChangedResolution(t *testing.T) {
	a := fixture(t)
	b := clone(t, a)
	newPath := filepath.Join(t.TempDir(), "追加PATH")
	for i := range b.Coverage.SearchPaths {
		b.Coverage.SearchPaths[i].Priority++
	}
	b.Coverage.SearchPaths = append(b.Coverage.SearchPaths, scan.SearchPath{Path: newPath, Priority: 1, Status: "complete"})
	for i := range b.Commands {
		c := &b.Commands[i]
		for j := range c.Candidates {
			c.Candidates[j].Priority++
			c.Candidates[j].PathPriority++
		}
		path := filepath.Join(newPath, c.Name+".exe")
		c.Candidates = append(c.Candidates, scan.Candidate{Path: path, Priority: 1, PathPriority: 1, ExtensionPriority: 2, ActualPath: &path, Resolution: "direct", PackageIDs: []string{}})
		c.PreferredPath, c.ActualPath = &path, &path
		c.Version, c.VersionSource, c.VersionReason = nil, nil, "導入元は未確認です。"
	}
	r := compare(t, a, b)
	if r.ExitCode() != 2 || !issue(r, "coverage_mismatch") || r.Coverage.Common.ResolutionComparable || len(r.Changes) != 0 {
		t.Fatalf("探索範囲の拡大を既知の変更としています: %+v", r)
	}
}

func TestIncompleteInventoryDoesNotInventRemovals(t *testing.T) {
	a := fixture(t)
	b := clone(t, a)
	b.Status, b.Coverage.ScoopRoots[1].Status = "incomplete", "incomplete"
	b.Packages = b.Packages[:1]
	for i := range b.Commands {
		b.Commands[i].PackageIDs = b.Commands[i].PackageIDs[:1]
		b.Commands[i].Candidates = b.Commands[i].Candidates[:1]
	}
	// 候補側にも読取不能を記録して、未確認の場所での削除を判定させない。
	b.Coverage.SearchPaths[1].Status = "incomplete"
	b.Packages[0].Version = ptr("2.0")
	r := compare(t, a, b)
	if r.ExitCode() != 2 || !issue(r, "incomplete_record") || !change(r, "version_changed", "package", "version") || change(r, "removed", "package", "") || change(r, "removed", "candidate", "") {
		t.Fatalf("不完全な範囲または既知の変更が不正です: %+v", r)
	}
}

func TestUnreadableManifestDoesNotImplyChangedDefinitions(t *testing.T) {
	a := fixture(t)
	b := clone(t, a)
	b.Status, b.Coverage.ScoopRoots[0].Status = "incomplete", "incomplete"
	p := &b.Packages[0]
	p.Kind, p.Commands, p.Sources = "unknown", []string{}, []string{}
	p.InstallPath = filepath.Join(b.Coverage.ScoopRoots[0].Path, "apps", "engine", "current")
	p.Version, p.VersionSource, p.VersionReason = nil, nil, "導入情報を読めませんでした。"
	for i := range b.Commands {
		c := &b.Commands[i]
		c.PackageIDs = c.PackageIDs[1:]
		c.Candidates[0].PackageIDs = []string{}
		c.Version, c.VersionSource, c.VersionReason = nil, nil, "導入版は未確認です。"
	}
	r := compare(t, a, b)
	if r.ExitCode() != 2 || !issue(r, "unknown_version") || len(r.Changes) != 0 {
		t.Fatalf("未確認の定義を変更としています: %+v", r)
	}
}

func TestCommonRootsPreserveKnownPackageChanges(t *testing.T) {
	a := fixture(t)
	b := clone(t, a)
	oldGlobal := b.Coverage.ScoopRoots[1].Path
	b.Coverage.ScoopRoots[1].Path = filepath.Join(t.TempDir(), "別の共通導入先")
	b.Coverage.ScoopRoots[1].Status = "absent"
	b.Packages = b.Packages[:1]
	b.Packages[0].Version = ptr("2.0")
	for i := range b.Commands {
		b.Commands[i].PackageIDs = b.Commands[i].PackageIDs[:1]
		b.Commands[i].Candidates[1].PackageIDs = []string{}
	}
	r := compare(t, a, b)
	if r.ExitCode() != 2 || !issue(r, "coverage_mismatch") || len(r.Coverage.Common.ScoopRoots) != 1 || r.Coverage.Common.PackageInventoryComparable || !change(r, "version_changed", "package", "version") || change(r, "removed", "package", "") {
		t.Fatalf("共通の導入先の比較が不正です: %+v", r)
	}
	for _, root := range r.Coverage.Common.ScoopRoots {
		if root.Path == oldGlobal {
			t.Fatal("片側だけの導入先を共通としています。")
		}
	}
}

func TestNotFoundAndUnknownTargets(t *testing.T) {
	a := fixture(t)
	for i := range a.Commands {
		c := &a.Commands[i]
		c.Status, c.Candidates, c.PreferredPath, c.ActualPath = "not_found", []scan.Candidate{}, nil, nil
		c.Version, c.VersionSource, c.VersionReason = nil, nil, "PATHから未検出です。"
	}
	if r := compare(t, a, a); r.ExitCode() != 0 || len(r.Issues) != 0 {
		t.Fatalf("不存在を版不明と取り違えています: %+v", r)
	}
	b := clone(t, a)
	b.Commands[0] = scan.Command{Name: "alpha", Status: "resolved", PreferredPath: ptr(filepath.Join(b.Coverage.SearchPaths[0].Path, "alpha.exe")),
		Candidates: []scan.Candidate{}, PackageIDs: []string{b.Packages[0].ID}, Version: b.Packages[0].Version, VersionSource: b.Packages[0].VersionSource, InvocationStatus: "not_tested"}
	b.Commands[0].Candidates = []scan.Candidate{{Path: *b.Commands[0].PreferredPath, Priority: 1, PathPriority: 1, ExtensionPriority: 2, Resolution: "unknown", PackageIDs: []string{}}}
	b.Status = "incomplete"
	r := compare(t, b, b)
	if r.ExitCode() != 2 || !issue(r, "unknown_target") || len(r.Changes) != 0 {
		t.Fatalf("実体の未確認を一致としています: %+v", r)
	}
	r = compare(t, a, b)
	if r.ExitCode() != 2 || !change(r, "updated", "command", "status") {
		t.Fatalf("既知の存在変更を失っています: %+v", r)
	}
}

func TestRecordValidationAndMetadataProtection(t *testing.T) {
	r := fixture(t)
	dir := t.TempDir()
	valid := save(t, dir, "valid.json", r)
	data, _ := os.ReadFile(valid)
	for _, tc := range []struct{ name, code, content string }{
		{"破損", "invalid_record", `{"fixture-secret":`},
		{"配列", "invalid_record", `[]`}, {"null", "invalid_record", `null`}, {"欠落", "invalid_record", `{}`},
		{"複数のJSON", "invalid_record", string(data) + `{}`},
		{"未対応版", "unsupported_schema_version", strings.Replace(string(data), `"schema_version":1`, `"schema_version":2`, 1)},
		{"重複キー", "invalid_record", strings.Replace(string(data), `"tool":"envfacts"`, `"tool":"fixture-secret","tool":"envfacts"`, 1)},
		{"大文字小文字だけ異なるキー", "invalid_record", strings.Replace(string(data), `"tool":"envfacts"`, `"tool":"envfacts","Commands":[{}]`, 1)},
		{"Unicodeで同一視されるキー", "invalid_record", strings.Replace(string(data), `"tool":"envfacts"`, `"tool":"envfacts","ſtatus":"complete"`, 1)},
		{"比較結果の再入力", "invalid_record", strings.Replace(string(data), `"command":"scan"`, `"command":"diff"`, 1)},
		{"配列のnull", "invalid_record", strings.Replace(string(data), `"issues":[]`, `"issues":null`, 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, "invalid.json")
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			result := diff.Files(valid, path)
			if result.ExitCode() != 2 || !issue(result, tc.code) || len(result.Changes) != 0 || result.Coverage.After != nil {
				t.Fatalf("不正な記録を受け入れています: %+v", result)
			}
			encoded, _ := json.Marshal(result)
			if strings.Contains(string(encoded), "fixture-secret") {
				t.Fatal("入力本文を出力しています。")
			}
		})
	}
	for _, mutate := range []func(*scan.Report){
		func(r *scan.Report) { r.Packages = append(r.Packages, r.Packages[0]) },
		func(r *scan.Report) { r.Commands = append(r.Commands, r.Commands[0]) },
		func(r *scan.Report) { r.Commands[0].Candidates[1].Priority = 1 },
		func(r *scan.Report) { r.Commands[0].PreferredPath = ptr(filepath.Join(dir, "wrong.exe")) },
		func(r *scan.Report) { r.Commands[0].Candidates[0].PathPriority = 9 },
		func(r *scan.Report) { r.Commands[0].PackageIDs = []string{"missing"} },
	} {
		bad := clone(t, r)
		mutate(&bad)
		if result := compare(t, r, bad); result.ExitCode() != 2 || !issue(result, "invalid_record") {
			t.Fatalf("項目間の不整合を受け入れています: %+v", result)
		}
	}
	if result := diff.Files(valid, filepath.Join(dir, "missing.json")); result.ExitCode() != 2 || !issue(result, "unreadable_record") {
		t.Fatal("読取不能を成功としています。")
	}
	// 未知の項目は無視し、元の調査の診断本文も再出力しない。
	withExtra := bytes.Replace(data, []byte(`"tool":"envfacts"`), []byte(`"tool":"envfacts","metadata":{"token":"fixture-secret"}`), 1)
	extraPath := filepath.Join(dir, "extra.json")
	if err := os.WriteFile(extraPath, withExtra, 0o600); err != nil {
		t.Fatal(err)
	}
	if result := diff.Files(valid, extraPath); result.ExitCode() != 0 {
		t.Fatalf("未知の項目を事実の変更としています: %+v", result)
	}
	r.Status = "incomplete"
	r.Issues = []scan.Issue{{Code: "fixture-secret", Message: "fixture-secret", Source: "fixture-secret"}}
	result := diff.Files(valid, save(t, dir, "issues.json", r))
	encoded, _ := json.Marshal(result)
	if result.ExitCode() != 2 || strings.Contains(string(encoded), "fixture-secret") {
		t.Fatal("入力の診断本文を再出力しています。")
	}
}

func TestComparisonDoesNotWriteFilesOrEnvironment(t *testing.T) {
	r := fixture(t)
	dir := t.TempDir()
	a, b := save(t, dir, "before.json", r), save(t, dir, "after.json", r)
	aBefore, _ := os.ReadFile(a)
	bBefore, _ := os.ReadFile(b)
	envBefore := os.Environ()
	result := diff.Files(a, b)
	aAfter, _ := os.ReadFile(a)
	bAfter, _ := os.ReadFile(b)
	entries, err := os.ReadDir(dir)
	if err != nil || result.ExitCode() != 0 || len(entries) != 2 || !bytes.Equal(aBefore, aAfter) || !bytes.Equal(bBefore, bAfter) || !reflect.DeepEqual(envBefore, os.Environ()) {
		t.Fatal("比較が入力ファイルまたは環境を変更しています。")
	}
}
