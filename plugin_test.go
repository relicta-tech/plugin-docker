// Package main provides tests for the Docker plugin.
package main

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/relicta-tech/relicta-plugin-sdk/plugin"
)

// MockCommandExecutor is a mock implementation of CommandExecutor for testing.
type MockCommandExecutor struct {
	RunFunc      func(ctx context.Context, name string, args []string, stdin io.Reader) error
	RunCalls     []MockRunCall
	FailOnCall   int  // Which call number should fail (1-indexed, 0 means never fail)
	callCount    int
	FailWithErr  error
}

// MockRunCall records a call to Run.
type MockRunCall struct {
	Name  string
	Args  []string
	Stdin string
}

// Run implements CommandExecutor.
func (m *MockCommandExecutor) Run(ctx context.Context, name string, args []string, stdin io.Reader) error {
	m.callCount++

	var stdinStr string
	if stdin != nil {
		data, _ := io.ReadAll(stdin)
		stdinStr = string(data)
	}

	m.RunCalls = append(m.RunCalls, MockRunCall{
		Name:  name,
		Args:  args,
		Stdin: stdinStr,
	})

	if m.FailOnCall > 0 && m.callCount == m.FailOnCall {
		if m.FailWithErr != nil {
			return m.FailWithErr
		}
		return errors.New("mock error")
	}

	if m.RunFunc != nil {
		return m.RunFunc(ctx, name, args, stdin)
	}

	return nil
}

func TestGetInfo(t *testing.T) {
	p := &DockerPlugin{}
	info := p.GetInfo()

	if info.Name != "docker" {
		t.Errorf("expected name 'docker', got '%s'", info.Name)
	}

	if info.Version == "" {
		t.Error("expected non-empty version")
	}

	if info.Description == "" {
		t.Error("expected non-empty description")
	}

	if info.Author == "" {
		t.Error("expected non-empty author")
	}

	// Check hooks
	if len(info.Hooks) == 0 {
		t.Error("expected at least one hook")
	}

	hasPostPublish := false
	for _, hook := range info.Hooks {
		if hook == plugin.HookPostPublish {
			hasPostPublish = true
			break
		}
	}
	if !hasPostPublish {
		t.Error("expected PostPublish hook")
	}

	// Check config schema is valid JSON
	if info.ConfigSchema == "" {
		t.Error("expected non-empty config schema")
	}
}

func TestValidate(t *testing.T) {
	p := &DockerPlugin{}
	ctx := context.Background()

	tests := []struct {
		name      string
		config    map[string]any
		wantValid bool
	}{
		{
			name:      "missing image",
			config:    map[string]any{},
			wantValid: false,
		},
		{
			name: "empty image string",
			config: map[string]any{
				"image": "",
			},
			wantValid: false,
		},
		{
			name: "valid config with image",
			config: map[string]any{
				"image": "myorg/myapp",
			},
			wantValid: true,
		},
		{
			name: "valid config with all options",
			config: map[string]any{
				"image":      "myorg/myapp",
				"registry":   "ghcr.io",
				"tags":       []any{"{{version}}", "latest"},
				"dockerfile": "Dockerfile.prod",
				"context":    "./app",
				"push":       true,
			},
			wantValid: true,
		},
		{
			name: "valid config with build args and labels",
			config: map[string]any{
				"image": "myorg/myapp",
				"build_args": map[string]any{
					"GO_VERSION": "1.22",
				},
				"labels": map[string]any{
					"org.opencontainers.image.source": "https://github.com/org/repo",
				},
			},
			wantValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := p.Validate(ctx, tt.config)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if resp.Valid != tt.wantValid {
				t.Errorf("expected valid=%v, got valid=%v, errors=%v", tt.wantValid, resp.Valid, resp.Errors)
			}
		})
	}
}

