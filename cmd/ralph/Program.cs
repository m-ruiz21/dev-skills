using System.CommandLine;
using System.Diagnostics;
using System.Text;

var iterationsArg = new Argument<int>("iterations", "Number of development iterations to run");
var rootCommand = new RootCommand("Automated PRD-driven development workflow. Discovers PRDs under .scratch/<feature>/PRD.md and runs triage → develop → review loops using Copilot CLI.")
{
    iterationsArg
};

rootCommand.SetHandler((int iterations) =>
{
    if (iterations < 1)
    {
        Console.Error.WriteLine("iterations must be a positive integer");
        return;
    }

    var prds = DiscoverPRDs();
    if (prds.Count == 0)
    {
        Console.Error.WriteLine("no PRDs found under .scratch/*/PRD.md");
        return;
    }

    var (selectedPrd, selectedFeature) = SelectPRD(prds);

    for (int i = 1; i <= iterations; i++)
    {
        RunIteration(i, selectedPrd, selectedFeature);

        if (i < iterations)
        {
            if (!PromptYesNo("Continue to next task? (y/n): "))
            {
                Console.WriteLine($"Stopping after {i} iterations.");
                return;
            }
        }
    }

    Console.WriteLine($"Completed all {iterations} iterations.");
}, iterationsArg);

return rootCommand.Invoke(args);

// --- Functions ---

static List<string> DiscoverPRDs()
{
    var pattern = Path.Combine(".scratch", "*", "PRD.md");
    var baseDir = Directory.GetCurrentDirectory();
    var scratchDir = Path.Combine(baseDir, ".scratch");

    if (!Directory.Exists(scratchDir))
        return new List<string>();

    var matches = Directory.GetDirectories(scratchDir)
        .Select(d => Path.Combine(d, "PRD.md"))
        .Where(File.Exists)
        .Select(p => Path.GetRelativePath(baseDir, p))
        .OrderBy(p => p)
        .ToList();

    return matches;
}

static (string prdPath, string featureName) SelectPRD(List<string> prds)
{
    Console.WriteLine();
    Console.WriteLine("==========================================");
    Console.WriteLine("  SELECT A PRD TO WORK ON");
    Console.WriteLine("==========================================");
    Console.WriteLine();

    for (int i = 0; i < prds.Count; i++)
    {
        Console.WriteLine($"  {i + 1}) {ExtractFeatureName(prds[i])}");
    }

    Console.WriteLine();
    int choice = PromptInt($"Choose a PRD [1-{prds.Count}]: ", 1, prds.Count);
    string prdPath = prds[choice - 1];
    string featureName = ExtractFeatureName(prdPath);

    Console.WriteLine();
    Console.WriteLine($"Selected: {featureName} ({prdPath})");
    Console.WriteLine();
    return (prdPath, featureName);
}

static void RunIteration(int i, string selectedPrd, string selectedFeature)
{
    // Step 1: Triage
    PrintBanner($"ITERATION {i} — TRIAGING");
    RunCopilot("--allow-tool", "read", "--allow-tool", "write",
        "-p", "/triage Review and triage remaining issues. Reprioritize based on completed work and add agent brief to issues that are now ready-for-agent.");

    // Step 2: Develop
    PrintBanner($"ITERATION {i} — WRITING CODE");
    var developPrompt = $"/develop-task Focus on the PRD at {selectedPrd} for feature: {selectedFeature}. " +
        "1. Find the highest-priority task from this PRD and implement it. " +
        "2. Run your tests and type checks. " +
        "3. Update the PRD with what was done. " +
        "4. Append your progress to progress.txt. " +
        "5. Stage your changes. " +
        "WORK ON ONLY ONE TASK AT A TIME";
    RunCopilot("--yolo", "-p", developPrompt);

    // Step 3: Review-fix loop
    while (true)
    {
        PrintBanner($"ITERATION {i} — REVIEWING");

        var reviewPrompt = $"/review-diff Review staged code changes. " +
            $"Also check the review doc under .scratch/{selectedFeature}/reviews/ for any " +
            "unaddressed comments tagged with [agent] under '# User Feedback'. " +
            "If every finding is resolved and there are no unaddressed [agent] comments, " +
            "output <promise>COMPLETE</promise>. Otherwise, provide a summary of remaining issues.";

        var output = RunCopilotCapture("--allow-tool", "write", "-p", reviewPrompt);
        Console.WriteLine(output);

        var reviewDoc = FindReviewDoc(selectedFeature);

        if (!output.Contains("<promise>COMPLETE</promise>"))
        {
            Console.WriteLine();
            Console.WriteLine("Review has unresolved findings. Code agent addressing...");
            var rdPath = !string.IsNullOrEmpty(reviewDoc) ? reviewDoc : $".scratch/{selectedFeature}/reviews/";
            var fixPrompt = $"/develop-task Focus on the PRD at {selectedPrd} for feature: {selectedFeature}. " +
                $"The review document at {rdPath} contains unresolved findings and/or user feedback tagged with [agent]. " +
                "Address every unresolved item. " +
                "1. Read the review doc and fix each issue. " +
                "2. Run your tests and type checks. " +
                "3. Update the PRD with what was done. " +
                "4. Append your progress to progress.txt. " +
                "5. Stage your changes.";
            RunCopilot("--yolo", "-p", fixPrompt);
            continue;
        }

        // Review complete — show summary
        PrintBanner($"ITERATION {i} — WORK SUMMARY");
        Console.WriteLine();
        Console.WriteLine("--- Staged changes ---");
        RunGit("diff", "--cached", "--stat");
        Console.WriteLine();
        Console.WriteLine("--- Detailed diff ---");
        RunGit("diff", "--cached");
        Console.WriteLine();
        if (!string.IsNullOrEmpty(reviewDoc))
            Console.WriteLine($"Review doc: {reviewDoc}");

        Console.WriteLine();
        var comments = PromptString("Any comments on this work? (enter comments, or press Enter to skip): ");

        if (string.IsNullOrEmpty(comments))
            break;

        // Append comment to review doc
        if (!string.IsNullOrEmpty(reviewDoc))
        {
            File.AppendAllText(reviewDoc, $"\n- [agent] {DateTime.Now:yyyy-MM-dd HH:mm} — {comments}");
            Console.WriteLine($"Comment appended to {reviewDoc}");
        }
        else
        {
            Console.WriteLine("Warning: No review document found.");
        }

        Console.WriteLine();
        Console.WriteLine("Code agent picking up your feedback...");
        var fbRdPath = !string.IsNullOrEmpty(reviewDoc) ? reviewDoc : $".scratch/{selectedFeature}/reviews/";
        var fbPrompt = $"/develop-task Focus on the PRD at {selectedPrd} for feature: {selectedFeature}. " +
            $"The review document at {fbRdPath} has new user feedback tagged with [agent] under '# User Feedback'. " +
            "Address every [agent] comment. " +
            "1. Read the review doc and fix each issue. " +
            "2. Run your tests and type checks. " +
            "3. Update the PRD with what was done. " +
            "4. Append your progress to progress.txt. " +
            "5. Stage your changes.";
        RunCopilot("--yolo", "-p", fbPrompt);
    }

    // Move completed issue to closed folder
    var finalReviewDoc = FindReviewDoc(selectedFeature);
    if (!string.IsNullOrEmpty(finalReviewDoc))
        MoveCompletedIssue(finalReviewDoc);

    // Clean up progress.txt
    if (File.Exists("progress.txt"))
        File.Delete("progress.txt");
}

