// Package main provides the entry point for the Docker plugin.
package main

import (
	"github.com/relicta-tech/relicta-plugin-sdk/plugin"
)

func main() {
	plugin.Serve(&DockerPlugin{})
}
