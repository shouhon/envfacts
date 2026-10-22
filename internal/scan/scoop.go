package scan

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
)

type scoopConfig struct {
	RootPath   string `json:"root_path"`
	GlobalPath string `json:"global_path"`
	NoJunction bool   `json:"no_junction"`
}

func (s *scanner) loadRoots() {
	home := os.Getenv("USERPROFILE")
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		configHome = filepath.Join(home, ".config")
	}
	configPath := filepath.Join(configHome, "scoop", "config.json")
	var config scoopConfig
	s.readConfig(configPath, &config)
	userPath, userSource := os.Getenv("SCOOP"), "SCOOP"
	if userPath == "" {
		userPath, userSource = config.RootPath, configPath
	}
	if userPath == "" {
		userPath, userSource = filepath.Join(home, "scoop"), "default"
	}
	// 導入先が分かる場合だけ、同梱された設定を優先する。
	portablePath := filepath.Join(userPath, "config.json")
	var portable scoopConfig
	if s.readConfig(portablePath, &portable) {
		config, configPath = portable, portablePath
		if os.Getenv("SCOOP") == "" && config.RootPath != "" {
			userPath, userSource = config.RootPath, configPath
		}
	}
	globalPath, globalSource := os.Getenv("SCOOP_GLOBAL"), "SCOOP_GLOBAL"
	if globalPath == "" {
		globalPath, globalSource = config.GlobalPath, configPath
	}
	if globalPath == "" {
		programData := os.Getenv("ProgramData")
		if programData == "" {
			programData = `C:\ProgramData`
		}
		globalPath, globalSource = filepath.Join(programData, "scoop"), "default"
	}
	for _, root := range []Root{{Scope: "user", Path: userPath, Source: userSource}, {Scope: "global", Path: globalPath, Source: globalSource}} {
		absolute, err := filepath.Abs(root.Path)
		if err != nil {
			s.report.AddIssue("invalid_scoop_root", "Scoopの導入先を絶対パスにできませんでした。", root.Source)
			root.Status = "incomplete"
		} else {
			root.Path = absolute
		}
		s.report.Coverage.ScoopRoots = append(s.report.Coverage.ScoopRoots, root)
	}
	if config.NoJunction {
		s.report.AddIssue("unsupported_scoop_configuration", "no_junction設定での使用中の版の選択には対応していません。", configPath)
	}
}

func (s *scanner) readConfig(path string, config *scoopConfig) bool {
	data, err := s.readFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false
	}
	if err != nil {
		s.report.AddIssue("unreadable_scoop_config", "Scoopの設定を読み取れませんでした。", path)
		return false
	}
	var parsed scoopConfig
	if err := decodeObject(data, &parsed); err != nil {
		s.report.AddIssue("invalid_scoop_config", "Scoopの設定が不正または未対応の形式です。", path)
		return false
	}
	*config = parsed
	return true
}

func (s *scanner) loadPackages(root *Root) {
	if root.Status == "incomplete" {
		return
	}
	appsPath := filepath.Join(root.Path, "apps")
	entries, err := s.readDir(appsPath)
	if errors.Is(err, fs.ErrNotExist) {
		root.Status = "absent"
		return
	}
	if err != nil {
		root.Status = "incomplete"
		s.report.AddIssue("unreadable_scoop_root", "Scoopの導入済みパッケージを列挙できませんでした。", appsPath)
		return
	}
	root.Status = "complete"
	for _, entry := range entries {
		appPath := filepath.Join(appsPath, entry.Name())
		info, err := os.Stat(appPath)
		if err != nil || !info.IsDir() {
			root.Status = "incomplete"
			s.report.AddIssue("invalid_package_directory", "パッケージの導入先を確認できませんでした。", appPath)
			continue
		}
		before := len(s.report.Issues)
		s.loadPackage(*root, entry.Name(), appPath)
		if len(s.report.Issues) > before {
			root.Status = "incomplete"
		}
	}
}

