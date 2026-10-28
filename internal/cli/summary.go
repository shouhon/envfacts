package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/shouhon/envfacts/internal/scan"
)

const summaryDetailsHint = "詳細は --json-summary を --json に替えて確認できます。--output <ファイル> は比較用の詳細記録を保存します。"

// scanSummary は表示専用の要約で、比較用の調査記録とは区別する。
type scanSummary struct {
	SchemaVersion    int             `json:"schema_version"`
	OutputFormat     string          `json:"output_format"`
	Tool             string          `json:"tool"`
	Command          string          `json:"command"`
	Status           string          `json:"status"`
	CapturedAt       string          `json:"captured_at"`
	Coverage         summaryCoverage `json:"coverage"`
	PackageCount     int             `json:"package_count"`
	Commands         []scan.Command  `json:"commands"`
	InvocationStatus string          `json:"invocation_status"`
	IssueCount       int             `json:"issue_count"`
	Issues           []summaryIssue  `json:"issues"`
	DetailsHint      string          `json:"details_hint"`
}

type summaryCoverage struct {
	ScoopRoots        []scan.Root `json:"scoop_roots"`
	CommandSelection  string      `json:"command_selection"`
	RequestedCommands []string    `json:"requested_commands"`
	SearchPathCounts  pathCounts  `json:"search_path_counts"`
	Extensions        []string    `json:"extensions"`
	Excluded          []string    `json:"excluded"`
}

type pathCounts struct {
	Complete   int `json:"complete"`
	Absent     int `json:"absent"`
	Incomplete int `json:"incomplete"`
}

type summaryIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Count   int    `json:"count"`
}

func summarize(r scan.Report) scanSummary {
	s := scanSummary{
		SchemaVersion: r.SchemaVersion, OutputFormat: "summary", Tool: r.Tool, Command: r.Command,
		Status: r.Status, CapturedAt: r.CapturedAt, PackageCount: len(r.Packages), Commands: r.Commands,
		InvocationStatus: r.InvocationStatus, IssueCount: len(r.Issues), Issues: []summaryIssue{},
		DetailsHint: summaryDetailsHint,
		Coverage: summaryCoverage{
			ScoopRoots: r.Coverage.ScoopRoots, CommandSelection: r.Coverage.CommandSelection,
			RequestedCommands: r.Coverage.RequestedCommands, Extensions: r.Coverage.Extensions,
			Excluded: r.Coverage.Excluded,
		},
	}
	for _, path := range r.Coverage.SearchPaths {
		switch path.Status {
		case "complete":
			s.Coverage.SearchPathCounts.Complete++
		case "absent":
			s.Coverage.SearchPathCounts.Absent++
		case "incomplete":
			s.Coverage.SearchPathCounts.Incomplete++
		}
	}
	// 情報源ごとの一覧を省き、同じコードと説明を持つ診断を件数でまとめる。
	type issueKey struct{ code, message string }
	counts := map[issueKey]int{}
	for _, issue := range r.Issues {
		counts[issueKey{issue.Code, issue.Message}]++
	}
	for key, count := range counts {
		s.Issues = append(s.Issues, summaryIssue{Code: key.code, Message: key.message, Count: count})
	}
	slices.SortFunc(s.Issues, func(a, b summaryIssue) int {
		if c := strings.Compare(a.Code, b.Code); c != 0 {
			return c
		}
		return strings.Compare(a.Message, b.Message)
	})
	return s
}

func writeSummaryReport(r scan.Report, stdout, stderr io.Writer) int {
	s := summarize(r)
	for _, issue := range s.Issues {
		fmt.Fprintf(stderr, "%s（%d件）: %s\n", issue.Code, issue.Count, issue.Message)
	}
	if len(s.Issues) > 0 {
		fmt.Fprintln(stderr, summaryDetailsHint)
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(s); err != nil {
		fmt.Fprintln(stderr, "標準出力へ書き込めませんでした。")
		return 2
	}
	return r.ExitCode()
}