func TestParseConfig(t *testing.T) {
	p := &DockerPlugin{}

	tests := []struct {
		name     string
		config   map[string]any
		envVars  map[string]string
		expected Config
	}{
		{
			name:   "defaults",
			config: map[string]any{},
			expected: Config{
				Registry:   "docker.io",
				Image:      "",
				Dockerfile: "Dockerfile",
				Context:    ".",
				Push:       true,
				NoCache:    false,
			},
		},
		{
			name: "custom values",
			config: map[string]any{
				"registry":   "ghcr.io",
				"image":      "myorg/myapp",
				"dockerfile": "Dockerfile.prod",
				"context":    "./app",
				"push":       false,
				"no_cache":   true,
				"target":     "builder",
				"tags":       []any{"v1.0.0", "latest"},
				"platforms":  []any{"linux/amd64", "linux/arm64"},
			},
			expected: Config{
				Registry:   "ghcr.io",
				Image:      "myorg/myapp",
				Dockerfile: "Dockerfile.prod",
				Context:    "./app",
				Push:       false,
				NoCache:    true,
				Target:     "builder",
				Tags:       []string{"v1.0.0", "latest"},
				Platforms:  []string{"linux/amd64", "linux/arm64"},
			},
		},
		{
			name:   "env var fallback",
			config: map[string]any{},
			envVars: map[string]string{
				"DOCKER_USERNAME": "testuser",
				"DOCKER_PASSWORD": "testpass",
			},
			expected: Config{
				Registry:   "docker.io",
				Dockerfile: "Dockerfile",
				Context:    ".",
				Push:       true,
				Username:   "testuser",
				Password:   "testpass",
			},
		},
		{
			name: "with build args and labels",
			config: map[string]any{
				"image": "myapp",
				"build_args": map[string]any{
					"GO_VERSION": "1.22",
					"NODE_ENV":   "production",
				},
				"labels": map[string]any{
					"version": "1.0.0",
					"author":  "test",
				},
				"cache_from": []any{"myapp:latest", "myapp:cache"},
			},
			expected: Config{
				Registry:   "docker.io",
				Image:      "myapp",
				Dockerfile: "Dockerfile",
				Context:    ".",
				Push:       true,
				BuildArgs: map[string]string{
					"GO_VERSION": "1.22",
					"NODE_ENV":   "production",
				},
				Labels: map[string]string{
					"version": "1.0.0",
					"author":  "test",
				},
				CacheFrom: []string{"myapp:latest", "myapp:cache"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear any existing env vars
			_ = os.Unsetenv("DOCKER_USERNAME")
			_ = os.Unsetenv("DOCKER_PASSWORD")

			// Set env vars
			for k, v := range tt.envVars {
				_ = os.Setenv(k, v)
				defer func(key string) { _ = os.Unsetenv(key) }(k)
			}

			cfg := p.parseConfig(tt.config)

			if cfg.Registry != tt.expected.Registry {
				t.Errorf("registry: expected '%s', got '%s'", tt.expected.Registry, cfg.Registry)
			}
			if cfg.Image != tt.expected.Image {
				t.Errorf("image: expected '%s', got '%s'", tt.expected.Image, cfg.Image)
			}
			if cfg.Dockerfile != tt.expected.Dockerfile {
				t.Errorf("dockerfile: expected '%s', got '%s'", tt.expected.Dockerfile, cfg.Dockerfile)
			}
			if cfg.Context != tt.expected.Context {
				t.Errorf("context: expected '%s', got '%s'", tt.expected.Context, cfg.Context)
			}
			if cfg.Push != tt.expected.Push {
				t.Errorf("push: expected %v, got %v", tt.expected.Push, cfg.Push)
			}
			if cfg.NoCache != tt.expected.NoCache {
				t.Errorf("no_cache: expected %v, got %v", tt.expected.NoCache, cfg.NoCache)
			}
			if cfg.Target != tt.expected.Target {
				t.Errorf("target: expected '%s', got '%s'", tt.expected.Target, cfg.Target)
			}
			if tt.expected.Username != "" && cfg.Username != tt.expected.Username {
				t.Errorf("username: expected '%s', got '%s'", tt.expected.Username, cfg.Username)
			}
			if tt.expected.Password != "" && cfg.Password != tt.expected.Password {
				t.Errorf("password: expected '%s', got '%s'", tt.expected.Password, cfg.Password)
			}
		})
	}
}

func TestParseConfigBuildArgsAndLabels(t *testing.T) {
	p := &DockerPlugin{}

	config := map[string]any{
		"image": "test",
		"build_args": map[string]any{
			"ARG1": "value1",
			"ARG2": "value2",
		},
		"labels": map[string]any{
			"label1": "val1",
			"label2": "val2",
		},
	}

	cfg := p.parseConfig(config)

	if len(cfg.BuildArgs) != 2 {
		t.Errorf("expected 2 build args, got %d", len(cfg.BuildArgs))
	}
	if cfg.BuildArgs["ARG1"] != "value1" {
		t.Errorf("expected build arg ARG1=value1, got %s", cfg.BuildArgs["ARG1"])
	}

	if len(cfg.Labels) != 2 {
		t.Errorf("expected 2 labels, got %d", len(cfg.Labels))
	}
	if cfg.Labels["label1"] != "val1" {
		t.Errorf("expected label label1=val1, got %s", cfg.Labels["label1"])
	}
}

func TestExecuteDryRun(t *testing.T) {
	p := &DockerPlugin{}
	ctx := context.Background()

	tests := []struct {
		name         string
		config       map[string]any
		releaseCtx   plugin.ReleaseContext
		expectedTags []string
	}{
		{
			name: "basic execution",
			config: map[string]any{
				"image": "myorg/myapp",
			},
			releaseCtx: plugin.ReleaseContext{
				Version: "v1.2.3",
			},
			expectedTags: []string{"1.2.3", "latest"},
		},
		{
			name: "custom tags with templates",
			config: map[string]any{
				"image": "myorg/myapp",
				"tags":  []any{"{{version}}", "{{major}}.{{minor}}", "stable"},
			},
			releaseCtx: plugin.ReleaseContext{
				Version: "v2.5.1",
			},
			expectedTags: []string{"2.5.1", "2.5", "stable"},
		},
		{
			name: "custom registry",
			config: map[string]any{
				"image":    "myorg/myapp",
				"registry": "ghcr.io",
				"tags":     []any{"{{version}}"},
			},
			releaseCtx: plugin.ReleaseContext{
				Version: "v1.0.0",
			},
			expectedTags: []string{"1.0.0"},
		},
		{
			name: "patch tag template",
			config: map[string]any{
				"image": "myorg/myapp",
				"tags":  []any{"{{major}}.{{minor}}.{{patch}}"},
			},
			releaseCtx: plugin.ReleaseContext{
				Version: "v3.2.1",
			},
			expectedTags: []string{"3.2.1"},
		},
		{
			name: "version without v prefix",
			config: map[string]any{
				"image": "myorg/myapp",
				"tags":  []any{"{{version}}"},
			},
			releaseCtx: plugin.ReleaseContext{
				Version: "1.0.0",
			},
			expectedTags: []string{"1.0.0"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := plugin.ExecuteRequest{
				Hook:    plugin.HookPostPublish,
				Config:  tt.config,
				Context: tt.releaseCtx,
				DryRun:  true,
			}

			resp, err := p.Execute(ctx, req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !resp.Success {
				t.Errorf("expected success, got error: %s", resp.Error)
			}

			// Check outputs
			if resp.Outputs == nil {
				t.Fatal("expected outputs to be set")
			}

			tags, ok := resp.Outputs["tags"].([]string)
			if !ok {
				t.Fatal("expected tags in outputs")
			}

			if len(tags) != len(tt.expectedTags) {
				t.Errorf("expected %d tags, got %d", len(tt.expectedTags), len(tags))
			}

			for i, expected := range tt.expectedTags {
				if i < len(tags) && tags[i] != expected {
					t.Errorf("tag[%d]: expected '%s', got '%s'", i, expected, tags[i])
				}
			}
		})
	}
}

func TestExecuteUnhandledHook(t *testing.T) {
	p := &DockerPlugin{}
	ctx := context.Background()

	req := plugin.ExecuteRequest{
		Hook:   plugin.HookPreInit,
		Config: map[string]any{"image": "test"},
		DryRun: true,
	}

	resp, err := p.Execute(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !resp.Success {
		t.Error("expected success for unhandled hook")
	}

	if !strings.Contains(resp.Message, "not handled") {
		t.Errorf("expected message to mention hook not handled, got: %s", resp.Message)
	}
}

func TestGetStringMap(t *testing.T) {
	tests := []struct {
		name     string
		raw      map[string]any
		key      string
		expected map[string]string
	}{
		{
			name:     "empty map",
			raw:      map[string]any{},
			key:      "labels",
			expected: map[string]string{},
		},
		{
			name: "with values",
			raw: map[string]any{
				"labels": map[string]any{
					"version": "1.0.0",
					"author":  "test",
				},
			},
			key: "labels",
			expected: map[string]string{
				"version": "1.0.0",
				"author":  "test",
			},
		},
		{
			name: "missing key",
			raw: map[string]any{
				"other": "value",
			},
			key:      "labels",
			expected: map[string]string{},
		},
		{
			name: "key with non-map value",
			raw: map[string]any{
				"labels": "not a map",
			},
			key:      "labels",
			expected: map[string]string{},
		},
		{
			name: "map with non-string values",
			raw: map[string]any{
				"labels": map[string]any{
					"valid":   "string",
					"invalid": 123,
					"also":    true,
				},
			},
			key: "labels",
			expected: map[string]string{
				"valid": "string",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getStringMap(tt.raw, tt.key)

			if len(result) != len(tt.expected) {
				t.Errorf("expected %d entries, got %d", len(tt.expected), len(result))
			}

			for k, v := range tt.expected {
				if result[k] != v {
					t.Errorf("key '%s': expected '%s', got '%s'", k, v, result[k])
				}
			}
		})
	}
}

func TestBuildAndPushWithMock(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		config      map[string]any
		releaseCtx  plugin.ReleaseContext
		mockSetup   func(*MockCommandExecutor)
		wantSuccess bool
		wantError   string
	}{
		{
			name: "successful build and push",
			config: map[string]any{
				"image": "myorg/myapp",
				"push":  true,
			},
			releaseCtx: plugin.ReleaseContext{
				Version: "v1.0.0",
			},
			mockSetup:   func(m *MockCommandExecutor) {},
			wantSuccess: true,
		},
		{
			name: "build only without push",
			config: map[string]any{
				"image": "myorg/myapp",
				"push":  false,
			},
			releaseCtx: plugin.ReleaseContext{
				Version: "v1.0.0",
			},
			mockSetup:   func(m *MockCommandExecutor) {},
			wantSuccess: true,
		},
		{
			name: "with login credentials",
			config: map[string]any{
				"image":    "myorg/myapp",
				"username": "testuser",
				"password": "testpass",
				"push":     true,
			},
			releaseCtx: plugin.ReleaseContext{
				Version: "v1.0.0",
			},
			mockSetup:   func(m *MockCommandExecutor) {},
			wantSuccess: true,
		},
		{
			name: "login failure",
			config: map[string]any{
				"image":    "myorg/myapp",
				"username": "testuser",
				"password": "testpass",
			},
			releaseCtx: plugin.ReleaseContext{
				Version: "v1.0.0",
			},
			mockSetup: func(m *MockCommandExecutor) {
				m.FailOnCall = 1 // First call (login) fails
			},
			wantSuccess: false,
			wantError:   "failed to login to registry",
		},
		{
			name: "build failure",
			config: map[string]any{
				"image": "myorg/myapp",
			},
			releaseCtx: plugin.ReleaseContext{
				Version: "v1.0.0",
			},
			mockSetup: func(m *MockCommandExecutor) {
				m.FailOnCall = 1 // First call (build) fails
			},
			wantSuccess: false,
			wantError:   "failed to build image",
		},
		{
			name: "push failure",
			config: map[string]any{
				"image": "myorg/myapp",
				"push":  true,
				"tags":  []any{"v1.0.0"},
			},
			releaseCtx: plugin.ReleaseContext{
				Version: "v1.0.0",
			},
			mockSetup: func(m *MockCommandExecutor) {
				m.FailOnCall = 2 // Second call (push) fails
			},
			wantSuccess: false,
			wantError:   "failed to push image",
		},
		{
			name: "with custom registry",
			config: map[string]any{
				"image":    "myorg/myapp",
				"registry": "ghcr.io",
				"push":     true,
			},
			releaseCtx: plugin.ReleaseContext{
				Version: "v1.0.0",
			},
			mockSetup:   func(m *MockCommandExecutor) {},
			wantSuccess: true,
		},
		{
			name: "with all build options",
			config: map[string]any{
				"image":      "myorg/myapp",
				"dockerfile": "Dockerfile.prod",
				"context":    "./app",
				"build_args": map[string]any{
					"GO_VERSION": "1.22",
				},
				"platforms": []any{"linux/amd64", "linux/arm64"},
				"labels": map[string]any{
					"version": "1.0.0",
				},
				"cache_from": []any{"myorg/myapp:cache"},
				"no_cache":   true,
				"target":     "production",
				"push":       true,
			},
			releaseCtx: plugin.ReleaseContext{
				Version: "v1.0.0",
			},
			mockSetup:   func(m *MockCommandExecutor) {},
			wantSuccess: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockCommandExecutor{}
			tt.mockSetup(mock)

			p := &DockerPlugin{executor: mock}

			req := plugin.ExecuteRequest{
				Hook:    plugin.HookPostPublish,
				Config:  tt.config,
				Context: tt.releaseCtx,
				DryRun:  false,
			}

			resp, err := p.Execute(ctx, req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if resp.Success != tt.wantSuccess {
				t.Errorf("expected success=%v, got success=%v, error=%s", tt.wantSuccess, resp.Success, resp.Error)
			}

			if tt.wantError != "" && !strings.Contains(resp.Error, tt.wantError) {
				t.Errorf("expected error containing '%s', got '%s'", tt.wantError, resp.Error)
			}
		})
	}
}

func TestDockerLogin(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name           string
		cfg            *Config
		expectedArgs   []string
		expectedStdin  string
		expectedErr    bool
	}{
		{
			name: "login to default registry",
			cfg: &Config{
				Registry: "docker.io",
				Username: "user",
				Password: "pass",
			},
			expectedArgs:  []string{"login", "-u", "user", "--password-stdin"},
			expectedStdin: "pass",
		},
		{
			name: "login to empty registry defaults",
			cfg: &Config{
				Registry: "",
				Username: "user",
				Password: "pass",
			},
			expectedArgs:  []string{"login", "-u", "user", "--password-stdin"},
			expectedStdin: "pass",
		},
		{
			name: "login to custom registry",
			cfg: &Config{
				Registry: "ghcr.io",
				Username: "user",
				Password: "pass",
			},
			expectedArgs:  []string{"login", "ghcr.io", "-u", "user", "--password-stdin"},
			expectedStdin: "pass",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockCommandExecutor{}
			p := &DockerPlugin{executor: mock}

			err := p.dockerLogin(ctx, tt.cfg)
			if (err != nil) != tt.expectedErr {
				t.Fatalf("expected error=%v, got %v", tt.expectedErr, err)
			}

			if len(mock.RunCalls) != 1 {
				t.Fatalf("expected 1 call, got %d", len(mock.RunCalls))
			}

			call := mock.RunCalls[0]
			if call.Name != "docker" {
				t.Errorf("expected command 'docker', got '%s'", call.Name)
			}

			// Check args match
			if len(call.Args) != len(tt.expectedArgs) {
				t.Errorf("expected %d args, got %d: %v", len(tt.expectedArgs), len(call.Args), call.Args)
			} else {
				for i, arg := range tt.expectedArgs {
					if call.Args[i] != arg {
						t.Errorf("arg[%d]: expected '%s', got '%s'", i, arg, call.Args[i])
					}
				}
			}

			if call.Stdin != tt.expectedStdin {
				t.Errorf("expected stdin '%s', got '%s'", tt.expectedStdin, call.Stdin)
			}
		})
	}
}

