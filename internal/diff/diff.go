package diff

import (
	"cmp"
	"path/filepath"
	"slices"
	"strings"

	"github.com/shouhon/envfacts/internal/scan"
)

// Files は指定された2つの記録だけを読み、導入先やPATHの再調査を行わない。
func Files(beforePath, afterPath string) Report {
	r := NewReport()
	before, beforeCode := readRecord(beforePath)
	after, afterCode := readRecord(afterPath)
	for _, input := range []struct{ path, code string }{{beforePath, beforeCode}, {afterPath, afterCode}} {
		if input.code != "" {
			message := map[string]string{
				"unreadable_record":          "比較する記録を読み取れませんでした。",
				"invalid_record":             "記録のJSON、必須項目、または項目間の整合性が不正です。",
				"unsupported_schema_version": "この記録形式の版には対応していません。",
			}[input.code]
			r.AddIssue(input.code, message, input.path)
		}
	}
	if before != nil {
		coverage := sortedCoverage(before.Coverage)
		r.Coverage.Before = &coverage
	}
	if after != nil {
		coverage := sortedCoverage(after.Coverage)
		r.Coverage.After = &coverage
	}
	if before == nil || after == nil {
		return r
	}
	for _, input := range []struct {
		record *scan.Report
		path   string
	}{{before, beforePath}, {after, afterPath}} {
		if incomplete(*input.record) {
			r.AddIssue("incomplete_record", "元の調査に未確認の範囲があります。確認できた事実だけを比較します。", input.path)
		}
	}
	r.compareCoverage(before.Coverage, after.Coverage)
	r.comparePackages(before.Packages, after.Packages)
	r.compareCommands(before.Commands, after.Commands)
	if len(r.Changes) > 0 && r.Status != "incomplete" {
		r.Status = "changed"
	}
	slices.SortFunc(r.Changes, func(a, b Change) int {
		return cmp.Or(strings.Compare(a.Entity, b.Entity), strings.Compare(a.ID, b.ID), strings.Compare(a.Command, b.Command), strings.Compare(a.Field, b.Field), strings.Compare(a.Kind, b.Kind))
	})
	slices.SortFunc(r.Issues, func(a, b scan.Issue) int {
		return cmp.Or(strings.Compare(a.Code, b.Code), strings.Compare(a.Source, b.Source), strings.Compare(a.Message, b.Message))
	})
	r.Issues = slices.Compact(r.Issues)
	return r
}

func incomplete(r scan.Report) bool {
	return r.Status == "incomplete" || len(r.Issues) > 0 ||
		slices.ContainsFunc(r.Coverage.ScoopRoots, func(root scan.Root) bool { return root.Status == "incomplete" }) ||
		slices.ContainsFunc(r.Coverage.SearchPaths, func(path scan.SearchPath) bool { return path.Status == "incomplete" })
}