type manifest struct {
	Version      string                     `json:"version"`
	Bin          json.RawMessage            `json:"bin"`
	Shortcuts    json.RawMessage            `json:"shortcuts"`
	EnvAddPath   json.RawMessage            `json:"env_add_path"`
	Architecture map[string]json.RawMessage `json:"architecture"`
}

func (s *scanner) loadPackage(root Root, name, appPath string) {
	current := filepath.Join(appPath, "current")
	p := Package{ID: "scoop:" + root.Scope + ":" + strings.ToLower(appPath), Name: name, Manager: "scoop", Scope: root.Scope,
		InstallPath: current, Kind: "unknown", Commands: []string{}, Sources: []string{}, VersionReason: "導入済みマニフェストを確認できませんでした。"}
	if actual, err := resolvedPath(current); err == nil {
		p.InstallPath = actual
	} else {
		s.report.AddIssue("unavailable_current", "使用中の導入先currentを確認できませんでした。版ディレクトリからの推測は行いません。", current)
		s.report.Packages = append(s.report.Packages, p)
		return
	}
	data, source, err := s.installedJSON(current, "manifest")
	if err != nil {
		s.report.AddIssue("unreadable_manifest", "導入済みマニフェストを読み取れませんでした。", source)
		s.report.Packages = append(s.report.Packages, p)
		return
	}
	p.Sources = append(p.Sources, source)
	var m manifest
	if err := decodeObject(data, &m); err != nil {
		s.report.AddIssue("invalid_manifest", "導入済みマニフェストが不正または未対応の形式です。", source)
		s.report.Packages = append(s.report.Packages, p)
		return
	}
	if !validVersion(m.Version) {
		p.VersionReason = "導入済みマニフェストに版がありません。"
		if m.Version != "" {
			p.VersionReason = "導入済みマニフェストの版が未対応の形式です。"
		}
		s.report.AddIssue("unknown_package_version", p.VersionReason, source)
	} else if m.Version == "nightly" {
		version := filepath.Base(p.InstallPath)
		if strings.HasPrefix(version, "nightly-") && validVersion(version) {
			p.Version, p.VersionSource, p.VersionReason = &version, &p.InstallPath, ""
		} else {
			p.VersionReason = "nightlyの導入版を実体のディレクトリから確認できませんでした。"
			s.report.AddIssue("unknown_package_version", p.VersionReason, source)
		}
	} else {
		p.Version, p.VersionSource, p.VersionReason = &m.Version, &source, ""
	}
	bin := m.Bin
	if len(m.Architecture) > 0 {
		installData, installSource, err := s.installedJSON(current, "install")
		var info struct {
			Architecture string `json:"architecture"`
		}
		if err != nil || decodeObject(installData, &info) != nil || !slices.Contains([]string{"32bit", "64bit", "arm64"}, info.Architecture) {
			s.report.AddIssue("unknown_architecture", "導入時のアーキテクチャを確認できないため公開コマンドは未確認です。", installSource)
			s.report.Packages = append(s.report.Packages, p)
			return
		}
		p.Sources = append(p.Sources, installSource)
		if specific, ok := m.Architecture[info.Architecture]; ok {
			var arch manifest
			if decodeObject(specific, &arch) != nil {
				s.report.AddIssue("unsupported_bin", "アーキテクチャ別の公開コマンド定義が未対応の形式です。", source)
				s.report.Packages = append(s.report.Packages, p)
				return
			}
			if len(arch.Bin) > 0 && string(arch.Bin) != "null" {
				bin = arch.Bin
			}
			if len(arch.Shortcuts) > 0 && string(arch.Shortcuts) != "null" {
				m.Shortcuts = arch.Shortcuts
			}
			if len(arch.EnvAddPath) > 0 && string(arch.EnvAddPath) != "null" {
				m.EnvAddPath = arch.EnvAddPath
			}
		}
	}
	bins, err := parseBins(bin, current, p.ID)
	if err != nil {
		s.report.AddIssue("unsupported_bin", "公開コマンド定義が未対応の形式です。", source)
	} else {
		p.Kind = "no_commands"
		if len(m.Shortcuts) > 0 && string(m.Shortcuts) != "null" {
			var shortcuts [][]string
			if json.Unmarshal(m.Shortcuts, &shortcuts) != nil {
				p.Kind = "unknown"
				s.report.AddIssue("unsupported_shortcuts", "ショートカット定義が未対応の形式です。", source)
			} else if len(shortcuts) > 0 {
				p.Kind = "gui"
			}
		}
		if len(bins) > 0 {
			p.Kind = "cli"
		}
		for _, b := range bins {
			p.Commands = append(p.Commands, b.name)
		}
		p.Commands = normalizedNames(p.Commands)
		s.bins = append(s.bins, bins...)
	}
	if value := string(bytes.TrimSpace(m.EnvAddPath)); value != "" && value != "null" && value != "[]" {
		s.report.AddIssue("unsupported_env_add_path", "env_add_pathで追加されるディレクトリからの公開コマンド列挙には対応していません。名前を明示したPATH探索は利用できます。", source)
		if len(bins) == 0 {
			p.Kind = "unknown"
		}
	}
	s.report.Packages = append(s.report.Packages, p)
}