func TestDockerBuild(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		cfg          *Config
		imageNames   []string
		releaseCtx   plugin.ReleaseContext
		checkArgs    func(t *testing.T, args []string)
	}{
		{
			name: "basic build",
			cfg: &Config{
				Dockerfile: "Dockerfile",
				Context:    ".",
			},
			imageNames: []string{"myapp:v1.0.0"},
			releaseCtx: plugin.ReleaseContext{
				Version: "v1.0.0",
			},
			checkArgs: func(t *testing.T, args []string) {
				if args[0] != "build" {
					t.Error("first arg should be 'build'")
				}
				if !containsArg(args, "-t", "myapp:v1.0.0") {
					t.Error("should contain -t myapp:v1.0.0")
				}
				if !containsArg(args, "-f", "Dockerfile") {
					t.Error("should contain -f Dockerfile")
				}
				if !containsArg(args, "--build-arg", "VERSION=v1.0.0") {
					t.Error("should contain --build-arg VERSION=v1.0.0")
				}
				if args[len(args)-1] != "." {
					t.Error("last arg should be build context '.'")
				}
			},
		},
		{
			name: "build with empty dockerfile uses default",
			cfg: &Config{
				Dockerfile: "",
				Context:    "",
			},
			imageNames: []string{"myapp:latest"},
			releaseCtx: plugin.ReleaseContext{Version: "v1.0.0"},
			checkArgs: func(t *testing.T, args []string) {
				if !containsArg(args, "-f", "Dockerfile") {
					t.Error("should default to Dockerfile")
				}
				if args[len(args)-1] != "." {
					t.Error("should default to . context")
				}
			},
		},
		{
			name: "build with all options",
			cfg: &Config{
				Dockerfile: "Dockerfile.prod",
				Context:    "./app",
				BuildArgs: map[string]string{
					"GO_VERSION": "1.22",
				},
				Platforms: []string{"linux/amd64", "linux/arm64"},
				Labels: map[string]string{
					"version": "1.0.0",
				},
				CacheFrom: []string{"myapp:cache"},
				NoCache:   true,
				Target:    "production",
			},
			imageNames: []string{"myapp:v1.0.0", "myapp:latest"},
			releaseCtx: plugin.ReleaseContext{
				Version: "v1.0.0",
			},
			checkArgs: func(t *testing.T, args []string) {
				if !containsArg(args, "-f", "Dockerfile.prod") {
					t.Error("should contain -f Dockerfile.prod")
				}
				if !containsArg(args, "--build-arg", "GO_VERSION=1.22") {
					t.Error("should contain build arg GO_VERSION=1.22")
				}
				if !containsArg(args, "--platform", "linux/amd64,linux/arm64") {
					t.Error("should contain platform arg")
				}
				if !containsArg(args, "--label", "version=1.0.0") {
					t.Error("should contain label arg")
				}
				if !containsArg(args, "--cache-from", "myapp:cache") {
					t.Error("should contain cache-from arg")
				}
				if !containsFlag(args, "--no-cache") {
					t.Error("should contain --no-cache flag")
				}
				if !containsArg(args, "--target", "production") {
					t.Error("should contain target arg")
				}
				if args[len(args)-1] != "./app" {
					t.Error("last arg should be build context './app'")
				}
			},
		},
		{
			name: "build with multiple tags",
			cfg: &Config{
				Dockerfile: "Dockerfile",
				Context:    ".",
			},
			imageNames: []string{"myapp:v1.0.0", "myapp:latest", "myapp:1"},
			releaseCtx: plugin.ReleaseContext{Version: "v1.0.0"},
			checkArgs: func(t *testing.T, args []string) {
				if !containsArg(args, "-t", "myapp:v1.0.0") {
					t.Error("should contain -t myapp:v1.0.0")
				}
				if !containsArg(args, "-t", "myapp:latest") {
					t.Error("should contain -t myapp:latest")
				}
				if !containsArg(args, "-t", "myapp:1") {
					t.Error("should contain -t myapp:1")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockCommandExecutor{}
			p := &DockerPlugin{executor: mock}

			err := p.dockerBuild(ctx, tt.cfg, tt.imageNames, tt.releaseCtx)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(mock.RunCalls) != 1 {
				t.Fatalf("expected 1 call, got %d", len(mock.RunCalls))
			}

			call := mock.RunCalls[0]
			if call.Name != "docker" {
				t.Errorf("expected command 'docker', got '%s'", call.Name)
			}

			tt.checkArgs(t, call.Args)
		})
	}
}

