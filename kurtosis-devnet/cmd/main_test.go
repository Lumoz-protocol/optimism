package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum-optimism/optimism/kurtosis-devnet/pkg/kurtosis"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFlags(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantCfg   *config
		wantError bool
	}{
		{
			name: "valid configuration",
			args: []string{
				"-template", "path/to/template.yaml",
				"-enclave", "test-enclave",
				"-local-hostname", "test.local",
			},
			wantCfg: &config{
				templateFile:    "path/to/template.yaml",
				enclave:         "test-enclave",
				localHostName:   "test.local",
				kurtosisPackage: kurtosis.DefaultPackageName,
			},
			wantError: false,
		},
		{
			name:      "missing required template",
			args:      []string{"-enclave", "test-enclave"},
			wantCfg:   nil,
			wantError: true,
		},
		{
			name: "with data file",
			args: []string{
				"-template", "path/to/template.yaml",
				"-data", "path/to/data.json",
			},
			wantCfg: &config{
				templateFile:    "path/to/template.yaml",
				dataFile:        "path/to/data.json",
				localHostName:   "host.docker.internal",
				enclave:         kurtosis.DefaultEnclave,
				kurtosisPackage: kurtosis.DefaultPackageName,
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			fs.SetOutput(&bytes.Buffer{}) // Suppress flag errors

			gotCfg, err := parseFlags(fs, tt.args)
			if tt.wantError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantCfg.templateFile, gotCfg.templateFile)
			assert.Equal(t, tt.wantCfg.enclave, gotCfg.enclave)
			assert.Equal(t, tt.wantCfg.localHostName, gotCfg.localHostName)
			assert.Equal(t, tt.wantCfg.kurtosisPackage, gotCfg.kurtosisPackage)
		})
	}
}

func TestLaunchStaticServer(t *testing.T) {
	cfg := &config{
		localHostName: "test.local",
	}

	ctx := context.Background()
	server, cleanup, err := launchStaticServer(ctx, cfg)
	require.NoError(t, err)
	defer cleanup()

	// Verify server properties
	assert.NotEmpty(t, server.dir)
	assert.DirExists(t, server.dir)
	assert.NotNil(t, server.Server)

	// Verify cleanup works
	cleanup()
	assert.NoDirExists(t, server.dir)
}

func TestRenderTemplate(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir, err := os.MkdirTemp("", "template-test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create a test template file
	templateContent := `
name: {{.name}}
image: {{localDockerImage "test-project"}}
artifacts: {{localContractArtifacts "l1"}}`

	templatePath := filepath.Join(tmpDir, "template.yaml")
	err = os.WriteFile(templatePath, []byte(templateContent), 0644)
	require.NoError(t, err)

	// Create a test data file
	dataContent := `{"name": "test-deployment"}`
	dataPath := filepath.Join(tmpDir, "data.json")
	err = os.WriteFile(dataPath, []byte(dataContent), 0644)
	require.NoError(t, err)

	cfg := &config{
		templateFile: templatePath,
		dataFile:     dataPath,
		enclave:      "test-enclave",
		dryRun:       true, // Important for tests
	}

	ctx := context.Background()
	server, cleanup, err := launchStaticServer(ctx, cfg)
	require.NoError(t, err)
	defer cleanup()

	buf, err := renderTemplate(cfg, server)
	require.NoError(t, err)

	// Verify template rendering
	assert.Contains(t, buf.String(), "test-deployment")
	assert.Contains(t, buf.String(), "test-project:test-enclave")
	assert.Contains(t, buf.String(), server.URL())
}

func TestDeploy(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create a temporary directory for the environment output
	tmpDir, err := os.MkdirTemp("", "deploy-test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	envPath := filepath.Join(tmpDir, "env.json")
	cfg := &config{
		environment: envPath,
		dryRun:      true,
	}

	// Create a simple deployment configuration
	deployConfig := bytes.NewBufferString(`{"test": "config"}`)

	err = deploy(ctx, cfg, deployConfig)
	require.NoError(t, err)

	// Verify the environment file was created
	assert.FileExists(t, envPath)

	// Read and verify the content
	content, err := os.ReadFile(envPath)
	require.NoError(t, err)

	var env map[string]interface{}
	err = json.Unmarshal(content, &env)
	require.NoError(t, err)
}

// TestMainFunc performs an integration test of the main function
func TestMainFunc(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir, err := os.MkdirTemp("", "main-test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create test template
	templatePath := filepath.Join(tmpDir, "template.yaml")
	err = os.WriteFile(templatePath, []byte("name: test"), 0644)
	require.NoError(t, err)

	// Create environment output path
	envPath := filepath.Join(tmpDir, "env.json")

	cfg := &config{
		templateFile: templatePath,
		environment:  envPath,
		dryRun:       true,
	}

	err = mainFunc(cfg)
	require.NoError(t, err)

	// Verify the environment file was created
	assert.FileExists(t, envPath)
}
