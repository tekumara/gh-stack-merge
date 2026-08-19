package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergesEachPullRequestBottomToTop(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "gh-stack-merge")
	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build command: %v\n%s", err, output)
	}

	log := filepath.Join(dir, "calls")
	fakeGH := `#!/bin/sh
printf '%s\n' "$*" >> "$CALL_LOG"
case "$*" in
  "stack view --json") cat <<'JSON'
{"branches":[
  {"isMerged":true,"pr":{"number":1,"state":"MERGED"}},
  {"isMerged":false,"pr":{"number":2,"state":"OPEN"}},
  {"isMerged":false,"pr":{"number":3,"state":"OPEN"}}
]}
JSON
    ;;
  "api repos/{owner}/{repo}/stacks/2 --silent"|"api repos/{owner}/{repo}/stacks/3 --silent") echo 'gh: Not Found (HTTP 404)' >&2; exit 1 ;;
  "api repos/{owner}/{repo}/stacks?pull_request=2") echo '[{"number":99,"pull_requests":[{"number":1},{"number":2},{"number":3}]}]' ;;
  "api repos/{owner}/{repo}/stacks?pull_request=3") echo '[{"number":99,"pull_requests":[{"number":1},{"number":2},{"number":3}]}]' ;;
  "pr checks 2 --required --json bucket") exit 8 ;;
  "pr checks 2 --required --watch --fail-fast --interval 10") ;;
  "pr checks 3 --required --json bucket")
    echo "no required checks reported" >&2
    exit 1
    ;;
  "stack merge 2 --yes --squash") echo 'Added #2 to the merge queue for main' ;;
  "stack merge 3 --yes --squash") ;;
  "pr view 2 --json state --jq .state"|"pr view 3 --json state --jq .state") echo MERGED ;;
  *) echo "unexpected gh command: $*" >&2; exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(fakeGH), 0o755); err != nil {
		t.Fatal(err)
	}

	command := exec.Command(binary)
	command.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "CALL_LOG="+log)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("gh stack-merge: %v\n%s", err, output)
	}

	wantOutput := "PR #2: checking required checks\nPR #2: merging\nPR #2: queued\nPR #2: merged\nPR #3: checking required checks\nPR #3: merging\nPR #3: merged\nMerged 2 pull requests.\n"
	if string(output) != wantOutput {
		t.Errorf("output:\n%s\nwant:\n%s", output, wantOutput)
	}

	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	wantCalls := strings.Join([]string{
		"stack view --json",
		"pr checks 2 --required --json bucket",
		"pr checks 2 --required --watch --fail-fast --interval 10",
		"api repos/{owner}/{repo}/stacks/2 --silent",
		"api repos/{owner}/{repo}/stacks?pull_request=2",
		"stack merge 2 --yes --squash",
		"pr view 2 --json state --jq .state",
		"pr checks 3 --required --json bucket",
		"api repos/{owner}/{repo}/stacks/3 --silent",
		"api repos/{owner}/{repo}/stacks?pull_request=3",
		"stack merge 3 --yes --squash",
		"pr view 3 --json state --jq .state",
	}, "\n") + "\n"
	if string(calls) != wantCalls {
		t.Errorf("gh calls:\n%s\nwant:\n%s", calls, wantCalls)
	}
}

func TestResumesAQueuedPullRequest(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "gh-stack-merge")
	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build command: %v\n%s", err, output)
	}

	log := filepath.Join(dir, "calls")
	fakeGH := `#!/bin/sh
printf '%s\n' "$*" >> "$CALL_LOG"
case "$*" in
  "stack view --json") echo '{"branches":[{"isMerged":false,"isQueued":true,"pr":{"number":4,"state":"QUEUED"}}]}' ;;
  "pr view 4 --json state --jq .state")
    count_file="$CALL_LOG.count"
    count=$(cat "$count_file" 2>/dev/null || echo 0)
    count=$((count + 1))
    echo "$count" > "$count_file"
    if [ "$count" -eq 1 ]; then echo OPEN; else echo MERGED; fi
    ;;
  *) echo "unexpected gh command: $*" >&2; exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(fakeGH), 0o755); err != nil {
		t.Fatal(err)
	}

	command := exec.Command(binary)
	command.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "CALL_LOG="+log)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("gh stack-merge: %v\n%s", err, output)
	}
	want := "PR #4: queued\nPR #4: waiting for GitHub\nPR #4: merged\nMerged 1 pull requests.\n"
	if string(output) != want {
		t.Errorf("output:\n%s\nwant:\n%s", output, want)
	}

	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	wantCalls := "stack view --json\npr view 4 --json state --jq .state\nstack view --json\npr view 4 --json state --jq .state\n"
	if string(calls) != wantCalls {
		t.Errorf("gh calls:\n%s\nwant:\n%s", calls, wantCalls)
	}
}

func TestPassesMergeMethod(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "gh-stack-merge")
	if output, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build command: %v\n%s", err, output)
	}

	log := filepath.Join(dir, "calls")
	fakeGH := `#!/bin/sh
printf '%s\n' "$*" >> "$CALL_LOG"
case "$*" in
  "stack view --json") echo '{"branches":[{"isMerged":false,"pr":{"number":5,"state":"OPEN"}}]}' ;;
  "api repos/{owner}/{repo}/stacks/5 --silent") echo 'gh: Not Found (HTTP 404)' >&2; exit 1 ;;
  "api repos/{owner}/{repo}/stacks?pull_request=5") echo '[{"number":99,"pull_requests":[{"number":5}]}]' ;;
  "pr checks 5 --required --json bucket") echo "no required checks reported" >&2; exit 1 ;;
  "stack merge 5 --yes --merge") ;;
  "pr view 5 --json state --jq .state") echo MERGED ;;
  *) echo "unexpected gh command: $*" >&2; exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(fakeGH), 0o755); err != nil {
		t.Fatal(err)
	}

	command := exec.Command(binary, "--merge")
	command.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "CALL_LOG="+log)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("gh stack-merge --merge: %v\n%s", err, output)
	}

	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(calls), "stack merge 5 --yes --merge\n") {
		t.Errorf("gh calls:\n%s", calls)
	}
}

func TestStopsWhenPRNumberCouldSelectAStack(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "gh-stack-merge")
	if output, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build command: %v\n%s", err, output)
	}

	log := filepath.Join(dir, "calls")
	fakeGH := `#!/bin/sh
printf '%s\n' "$*" >> "$CALL_LOG"
case "$*" in
  "stack view --json") echo '{"branches":[{"isMerged":false,"pr":{"number":6}}]}' ;;
  "pr checks 6 --required --json bucket") echo "no required checks reported" >&2; exit 1 ;;
  "api repos/{owner}/{repo}/stacks/6 --silent") ;;
  "api repos/{owner}/{repo}/stacks?pull_request=6") echo '[{"number":99,"pull_requests":[{"number":6}]}]' ;;
  *) echo "unexpected gh command: $*" >&2; exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(fakeGH), 0o755); err != nil {
		t.Fatal(err)
	}

	command := exec.Command(binary)
	command.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "CALL_LOG="+log)
	output, err := command.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "cannot safely merge PR #6") {
		t.Fatalf("gh stack-merge: %v\n%s", err, output)
	}

	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	want := "stack view --json\npr checks 6 --required --json bucket\napi repos/{owner}/{repo}/stacks/6 --silent\n"
	if string(calls) != want {
		t.Errorf("gh calls:\n%s\nwant:\n%s", calls, want)
	}
}