func (r *Report) compareCoverage(before, after scan.Coverage) {
	aRoots, bRoots := rootMap(before.ScoopRoots), rootMap(after.ScoopRoots)
	aPaths, bPaths := searchMap(before.SearchPaths), searchMap(after.SearchPaths)
	common := &r.Coverage.Common
	sameRoots, samePaths := sameSet(keys(aRoots), keys(bRoots)), sameSet(keys(aPaths), keys(bPaths))
	common.PackageInventoryComparable = sameRoots
	for _, key := range keys(aRoots) {
		a := aRoots[key]
		b, exists := bRoots[key]
		if !exists {
			continue
		}
		complete := a.Status != "incomplete" && b.Status != "incomplete"
		common.ScoopRoots = append(common.ScoopRoots, Root{Scope: a.Scope, Path: a.Path, Complete: complete})
		common.PackageInventoryComparable = common.PackageInventoryComparable && complete
	}
	common.ResolutionComparable = samePaths && sameSet(before.Extensions, after.Extensions)
	for _, key := range keys(aPaths) {
		a := aPaths[key]
		b, exists := bPaths[key]
		if !exists {
			continue
		}
		if a.Status == "incomplete" || b.Status == "incomplete" {
			common.ResolutionComparable = false
			continue
		}
		common.SearchPaths = append(common.SearchPaths, a.Path)
	}
	common.Extensions = intersection(before.Extensions, after.Extensions)
	sameSelection := before.CommandSelection == after.CommandSelection && sameSet(before.RequestedCommands, after.RequestedCommands)
	common.CommandInventoryComparable = sameSelection && (before.CommandSelection == "explicit" || common.PackageInventoryComparable)
	if !sameRoots || !samePaths || !sameSelection || !sameSet(before.Extensions, after.Extensions) || !sameSet(before.Excluded, after.Excluded) {
		r.AddIssue("coverage_mismatch", "調査範囲が異なります。coverage.commonに示す共通範囲だけを比較し、全体の一致は確認していません。", "coverage")
	}
	aOrder, bOrder := []string{}, []string{}
	for _, paths := range []struct {
		coverage scan.Coverage
		order    *[]string
	}{{before, &aOrder}, {after, &bOrder}} {
		for _, path := range sortedCoverage(paths.coverage).SearchPaths {
			if containsPath(common.SearchPaths, path.Path) {
				*paths.order = append(*paths.order, pathKey(path.Path))
			}
		}
	}
	if !slices.Equal(aOrder, bOrder) {
		r.change("priority_changed", "coverage", "search_paths", "order", aOrder, bOrder)
	}
	aExtensions, bExtensions := filterSet(before.Extensions, common.Extensions), filterSet(after.Extensions, common.Extensions)
	if !slices.Equal(aExtensions, bExtensions) {
		r.change("priority_changed", "coverage", "extensions", "order", aExtensions, bExtensions)
	}
}

func (r *Report) comparePackages(before, after []scan.Package) {
	a, b := map[string]scan.Package{}, map[string]scan.Package{}
	for _, side := range []struct {
		packages []scan.Package
		target   map[string]scan.Package
	}{{before, a}, {after, b}} {
		for _, p := range side.packages {
			if _, exists := r.commonRoot(p); exists {
				side.target[p.ID] = p
			}
		}
	}
	for _, id := range union(keys(a), keys(b)) {
		x, xOK := a[id]
		y, yOK := b[id]
		if !xOK || !yOK {
			p, kind := y, "added"
			if xOK {
				p, kind = x, "removed"
			}
			if root, _ := r.commonRoot(p); root.Complete {
				var before, after any
				if xOK {
					before = packageFact(x)
				} else {
					after = packageFact(y)
				}
				r.change(kind, "package", id, "", before, after)
			}
			continue
		}
		r.compareVersion("package", id, x.Version, y.Version)
		if len(x.Sources) > 0 && len(y.Sources) > 0 && !equalPath(&x.InstallPath, &y.InstallPath) {
			r.change("actual_path_changed", "package", id, "install_path", x.InstallPath, y.InstallPath)
		}
		if x.Kind != "unknown" && y.Kind != "unknown" && x.Kind != y.Kind {
			r.change("updated", "package", id, "kind", x.Kind, y.Kind)
		}
		if x.Kind != "unknown" && y.Kind != "unknown" && !sameSet(x.Commands, y.Commands) {
			r.change("updated", "package", id, "commands", sortedSet(x.Commands), sortedSet(y.Commands))
		}
	}
}