// --- Helpers ---

static string ExtractFeatureName(string prdPath)
{
    var parts = prdPath.Replace('\\', '/').Split('/');
    if (parts.Length >= 3)
        return parts[1];
    return prdPath;
}

static string? FindReviewDoc(string feature)
{
    var dir = Path.Combine(".scratch", feature, "reviews");
    if (!Directory.Exists(dir))
        return null;

    return Directory.GetFiles(dir, "*.md")
        .Select(f => new FileInfo(f))
        .OrderByDescending(f => f.LastWriteTime)
        .FirstOrDefault()?.FullName;
}

static void MoveCompletedIssue(string reviewDoc)
{
    if (!File.Exists(reviewDoc)) return;

    foreach (var line in File.ReadLines(reviewDoc))
    {
        if (line.StartsWith("issue:"))
        {
            var issuePath = line["issue:".Length..].Trim();
            if (string.IsNullOrEmpty(issuePath) || !File.Exists(issuePath))
                return;

            var closedDir = Path.Combine(Path.GetDirectoryName(issuePath)!, "closed");
            Directory.CreateDirectory(closedDir);
            var dest = Path.Combine(closedDir, Path.GetFileName(issuePath));
            File.Move(issuePath, dest);
            Console.WriteLine($"Issue moved to {dest}");
            return;
        }
    }
}

static void RunCopilot(params string[] arguments)
{
    var psi = new ProcessStartInfo("copilot")
    {
        UseShellExecute = false
    };
    foreach (var arg in arguments)
        psi.ArgumentList.Add(arg);

    using var proc = Process.Start(psi)!;
    proc.WaitForExit();
    if (proc.ExitCode != 0)
        throw new Exception($"copilot exited with code {proc.ExitCode}");
}

static string RunCopilotCapture(params string[] arguments)
{
    var psi = new ProcessStartInfo("copilot")
    {
        UseShellExecute = false,
        RedirectStandardOutput = true
    };
    foreach (var arg in arguments)
        psi.ArgumentList.Add(arg);

    using var proc = Process.Start(psi)!;
    var output = proc.StandardOutput.ReadToEnd();
    proc.WaitForExit();
    if (proc.ExitCode != 0)
        throw new Exception($"copilot exited with code {proc.ExitCode}");
    return output;
}

static void RunGit(params string[] arguments)
{
    var psi = new ProcessStartInfo("git")
    {
        UseShellExecute = false
    };
    psi.ArgumentList.Add("--no-pager");
    foreach (var arg in arguments)
        psi.ArgumentList.Add(arg);

    using var proc = Process.Start(psi)!;
    proc.WaitForExit();
}

static void PrintBanner(string text)
{
    Console.WriteLine();
    Console.WriteLine("==========================================");
    Console.WriteLine($"  {text}");
    Console.WriteLine("==========================================");
}

static string PromptString(string prompt)
{
    Console.Write(prompt);
    return Console.ReadLine()?.Trim() ?? "";
}

static int PromptInt(string prompt, int min, int max)
{
    while (true)
    {
        var s = PromptString(prompt);
        if (int.TryParse(s, out int n) && n >= min && n <= max)
            return n;
        Console.WriteLine("Invalid selection.");
    }
}

static bool PromptYesNo(string prompt)
{
    var s = PromptString(prompt);
    return s.Equals("y", StringComparison.OrdinalIgnoreCase);
}
