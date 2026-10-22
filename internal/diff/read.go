package diff

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/shouhon/envfacts/internal/scan"
)

func readRecord(path string) (*scan.Report, string) {
	if !filepath.IsLocal(filepath.Base(path)) {
		return nil, "unreadable_record"
	}
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		return nil, "unreadable_record"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "unreadable_record"
	}
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	decoder := json.NewDecoder(bytes.NewReader(data))
	value, err := uniqueValue(decoder)
	if err != nil {
		return nil, "invalid_record"
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, "invalid_record"
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, "invalid_record"
	}
	if version, ok := object["schema_version"].(float64); ok && version != 1 {
		return nil, "unsupported_schema_version"
	}
	var r scan.Report
	if json.Unmarshal(data, &r) != nil || !validRecord(r, object) {
		return nil, "invalid_record"
	}
	return &r, ""
}

// uniqueValue は重複したキーを拒否する。未知の項目も解釈せず、出力には使わない。
func uniqueValue(d *json.Decoder) (any, error) {
	token, err := d.Token()
	if err != nil {
		return nil, err
	}
	switch token {
	case json.Delim('{'):
		object := map[string]any{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return nil, err
			}
			name, ok := key.(string)
			if !ok {
				return nil, errors.New("JSONのキーが不正または重複しています。")
			}
			// 構造体への復号も大文字小文字を区別しないため、同じ規則で重複を拒否する。
			for existing := range object {
				if strings.EqualFold(existing, name) {
					return nil, errors.New("JSONのキーが不正または重複しています。")
				}
			}
			value, err := uniqueValue(d)
			if err != nil {
				return nil, err
			}
			object[name] = value
		}
		_, err := d.Token()
		return object, err
	case json.Delim('['):
		array := []any{}
		for d.More() {
			value, err := uniqueValue(d)
			if err != nil {
				return nil, err
			}
			array = append(array, value)
		}
		_, err := d.Token()
		return array, err
	default:
		return token, nil
	}
}

func fields(value any, required string) bool {
	object, ok := value.(map[string]any)
	if !ok {
		return false
	}
	for _, key := range strings.Fields(required) {
		if _, exists := object[key]; !exists {
			return false
		}
		if object[key] == nil && !slices.Contains([]string{"version", "version_source", "preferred_path", "actual_path"}, key) {
			return false
		}
	}
	return true
}