func TestDockerPush(t *testing.T) {
	ctx := context.Background()
	mock := &MockCommandExecutor{}
	p := &DockerPlugin{executor: mock}

	err := p.dockerPush(ctx, "myapp:v1.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.RunCalls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(mock.RunCalls))
	}

	call := mock.RunCalls[0]
	if call.Name != "docker" {
		t.Errorf("expected command 'docker', got '%s'", call.Name)
	}

	expectedArgs := []string{"push", "myapp:v1.0.0"}
	if len(call.Args) != len(expectedArgs) {
		t.Errorf("expected %d args, got %d", len(expectedArgs), len(call.Args))
	} else {
		for i, arg := range expectedArgs {
			if call.Args[i] != arg {
				t.Errorf("arg[%d]: expected '%s', got '%s'", i, arg, call.Args[i])
			}
		}
	}
}

func TestDockerPushError(t *testing.T) {
	ctx := context.Background()
	mock := &MockCommandExecutor{
		FailOnCall: 1,
	}
	p := &DockerPlugin{executor: mock}

	err := p.dockerPush(ctx, "myapp:v1.0.0")
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestGetExecutor(t *testing.T) {
	t.Run("returns injected executor", func(t *testing.T) {
		mock := &MockCommandExecutor{}
		p := &DockerPlugin{executor: mock}

		exec := p.getExecutor()
		if exec != mock {
			t.Error("expected mock executor to be returned")
		}
	})

	t.Run("returns default executor when nil", func(t *testing.T) {
		p := &DockerPlugin{}

		exec := p.getExecutor()
		if exec == nil {
			t.Error("expected non-nil executor")
		}
		if _, ok := exec.(*RealCommandExecutor); !ok {
			t.Error("expected RealCommandExecutor")
		}
	})
}

func TestBuildAndPushVersionParsing(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		version      string
		tags         []any
		expectedTags []string
	}{
		{
			name:         "full semver",
			version:      "v1.2.3",
			tags:         []any{"{{version}}", "{{major}}", "{{major}}.{{minor}}"},
			expectedTags: []string{"1.2.3", "1", "1.2"},
		},
		{
			name:         "semver without patch",
			version:      "v1.2",
			tags:         []any{"{{version}}", "{{major}}", "{{minor}}", "{{patch}}"},
			expectedTags: []string{"1.2", "1", "2", ""},
		},
		{
			name:         "major only",
			version:      "v1",
			tags:         []any{"{{version}}", "{{major}}", "{{minor}}", "{{patch}}"},
			expectedTags: []string{"1", "1", "", ""},
		},
		{
			name:         "empty version parts",
			version:      "v",
			tags:         []any{"{{version}}", "{{major}}"},
			expectedTags: []string{"", ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &DockerPlugin{}

			req := plugin.ExecuteRequest{
				Hook: plugin.HookPostPublish,
				Config: map[string]any{
					"image": "myapp",
					"tags":  tt.tags,
				},
				Context: plugin.ReleaseContext{
					Version: tt.version,
				},
				DryRun: true,
			}

			resp, err := p.Execute(ctx, req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			tags := resp.Outputs["tags"].([]string)
			for i, expected := range tt.expectedTags {
				if i < len(tags) && tags[i] != expected {
					t.Errorf("tag[%d]: expected '%s', got '%s'", i, expected, tags[i])
				}
			}
		})
	}
}

func TestBuildAndPushRegistryHandling(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name           string
		registry       string
		image          string
		expectedImages func([]MockRunCall) bool
	}{
		{
			name:     "docker.io registry uses image directly",
			registry: "docker.io",
			image:    "myorg/myapp",
			expectedImages: func(calls []MockRunCall) bool {
				for _, call := range calls {
					for i, arg := range call.Args {
						if arg == "-t" && i+1 < len(call.Args) {
							// Should NOT have docker.io prefix
							if strings.HasPrefix(call.Args[i+1], "docker.io/") {
								return false
							}
						}
					}
				}
				return true
			},
		},
		{
			name:     "custom registry prepends to image",
			registry: "ghcr.io",
			image:    "myorg/myapp",
			expectedImages: func(calls []MockRunCall) bool {
				for _, call := range calls {
					for i, arg := range call.Args {
						if arg == "-t" && i+1 < len(call.Args) {
							// Should have ghcr.io prefix
							if strings.HasPrefix(call.Args[i+1], "ghcr.io/") {
								return true
							}
						}
					}
				}
				return false
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockCommandExecutor{}
			p := &DockerPlugin{executor: mock}

			req := plugin.ExecuteRequest{
				Hook: plugin.HookPostPublish,
				Config: map[string]any{
					"image":    tt.image,
					"registry": tt.registry,
					"push":     false,
				},
				Context: plugin.ReleaseContext{
					Version: "v1.0.0",
				},
				DryRun: false,
			}

			_, err := p.Execute(ctx, req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !tt.expectedImages(mock.RunCalls) {
				t.Errorf("image handling not correct for registry %s", tt.registry)
			}
		})
	}
}

func TestMultiplePushCalls(t *testing.T) {
	ctx := context.Background()
	mock := &MockCommandExecutor{}
	p := &DockerPlugin{executor: mock}

	req := plugin.ExecuteRequest{
		Hook: plugin.HookPostPublish,
		Config: map[string]any{
			"image": "myapp",
			"tags":  []any{"v1.0.0", "latest", "stable"},
			"push":  true,
		},
		Context: plugin.ReleaseContext{
			Version: "v1.0.0",
		},
		DryRun: false,
	}

	resp, err := p.Execute(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !resp.Success {
		t.Errorf("expected success, got error: %s", resp.Error)
	}

	// Count push calls (should be 3: one for each tag)
	pushCount := 0
	for _, call := range mock.RunCalls {
		if len(call.Args) > 0 && call.Args[0] == "push" {
			pushCount++
		}
	}

	if pushCount != 3 {
		t.Errorf("expected 3 push calls, got %d", pushCount)
	}
}

func TestLoginWithCustomRegistry(t *testing.T) {
	ctx := context.Background()
	mock := &MockCommandExecutor{}
	p := &DockerPlugin{executor: mock}

	req := plugin.ExecuteRequest{
		Hook: plugin.HookPostPublish,
		Config: map[string]any{
			"image":    "myorg/myapp",
			"registry": "ghcr.io",
			"username": "user",
			"password": "pass",
			"push":     false,
		},
		Context: plugin.ReleaseContext{
			Version: "v1.0.0",
		},
		DryRun: false,
	}

	_, err := p.Execute(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// First call should be login
	if len(mock.RunCalls) < 1 {
		t.Fatal("expected at least one call")
	}

	loginCall := mock.RunCalls[0]
	if loginCall.Args[0] != "login" {
		t.Error("first call should be login")
	}

	// Should include registry in login args
	foundRegistry := false
	for _, arg := range loginCall.Args {
		if arg == "ghcr.io" {
			foundRegistry = true
			break
		}
	}
	if !foundRegistry {
		t.Error("login should include custom registry")
	}
}

// Helper functions for checking args
func containsArg(args []string, flag, value string) bool {
	for i, arg := range args {
		if arg == flag && i+1 < len(args) && args[i+1] == value {
			return true
		}
	}
	return false
}

func containsFlag(args []string, flag string) bool {
	for _, arg := range args {
		if arg == flag {
			return true
		}
	}
	return false
}
