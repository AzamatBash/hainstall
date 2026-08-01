package dockerctl

import (
	"context"
	"fmt"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// Controller reloads/restarts the HAProxy container via the Docker Engine API.
type Controller struct {
	cli           *client.Client
	ContainerName string
}

// New creates a Docker controller. socketPath is typically unix:///var/run/docker.sock.
func New(socketPath, containerName string) (*Controller, error) {
	if socketPath == "" {
		socketPath = "unix:///var/run/docker.sock"
	}
	if containerName == "" {
		containerName = "haproxy"
	}
	opts := []client.Opt{
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	}
	if socketPath != "" {
		opts = append(opts, client.WithHost(socketPath))
	}
	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	return &Controller{cli: cli, ContainerName: containerName}, nil
}

// Close releases the Docker client.
func (c *Controller) Close() error {
	if c.cli == nil {
		return nil
	}
	return c.cli.Close()
}

// Reload sends SIGHUP to the HAProxy container (graceful config reload).
func (c *Controller) Reload(ctx context.Context) error {
	id, err := c.resolveID(ctx)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	// Docker API Kill with signal — HAProxy treats SIGUSR2 as soft-reload in
	// master-worker; SIGHUP also reloads workers on many images. Prefer USR2
	// when master-worker is enabled; compose uses `-W` so USR2 is correct.
	if err := c.cli.ContainerKill(ctx, id, "SIGUSR2"); err != nil {
		// Fallback to HUP for non-master-worker setups.
		if err2 := c.cli.ContainerKill(ctx, id, "SIGHUP"); err2 != nil {
			return fmt.Errorf("signal reload (USR2: %v, HUP: %w)", err, err2)
		}
	}
	return nil
}

// Restart restarts the HAProxy container.
func (c *Controller) Restart(ctx context.Context) error {
	id, err := c.resolveID(ctx)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	timeout := 10
	if err := c.cli.ContainerRestart(ctx, id, container.StopOptions{Timeout: &timeout}); err != nil {
		return fmt.Errorf("restart container: %w", err)
	}
	return nil
}

func (c *Controller) resolveID(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	// ContainerInspect accepts name or ID.
	info, err := c.cli.ContainerInspect(ctx, c.ContainerName)
	if err != nil {
		return "", fmt.Errorf("inspect %q: %w", c.ContainerName, err)
	}
	if info.ID == "" {
		return "", fmt.Errorf("container %q not found", c.ContainerName)
	}
	return info.ID, nil
}
