package acceptance

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/InfernalRabbit/cenvkit/internal/provenance"
)

// Count the contract checks below (including table cases): expansion 2,
// discovery 2, override gap 1, missing overlays 2, empty chain 3, runtime 2,
// conditional/profile gaps 3, expansion flags 2, color flags 2, PATH 1, launcher 2.
const reviewRegressionAssertions = 22

func reviewFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func reviewCLI(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	c := exec.Command(cenvkitBin, args...)
	c.Dir = dir
	c.Env = envWithout("COMPOSE_FILE", "COMPOSE_ENV_FILES", "COMPOSE_DISABLE_ENV_FILE", "COMPOSE_PROFILES",
		"COMPOSE_PATH_SEPARATOR", "CENVKIT_ENV", "CK_BASE", "CK_VALUE", "CK_RUNTIME", "CK_LOCAL", "CK_COPY", "CK_SET")
	var out, stderr strings.Builder
	c.Stdout, c.Stderr = &out, &stderr
	err := c.Run()
	if err != nil {
		t.Logf("%v: %v\n%s", args, err, stderr.String())
	}
	return out.String(), err
}

func reviewDebugEnv(t *testing.T, dir string, args ...string) map[string]map[string]string {
	t.Helper()
	out, err := reviewCLI(t, dir, append([]string{"env-debug", "--effective", "--json"}, args...)...)
	if err != nil {
		t.Fatalf("env-debug: %v", err)
	}
	var rep provenance.Report
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatal(err)
	}
	services := map[string]map[string]string{}
	for _, service := range rep.Services {
		env := map[string]string{}
		for _, e := range service.Entries {
			env[e.Key] = e.Value
		}
		services[service.Service] = env
	}
	return services
}

func reviewComposeEnv(t *testing.T, dir string, args ...string) map[string]map[string]string {
	t.Helper()
	if !dockerAvailable() {
		t.Skip("Docker Compose CLI unavailable or SMOKE_SKIP_DOCKER=1")
	}
	out, err := reviewCLI(t, dir, append([]string{"compose", "config", "--format", "json"}, args...)...)
	if err != nil {
		t.Fatalf("compose config: %v", err)
	}
	var model struct {
		Services map[string]struct {
			Environment map[string]string `json:"environment"`
		} `json:"services"`
	}
	if err := json.Unmarshal([]byte(out), &model); err != nil {
		t.Fatal(err)
	}
	services := map[string]map[string]string{}
	for name, service := range model.Services {
		if service.Environment == nil {
			service.Environment = map[string]string{}
		}
		// Compose escapes dollar signs when serializing a resolved model so the
		// output can itself be loaded without interpreting literal dollars again.
		for key, value := range service.Environment {
			service.Environment[key] = strings.ReplaceAll(value, "$$", "$")
		}
		services[name] = service.Environment
	}
	return services
}

func TestReview_ChainExpansionParity(t *testing.T) {
	dir := reviewFixture(t, map[string]string{
		".env":         "CK_BASE=hello\nCK_VALUE=${CK_BASE}/world\n",
		"compose.yaml": "services:\n  app:\n    image: busybox\n    environment:\n      CK_VALUE: ${CK_VALUE}\n",
	})
	debug := reviewDebugEnv(t, dir)
	out, err := reviewCLI(t, dir, "env", "--format", "json")
	if err != nil {
		t.Fatal(err)
	}
	var chain map[string]string
	if err := json.Unmarshal([]byte(out), &chain); err != nil {
		t.Fatal(err)
	}
	if chain["CK_VALUE"] != "hello/world" || debug["app"]["CK_VALUE"] != chain["CK_VALUE"] {
		t.Fatalf("env/debug expansion mismatch: chain=%v debug=%v", chain, debug)
	}
	if compose := reviewComposeEnv(t, dir); !reflect.DeepEqual(debug, compose) {
		t.Fatalf("debug=%v compose=%v", debug, compose)
	}
}

