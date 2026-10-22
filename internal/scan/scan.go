package scan

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type scanner struct {
	report   Report
	readFile func(string) ([]byte, error)
	readDir  func(string) ([]os.DirEntry, error)
	bins     []installedBin
}

type installedBin struct {
	name, target, packageID string
}

// Scan はローカル情報を読むだけで、対象コマンドや管理ツールを実行しない。
func Scan(names []string) Report {
	s := scanner{report: NewReport("scan"), readFile: os.ReadFile, readDir: os.ReadDir}
	return s.scan(names)
}

func (s *scanner) scan(names []string) Report {
	s.report.Coverage.CommandSelection = "scoop"
	if len(names) > 0 {
		s.report.Coverage.CommandSelection = "explicit"
		s.report.Coverage.RequestedCommands = normalizedNames(names)
	}
	s.loadRoots()
	for i := range s.report.Coverage.ScoopRoots {
		s.loadPackages(&s.report.Coverage.ScoopRoots[i])
	}
	if len(names) == 0 {
		for _, p := range s.report.Packages {
			names = append(names, p.Commands...)
		}
	}
	s.resolve(normalizedNames(names))
	slices.SortFunc(s.report.Packages, func(a, b Package) int { return strings.Compare(a.ID, b.ID) })
	return s.report
}

func normalizedNames(names []string) []string {
	result := make([]string, 0, len(names))
	for _, name := range names {
		result = append(result, strings.ToLower(name))
	}
	slices.Sort(result)
	return slices.Compact(result)
}

func samePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

func physicalPath(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fs.ErrInvalid
	}
	return resolvedPath(path)
}

func resolvedPath(path string) (string, error) {
	return resolveLinks(filepath.Clean(path), 0)
}

func resolveLinks(path string, links int) (string, error) {
	if links > 255 {
		return "", fs.ErrInvalid
	}
	parent := filepath.Dir(path)
	if parent == path {
		return filepath.EvalSymlinks(path)
	}
	actualParent, err := resolveLinks(parent, links)
	if err != nil {
		return "", err
	}
	path = filepath.Join(actualParent, filepath.Base(path))
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	// GoではWindowsのjunctionがModeIrregularになるため、Readlinkで明示的に確認する。
	if info.Mode()&(os.ModeSymlink|os.ModeIrregular) == 0 {
		return path, nil
	}
	target, err := os.Readlink(path)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(actualParent, target)
	}
	return resolveLinks(filepath.Clean(target), links+1)
}
