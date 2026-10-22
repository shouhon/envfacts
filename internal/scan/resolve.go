package scan

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type searchDirectory struct {
	coverage SearchPath
	files    map[string]string
}

func (s *scanner) resolve(names []string) {
	extensions := []string{".ps1"}
	for _, ext := range strings.Split(os.Getenv("PATHEXT"), ";") {
		ext = strings.ToLower(strings.TrimSpace(ext))
		if ext == "" || slices.Contains(extensions, ext) {
			continue
		}
		if !strings.HasPrefix(ext, ".") || !ValidCommandName("x"+ext) || strings.Contains(ext[1:], ".") {
			s.report.AddIssue("invalid_pathext", "PATHEXTに未対応の拡張子指定があります。", "PATHEXT")
			continue
		}
		extensions = append(extensions, ext)
	}
	s.report.Coverage.Extensions = extensions
	var directories []searchDirectory
	seen := map[string]bool{}
	for i, path := range strings.Split(os.Getenv("PATH"), ";") {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if path == "~" || strings.HasPrefix(path, `~\`) {
			home, err := os.UserHomeDir()
			if err != nil {
				s.report.AddIssue("invalid_search_path", "PATHの利用者フォルダー指定を解決できませんでした。", "PATH")
				continue
			}
			path = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(path, "~"), `\`))
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			s.report.AddIssue("invalid_search_path", "PATHの探索先を絶対パスにできませんでした。", "PATH")
			continue
		}
		key := strings.ToLower(absolute)
		if seen[key] {
			continue
		}
		seen[key] = true
		d := searchDirectory{coverage: SearchPath{Path: absolute, Priority: i + 1, Status: "complete"}, files: map[string]string{}}
		entries, err := s.readDir(absolute)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			d.coverage.Status = "absent"
		case err != nil:
			d.coverage.Status = "incomplete"
			s.report.AddIssue("unreadable_search_path", "PATHの探索先を読み取れませんでした。", absolute)
		default:
			for _, entry := range entries {
				if !entry.IsDir() {
					d.files[strings.ToLower(entry.Name())] = entry.Name()
				}
			}
		}
		s.report.Coverage.SearchPaths = append(s.report.Coverage.SearchPaths, d.coverage)
		directories = append(directories, d)
	}
	for _, name := range names {
		c := Command{Name: name, Status: "not_found", Candidates: []Candidate{}, PackageIDs: []string{}, VersionReason: "PATHから実行ファイルを確認できませんでした。", InvocationStatus: "not_tested"}
		commandName := name
		ext := strings.ToLower(filepath.Ext(name))
		fileNames := []string{}
		if slices.Contains(extensions, ext) {
			fileNames = append(fileNames, name)
			commandName = strings.TrimSuffix(name, ext)
		} else {
			for _, extension := range extensions {
				fileNames = append(fileNames, name+extension)
			}
		}
		for _, b := range s.bins {
			if b.name == commandName {
				c.PackageIDs = append(c.PackageIDs, b.packageID)
			}
		}
		c.PackageIDs = normalizedNames(c.PackageIDs)
		for _, directory := range directories {
			for ei, fileName := range fileNames {
				actualName, ok := directory.files[fileName]
				if !ok {
					continue
				}
				path := filepath.Join(directory.coverage.Path, actualName)
				candidate := Candidate{Path: path, Priority: len(c.Candidates) + 1, PathPriority: directory.coverage.Priority, ExtensionPriority: ei + 1, Resolution: "unknown", PackageIDs: []string{}}
				if len(fileNames) == 1 {
					candidate.ExtensionPriority = slices.Index(extensions, ext) + 1
				}
				s.resolveCandidate(&candidate, commandName)
				c.Candidates = append(c.Candidates, candidate)
			}
		}
		if len(c.Candidates) > 0 {
			c.Status = "resolved"
			first := c.Candidates[0]
			c.PreferredPath, c.ActualPath = &first.Path, first.ActualPath
			c.VersionReason = "優先する候補の導入版を示すローカル情報がありません。"
			if len(first.PackageIDs) == 1 {
				for _, p := range s.report.Packages {
					if p.ID == first.PackageIDs[0] {
						c.Version, c.VersionSource, c.VersionReason = p.Version, p.VersionSource, p.VersionReason
					}
				}
			}
		} else if s.report.Coverage.CommandSelection == "explicit" && s.report.Status != "incomplete" {
			s.report.Status = "missing"
		}
		s.report.Commands = append(s.report.Commands, c)
	}
}

func (s *scanner) resolveCandidate(c *Candidate, name string) {
	path := c.Path
	if _, err := physicalPath(path); err != nil {
		s.report.AddIssue("unavailable_command_target", "候補の実行ファイルを確認できませんでした。", path)
		return
	}
	isShim := false
	for _, root := range s.report.Coverage.ScoopRoots {
		if samePath(filepath.Dir(path), filepath.Join(root.Path, "shims")) {
			isShim = true
			break
		}
	}
	if isShim {
		if !strings.EqualFold(filepath.Ext(path), ".exe") {
			s.report.AddIssue("unsupported_shim", "このScoop shimの形式には対応していないため、参照先と導入版は未確認です。", path)
			return
		}
		source := strings.TrimSuffix(path, filepath.Ext(path)) + ".shim"
		data, err := s.readFile(source)
		if err != nil {
			s.report.AddIssue("unreadable_shim", "Scoop shimの参照情報を読み取れませんでした。", source)
			return
		}
		target, err := shimTarget(data)
		if err != nil {
			s.report.AddIssue("unsupported_shim", "Scoop shimの参照情報が未対応の形式です。", source)
			return
		}
		path = target
	}
	actual, err := physicalPath(path)
	if err != nil {
		s.report.AddIssue("unavailable_command_target", "候補の実体を確認できませんでした。起動確認は行っていません。", c.Path)
		return
	}
	c.ActualPath, c.Resolution = &actual, "direct"
	if isShim {
		c.Resolution = "scoop_shim"
	}
	for _, b := range s.bins {
		if b.name != name {
			continue
		}
		if target, err := physicalPath(b.target); err == nil && samePath(target, actual) {
			c.PackageIDs = append(c.PackageIDs, b.packageID)
		}
	}
	c.PackageIDs = normalizedNames(c.PackageIDs)
}

func shimTarget(data []byte) (string, error) {
	text := strings.TrimPrefix(string(data), "\ufeff")
	target := ""
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "args = ") {
			continue
		}
		if target != "" || !strings.HasPrefix(line, `path = "`) || !strings.HasSuffix(line, `"`) {
			return "", fs.ErrInvalid
		}
		target = strings.TrimSuffix(strings.TrimPrefix(line, `path = "`), `"`)
		if !filepath.IsAbs(target) || strings.ContainsAny(target, "\"\x00\r\n") {
			return "", fs.ErrInvalid
		}
	}
	if target == "" {
		return "", fs.ErrInvalid
	}
	return target, nil
}