func TestReview_DefaultConfigParity(t *testing.T) {
	dir := reviewFixture(t, map[string]string{
		"compose.yaml":          "services:\n  app: {image: busybox, environment: {WINNER: yaml}}\n",
		"compose.yml":           "services:\n  app: {image: busybox, environment: {WINNER: yml}}\n",
		"compose.override.yaml": "services:\n  app:\n    environment: {OVERLAY: present}\n",
	})
	debug := reviewDebugEnv(t, dir)
	want := map[string]map[string]string{"app": {"WINNER": "yaml", "OVERLAY": "present"}}
	if !reflect.DeepEqual(debug, want) {
		t.Fatalf("wrong default selection: %v", debug)
	}
	if compose := reviewComposeEnv(t, dir); !reflect.DeepEqual(debug, compose) {
		t.Fatalf("debug=%v compose=%v", debug, compose)
	}
}

func TestReview_OverrideGap(t *testing.T) {
	dir := reviewFixture(t, map[string]string{
		"compose.yaml":          "services:\n  app: {image: busybox, env_file: service.env}\n",
		"compose.override.yaml": "services:\n  app:\n    ports: ['${CK_RUNTIME:-7000}:80']\n",
		"service.env":           "CK_RUNTIME=8000\n",
	})
	out, err := reviewCLI(t, dir, "gap-report", "--json")
	var gr provenance.GapReport
	if jsonErr := json.Unmarshal([]byte(out), &gr); jsonErr != nil {
		t.Fatal(jsonErr)
	}
	if gapExitCode(err) != 1 || gr.Count != 1 || gr.Gaps[0].Field != "ports[0]" {
		t.Fatalf("override gap not found: exit=%d report=%+v", gapExitCode(err), gr)
	}
}

func TestReview_MissingComposeOverlay(t *testing.T) {
	dir := reviewFixture(t, map[string]string{
		".env":         "COMPOSE_FILE=compose.yaml" + string(os.PathListSeparator) + "missing.yaml\n",
		"compose.yaml": "services:\n  app: {image: busybox}\n",
	})
	for _, command := range []string{"env-debug", "gap-report"} {
		t.Run(command, func(t *testing.T) {
			out, err := reviewCLI(t, dir, command, "--json")
			if err == nil || strings.Contains(out, `"count": 0`) {
				t.Fatalf("missing overlay silently ignored: %v %s", err, out)
			}
		})
	}
}

func TestReview_EmptyNamedChainParity(t *testing.T) {
	dir := reviewFixture(t, map[string]string{
		".env":              "CK_VALUE=root\n",
		".cenvkit.envchain": "[empty]\n",
		"compose.yaml":      "services:\n  app:\n    image: busybox\n    environment: {CK_VALUE: '${CK_VALUE:-fallback}'}\n",
	})
	debug := reviewDebugEnv(t, dir, "--chain", "empty")
	if debug["app"]["CK_VALUE"] != "fallback" {
		t.Fatalf("debug inherited excluded .env: %v", debug)
	}
	if compose := reviewComposeEnv(t, dir, "--chain", "empty"); !reflect.DeepEqual(debug, compose) {
		t.Fatalf("empty chain mismatch: debug=%v compose=%v", debug, compose)
	}
	if out, err := reviewCLI(t, dir, "validate", "--chain", "empty"); err != nil {
		t.Fatalf("empty-chain validate: %v %s", err, out)
	}
}

func TestReview_RuntimeEnvironmentParity(t *testing.T) {
	dir := reviewFixture(t, map[string]string{
		".env": "CK_BASE=project\n",
		"compose.yaml": `services:
  app:
    image: busybox
    env_file: [first.env, second.env]
    environment: {CK_INLINE: inline}
  other:
    image: busybox
    env_file: second.env
  raw:
    image: busybox
    env_file:
      - path: raw.env
        format: raw
`,
		"first.env":  "CK_LOCAL=first\n",
		"second.env": "CK_COPY=${CK_LOCAL:-missing}\nCK_REF=${CK_INLINE:-missing}\n",
		"raw.env":    "CK_RAW=${CK_BASE}\nCK_QUOTED=\"literal\"\nCK_HASH=value # literal\n",
	})
	debug := reviewDebugEnv(t, dir)
	if debug["app"]["CK_COPY"] != "first" || debug["app"]["CK_REF"] != "inline" || debug["other"]["CK_COPY"] != "missing" || debug["raw"]["CK_RAW"] != "${CK_BASE}" {
		t.Fatalf("wrong runtime environment: %v", debug)
	}
	if compose := reviewComposeEnv(t, dir); !reflect.DeepEqual(debug, compose) {
		t.Fatalf("debug=%v compose=%v", debug, compose)
	}
}