func (r *Report) compareCommands(before, after []scan.Command) {
	a, b := map[string]scan.Command{}, map[string]scan.Command{}
	for _, c := range before {
		a[c.Name] = c
	}
	for _, c := range after {
		b[c.Name] = c
	}
	r.Coverage.Common.Commands = intersection(keys(a), keys(b))
	for _, name := range union(keys(a), keys(b)) {
		x, xOK := a[name]
		y, yOK := b[name]
		if !xOK || !yOK {
			if r.Coverage.Common.CommandInventoryComparable {
				if xOK {
					r.change("removed", "command", name, "", commandFact(x), nil)
				} else {
					r.change("added", "command", name, "", nil, commandFact(y))
				}
			}
			continue
		}
		if r.Coverage.Common.ResolutionComparable {
			if x.Status != y.Status {
				r.change("updated", "command", name, "status", x.Status, y.Status)
			}
			if !equalPath(x.PreferredPath, y.PreferredPath) {
				r.change("priority_changed", "command", name, "preferred_path", x.PreferredPath, y.PreferredPath)
			}
			if (x.Status == "not_found" || x.ActualPath != nil) && (y.Status == "not_found" || y.ActualPath != nil) && !equalPath(x.ActualPath, y.ActualPath) {
				r.change("actual_path_changed", "command", name, "actual_path", x.ActualPath, y.ActualPath)
			}
			if x.Status == "resolved" && y.Status == "resolved" {
				r.compareVersion("command", name, x.Version, y.Version)
			}
		}
		if r.Coverage.Common.PackageInventoryComparable && !sameSet(x.PackageIDs, y.PackageIDs) {
			r.change("updated", "command", name, "package_ids", sortedSet(x.PackageIDs), sortedSet(y.PackageIDs))
		}
		r.compareCandidates(name, x.Candidates, y.Candidates)
	}
}

func (r *Report) compareCandidates(command string, before, after []scan.Candidate) {
	a, b := map[string]scan.Candidate{}, map[string]scan.Candidate{}
	for _, side := range []struct {
		candidates []scan.Candidate
		target     map[string]scan.Candidate
	}{{before, a}, {after, b}} {
		for _, c := range side.candidates {
			if containsPath(r.Coverage.Common.SearchPaths, filepath.Dir(c.Path)) && slices.Contains(r.Coverage.Common.Extensions, strings.ToLower(filepath.Ext(c.Path))) {
				side.target[pathKey(c.Path)] = c
			}
		}
	}
	for _, key := range union(keys(a), keys(b)) {
		x, xOK := a[key]
		y, yOK := b[key]
		start := len(r.Changes)
		switch {
		case !xOK:
			r.change("added", "candidate", key, "", nil, candidateFact(y))
		case !yOK:
			r.change("removed", "candidate", key, "", candidateFact(x), nil)
		default:
			if x.ActualPath == nil || y.ActualPath == nil {
				r.AddIssue("unknown_target", "候補の実体に未確認の値があるため、同じ実体か確認できません。", key)
			} else if !equalPath(x.ActualPath, y.ActualPath) {
				r.change("actual_path_changed", "candidate", key, "actual_path", x.ActualPath, y.ActualPath)
			}
			if x.Resolution != "unknown" && y.Resolution != "unknown" && x.Resolution != y.Resolution {
				r.change("updated", "candidate", key, "resolution", x.Resolution, y.Resolution)
			}
			if r.Coverage.Common.PackageInventoryComparable && !sameSet(x.PackageIDs, y.PackageIDs) {
				r.change("updated", "candidate", key, "package_ids", sortedSet(x.PackageIDs), sortedSet(y.PackageIDs))
			}
		}
		for i := start; i < len(r.Changes); i++ {
			r.Changes[i].Command = command
		}
	}
	shared := intersection(keys(a), keys(b))
	aOrder, bOrder := candidateOrder(before, shared), candidateOrder(after, shared)
	if !slices.Equal(aOrder, bOrder) {
		r.change("priority_changed", "command", command, "candidate_order", aOrder, bOrder)
	}
}

func (r *Report) compareVersion(entity, id string, before, after *string) {
	if before == nil || after == nil {
		r.AddIssue("unknown_version", "導入版に不明な値があるため、同じ版か確認できません。", entity+":"+id)
	} else if *before != *after {
		r.change("version_changed", entity, id, "version", *before, *after)
	}
}

