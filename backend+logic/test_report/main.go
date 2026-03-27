package main

import (
	"bufio"
	"fmt"
	"html/template"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

type TestResult struct {
	Name     string
	Status   string
	Duration string
	Logs     []string
}

type Section struct {
	Title string
	Tests []TestResult
}

type TestReport struct {
	Timestamp string
	Duration  string
	Total     int
	Passed    int
	Failed    int
	Sections  []Section
}

const reportHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<title>Proctor-Alpha — Test Report</title>
<style>
  @import url('https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:wght@400;500;600&family=IBM+Plex+Sans:wght@400;500;600;700&display=swap');

  :root {
    --bg: #ffffff;
    --bg-card: #fafafa;
    --bg-log: #f5f5f5;
    --border: #e0e0e0;
    --text: #1a1a1a;
    --text-secondary: #666666;
    --text-muted: #999999;
    --green: #16a34a;
    --green-bg: #f0fdf4;
    --green-border: #bbf7d0;
    --red: #dc2626;
    --red-bg: #fef2f2;
    --red-border: #fecaca;
    --mono: 'IBM Plex Mono', 'Menlo', monospace;
    --sans: 'IBM Plex Sans', -apple-system, sans-serif;
  }

  * { margin: 0; padding: 0; box-sizing: border-box; }

  body {
    font-family: var(--sans);
    background: var(--bg);
    color: var(--text);
    max-width: 880px;
    margin: 0 auto;
    padding: 48px 32px;
    line-height: 1.6;
  }

  /* ---------- Header ---------- */
  .header {
    margin-bottom: 40px;
    padding-bottom: 24px;
    border-bottom: 1px solid var(--border);
  }
  .header h1 {
    font-size: 22px;
    font-weight: 700;
    letter-spacing: -0.3px;
    margin-bottom: 6px;
  }
  .header .meta {
    font-size: 13px;
    color: var(--text-secondary);
  }

  /* ---------- Stats ---------- */
  .stats {
    display: flex;
    gap: 1px;
    background: var(--border);
    border: 1px solid var(--border);
    border-radius: 8px;
    overflow: hidden;
    margin-bottom: 40px;
  }
  .stat {
    flex: 1;
    background: var(--bg);
    padding: 20px;
    text-align: center;
  }
  .stat .number {
    font-size: 32px;
    font-weight: 700;
    font-family: var(--mono);
    line-height: 1.1;
  }
  .stat .label {
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 1.5px;
    color: var(--text-muted);
    margin-top: 4px;
  }
  .stat.pass .number { color: var(--green); }
  .stat.fail .number { color: var(--red); }

  /* ---------- Sections ---------- */
  .section {
    margin-bottom: 36px;
  }
  .section-title {
    font-size: 11px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 2px;
    color: var(--text-muted);
    margin-bottom: 12px;
  }

  /* ---------- Test Items ---------- */
  .test {
    border: 1px solid var(--border);
    border-radius: 6px;
    margin-bottom: 6px;
    overflow: hidden;
    background: var(--bg);
  }
  .test-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 10px 16px;
  }
  .test-name {
    font-family: var(--mono);
    font-size: 13px;
    font-weight: 500;
    color: var(--text);
  }
  .test-right {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-shrink: 0;
  }
  .test-dur {
    font-family: var(--mono);
    font-size: 11px;
    color: var(--text-muted);
  }
  .badge {
    font-family: var(--mono);
    font-size: 10px;
    font-weight: 600;
    letter-spacing: 1px;
    padding: 2px 8px;
    border-radius: 3px;
  }
  .badge.pass {
    background: var(--green-bg);
    color: var(--green);
    border: 1px solid var(--green-border);
  }
  .badge.fail {
    background: var(--red-bg);
    color: var(--red);
    border: 1px solid var(--red-border);
  }

  /* ---------- Logs ---------- */
  .test-logs {
    border-top: 1px solid var(--border);
    padding: 10px 16px;
    background: var(--bg-log);
  }
  .log-line {
    font-family: var(--mono);
    font-size: 12px;
    color: var(--text-secondary);
    padding: 2px 0;
    line-height: 1.5;
  }
  .log-line.worker {
    color: var(--text-muted);
    font-style: italic;
  }

  /* ---------- Footer ---------- */
  .footer {
    margin-top: 48px;
    padding-top: 20px;
    border-top: 1px solid var(--border);
    text-align: center;
    font-size: 12px;
    color: var(--text-muted);
  }