func (s *scanner) installedJSON(current, kind string) ([]byte, string, error) {
	path := filepath.Join(current, "scoop-"+kind+".json")
	data, err := s.readFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		path = filepath.Join(current, kind+".json")
		data, err = s.readFile(path)
	}
	return data, path, err
}

func decodeObject(data []byte, target any) error {
	data = bytes.TrimSpace(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf}))
	if len(data) == 0 || data[0] != '{' {
		return fs.ErrInvalid
	}
	return json.Unmarshal(data, target)
}

func validVersion(version string) bool {
	if version == "" {
		return false
	}
	for _, r := range version {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune("._+-", r) {
			return false
		}
	}
	return true
}

func parseBins(data json.RawMessage, current, packageID string) ([]installedBin, error) {
	if len(data) == 0 || string(data) == "null" {
		return nil, nil
	}
	var items []json.RawMessage
	if len(data) > 0 && data[0] == '"' {
		items = []json.RawMessage{data}
	} else if err := json.Unmarshal(data, &items); err != nil {
		return nil, err
	}
	var result []installedBin
	for _, item := range items {
		var target, name string
		if json.Unmarshal(item, &target) != nil {
			var tuple []json.RawMessage
			if json.Unmarshal(item, &tuple) != nil || len(tuple) < 2 || len(tuple) > 3 || json.Unmarshal(tuple[0], &target) != nil || json.Unmarshal(tuple[1], &name) != nil {
				return nil, fs.ErrInvalid
			}
		}
		if target == "" || strings.ContainsAny(target, "\x00\r\n") {
			return nil, fs.ErrInvalid
		}
		if name == "" {
			base := filepath.Base(target)
			name = strings.TrimSuffix(base, filepath.Ext(base))
		}
		if !ValidCommandName(name) {
			return nil, fs.ErrInvalid
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(current, target)
		}
		result = append(result, installedBin{name: strings.ToLower(name), target: filepath.Clean(target), packageID: packageID})
	}
	return result, nil
}

// ValidCommandName はPATH内のファイル名だけを許可し、パスやワイルドカードを拒否する。
func ValidCommandName(name string) bool {
	if name == "" || strings.HasPrefix(name, "-") || strings.TrimSpace(name) != name || strings.HasSuffix(name, ".") {
		return false
	}
	for _, r := range name {
		if r < 32 || strings.ContainsRune(`\/:*?"<>|`, r) {
			return false
		}
	}
	return true
}
