// Deploy-artifact validation: YAML/structural checks over everything in
// cloud/deploy plus the cloud-ci workflow. Where helm/docker are absent (CI
// runners, this lane's node), the Go test is the gate — `helm template` and
// `docker compose config` re-verify the same files where those tools exist.
package deploy

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(".")
	require.NoError(t, err)
	// This file lives at cloud/deploy; the repo root is two levels up.
	return filepath.Dir(filepath.Dir(abs))
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err, "expected deploy artifact to exist: %s", path)
	return string(b)
}

func parseYAML(t *testing.T, path string) map[string]any {
	t.Helper()
	var doc map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(readFile(t, path)), &doc), "invalid YAML: %s", path)
	return doc
}

func TestRequiredArtifactsExist(t *testing.T) {
	root := repoRoot(t)
	for _, rel := range []string{
		"cloud/deploy/docker/Dockerfile.cloudcontrold",
		"cloud/deploy/docker/Dockerfile.cloudnoded",
		"cloud/deploy/compose/docker-compose.yml",
		"cloud/deploy/compose/.env.example",
		"cloud/deploy/helm/carbon-cloud/Chart.yaml",
		"cloud/deploy/helm/carbon-cloud/values.yaml",
		"cloud/deploy/helm/carbon-cloud/values.schema.json",
		"cloud/deploy/helm/carbon-cloud/templates/controld-deployment.yaml",
		"cloud/deploy/helm/carbon-cloud/templates/noded-deployment.yaml",
		"cloud/deploy/helm/carbon-cloud/templates/services.yaml",
		"cloud/deploy/helm/carbon-cloud/templates/ingress.yaml",
		"cloud/deploy/helm/carbon-cloud/templates/secret.yaml",
		"cloud/Makefile",
		".github/workflows/cloud-ci.yml",
		"cloud/docs/runbooks/control-plane-outage.md",
		"cloud/docs/runbooks/node-join-failure.md",
		"cloud/docs/runbooks/key-rotation.md",
		"cloud/docs/runbooks/provisioning.md",
		"cloud/docs/runbooks/incident-response.md",
		"cloud/docs/runbooks/billing-reconciliation.md",
		"cloud/docs/runbooks/disaster-recovery.md",
	} {
		require.FileExists(t, filepath.Join(root, rel), "missing deploy artifact")
	}
}

func TestDockerfilesAreCorrectByConstruction(t *testing.T) {
	root := repoRoot(t)
	cases := map[string]string{
		"cloud/deploy/docker/Dockerfile.cloudcontrold": "./cloud/cmd/cloudcontrold",
		"cloud/deploy/docker/Dockerfile.cloudnoded":    "./cloud/cmd/cloudnoded",
	}
	for rel, cmd := range cases {
		body := readFile(t, filepath.Join(root, rel))
		require.Contains(t, body, cmd, "%s must build the future main package %s", rel, cmd)
		require.Contains(t, body, "-trimpath", "%s must build with -trimpath", rel)
		require.Contains(t, body, "distroless", "%s must end on a distroless image", rel)
		require.Contains(t, body, "65532", "%s must run as non-root", rel)
		for _, secret := range []string{"CARBONCLOUD_", "TOKEN=", "PASSWORD=", "SECRET_KEY"} {
			require.NotContains(t, body, secret, "%s must not bake in secrets", rel)
		}
	}
}

func TestComposeFile(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, "cloud/deploy/compose/docker-compose.yml")
	doc := parseYAML(t, path)

	services, ok := doc["services"].(map[string]any)
	require.True(t, ok, "compose file must define services")
	for _, svc := range []string{"postgres", "controld", "noded", "console"} {
		require.Contains(t, services, svc, "compose service missing")
	}
	body := readFile(t, path)
	require.NotContains(t, body, "network_mode:", "compose must not use host networking")

	volumes, ok := doc["volumes"].(map[string]any)
	require.True(t, ok, "compose file must declare named volumes")
	require.Contains(t, volumes, "pgdata")

	envExample := readFile(t, filepath.Join(root, "cloud/deploy/compose/.env.example"))
	require.Contains(t, envExample, "changeme", ".env.example must use obvious fakes")
}

func TestHelmValuesReferenceSecretsIndirectly(t *testing.T) {
	root := repoRoot(t)
	chart := filepath.Join(root, "cloud/deploy/helm/carbon-cloud")
	values := parseYAML(t, filepath.Join(chart, "values.yaml"))
	global, ok := values["global"].(map[string]any)
	require.True(t, ok, "values.yaml must have a global block")
	require.NotEmpty(t, global["existingSecret"], "deployments must reference an external secret")

	for _, tpl := range []string{"controld-deployment.yaml", "noded-deployment.yaml"} {
		body := readFile(t, filepath.Join(chart, "templates", tpl))
		require.Contains(t, body, "secretKeyRef", "%s must use secret refs", tpl)
		require.NotContains(t, body, "password: ", "%s must not carry literal secrets", tpl)
	}
	// The optional placeholder Secret is gated off by default and carries
	// only obvious CHANGEME fakes.
	secretBody := readFile(t, filepath.Join(chart, "templates", "secret.yaml"))
	require.Contains(t, secretBody, ".Values.secrets.create", "placeholder secret must be opt-in")
	require.Contains(t, secretBody, "CHANGEME", "placeholder secret must use obvious fakes")
	require.FileExists(t, filepath.Join(chart, "values.schema.json"))
}

func TestCloudCIWorkflow(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, ".github/workflows/cloud-ci.yml")
	body := readFile(t, path)
	for _, want := range []string{
		"go build ./cloud/...",
		"go vet ./cloud/...",
		"go test ./cloud/...",
		"go build ./...",
		"bun install --frozen-lockfile",
		"bun run check",
		"bun run test",
		"bun run build",
		"cloud/web/cloud-console",
	} {
		require.Contains(t, body, want, "cloud-ci.yml must contain %q", want)
	}
	require.Contains(t, body, "find cloud", "gofmt must be find-based, not `gofmt ./cloud/...`")
	require.NotContains(t, body, "run: gofmt", "gofmt must be find-based, not `gofmt ./cloud/...`")

	var doc map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(body), &doc), "cloud-ci.yml must be valid YAML")
}

func TestNoLiteralSecretsInDeploy(t *testing.T) {
	root := repoRoot(t)
	// Flag only values shaped like real credential material: a known prefix
	// followed by substantial token characters. Documentation mentions such
	// as "sk_live_..." (ellipsis, no token chars) are allowed.
	credPattern := regexp.MustCompile(`sk_live_[A-Za-z0-9]{10,}|ghp_[A-Za-z0-9]{10,}|dckr_pat_[A-Za-z0-9_-]{10,}|BEGIN (RSA|EC) PRIVATE KEY`)
	var offenders []string
	err := filepath.Walk(filepath.Join(root, "cloud/deploy"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		// Skip binary files and this test itself.
		if strings.HasSuffix(path, ".png") || strings.HasSuffix(path, ".ico") || strings.HasSuffix(path, "deploy_test.go") {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		if credPattern.Match(data) {
			rel, _ := filepath.Rel(root, path)
			offenders = append(offenders, rel)
		}
		return nil
	})
	require.NoError(t, err)
	require.Empty(t, offenders, "literal credentials found in deploy tree: %v", offenders)
}
