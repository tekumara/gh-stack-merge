package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"
)

type stack struct {
	Branches []branch `json:"branches"`
}

type branch struct {
	IsMerged bool         `json:"isMerged"`
	IsQueued bool         `json:"isQueued"`
	PR       *pullRequest `json:"pr"`
}

type pullRequest struct {
	Number int `json:"number"`
}

type remoteStack struct {
	Number       int           `json:"number"`
	PullRequests []pullRequest `json:"pull_requests"`
}

func gh(args ...string) ([]byte, error) {
	output, err := exec.Command("gh", args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("gh %s: %s", strings.Join(args, " "), strings.TrimSpace(string(output)))
	}
	return output, nil
}

func waitForChecks(pr int) error {
	args := []string{"pr", "checks", fmt.Sprint(pr), "--required", "--json", "bucket"}
	command := exec.Command("gh", args...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 8 {
			_, err := gh("pr", "checks", fmt.Sprint(pr), "--required", "--watch", "--fail-fast", "--interval", "10")
			return err
		}
		if strings.Contains(stderr.String(), "no required checks reported") {
			return nil
		}
		message := strings.TrimSpace(stdout.String() + "\n" + stderr.String())
		return fmt.Errorf("gh %s: %s", strings.Join(args, " "), message)
	}
	return nil
}

func queued(pr int) (bool, error) {
	output, err := gh("stack", "view", "--json")
	if err != nil {
		return false, err
	}
	var current stack
	if err := json.Unmarshal(output, &current); err != nil {
		return false, fmt.Errorf("read stack: %w", err)
	}
	for _, branch := range current.Branches {
		if branch.PR != nil && branch.PR.Number == pr {
			return branch.IsQueued, nil
		}
	}
	return false, nil
}

func validateStack(pr int, expected []int) error {
	path := fmt.Sprintf("repos/{owner}/{repo}/stacks/%d", pr)
	command := exec.Command("gh", "api", path, "--silent")
	output, err := command.CombinedOutput()
	if err == nil {
		return fmt.Errorf("cannot safely merge PR #%d because a stack has the same number", pr)
	}
	if !strings.Contains(string(output), "HTTP 404") {
		return fmt.Errorf("gh api %s --silent: %s", path, strings.TrimSpace(string(output)))
	}

	output, err = gh("api", fmt.Sprintf("repos/{owner}/{repo}/stacks?pull_request=%d", pr))
	if err != nil {
		return err
	}
	var stacks []remoteStack
	if err := json.Unmarshal(output, &stacks); err != nil {
		return fmt.Errorf("read remote stack: %w", err)
	}
	if len(stacks) != 1 {
		return fmt.Errorf("PR #%d does not belong to one remote stack", pr)
	}
	if stacks[0].Number == pr {
		return fmt.Errorf("cannot safely merge PR #%d because a stack has the same number", pr)
	}
	actual := make([]int, 0, len(stacks[0].PullRequests))
	for _, remotePR := range stacks[0].PullRequests {
		actual = append(actual, remotePR.Number)
		if remotePR.Number == pr {
			break
		}
	}
	if !slices.Equal(actual, expected) {
		return fmt.Errorf("stack changed before merging PR #%d; run the command again", pr)
	}
	return nil
}

func checksFailed(pr int) (bool, error) {
	args := []string{"pr", "checks", fmt.Sprint(pr), "--json", "bucket"}
	output, commandErr := exec.Command("gh", args...).CombinedOutput()
	var checks []struct {
		Bucket string `json:"bucket"`
	}
	if err := json.Unmarshal(output, &checks); err != nil {
		if strings.Contains(string(output), "no checks reported") {
			return false, nil
		}
		return false, fmt.Errorf("gh %s: %s", strings.Join(args, " "), strings.TrimSpace(string(output)))
	}
	for _, check := range checks {
		if check.Bucket == "fail" || check.Bucket == "cancel" {
			return true, nil
		}
	}
	if commandErr != nil {
		if exit, ok := commandErr.(*exec.ExitError); !ok || (exit.ExitCode() != 1 && exit.ExitCode() != 8) {
			return false, fmt.Errorf("gh %s: %s", strings.Join(args, " "), strings.TrimSpace(string(output)))
		}
	}
	return false, nil
}

