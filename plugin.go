// Package main implements the Docker Hub / Container registry plugin for Relicta.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/relicta-tech/relicta-plugin-sdk/helpers"
	"github.com/relicta-tech/relicta-plugin-sdk/plugin"
)

// DockerPlugin implements the Docker container registry plugin.
type DockerPlugin struct{}

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

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdin = strings.NewReader(cfg.Password)
	cmd.Stderr = os.Stderr

	return cmd.Run()
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

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func (p *DockerPlugin) dockerPush(ctx context.Context, imageName string) error {
	cmd := exec.CommandContext(ctx, "docker", "push", imageName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
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

	image := parser.GetString("image", "", "")
	if image == "" {
		vb.AddError("image", "Docker image name is required")
	}

	return vb.Build(), nil
}
