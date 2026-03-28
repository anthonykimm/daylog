package main

import (
	"os/exec"
	"strings"
	"time"
)

type Commit struct {
	Hash    string
	Subject string
	Hidden  bool
}

func isGitRepo() bool {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	err := cmd.Run()
	return err == nil
}

func gitUserEmail() string {
	cmd := exec.Command("git", "config", "user.email")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func loadCommits(hidden map[string]bool, showHidden bool) []Commit {
	if !isGitRepo() {
		return nil
	}

	email := gitUserEmail()
	if email == "" {
		return nil
	}

	midnight := time.Now().Truncate(24 * time.Hour)
	since := midnight.Format("2006-01-02T15:04:05")

	cmd := exec.Command("git", "log", "--all",
		"--author="+email,
		"--since="+since,
		"--format=%h %s",
	)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	var commits []Commit
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		hash := parts[0]
		subject := ""
		if len(parts) > 1 {
			subject = parts[1]
		}

		isHidden := hidden[hash]
		if isHidden && !showHidden {
			continue
		}

		commits = append(commits, Commit{
			Hash:    hash,
			Subject: subject,
			Hidden:  isHidden,
		})
	}

	return commits
}