func mergePR(pr int, method string, expected []int) ([]byte, error) {
	args := []string{"stack", "merge", fmt.Sprint(pr), "--yes", method}
	waiting := false
	for {
		if err := validateStack(pr, expected); err != nil {
			return nil, err
		}
		output, err := exec.Command("gh", args...).CombinedOutput()
		if err == nil {
			return output, nil
		}
		message := string(output)
		workflowPending := strings.Contains(message, "is not satisfied") || strings.Contains(message, "is still running")
		if !strings.Contains(message, "Required workflow") ||
			!workflowPending ||
			!strings.Contains(message, "Stack merges are atomic, so nothing was merged.") {
			return nil, fmt.Errorf("gh %s: %s", strings.Join(args, " "), strings.TrimSpace(message))
		}
		failed, err := checksFailed(pr)
		if err != nil {
			return nil, err
		}
		if failed {
			return nil, fmt.Errorf("PR #%d has failed checks", pr)
		}
		if !waiting {
			fmt.Printf("PR #%d: waiting for required workflows\n", pr)
			waiting = true
		}
		time.Sleep(5 * time.Second)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("gh stack-merge", flag.ContinueOnError)
	squash := flags.Bool("squash", false, "squash and merge")
	merge := flags.Bool("merge", false, "merge with a merge commit")
	rebase := flags.Bool("rebase", false, "rebase and merge")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected argument: %s", flags.Arg(0))
	}
	selected := 0
	for _, enabled := range []bool{*squash, *merge, *rebase} {
		if enabled {
			selected++
		}
	}
	if selected > 1 {
		return fmt.Errorf("choose only one merge method")
	}
	method := "--squash"
	if *merge {
		method = "--merge"
	}
	if *rebase {
		method = "--rebase"
	}

	output, err := gh("stack", "view", "--json")
	if err != nil {
		return err
	}
	var current stack
	if err := json.Unmarshal(output, &current); err != nil {
		return fmt.Errorf("read stack: %w", err)
	}

	snapshot := make([]int, 0, len(current.Branches))
	for _, branch := range current.Branches {
		if branch.PR != nil {
			snapshot = append(snapshot, branch.PR.Number)
		}
	}

	merged := 0
	seen := 0
	for _, branch := range current.Branches {
		if branch.PR != nil {
			seen++
		}
		if branch.IsMerged {
			continue
		}
		if branch.PR == nil {
			return fmt.Errorf("unmerged branch has no pull request")
		}
		pr := branch.PR.Number
		wasQueued := branch.IsQueued
		if branch.IsQueued {
			fmt.Printf("PR #%d: queued\n", pr)
		} else {
			fmt.Printf("PR #%d: checking required checks\n", pr)
			if err := waitForChecks(pr); err != nil {
				return err
			}
			fmt.Printf("PR #%d: merging\n", pr)
			output, err := mergePR(pr, method, snapshot[:seen])
			if err != nil {
				return err
			}
			if strings.Contains(string(output), "merge queue") {
				fmt.Printf("PR #%d: queued\n", pr)
				wasQueued = true
			}
		}
		waiting := false
		for {
			state, err := gh("pr", "view", fmt.Sprint(pr), "--json", "state", "--jq", ".state")
			if err != nil {
				return err
			}
			switch strings.TrimSpace(string(state)) {
			case "MERGED":
				fmt.Printf("PR #%d: merged\n", pr)
				goto merged
			case "CLOSED":
				return fmt.Errorf("PR #%d closed without merging", pr)
			}
			isQueued, err := queued(pr)
			if err != nil {
				return err
			}
			if isQueued {
				wasQueued = true
			} else if wasQueued {
				state, err := gh("pr", "view", fmt.Sprint(pr), "--json", "state", "--jq", ".state")
				if err != nil {
					return err
				}
				if strings.TrimSpace(string(state)) == "MERGED" {
					fmt.Printf("PR #%d: merged\n", pr)
					goto merged
				}
				return fmt.Errorf("PR #%d left the merge queue without merging", pr)
			}
			if !waiting {
				fmt.Printf("PR #%d: waiting for GitHub\n", pr)
				waiting = true
			}
			time.Sleep(5 * time.Second)
		}
	merged:
		merged++
	}
	fmt.Printf("Merged %d pull requests.\n", merged)
	return nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
