package diff_test

import (
	"slices"
	"testing"

	"github.com/shouhon/envfacts/internal/scan"
)

func pathOnlyFixture(t *testing.T, full scan.Report) scan.Report {
	t.Helper()
	r := clone(t, full)
	r.SchemaVersion, r.Coverage.ScanMode = 2, "path_only"
	r.Coverage.CommandSelection = "explicit"
	r.Coverage.ScoopRoots, r.Packages = []scan.Root{}, []scan.Package{}
	r.Coverage.RequestedCommands = []string{"alpha", "beta"}
	r.Coverage.Excluded = append(r.Coverage.Excluded, "Scoopの導入台帳・導入元・導入版", "候補の実体・shimの参照先")
	for i := range r.Commands {
		c := &r.Commands[i]
		c.Version, c.VersionSource, c.ActualPath = nil, nil, nil
		c.VersionReason, c.PackageIDs = "導入台帳は未調査です。", []string{}
		for j := range c.Candidates {
			candidate := &c.Candidates[j]
			candidate.ActualPath, candidate.PackageIDs, candidate.Resolution = nil, []string{}, "not_checked"
		}
	}
	return r
}

func TestPathOnlyComparesOnlySelectedFacts(t *testing.T) {
	a := pathOnlyFixture(t, fixture(t))
	for _, tc := range []struct {
		name   string
		code   int
		mutate func(*scan.Report)
	}{
		{"日時と配列順", 0, func(r *scan.Report) {
			r.CapturedAt = "2025-01-01T00:00:00Z"
			slices.Reverse(r.Commands)
			for i := range r.Commands {
				slices.Reverse(r.Commands[i].Candidates)
			}
		}},
		{"PATH優先順位", 1, func(r *scan.Report) {
			r.Coverage.SearchPaths[0].Priority, r.Coverage.SearchPaths[1].Priority = 2, 1
			for i := range r.Commands {
				c := &r.Commands[i]
				slices.Reverse(c.Candidates)
				for j := range c.Candidates {
					c.Candidates[j].Priority, c.Candidates[j].PathPriority = j+1, j+1
				}
				c.PreferredPath = &c.Candidates[0].Path
			}
		}},
		{"候補の不足", 1, func(r *scan.Report) {
			c := &r.Commands[0]
			c.Status, c.Candidates, c.PreferredPath = "not_found", []scan.Candidate{}, nil
			r.Status = "missing"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := clone(t, a)
			tc.mutate(&b)
			r := compare(t, a, b)
			if r.ExitCode() != tc.code || len(r.Issues) != 0 || r.Coverage.Common.PackageInventoryComparable || !r.Coverage.Common.CommandInventoryComparable || !r.Coverage.Common.ResolutionComparable {
				t.Fatalf("限定した比較の結果が不正です: %+v", r)
			}
			if tc.code == 1 && len(r.Changes) == 0 {
				t.Fatal("PATH上の変更を失っています。")
			}
			for _, c := range r.Changes {
				if c.Entity == "package" || c.Field == "actual_path" || c.Field == "version" || c.Field == "package_ids" || c.Field == "resolution" {
					t.Fatal("未調査の事実を比較しています。")
				}
			}
		})
	}
}

func TestPathOnlyAndFullNeverInventPackageRemovals(t *testing.T) {
	full := fixture(t)
	for _, changed := range []bool{false, true} {
		limited := pathOnlyFixture(t, full)
		if changed {
			c := &limited.Commands[0]
			c.Candidates = c.Candidates[1:]
			c.Candidates[0].Priority = 1
			c.PreferredPath = &c.Candidates[0].Path
		}
		for _, reverse := range []bool{false, true} {
			a, b := full, limited
			if reverse {
				a, b = b, a
			}
			r := compare(t, a, b)
			if r.ExitCode() != 2 || !issue(r, "coverage_mismatch") || r.Coverage.Common.PackageInventoryComparable || issue(r, "unknown_target") || issue(r, "unknown_version") {
				t.Fatalf("全体調査との共通範囲が不正です: %+v", r)
			}
			if changed != (len(r.Changes) > 0) {
				t.Fatal("範囲違いで既知の候補の変更を失うか、架空の変更を作っています。")
			}
			for _, c := range r.Changes {
				if c.Entity == "package" || c.Field == "package_ids" || c.Field == "actual_path" || c.Field == "version" || c.Field == "resolution" {
					t.Fatal("調査していない導入情報や実体を変更扱いにしています。")
				}
			}
		}
	}
}

func TestPathOnlyRejectsInconsistentRecords(t *testing.T) {
	full := fixture(t)
	for _, tc := range []struct {
		name   string
		mutate func(*scan.Report)
	}{
		{"範囲なし", func(r *scan.Report) { r.Coverage.ScanMode = "" }},
		{"未知の範囲", func(r *scan.Report) { r.Coverage.ScanMode = "other" }},
		{"旧形式の限定記録", func(r *scan.Report) { r.SchemaVersion = 1 }},
		{"導入先あり", func(r *scan.Report) { r.Coverage.ScoopRoots = full.Coverage.ScoopRoots }},
		{"パッケージあり", func(r *scan.Report) { r.Packages = full.Packages }},
		{"Scoopの自動選択", func(r *scan.Report) { r.Coverage.CommandSelection = "scoop"; r.Coverage.RequestedCommands = []string{} }},
		{"版あり", func(r *scan.Report) {
			r.Commands[0].Version = ptr("1")
			r.Commands[0].VersionSource = full.Commands[0].VersionSource
			r.Commands[0].VersionReason = ""
		}},
		{"実体あり", func(r *scan.Report) { r.Commands[0].ActualPath = full.Commands[0].ActualPath }},
		{"候補の実体あり", func(r *scan.Report) { r.Commands[0].Candidates[0] = full.Commands[0].Candidates[0] }},
		{"候補を解決済み", func(r *scan.Report) { r.Commands[0].Candidates[0].Resolution = "direct" }},
		{"候補未確認なのに完了", func(r *scan.Report) { r.Commands[0].Candidates[0].Resolution = "unknown" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := pathOnlyFixture(t, full)
			tc.mutate(&r)
			result := compare(t, r, r)
			if result.ExitCode() != 2 || !issue(result, "invalid_record") {
				t.Fatalf("不整合な限定記録を受理しています: %+v", result)
			}
		})
	}
}

func TestPathOnlyKeepsUnavailableCandidateIncomplete(t *testing.T) {
	r := pathOnlyFixture(t, fixture(t))
	r.Status = "incomplete"
	r.Commands[0].Candidates[0].Resolution = "unknown"
	r.Issues = append(r.Issues, scan.Issue{Code: "unavailable_command_target", Message: "候補の存在を確認できません。", Source: r.Commands[0].Candidates[0].Path})
	result := compare(t, r, r)
	if result.ExitCode() != 2 || !issue(result, "incomplete_record") || issue(result, "invalid_record") || len(result.Changes) != 0 {
		t.Fatal("限定した範囲内の候補確認失敗を受理して保持していません。")
	}
}