func validRecord(r scan.Report, raw map[string]any) bool {
	if !fields(raw, "schema_version tool command status captured_at coverage packages commands invocation_status issues") ||
		r.SchemaVersion != 1 || r.Tool != "envfacts" || r.Command != "scan" || r.InvocationStatus != "not_tested" ||
		!slices.Contains([]string{"complete", "missing", "incomplete"}, r.Status) {
		return false
	}
	if _, err := time.Parse(time.RFC3339Nano, r.CapturedAt); err != nil {
		return false
	}
	coverage, ok := raw["coverage"].(map[string]any)
	if !ok || !fields(coverage, "scoop_roots command_selection requested_commands search_paths extensions excluded") {
		return false
	}
	c := r.Coverage
	if !slices.Contains([]string{"scoop", "explicit"}, c.CommandSelection) ||
		!uniqueStrings(c.RequestedCommands) || !uniqueStrings(c.Extensions) || !uniqueStrings(c.Excluded) ||
		c.CommandSelection == "scoop" && len(c.RequestedCommands) != 0 || c.CommandSelection == "explicit" && len(c.RequestedCommands) == 0 {
		return false
	}
	roots := map[string]scan.Root{}
	for i, root := range c.ScoopRoots {
		if !fields(coverage["scoop_roots"].([]any)[i], "scope path source status") ||
			!slices.Contains([]string{"user", "global"}, root.Scope) || !filepath.IsAbs(root.Path) || !validStatus(root.Status) {
			return false
		}
		if _, exists := roots[root.Scope]; exists {
			return false
		}
		roots[root.Scope] = root
	}
	if len(roots) != 2 {
		return false
	}
	paths, priorities := map[string]scan.SearchPath{}, map[int]bool{}
	for i, path := range c.SearchPaths {
		if !fields(coverage["search_paths"].([]any)[i], "path priority status") || !filepath.IsAbs(path.Path) || path.Priority < 1 || !validStatus(path.Status) || priorities[path.Priority] {
			return false
		}
		key := pathKey(path.Path)
		if _, exists := paths[key]; exists {
			return false
		}
		paths[key], priorities[path.Priority] = path, true
	}
	for _, ext := range c.Extensions {
		if !strings.HasPrefix(ext, ".") || !scan.ValidCommandName("x"+ext) || strings.ToLower(ext) != ext {
			return false
		}
	}
	packages := map[string]scan.Package{}
	for i, p := range r.Packages {
		root, exists := roots[p.Scope]
		if !fields(raw["packages"].([]any)[i], "id name manager scope install_path kind commands version version_source version_reason sources") ||
			!exists || p.Manager != "scoop" || !scan.ValidCommandName(p.Name) || !filepath.IsAbs(p.InstallPath) ||
			p.ID != "scoop:"+p.Scope+":"+pathKey(filepath.Join(root.Path, "apps", p.Name)) ||
			!slices.Contains([]string{"cli", "gui", "no_commands", "unknown"}, p.Kind) || !uniqueStrings(p.Commands) ||
			!uniqueStrings(p.Sources) || !validVersion(p.Version, p.VersionSource, p.VersionReason) {
			return false
		}
		if _, exists := packages[p.ID]; exists {
			return false
		}
		for _, name := range p.Commands {
			if !scan.ValidCommandName(name) || strings.ToLower(name) != name {
				return false
			}
		}
		packages[p.ID] = p
	}
	names := map[string]bool{}
	for i, command := range r.Commands {
		if !fields(raw["commands"].([]any)[i], "name status preferred_path actual_path candidates package_ids version version_source version_reason invocation_status") ||
			!scan.ValidCommandName(command.Name) || strings.ToLower(command.Name) != command.Name || names[command.Name] || command.InvocationStatus != "not_tested" ||
			!validVersion(command.Version, command.VersionSource, command.VersionReason) || !validReferences(command.PackageIDs, packages) {
			return false
		}
		names[command.Name] = true
		if command.Status == "not_found" {
			if len(command.Candidates) != 0 || command.PreferredPath != nil || command.ActualPath != nil || command.Version != nil {
				return false
			}
		} else if command.Status != "resolved" || len(command.Candidates) == 0 {
			return false
		}
		candidatePaths, candidatePriorities := map[string]bool{}, map[int]bool{}
		for j, candidate := range command.Candidates {
			path, exists := paths[pathKey(filepath.Dir(candidate.Path))]
			expectedName := command.Name
			if !slices.Contains(c.Extensions, strings.ToLower(filepath.Ext(command.Name))) {
				expectedName += filepath.Ext(candidate.Path)
			}
			if !fields(raw["commands"].([]any)[i].(map[string]any)["candidates"].([]any)[j], "path priority path_priority extension_priority actual_path resolution package_ids") ||
				!exists || !filepath.IsAbs(candidate.Path) || candidatePaths[pathKey(candidate.Path)] ||
				!strings.EqualFold(filepath.Base(candidate.Path), expectedName) ||
				candidate.Priority < 1 || candidate.Priority > len(command.Candidates) || candidatePriorities[candidate.Priority] || candidate.PathPriority != path.Priority ||
				candidate.ExtensionPriority < 1 || candidate.ExtensionPriority > len(c.Extensions) ||
				!strings.EqualFold(filepath.Ext(candidate.Path), c.Extensions[candidate.ExtensionPriority-1]) ||
				!validReferences(candidate.PackageIDs, packages) ||
				!slices.Contains([]string{"direct", "scoop_shim", "unknown"}, candidate.Resolution) ||
				(candidate.Resolution == "unknown") != (candidate.ActualPath == nil) || candidate.ActualPath != nil && !filepath.IsAbs(*candidate.ActualPath) {
				return false
			}
			candidatePaths[pathKey(candidate.Path)], candidatePriorities[candidate.Priority] = true, true
			if candidate.Priority == 1 && (!equalPath(command.PreferredPath, &candidate.Path) || !equalPath(command.ActualPath, candidate.ActualPath)) {
				return false
			}
		}
		ordered := slices.Clone(command.Candidates)
		slices.SortFunc(ordered, func(a, b scan.Candidate) int { return a.Priority - b.Priority })
		for j := 1; j < len(ordered); j++ {
			a, b := ordered[j-1], ordered[j]
			if a.PathPriority > b.PathPriority || a.PathPriority == b.PathPriority && a.ExtensionPriority >= b.ExtensionPriority {
				return false
			}
		}
	}
	expectedNames := slices.Clone(c.RequestedCommands)
	if c.CommandSelection == "scoop" {
		for _, p := range r.Packages {
			expectedNames = append(expectedNames, p.Commands...)
		}
	}
	if !sameSet(expectedNames, keys(names)) {
		return false
	}
	for i := range r.Issues {
		if !fields(raw["issues"].([]any)[i], "code message source") {
			return false
		}
	}
	return true
}

func validStatus(status string) bool {
	return slices.Contains([]string{"complete", "absent", "incomplete"}, status)
}

func uniqueStrings(values []string) bool {
	if values == nil {
		return false
	}
	seen := map[string]bool{}
	for _, value := range values {
		if value == "" || seen[value] {
			return false
		}
		seen[value] = true
	}
	return true
}

func validVersion(version, source *string, reason string) bool {
	if version == nil {
		return source == nil && reason != ""
	}
	return *version != "" && source != nil && filepath.IsAbs(*source) && reason == ""
}

func validReferences(ids []string, packages map[string]scan.Package) bool {
	if !uniqueStrings(ids) {
		return false
	}
	for _, id := range ids {
		if _, exists := packages[id]; !exists {
			return false
		}
	}
	return true
}