func TestReview_ConditionalAndProfileGaps(t *testing.T) {
	for _, tc := range []struct {
		name, chain, profile, expression string
		gap                              bool
	}{
		{"inactive profile", "", "extra", "${CK_RUNTIME:-fallback}", false},
		{"unused default", "CK_SET=chosen\n", "", "${CK_SET:-${CK_RUNTIME:-fallback}}", false},
		{"used default", "", "", "${CK_SET:-${CK_RUNTIME:-fallback}}", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			profile := ""
			if tc.profile != "" {
				profile = "    profiles: [" + tc.profile + "]\n"
			}
			dir := reviewFixture(t, map[string]string{
				".env":         tc.chain,
				"service.env":  "CK_RUNTIME=runtime\n",
				"compose.yaml": "services:\n  active: {image: busybox, env_file: service.env}\n  consumer:\n    image: busybox\n" + profile + "    environment:\n      CK_VALUE: '" + tc.expression + "'\n",
			})
			out, err := reviewCLI(t, dir, "gap-report", "--json")
			var gr provenance.GapReport
			if jsonErr := json.Unmarshal([]byte(out), &gr); jsonErr != nil {
				t.Fatal(jsonErr)
			}
			if (gr.Count > 0) != tc.gap || gapExitCode(err) != gr.Count {
				t.Fatalf("unexpected gap result: exit=%d report=%+v", gapExitCode(err), gr)
			}
		})
	}
}

func TestReview_ExpandFalse(t *testing.T) {
	dir := reviewFixture(t, map[string]string{".env": "CK_VALUE=${CK_BASE:-expanded}\n"})
	for _, args := range [][]string{
		{"env", "--expand=false", "--format", "json"},
		{"run", "--expand=false", "--print", "--", "unused"},
	} {
		out, err := reviewCLI(t, dir, args...)
		if err != nil || !strings.Contains(out, "${CK_BASE:-expanded}") {
			t.Fatalf("--expand=false ignored: args=%v err=%v out=%s", args, err, out)
		}
	}
}

func TestReview_ComposeColorFlag(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("Docker Compose CLI unavailable or SMOKE_SKIP_DOCKER=1")
	}
	dir := reviewFixture(t, map[string]string{"compose.yaml": "services:\n  app: {image: busybox}\n"})
	for _, args := range [][]string{
		{"compose", "--color=never", "config", "-q"},
		{"--color", "never", "compose", "config", "-q"},
	} {
		if out, err := reviewCLI(t, dir, args...); err != nil {
			t.Fatalf("wrapper color leaked to Docker: %v %s", err, out)
		}
	}
}

func TestReview_RunChainPATH(t *testing.T) {
	dir := reviewFixture(t, map[string]string{})
	binDir := filepath.Join(dir, "bin")
	if err := os.Mkdir(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(binDir, "review-unique-child")
	if err := os.WriteFile(child, []byte("#!/bin/sh\nexit 7\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("PATH="+binDir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := exec.Command(cenvkitBin, "run", "--", "review-unique-child")
	c.Dir, c.Env = dir, envWithout("PATH", "CENVKIT_ENV")
	out, err := c.CombinedOutput()
	if gapExitCode(err) != 7 {
		t.Fatalf("chain PATH not used or child exit lost: exit=%d out=%s", gapExitCode(err), out)
	}
}

func TestReview_SourceLauncherContext(t *testing.T) {
	launcher, err := filepath.Abs("../cenvkit")
	if err != nil {
		t.Fatal(err)
	}
	for _, module := range []bool{false, true} {
		dir := reviewFixture(t, map[string]string{".env": "CK_VALUE=caller\n"})
		if module {
			if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/unrelated\n\ngo 1.26.3\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		c := exec.Command("sh", launcher, "env", "--format", "json")
		c.Dir, c.Env = dir, envWithout("CK_VALUE", "CENVKIT_ENV")
		out, err := c.CombinedOutput()
		if err != nil || !strings.Contains(string(out), `"CK_VALUE": "caller"`) {
			t.Fatalf("module=%v: launcher lost module context/caller cwd: %v %s", module, err, out)
		}
	}
}
