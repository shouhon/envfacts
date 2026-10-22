package scan

import "time"

// Report は schema_version 1 の調査記録を表す。
type Report struct {
	SchemaVersion    int       `json:"schema_version"`
	Tool             string    `json:"tool"`
	Command          string    `json:"command"`
	Status           string    `json:"status"`
	CapturedAt       string    `json:"captured_at"`
	Coverage         Coverage  `json:"coverage"`
	Packages         []Package `json:"packages"`
	Commands         []Command `json:"commands"`
	InvocationStatus string    `json:"invocation_status"`
	Issues           []Issue   `json:"issues"`
}

type Coverage struct {
	ScoopRoots        []Root       `json:"scoop_roots"`
	CommandSelection  string       `json:"command_selection"`
	RequestedCommands []string     `json:"requested_commands"`
	SearchPaths       []SearchPath `json:"search_paths"`
	Extensions        []string     `json:"extensions"`
	Excluded          []string     `json:"excluded"`
}

type Root struct {
	Scope  string `json:"scope"`
	Path   string `json:"path"`
	Source string `json:"source"`
	Status string `json:"status"`
}

type SearchPath struct {
	Path     string `json:"path"`
	Priority int    `json:"priority"`
	Status   string `json:"status"`
}

type Package struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Manager       string   `json:"manager"`
	Scope         string   `json:"scope"`
	InstallPath   string   `json:"install_path"`
	Kind          string   `json:"kind"`
	Commands      []string `json:"commands"`
	Version       *string  `json:"version"`
	VersionSource *string  `json:"version_source"`
	VersionReason string   `json:"version_reason"`
	Sources       []string `json:"sources"`
}

type Command struct {
	Name             string      `json:"name"`
	Status           string      `json:"status"`
	PreferredPath    *string     `json:"preferred_path"`
	ActualPath       *string     `json:"actual_path"`
	Candidates       []Candidate `json:"candidates"`
	PackageIDs       []string    `json:"package_ids"`
	Version          *string     `json:"version"`
	VersionSource    *string     `json:"version_source"`
	VersionReason    string      `json:"version_reason"`
	InvocationStatus string      `json:"invocation_status"`
}

type Candidate struct {
	Path              string   `json:"path"`
	Priority          int      `json:"priority"`
	PathPriority      int      `json:"path_priority"`
	ExtensionPriority int      `json:"extension_priority"`
	ActualPath        *string  `json:"actual_path"`
	Resolution        string   `json:"resolution"`
	PackageIDs        []string `json:"package_ids"`
}

type Issue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Source  string `json:"source"`
}

// NewReport は情報源を読み取らずに空の記録を作る。引数エラーにも使用する。
func NewReport(command string) Report {
	return Report{
		SchemaVersion: 1, Tool: "envfacts", Command: command, Status: "complete",
		CapturedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Coverage: Coverage{
			ScoopRoots: []Root{}, RequestedCommands: []string{}, SearchPaths: []SearchPath{}, Extensions: []string{},
			Excluded: []string{"対象CLIの実行・認証・ネットワーク接続", "PowerShellの関数・エイリアス・プロファイル・実行ポリシー", "WSL・コンテナー・仮想環境", "Scoop以外の導入台帳"},
		},
		Packages: []Package{}, Commands: []Command{}, InvocationStatus: "not_tested", Issues: []Issue{},
	}
}

// AddIssue は判明した結果を保持したまま、不完全な範囲を記録する。
func (r *Report) AddIssue(code, message, source string) {
	r.Status = "incomplete"
	r.Issues = append(r.Issues, Issue{Code: code, Message: message, Source: source})
}

// ExitCode は調査結果を製品の終了コードに対応させる。
func (r Report) ExitCode() int {
	switch r.Status {
	case "invalid_arguments":
		return 64
	case "incomplete":
		return 2
	case "missing":
		return 1
	default:
		return 0
	}
}
