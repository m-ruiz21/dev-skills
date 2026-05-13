package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "ralph [iterations]",
		Short: "Automated PRD-driven development workflow",
		Long:  "Discovers PRDs under .scratch/<feature>/PRD.md and runs triage → develop → review loops using Copilot CLI.",
		Args:  cobra.ExactArgs(1),
		RunE:  run,
	}

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, args []string) error {
	iterations, err := strconv.Atoi(args[0])
	if err != nil || iterations < 1 {
		return fmt.Errorf("iterations must be a positive integer")
	}

	prds, err := discoverPRDs()
	if err != nil {
		return err
	}

	selectedPrd, selectedFeature := selectPRD(prds)

	for i := 1; i <= iterations; i++ {
		if err := runIteration(i, selectedPrd, selectedFeature); err != nil {
			return fmt.Errorf("iteration %d failed: %w", i, err)
		}

		if i < iterations {
			if !promptYesNo("Continue to next task? (y/n): ") {
				fmt.Printf("Stopping after %d iterations.\n", i)
				return nil
			}
		}
	}

	fmt.Printf("Completed all %d iterations.\n", iterations)
	return nil
}

// discoverPRDs finds all PRD.md files under .scratch/*/
func discoverPRDs() ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(".scratch", "*", "PRD.md"))
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no PRDs found under .scratch/*/PRD.md")
	}
	sort.Strings(matches)
	return matches, nil
}

// selectPRD displays a menu and returns the chosen PRD path and feature name.
func selectPRD(prds []string) (prdPath, featureName string) {
	fmt.Println()
	fmt.Println("==========================================")
	fmt.Println("  SELECT A PRD TO WORK ON")
	fmt.Println("==========================================")
	fmt.Println()

	for i, prd := range prds {
		name := extractFeatureName(prd)
		fmt.Printf("  %d) %s\n", i+1, name)
	}

	fmt.Println()
	choice := promptInt(fmt.Sprintf("Choose a PRD [1-%d]: ", len(prds)), 1, len(prds))
	prdPath = prds[choice-1]
	featureName = extractFeatureName(prdPath)

	fmt.Println()
	fmt.Printf("Selected: %s (%s)\n", featureName, prdPath)
	fmt.Println()
	return prdPath, featureName
}

func runIteration(i int, selectedPrd, selectedFeature string) error {
	// Step 1: Triage
	printBanner(fmt.Sprintf("ITERATION %d — TRIAGING", i))
	if err := runCopilot(
		"--allow-tool", "read",
		"--allow-tool", "write",
		"-p", "/triage Review and triage remaining issues. Reprioritize based on completed work and add agent brief to issues that are now ready-for-agent.",
	); err != nil {
		return fmt.Errorf("triage failed: %w", err)
	}

	// Step 2: Develop
	printBanner(fmt.Sprintf("ITERATION %d — WRITING CODE", i))
	developPrompt := fmt.Sprintf(`/develop-task Focus on the PRD at %s for feature: %s. `+
		`1. Find the highest-priority task from this PRD and implement it. `+
		`2. Run your tests and type checks. `+
		`3. Update the PRD with what was done. `+
		`4. Append your progress to progress.txt. `+
		`5. Stage your changes. `+
		`WORK ON ONLY ONE TASK AT A TIME`, selectedPrd, selectedFeature)
	if err := runCopilot("--yolo", "-p", developPrompt); err != nil {
		return fmt.Errorf("develop failed: %w", err)
	}

	// Step 3: Review-fix loop
	for {
		printBanner(fmt.Sprintf("ITERATION %d — REVIEWING", i))

		reviewPrompt := fmt.Sprintf(`/review-diff Review staged code changes. `+
			`Also check the review doc under .scratch/%s/reviews/ for any `+
			`unaddressed comments tagged with [agent] under '# User Feedback'. `+
			`If every finding is resolved and there are no unaddressed [agent] comments, `+
			`output <promise>COMPLETE</promise>. Otherwise, provide a summary of remaining issues.`, selectedFeature)

		output, err := runCopilotCapture("--allow-tool", "write", "-p", reviewPrompt)
		if err != nil {
			return fmt.Errorf("review failed: %w", err)
		}
		fmt.Println(output)

		reviewDoc := findReviewDoc(selectedFeature)

		if !strings.Contains(output, "<promise>COMPLETE</promise>") {
			// Review not complete — fix and loop
			fmt.Println()
			fmt.Println("Review has unresolved findings. Code agent addressing...")
			rdPath := reviewDoc
			if rdPath == "" {
				rdPath = fmt.Sprintf(".scratch/%s/reviews/", selectedFeature)
			}
			fixPrompt := fmt.Sprintf(`/develop-task Focus on the PRD at %s for feature: %s. `+
				`The review document at %s contains unresolved findings and/or user feedback tagged with [agent]. `+
				`Address every unresolved item. `+
				`1. Read the review doc and fix each issue. `+
				`2. Run your tests and type checks. `+
				`3. Update the PRD with what was done. `+
				`4. Append your progress to progress.txt. `+
				`5. Stage your changes.`, selectedPrd, selectedFeature, rdPath)
			if err := runCopilot("--yolo", "-p", fixPrompt); err != nil {
				return fmt.Errorf("fix failed: %w", err)
			}
			continue
		}

		// Review complete — show summary
		printBanner(fmt.Sprintf("ITERATION %d — WORK SUMMARY", i))
		fmt.Println()
		fmt.Println("--- Staged changes ---")
		runGit("diff", "--cached", "--stat")
		fmt.Println()
		fmt.Println("--- Detailed diff ---")
		runGit("diff", "--cached")
		fmt.Println()
		if reviewDoc != "" {
			fmt.Printf("Review doc: %s\n", reviewDoc)
		}

		fmt.Println()
		comments := promptString("Any comments on this work? (enter comments, or press Enter to skip): ")

		if comments == "" {
			break
		}

		// Append comment to review doc
		if reviewDoc != "" {
			appendToFile(reviewDoc, fmt.Sprintf("\n- [agent] %s — %s", time.Now().Format("2006-01-02 15:04"), comments))
			fmt.Printf("Comment appended to %s\n", reviewDoc)
		} else {
			fmt.Println("Warning: No review document found.")
		}

		fmt.Println()
		fmt.Println("Code agent picking up your feedback...")
		rdPath := reviewDoc
		if rdPath == "" {
			rdPath = fmt.Sprintf(".scratch/%s/reviews/", selectedFeature)
		}
		fbPrompt := fmt.Sprintf(`/develop-task Focus on the PRD at %s for feature: %s. `+
			`The review document at %s has new user feedback tagged with [agent] under '# User Feedback'. `+
			`Address every [agent] comment. `+
			`1. Read the review doc and fix each issue. `+
			`2. Run your tests and type checks. `+
			`3. Update the PRD with what was done. `+
			`4. Append your progress to progress.txt. `+
			`5. Stage your changes.`, selectedPrd, selectedFeature, rdPath)
		if err := runCopilot("--yolo", "-p", fbPrompt); err != nil {
			return fmt.Errorf("feedback fix failed: %w", err)
		}
	}

	// Move completed issue to closed folder
	if reviewDoc := findReviewDoc(selectedFeature); reviewDoc != "" {
		moveCompletedIssue(reviewDoc)
	}

	// Clean up progress.txt
	os.Remove("progress.txt")

	return nil
}

// --- Helpers ---

func extractFeatureName(prdPath string) string {
	// .scratch/<feature>/PRD.md → <feature>
	parts := strings.Split(filepath.ToSlash(prdPath), "/")
	if len(parts) >= 3 {
		return parts[1]
	}
	return prdPath
}

func findReviewDoc(feature string) string {
	dir := filepath.Join(".scratch", feature, "reviews")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}

	var latest string
	var latestTime time.Time
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(latestTime) {
			latestTime = info.ModTime()
			latest = filepath.Join(dir, e.Name())
		}
	}
	return latest
}

func moveCompletedIssue(reviewDoc string) {
	data, err := os.ReadFile(reviewDoc)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "issue:") {
			issuePath := strings.TrimSpace(strings.TrimPrefix(line, "issue:"))
			if issuePath == "" {
				return
			}
			if _, err := os.Stat(issuePath); err != nil {
				return
			}
			closedDir := filepath.Join(filepath.Dir(issuePath), "closed")
			os.MkdirAll(closedDir, 0755)
			dest := filepath.Join(closedDir, filepath.Base(issuePath))
			if err := os.Rename(issuePath, dest); err == nil {
				fmt.Printf("Issue moved to %s\n", dest)
			}
			return
		}
	}
}

func runCopilot(args ...string) error {
	cmd := exec.Command("copilot", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runCopilotCapture(args ...string) (string, error) {
	cmd := exec.Command("copilot", args...)
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	return string(out), err
}

func runGit(args ...string) {
	cmd := exec.Command("git", append([]string{"--no-pager"}, args...)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run()
}

func appendToFile(path, content string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString(content)
}

func printBanner(text string) {
	fmt.Println()
	fmt.Println("==========================================")
	fmt.Printf("  %s\n", text)
	fmt.Println("==========================================")
}

func promptString(prompt string) string {
	fmt.Print(prompt)
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		return strings.TrimSpace(scanner.Text())
	}
	return ""
}

func promptInt(prompt string, min, max int) int {
	for {
		s := promptString(prompt)
		n, err := strconv.Atoi(s)
		if err == nil && n >= min && n <= max {
			return n
		}
		fmt.Println("Invalid selection.")
	}
}

func promptYesNo(prompt string) bool {
	s := promptString(prompt)
	return strings.EqualFold(s, "y")
}
