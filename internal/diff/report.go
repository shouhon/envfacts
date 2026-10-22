package diff

import (
	"time"

	"github.com/shouhon/envfacts/internal/scan"
)

// Report は保存済みの調査記録だけから得た比較結果を表す。
type Report struct {
	SchemaVersion    int            `json:"schema_version"`
	Tool             string         `json:"tool"`
	Command          string         `json:"command"`
	Status           string         `json:"status"`
	CapturedAt       string         `json:"captured_at"`
	Coverage         Coverage       `json:"coverage"`
	Packages         []scan.Package `json:"packages"`
	Commands         []scan.Command `json:"commands"`
	InvocationStatus string         `json:"invocation_status"`
	Changes          []Change       `json:"changes"`
	Issues           []scan.Issue   `json:"issues"`
}

type Coverage struct {
	Before *scan.Coverage `json:"before"`
	After  *scan.Coverage `json:"after"`
	Common Common         `json:"common"`
}

type Common struct {
	ScoopRoots                 []Root   `json:"scoop_roots"`
	Commands                   []string `json:"commands"`
	SearchPaths                []string `json:"search_paths"`
	Extensions                 []string `json:"extensions"`
	PackageInventoryComparable bool     `json:"package_inventory_comparable"`
	CommandInventoryComparable bool     `json:"command_inventory_comparable"`
	ResolutionComparable       bool     `json:"resolution_comparable"`
}

type Root struct {
	Scope    string `json:"scope"`
	Path     string `json:"path"`
	Complete bool   `json:"complete"`
}

// Change の値には比較に必要な事実だけを入れ、入力の本文や診断を転記しない。
type Change struct {
	Kind    string `json:"kind"`
	Entity  string `json:"entity"`
	ID      string `json:"id"`
	Command string `json:"command,omitempty"`
	Field   string `json:"field"`
	Before  any    `json:"before"`
	After   any    `json:"after"`
}

func NewReport() Report {
	return Report{
		SchemaVersion: 1, Tool: "envfacts", Command: "diff", Status: "unchanged",
		CapturedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Coverage:   Coverage{Common: Common{ScoopRoots: []Root{}, Commands: []string{}, SearchPaths: []string{}, Extensions: []string{}}},
		Packages:   []scan.Package{}, Commands: []scan.Command{}, InvocationStatus: "not_tested",
		Changes: []Change{}, Issues: []scan.Issue{},
	}
}

func (r *Report) AddIssue(code, message, source string) {
	r.Status = "incomplete"
	r.Issues = append(r.Issues, scan.Issue{Code: code, Message: message, Source: source})
}

func (r Report) ExitCode() int {
	switch r.Status {
	case "invalid_arguments":
		return 64
	case "incomplete":
		return 2
	case "changed":
		return 1
	default:
		return 0
	}
}
