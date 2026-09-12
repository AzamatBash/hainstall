package dockerctl

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
)

const hostHelperImage = "alpine:3.20"

// ComposeProjectDir returns the compose working directory for the HAProxy container.
func (c *Controller) ComposeProjectDir(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	info, err := c.cli.ContainerInspect(ctx, c.ContainerName)
	if err != nil {
		return "", fmt.Errorf("inspect %q: %w", c.ContainerName, err)
	}
	if info.Config == nil || info.Config.Labels == nil {
		return "", fmt.Errorf("container %q has no labels", c.ContainerName)
	}
	dir := strings.TrimSpace(info.Config.Labels["com.docker.compose.project.working_dir"])
	if dir == "" {
		return "", fmt.Errorf("compose working_dir label missing on %q", c.ContainerName)
	}
	return dir, nil
}

// HostShell runs a shell script on the VPS host via a privileged helper container.
func (c *Controller) HostShell(ctx context.Context, script string) (string, error) {
	if err := c.ensureHelperImage(ctx); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	cfg := &container.Config{
		Image: hostHelperImage,
		Cmd:   []string{"chroot", "/host", "sh", "-c", script},
	}
	hostCfg := &container.HostConfig{
		Privileged:  true,
		NetworkMode: "host",
		PidMode:     "host",
		AutoRemove:  false,
		Mounts: []mount.Mount{
			{Type: mount.TypeBind, Source: "/", Target: "/host"},
			{Type: mount.TypeBind, Source: "/var/run/docker.sock", Target: "/var/run/docker.sock"},
		},
	}
	resp, err := c.cli.ContainerCreate(ctx, cfg, hostCfg, nil, nil, "")
	if err != nil {
		return "", fmt.Errorf("host helper create: %w", err)
	}
	defer func() {
		_ = c.cli.ContainerRemove(context.Background(), resp.ID, container.RemoveOptions{Force: true})
	}()
	if err := c.cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return "", fmt.Errorf("host helper start: %w", err)
	}
	statusCh, errCh := c.cli.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			return "", fmt.Errorf("host helper wait: %w", err)
		}
	case st := <-statusCh:
		logs, _ := c.cli.ContainerLogs(ctx, resp.ID, container.LogsOptions{ShowStdout: true, ShowStderr: true})
		var buf bytes.Buffer
		if logs != nil {
			_, _ = io.Copy(&buf, logs)
			_ = logs.Close()
		}
		out := stripDockerLogHeaders(buf.Bytes())
		if st.StatusCode != 0 {
			return out, fmt.Errorf("host command exit %d: %s", st.StatusCode, strings.TrimSpace(out))
		}
		return out, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
	return "", fmt.Errorf("host helper: unexpected wait end")
}

// ReadHostFile reads a file from the host filesystem.
func (c *Controller) ReadHostFile(ctx context.Context, path string) ([]byte, error) {
	out, err := c.HostShell(ctx, fmt.Sprintf(`cat -- %q`, path))
	if err != nil {
		return nil, err
	}
	return []byte(out), nil
}

// WriteHostFile writes a file on the host (atomic via temp + mv).
func (c *Controller) WriteHostFile(ctx context.Context, path string, data []byte) error {
	b64 := base64.StdEncoding.EncodeToString(data)
	script := fmt.Sprintf(`set -e
tmp="%s.tmp.$$"
echo %s | base64 -d > "$tmp"
mv -f "$tmp" %q
`, path, b64, path)
	_, err := c.HostShell(ctx, script)
	return err
}

func (c *Controller) ensureHelperImage(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	_, _, err := c.cli.ImageInspectWithRaw(ctx, hostHelperImage)
	if err == nil {
		return nil
	}
	rc, err := c.cli.ImagePull(ctx, hostHelperImage, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("pull %s: %w", hostHelperImage, err)
	}
	defer rc.Close()
	_, _ = io.Copy(io.Discard, rc)
	return nil
}

// stripDockerLogHeaders removes the 8-byte multiplex headers from ContainerLogs.
func stripDockerLogHeaders(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	var out strings.Builder
	i := 0
	for i+8 <= len(b) {
		// stream type in b[i]; size is big-endian uint32 at i+4
		size := int(b[i+4])<<24 | int(b[i+5])<<16 | int(b[i+6])<<8 | int(b[i+7])
		i += 8
		if size < 0 || i+size > len(b) {
			out.Write(b[i-8:])
			break
		}
		out.Write(b[i : i+size])
		i += size
	}
	if i < len(b) && out.Len() == 0 {
		return string(b)
	}
	return out.String()
}
