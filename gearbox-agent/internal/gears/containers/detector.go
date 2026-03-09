package containers

import (
	"context"
	"time"

	dockerclient "github.com/moby/moby/client"
)

// detectRuntime checks for a supported container runtime.
// Returns a RuntimeInfo and a Docker client if Docker is available.
// The caller is responsible for closing the client.
func detectRuntime(ctx context.Context) (RuntimeInfo, *dockerclient.Client) {
	cli, err := dockerclient.New()
	if err != nil {
		return RuntimeInfo{Available: false, Runtime: "none"}, nil
	}

	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	ping, err := cli.Ping(pingCtx, dockerclient.PingOptions{})
	if err != nil {
		_ = cli.Close()
		return RuntimeInfo{Available: false, Runtime: "none"}, nil
	}

	infoResult, err := cli.Info(pingCtx, dockerclient.InfoOptions{})
	if err != nil {
		return RuntimeInfo{
			Available:  true,
			Runtime:    "docker",
			APIVersion: ping.APIVersion,
		}, cli
	}

	info := infoResult.Info
	return RuntimeInfo{
		Available:  true,
		Runtime:    "docker",
		Version:    info.ServerVersion,
		APIVersion: ping.APIVersion,
		ServerOS:   info.OSType,
		ServerArch: info.Architecture,
	}, cli
}
