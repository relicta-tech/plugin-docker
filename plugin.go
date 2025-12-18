// Package main implements the Docker Hub / Container registry plugin for Relicta.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/relicta-tech/relicta-plugin-sdk/helpers"
	"github.com/relicta-tech/relicta-plugin-sdk/plugin"
)

// Security validation patterns
var (
	// Docker image name pattern: [registry/]name[:tag]
	// Allows: alphanumerics, dots, dashes, underscores, forward slashes, colons
	imageNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._/-]*[a-zA-Z0-9]$`)

	// Tag pattern: alphanumerics, dots, dashes, underscores
	tagPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

	// Registry pattern: hostname with optional port
	registryPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.-]*(:[0-9]+)?$`)

	// Build arg key pattern: alphanumerics and underscores (environment variable style)
	buildArgKeyPattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

	// Label key pattern: OCI standard allows reverse-DNS style with dots, dashes
	// e.g., org.opencontainers.image.source, com.example.my-label
	labelKeyPattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9._-]*[a-zA-Z0-9]$`)
)

// validateImageName validates a Docker image name.
func validateImageName(name string) error {
	if name == "" {
		return fmt.Errorf("image name cannot be empty")
	}
	if len(name) > 256 {
		return fmt.Errorf("image name too long (max 256 characters)")
	}
	if !imageNamePattern.MatchString(name) {
		return fmt.Errorf("invalid image name: contains disallowed characters")
	}
	// Check for path traversal attempts
	if strings.Contains(name, "..") {
		return fmt.Errorf("image name cannot contain '..'")
	}
	return nil
}

// validateTag validates a Docker image tag.
func validateTag(tag string) error {
	if tag == "" {
		return fmt.Errorf("tag cannot be empty")
	}
	if len(tag) > 128 {
		return fmt.Errorf("tag too long (max 128 characters)")
	}
	if !tagPattern.MatchString(tag) {
		return fmt.Errorf("invalid tag: contains disallowed characters")
	}
	return nil
}

// validateRegistry validates a Docker registry URL.
func validateRegistry(registry string) error {
	if registry == "" || registry == "docker.io" {
		return nil
	}
	if len(registry) > 256 {
		return fmt.Errorf("registry URL too long")
	}
	if !registryPattern.MatchString(registry) {
		return fmt.Errorf("invalid registry URL format")
	}
	return nil
}

// validateBuildArgKey validates a build argument key.
func validateBuildArgKey(key string) error {
	if !buildArgKeyPattern.MatchString(key) {
		return fmt.Errorf("invalid build arg key: must be alphanumeric with underscores")
	}
	return nil
}

// validateLabelKey validates a Docker label key.
func validateLabelKey(key string) error {
	if key == "" {
		return fmt.Errorf("label key cannot be empty")
	}
	if len(key) > 256 {
		return fmt.Errorf("label key too long (max 256 characters)")
	}
	if !labelKeyPattern.MatchString(key) {
		return fmt.Errorf("invalid label key: must be alphanumeric with dots, dashes, or underscores")
	}
	return nil
}

// validatePath validates a file path to prevent path traversal.
func validatePath(path string) error {
	if path == "" {
		return nil
	}

	// Clean the path
	cleaned := filepath.Clean(path)

	// Check for absolute paths (potential escape from working directory)
	if filepath.IsAbs(cleaned) {
		return fmt.Errorf("absolute paths are not allowed")
	}

	// Check for path traversal attempts
	if strings.HasPrefix(cleaned, "..") || strings.Contains(cleaned, string(filepath.Separator)+"..") {
		return fmt.Errorf("path traversal detected: cannot use '..' to escape working directory")
	}

	return nil
}

// CommandExecutor abstracts command execution for testability.
type CommandExecutor interface {
	Run(ctx context.Context, name string, args []string, stdin io.Reader) error
}

// RealCommandExecutor executes actual system commands.
type RealCommandExecutor struct{}

// Run executes the command with the given arguments.
func (e *RealCommandExecutor) Run(ctx context.Context, name string, args []string, stdin io.Reader) error {
	cmd := exec.CommandContext(ctx, name, args...)
	if stdin != nil {
		cmd.Stdin = stdin
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// DockerPlugin implements the Docker container registry plugin.
type DockerPlugin struct {
	executor CommandExecutor
}

// getExecutor returns the command executor, defaulting to RealCommandExecutor.
func (p *DockerPlugin) getExecutor() CommandExecutor {
	if p.executor != nil {
		return p.executor
	}
	return &RealCommandExecutor{}
}

// Config represents the Docker plugin configuration.
type Config struct {
	Registry   string
	Image      string
	Tags       []string
	Dockerfile string
	Context    string
	BuildArgs  map[string]string
	Platforms  []string
	Username   string
	Password   string
	Push       bool
	Labels     map[string]string
	CacheFrom  []string
	NoCache    bool
	Target     string
}

// GetInfo returns plugin metadata.
func (p *DockerPlugin) GetInfo() plugin.Info {
	return plugin.Info{
		Name:        "docker",
		Version:     "2.0.0",
		Description: "Build and push Docker images to container registries",
		Author:      "Relicta Team",
		Hooks: []plugin.Hook{
			plugin.HookPostPublish,
		},
		ConfigSchema: `{
			"type": "object",
			"properties": {
				"registry": {"type": "string", "description": "Container registry URL", "default": "docker.io"},
				"image": {"type": "string", "description": "Image name (e.g., user/image)"},
				"tags": {"type": "array", "items": {"type": "string"}, "description": "Tags to apply (supports {{version}})"},
				"dockerfile": {"type": "string", "description": "Dockerfile path", "default": "Dockerfile"},
				"context": {"type": "string", "description": "Build context", "default": "."},
				"build_args": {"type": "object", "description": "Build arguments"},
				"platforms": {"type": "array", "items": {"type": "string"}, "description": "Target platforms"},
				"username": {"type": "string", "description": "Registry username (or use DOCKER_USERNAME env)"},
				"password": {"type": "string", "description": "Registry password (or use DOCKER_PASSWORD env)"},
				"push": {"type": "boolean", "description": "Push after building", "default": true},
				"labels": {"type": "object", "description": "Image labels"},
				"cache_from": {"type": "array", "items": {"type": "string"}, "description": "Cache source images"},
				"no_cache": {"type": "boolean", "description": "Disable build cache"},
				"target": {"type": "string", "description": "Target build stage"}
			},
			"required": ["image"]
		}`,
	}
}

// Execute runs the plugin for a given hook.
func (p *DockerPlugin) Execute(ctx context.Context, req plugin.ExecuteRequest) (*plugin.ExecuteResponse, error) {
	cfg := p.parseConfig(req.Config)

	switch req.Hook {
	case plugin.HookPostPublish:
		return p.buildAndPush(ctx, cfg, req.Context, req.DryRun)
	default:
		return &plugin.ExecuteResponse{
			Success: true,
			Message: fmt.Sprintf("Hook %s not handled", req.Hook),
		}, nil
	}
}

func (p *DockerPlugin) buildAndPush(ctx context.Context, cfg *Config, releaseCtx plugin.ReleaseContext, dryRun bool) (*plugin.ExecuteResponse, error) {
	// Security validation
	if err := validateImageName(cfg.Image); err != nil {
		return &plugin.ExecuteResponse{
			Success: false,
			Error:   fmt.Sprintf("invalid image configuration: %v", err),
		}, nil
	}

	if err := validateRegistry(cfg.Registry); err != nil {
		return &plugin.ExecuteResponse{
			Success: false,
			Error:   fmt.Sprintf("invalid registry configuration: %v", err),
		}, nil
	}

	if err := validatePath(cfg.Dockerfile); err != nil {
		return &plugin.ExecuteResponse{
			Success: false,
			Error:   fmt.Sprintf("invalid dockerfile path: %v", err),
		}, nil
	}

	if err := validatePath(cfg.Context); err != nil {
		return &plugin.ExecuteResponse{
			Success: false,
			Error:   fmt.Sprintf("invalid build context path: %v", err),
		}, nil
	}

	// Validate build args keys
	for key := range cfg.BuildArgs {
		if err := validateBuildArgKey(key); err != nil {
			return &plugin.ExecuteResponse{
				Success: false,
				Error:   fmt.Sprintf("invalid build arg key '%s': %v", key, err),
			}, nil
		}
	}

	// Validate label keys
	for key := range cfg.Labels {
		if err := validateLabelKey(key); err != nil {
			return &plugin.ExecuteResponse{
				Success: false,
				Error:   fmt.Sprintf("invalid label key '%s': %v", key, err),
			}, nil
		}
	}

	version := strings.TrimPrefix(releaseCtx.Version, "v")
	parts := strings.Split(version, ".")

	major, minor, patch := "", "", ""
	if len(parts) >= 1 {
		major = parts[0]
	}
	if len(parts) >= 2 {
		minor = parts[1]
	}
	if len(parts) >= 3 {
		patch = parts[2]
	}

	tags := cfg.Tags
	if len(tags) == 0 {
		tags = []string{"{{version}}", "latest"}
	}

	resolvedTags := make([]string, 0, len(tags))
	for _, tag := range tags {
		resolved := tag
		resolved = strings.ReplaceAll(resolved, "{{version}}", version)
		resolved = strings.ReplaceAll(resolved, "{{major}}", major)
		resolved = strings.ReplaceAll(resolved, "{{minor}}", minor)
		resolved = strings.ReplaceAll(resolved, "{{patch}}", patch)

		// Skip empty tags (e.g., when {{patch}} resolves to empty string)
		if resolved == "" {
			continue
		}

		// Validate resolved tag
		if err := validateTag(resolved); err != nil {
			return &plugin.ExecuteResponse{
				Success: false,
				Error:   fmt.Sprintf("invalid tag '%s': %v", resolved, err),
			}, nil
		}
		resolvedTags = append(resolvedTags, resolved)
	}

	imageNames := make([]string, 0, len(resolvedTags))
	for _, tag := range resolvedTags {
		imageName := cfg.Image
		if cfg.Registry != "" && cfg.Registry != "docker.io" {
			imageName = fmt.Sprintf("%s/%s", cfg.Registry, cfg.Image)
		}
		imageNames = append(imageNames, fmt.Sprintf("%s:%s", imageName, tag))
	}

	if dryRun {
		return &plugin.ExecuteResponse{
			Success: true,
			Message: "Would build and push Docker image",
			Outputs: map[string]any{
				"image":    cfg.Image,
				"tags":     resolvedTags,
				"registry": cfg.Registry,
			},
		}, nil
	}

	if cfg.Username != "" && cfg.Password != "" {
		if err := p.dockerLogin(ctx, cfg); err != nil {
			return &plugin.ExecuteResponse{
				Success: false,
				Error:   fmt.Sprintf("failed to login to registry: %v", err),
			}, nil
		}
	}

	if err := p.dockerBuild(ctx, cfg, imageNames, releaseCtx); err != nil {
		return &plugin.ExecuteResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to build image: %v", err),
		}, nil
	}

	if cfg.Push {
		for _, imageName := range imageNames {
			if err := p.dockerPush(ctx, imageName); err != nil {
				return &plugin.ExecuteResponse{
					Success: false,
					Error:   fmt.Sprintf("failed to push image %s: %v", imageName, err),
				}, nil
			}
		}
	}

	return &plugin.ExecuteResponse{
		Success: true,
		Message: fmt.Sprintf("Built and pushed Docker image with %d tags", len(resolvedTags)),
		Outputs: map[string]any{
			"image":  cfg.Image,
			"tags":   resolvedTags,
			"pushed": cfg.Push,
		},
	}, nil
}

func (p *DockerPlugin) dockerLogin(ctx context.Context, cfg *Config) error {
	registry := cfg.Registry
	if registry == "" || registry == "docker.io" {
		registry = ""
	}

	args := []string{"login"}
	if registry != "" {
		args = append(args, registry)
	}
	args = append(args, "-u", cfg.Username, "--password-stdin")

	return p.getExecutor().Run(ctx, "docker", args, strings.NewReader(cfg.Password))
}

func (p *DockerPlugin) dockerBuild(ctx context.Context, cfg *Config, imageNames []string, releaseCtx plugin.ReleaseContext) error {
	args := []string{"build"}

	for _, name := range imageNames {
		args = append(args, "-t", name)
	}

	dockerfile := cfg.Dockerfile
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}
	args = append(args, "-f", dockerfile)

	for key, value := range cfg.BuildArgs {
		args = append(args, "--build-arg", fmt.Sprintf("%s=%s", key, value))
	}

	args = append(args, "--build-arg", fmt.Sprintf("VERSION=%s", releaseCtx.Version))

	if len(cfg.Platforms) > 0 {
		args = append(args, "--platform", strings.Join(cfg.Platforms, ","))
	}

	for key, value := range cfg.Labels {
		args = append(args, "--label", fmt.Sprintf("%s=%s", key, value))
	}

	for _, cache := range cfg.CacheFrom {
		args = append(args, "--cache-from", cache)
	}
	if cfg.NoCache {
		args = append(args, "--no-cache")
	}

	if cfg.Target != "" {
		args = append(args, "--target", cfg.Target)
	}

	buildContext := cfg.Context
	if buildContext == "" {
		buildContext = "."
	}
	args = append(args, buildContext)

	return p.getExecutor().Run(ctx, "docker", args, nil)
}

func (p *DockerPlugin) dockerPush(ctx context.Context, imageName string) error {
	return p.getExecutor().Run(ctx, "docker", []string{"push", imageName}, nil)
}

func (p *DockerPlugin) parseConfig(raw map[string]any) *Config {
	parser := helpers.NewConfigParser(raw)

	return &Config{
		Registry:   parser.GetString("registry", "", "docker.io"),
		Image:      parser.GetString("image", "", ""),
		Tags:       parser.GetStringSlice("tags", nil),
		Dockerfile: parser.GetString("dockerfile", "", "Dockerfile"),
		Context:    parser.GetString("context", "", "."),
		BuildArgs:  getStringMap(raw, "build_args"),
		Platforms:  parser.GetStringSlice("platforms", nil),
		Username:   parser.GetString("username", "DOCKER_USERNAME", ""),
		Password:   parser.GetString("password", "DOCKER_PASSWORD", ""),
		Push:       parser.GetBool("push", true),
		Labels:     getStringMap(raw, "labels"),
		CacheFrom:  parser.GetStringSlice("cache_from", nil),
		NoCache:    parser.GetBool("no_cache", false),
		Target:     parser.GetString("target", "", ""),
	}
}

func getStringMap(raw map[string]any, key string) map[string]string {
	result := make(map[string]string)
	if v, ok := raw[key]; ok {
		if m, ok := v.(map[string]any); ok {
			for k, val := range m {
				if s, ok := val.(string); ok {
					result[k] = s
				}
			}
		}
	}
	return result
}

// Validate validates the plugin configuration.
func (p *DockerPlugin) Validate(_ context.Context, config map[string]any) (*plugin.ValidateResponse, error) {
	vb := helpers.NewValidationBuilder()
	parser := helpers.NewConfigParser(config)

	// Validate image name
	image := parser.GetString("image", "", "")
	if image == "" {
		vb.AddError("image", "Docker image name is required")
	} else if err := validateImageName(image); err != nil {
		vb.AddError("image", err.Error())
	}

	// Validate registry if provided
	registry := parser.GetString("registry", "", "docker.io")
	if err := validateRegistry(registry); err != nil {
		vb.AddError("registry", err.Error())
	}

	// Validate dockerfile path
	dockerfile := parser.GetString("dockerfile", "", "Dockerfile")
	if err := validatePath(dockerfile); err != nil {
		vb.AddError("dockerfile", err.Error())
	}

	// Validate context path
	contextPath := parser.GetString("context", "", ".")
	if err := validatePath(contextPath); err != nil {
		vb.AddError("context", err.Error())
	}

	// Validate build args keys
	if buildArgs, ok := config["build_args"].(map[string]any); ok {
		for key := range buildArgs {
			if err := validateBuildArgKey(key); err != nil {
				vb.AddError("build_args", fmt.Sprintf("invalid key '%s': %s", key, err.Error()))
			}
		}
	}

	// Validate label keys
	if labels, ok := config["labels"].(map[string]any); ok {
		for key := range labels {
			if err := validateLabelKey(key); err != nil {
				vb.AddError("labels", fmt.Sprintf("invalid key '%s': %s", key, err.Error()))
			}
		}
	}

	// Validate tags
	tags := parser.GetStringSlice("tags", nil)
	for _, tag := range tags {
		// Skip template tags, they'll be validated at runtime
		if strings.Contains(tag, "{{") {
			continue
		}
		if err := validateTag(tag); err != nil {
			vb.AddError("tags", fmt.Sprintf("invalid tag '%s': %s", tag, err.Error()))
		}
	}

	return vb.Build(), nil
}