func (r *Report) commonRoot(p scan.Package) (Root, bool) {
	for _, root := range r.Coverage.Common.ScoopRoots {
		if p.ID == "scoop:"+root.Scope+":"+pathKey(filepath.Join(root.Path, "apps", p.Name)) {
			return root, true
		}
	}
	return Root{}, false
}

func (r *Report) change(kind, entity, id, field string, before, after any) {
	r.Changes = append(r.Changes, Change{Kind: kind, Entity: entity, ID: id, Field: field, Before: before, After: after})
}

func packageFact(p scan.Package) any {
	return map[string]any{"name": p.Name, "manager": p.Manager, "scope": p.Scope, "install_path": p.InstallPath, "kind": p.Kind, "commands": sortedSet(p.Commands), "version": p.Version}
}

func commandFact(c scan.Command) any {
	return map[string]any{"name": c.Name, "status": c.Status, "preferred_path": c.PreferredPath, "actual_path": c.ActualPath, "version": c.Version}
}

func candidateFact(c scan.Candidate) any {
	return map[string]any{"path": c.Path, "actual_path": c.ActualPath, "resolution": c.Resolution, "package_ids": sortedSet(c.PackageIDs)}
}

func pathKey(path string) string { return strings.ToLower(filepath.Clean(path)) }

func equalPath(a, b *string) bool {
	return a == nil && b == nil || a != nil && b != nil && pathKey(*a) == pathKey(*b)
}

func containsPath(paths []string, path string) bool {
	return slices.ContainsFunc(paths, func(p string) bool { return pathKey(p) == pathKey(path) })
}

func keys[T any](values map[string]T) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	slices.Sort(result)
	return result
}

func sortedSet(values []string) []string {
	result := append([]string{}, values...)
	slices.Sort(result)
	return slices.Compact(result)
}

func sameSet(a, b []string) bool { return slices.Equal(sortedSet(a), sortedSet(b)) }

func union(a, b []string) []string { return sortedSet(append(slices.Clone(a), b...)) }

func intersection(a, b []string) []string { return sortedSet(filterSet(a, b)) }

func filterSet(a, b []string) []string {
	result := []string{}
	for _, value := range a {
		if slices.Contains(b, value) {
			result = append(result, value)
		}
	}
	return result
}

func rootMap(roots []scan.Root) map[string]scan.Root {
	result := map[string]scan.Root{}
	for _, root := range roots {
		result[root.Scope+":"+pathKey(root.Path)] = root
	}
	return result
}

func searchMap(paths []scan.SearchPath) map[string]scan.SearchPath {
	result := map[string]scan.SearchPath{}
	for _, path := range paths {
		result[pathKey(path.Path)] = path
	}
	return result
}

func sortedCoverage(c scan.Coverage) scan.Coverage {
	c.ScoopRoots = slices.Clone(c.ScoopRoots)
	slices.SortFunc(c.ScoopRoots, func(a, b scan.Root) int {
		return strings.Compare(a.Scope+":"+pathKey(a.Path), b.Scope+":"+pathKey(b.Path))
	})
	c.SearchPaths = slices.Clone(c.SearchPaths)
	slices.SortFunc(c.SearchPaths, func(a, b scan.SearchPath) int { return a.Priority - b.Priority })
	c.RequestedCommands, c.Excluded = sortedSet(c.RequestedCommands), sortedSet(c.Excluded)
	return c
}

func candidateOrder(candidates []scan.Candidate, shared []string) []string {
	ordered := slices.Clone(candidates)
	slices.SortFunc(ordered, func(a, b scan.Candidate) int { return a.Priority - b.Priority })
	result := []string{}
	for _, c := range ordered {
		if slices.Contains(shared, pathKey(c.Path)) {
			result = append(result, pathKey(c.Path))
		}
	}
	return result
}