</style>
</head>
<body>
  <div class="header">
    <h1>Proctor-Alpha Test Report</h1>
    <div class="meta">{{.Timestamp}} &middot; Runtime: {{.Duration}}</div>
  </div>

  <div class="stats">
    <div class="stat">
      <div class="number">{{.Total}}</div>
      <div class="label">Total</div>
    </div>
    <div class="stat pass">
      <div class="number">{{.Passed}}</div>
      <div class="label">Passed</div>
    </div>
    <div class="stat fail">
      <div class="number">{{.Failed}}</div>
      <div class="label">Failed</div>
    </div>
  </div>

  {{range .Sections}}
  <div class="section">
    <div class="section-title">{{.Title}}</div>
    {{range .Tests}}
    <div class="test">
      <div class="test-row">
        <span class="test-name">{{.Name}}</span>
        <div class="test-right">
          {{if .Duration}}<span class="test-dur">{{.Duration}}</span>{{end}}
          <span class="badge {{if eq .Status "PASS"}}pass{{else}}fail{{end}}">{{.Status}}</span>
        </div>
      </div>
      {{if .Logs}}
      <div class="test-logs">
        {{range .Logs}}<div class="log-line">{{.}}</div>{{end}}
      </div>
      {{end}}
    </div>
    {{end}}
  </div>
  {{end}}

  <div class="footer">Generated by Proctor-Alpha test runner &middot; Go testing framework</div>
</body>
</html>`

func main() {
	fmt.Println("Running tests...")

	cmd := exec.Command("go", "test", "-v", "-count=1")
	cmd.Dir = ".."
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("Tests finished with errors: %v\n", err)
	}

	lines := strings.Split(string(output), "\n")

	type parsedTest struct {
		status   string
		duration string
		logs     []string
	}

	testMap := make(map[string]*parsedTest)
	var order []string
	var currentName string

	reRun := regexp.MustCompile(`^=== RUN\s+(.+)`)
	reResult := regexp.MustCompile(`^\s*--- (PASS|FAIL): (\S+)\s+\((.+)\)`)
	reOk := regexp.MustCompile(`^ok\s+\S+\s+(.+)`)
	reLog := regexp.MustCompile(`^\s+\S+\.go:\d+:\s(.+)`)

	totalDuration := ""

	for _, line := range lines {
		line = strings.TrimRight(line, "\r")

		if m := reRun.FindStringSubmatch(line); m != nil {
			name := strings.TrimSpace(m[1])
			currentName = name
			if _, exists := testMap[name]; !exists {
				testMap[name] = &parsedTest{}
				order = append(order, name)
			}
			continue
		}

		if m := reResult.FindStringSubmatch(line); m != nil {
			status := m[1]
			name := m[2]
			dur := m[3]
			if p, ok := testMap[name]; ok {
				p.status = status
				p.duration = dur
			}
			continue
		}

		if m := reOk.FindStringSubmatch(line); m != nil {
			totalDuration = m[1]
			continue
		}

		if m := reLog.FindStringSubmatch(line); m != nil {
			if currentName != "" {
				if p, ok := testMap[currentName]; ok {
					p.logs = append(p.logs, m[1])
				}
			}
			continue
		}

		if strings.HasPrefix(line, "[Telemetry Worker]") && currentName != "" {
			if p, ok := testMap[currentName]; ok {
				p.logs = append(p.logs, line)
			}
		}
	}

	// Build sections — skip parent tests that have subtests
	hasChildren := make(map[string]bool)
	for _, name := range order {
		if idx := strings.Index(name, "/"); idx > 0 {
			hasChildren[name[:idx]] = true
		}
	}

	var levenshtein, processEval, lifecycle []TestResult

	for _, name := range order {
		p := testMap[name]
		if p.status == "" {
			continue
		}
		// Skip parent test if it has subtests (avoid double counting)
		if hasChildren[name] {
			continue
		}

		displayName := name
		if idx := strings.Index(name, "/"); idx > 0 {
			displayName = name[idx+1:]
		}

		tr := TestResult{
			Name:     displayName,
			Status:   p.status,
			Duration: p.duration,
			Logs:     p.logs,
		}

		switch {
		case strings.HasPrefix(name, "TestLevenshtein"):
			levenshtein = append(levenshtein, tr)
		case strings.HasPrefix(name, "TestEvaluateProcesses"):
			processEval = append(processEval, tr)
		default:
			lifecycle = append(lifecycle, tr)
		}
	}

	var sections []Section
	if len(levenshtein) > 0 {
		sections = append(sections, Section{
			Title: "Levenshtein Distance — Mathematical Computation",
			Tests: levenshtein,
		})
	}
	if len(processEval) > 0 {
		sections = append(sections, Section{
			Title: "Process Evaluation — Advanced String Manipulation",
			Tests: processEval,
		})
	}
	if len(lifecycle) > 0 {
		sections = append(sections, Section{
			Title: "Full Exam Lifecycle — Integration Test",
			Tests: lifecycle,
		})
	}

	passed, failed := 0, 0
	for _, sec := range sections {
		for _, t := range sec.Tests {
			if t.Status == "PASS" {
				passed++
			} else {
				failed++
			}
		}
	}
	total := passed + failed

	report := TestReport{
		Timestamp: time.Now().Format("02 Jan 2006, 03:04 PM"),
		Duration:  totalDuration,
		Total:     total,
		Passed:    passed,
		Failed:    failed,
		Sections:  sections,
	}

	outFile, _ := os.Create("test_report.html")
	defer outFile.Close()
	w := bufio.NewWriter(outFile)
	tmpl := template.Must(template.New("report").Parse(reportHTML))
	tmpl.Execute(w, report)
	w.Flush()

	fmt.Printf("Report: %d passed, %d failed out of %d\n", passed, failed, total)
	fmt.Println("Open: test_report.html")
}
