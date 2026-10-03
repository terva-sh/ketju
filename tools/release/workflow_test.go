package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The two release workflows, one per forge, the image and the README's
// "Publishing a release" describe one pipeline (TKT-01M3YSVEDJ,
// TKT-01M41WY0FE). These tests keep them in step with this tool and with each
// other; none of them runs a workflow, which only the maintainer's tag does.

const (
	forgejoWorkflow = ".forgejo/workflows/release.yml"
	githubWorkflow  = ".github/workflows/release.yml"
)

func readRoot(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(moduleRoot, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestReleaseWorkflowNamesItsSecrets: every secret and variable the Forgejo
// release workflow reads is in the README's table, and the table names
// nothing the workflow does not read. No name takes a prefix Forgejo
// reserves. The GitHub twin reads only the GITHUB_TOKEN Actions provides.
func TestReleaseWorkflowNamesItsSecrets(t *testing.T) {
	wf := readRoot(t, forgejoWorkflow)
	var used []string
	for _, m := range regexp.MustCompile(`\b(?:secrets|vars)\.([A-Z0-9_]+)`).FindAllStringSubmatch(wf, -1) {
		if !slices.Contains(used, m[1]) {
			used = append(used, m[1])
		}
	}
	slices.Sort(used)

	readme := readRoot(t, "README.md")
	_, section, ok := strings.Cut(readme, "### Publishing a release\n")
	if !ok {
		t.Fatal(`the README has no "### Publishing a release" section`)
	}
	if next := strings.Index(section, "\n## "); next >= 0 {
		section = section[:next]
	}
	var listed []string
	for _, m := range regexp.MustCompile("(?m)^\\| `([A-Z0-9_]+)` \\|").FindAllStringSubmatch(section, -1) {
		listed = append(listed, m[1])
	}
	slices.Sort(listed)

	if len(used) == 0 || !slices.Equal(used, listed) {
		t.Errorf("the workflow reads %v, and the README lists %v", used, listed)
	}
	for _, n := range used {
		for _, p := range []string{"GITHUB_", "GITEA_", "FORGEJO_"} {
			if strings.HasPrefix(n, p) {
				t.Errorf("%s takes the reserved prefix %s", n, p)
			}
		}
	}
	// The header comment names them too.
	for _, n := range used {
		if !strings.Contains(wf[:strings.Index(wf, "\nname:")], n) {
			t.Errorf("the workflow's header comment does not name %s", n)
		}
	}
}

// TestReleaseWorkflowUsesThisTool: both workflows build with this tool, and
// the image copies the file name this tool writes for each platform.
func TestReleaseWorkflowUsesThisTool(t *testing.T) {
	for _, f := range []string{forgejoWorkflow, githubWorkflow} {
		if !strings.Contains(readRoot(t, f), `go run ./tools/release --version "${TAG}"`) {
			t.Errorf("%s does not build with go run ./tools/release", f)
		}
	}
	wf := readRoot(t, forgejoWorkflow) + readRoot(t, githubWorkflow)
	dockerfile := readRoot(t, "Dockerfile")
	// The name run writes, with the Dockerfile's build arguments in place of
	// the version and architecture.
	want := fmt.Sprintf("COPY ketju_%s_%s_%s /ketju", "${VERSION}", "linux", "${TARGETARCH}")
	if !strings.Contains(dockerfile, want) {
		t.Errorf("the Dockerfile does not %q", want)
	}
	for _, p := range Platforms {
		if p.OS != "linux" {
			t.Errorf("platform %s: the image is linux only", p)
		}
		if !strings.Contains(wf, p.Arch) {
			t.Errorf("the workflow's image job does not build %s", p.Arch)
		}
	}
	for _, line := range []string{"FROM scratch", "USER 65532:65532", "VOLUME /data", "ENV KETJU_CONFIG=/data/config.toml",
		"/etc/ssl/certs/ca-certificates.crt"} {
		if !strings.Contains(dockerfile, line) {
			t.Errorf("the Dockerfile lacks %q", line)
		}
	}
}

// TestReleaseWorkflowPassesValuesThroughEnv: no run block in either
// workflow holds an expression, so a ref or a secret is data to the shell
// and never source.
func TestReleaseWorkflowPassesValuesThroughEnv(t *testing.T) {
	for _, f := range []string{forgejoWorkflow, githubWorkflow} {
		t.Run(f, func(t *testing.T) { checkRunBlocks(t, readRoot(t, f)) })
	}
}

func checkRunBlocks(t *testing.T, wf string) {
	t.Helper()
	lines := strings.Split(wf, "\n")
	runAt := regexp.MustCompile(`^(\s*)(?:- )?run: (.*)$`)
	for i := 0; i < len(lines); i++ {
		m := runAt.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		if strings.TrimSpace(m[2]) != "|" {
			if strings.Contains(m[2], "${{") {
				t.Errorf("line %d: an expression in a run line", i+1)
			}
			continue
		}
		indent := len(m[1])
		for j := i + 1; j < len(lines); j++ {
			l := lines[j]
			if strings.TrimSpace(l) != "" && len(l)-len(strings.TrimLeft(l, " ")) <= indent {
				break
			}
			if strings.Contains(l, "${{") {
				t.Errorf("line %d: an expression inside a run block", j+1)
			}
			if regexp.MustCompile(`^\s*set -[a-z]*x`).MatchString(l) {
				t.Errorf("line %d: set -x in a job that holds tokens", j+1)
			}
		}
	}
}

// TestReleaseWorkflowsSplitByForge: the organization's pattern. Every job in
// the GitHub twin runs only on github.com and reads no secret but
// GITHUB_TOKEN, and the twin alone pushes the image, to ghcr.io. The Forgejo
// workflow logs in to no registry and publishes nothing outside this forge.
func TestReleaseWorkflowsSplitByForge(t *testing.T) {
	gh := readRoot(t, githubWorkflow)
	_, jobs, ok := strings.Cut(gh, "\njobs:\n")
	if !ok {
		t.Fatalf("%s has no jobs", githubWorkflow)
	}
	jobAt := regexp.MustCompile(`(?m)^  ([a-z][a-z0-9-]*):\n`)
	starts := jobAt.FindAllStringSubmatchIndex(jobs, -1)
	if len(starts) == 0 {
		t.Fatalf("%s has no jobs", githubWorkflow)
	}
	for i, m := range starts {
		end := len(jobs)
		if i+1 < len(starts) {
			end = starts[i+1][0]
		}
		if !strings.Contains(jobs[m[0]:end], "\n    if: github.server_url == 'https://github.com'\n") {
			t.Errorf("%s: job %s is not guarded to github.com", githubWorkflow, jobs[m[2]:m[3]])
		}
	}
	for _, m := range regexp.MustCompile(`\b(?:secrets|vars)\.([A-Z0-9_]+)`).FindAllStringSubmatch(gh, -1) {
		if m[1] != "GITHUB_TOKEN" {
			t.Errorf("%s reads %s; it needs only GITHUB_TOKEN", githubWorkflow, m[1])
		}
	}
	if !strings.Contains(gh, "IMAGE=\"ghcr.io/${REPOSITORY}\"") {
		t.Errorf("%s does not push to ghcr.io", githubWorkflow)
	}

	fj := readRoot(t, forgejoWorkflow)
	for _, s := range []string{"buildah login", "buildah push", "manifest push", "api.github.com", "uploads.github.com"} {
		if strings.Contains(fj, s) {
			t.Errorf("%s holds %q; it publishes only its own release", forgejoWorkflow, s)
		}
	}
}
