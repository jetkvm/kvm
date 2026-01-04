//go:build linux

package tailscale

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/jetkvm/kvm/internal/meshvpn"
)

// CLI wraps the tailscale command-line interface.
type CLI struct {
	binaryPath string
}

func NewCLI() *CLI {
	return &CLI{binaryPath: TailscalePath}
}

type UpOptions struct {
	ControlServer string
	AuthKey       string
}

type UpResult struct {
	AuthURL string
}

type StatusResponse struct {
	Version        string          `json:"Version"`
	TailscaleIPs   []string        `json:"TailscaleIPs,omitempty"`
	BackendState   string          `json:"BackendState"`
	AuthURL        string          `json:"AuthURL,omitempty"`
	Self           StatusSelf      `json:"Self"`
	Peer           map[string]Peer `json:"Peer"`
	ExitNodeStatus *ExitNodeStatus `json:"ExitNodeStatus,omitempty"`
}

type StatusSelf struct {
	ID           string   `json:"ID"`
	PublicKey    string   `json:"PublicKey"`
	HostName     string   `json:"HostName"`
	DNSName      string   `json:"DNSName"`
	TailscaleIPs []string `json:"TailscaleIPs"`
	Online       bool     `json:"Online"`
}

type Peer struct {
	ID             string   `json:"ID"`
	PublicKey      string   `json:"PublicKey"`
	HostName       string   `json:"HostName"`
	DNSName        string   `json:"DNSName"`
	TailscaleIPs   []string `json:"TailscaleIPs"`
	Online         bool     `json:"Online"`
	ExitNode       bool     `json:"ExitNode"`
	ExitNodeOption bool     `json:"ExitNodeOption"`
}

type ExitNodeStatus struct {
	ID           string   `json:"ID"`
	Online       bool     `json:"Online"`
	TailscaleIPs []string `json:"TailscaleIPs"`
}

type ExitNodeListItem struct {
	ID       string `json:"ID"`
	Name     string `json:"Name"`
	Location struct {
		Country string `json:"Country"`
		City    string `json:"City"`
	} `json:"Location,omitempty"`
	Online bool `json:"Online"`
}

func (c *CLI) run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, c.binaryPath, args...)
	cmd.Env = append(cmd.Environ(), "TAILSCALE_SOCKET="+SocketPath)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("tailscale %s: %w (output: %s)", strings.Join(args, " "), err, string(output))
	}

	return output, nil
}

func (c *CLI) Up(ctx context.Context, opts UpOptions) (*UpResult, error) {
	args := []string{"up"}

	if opts.ControlServer != "" {
		args = append(args, "--login-server="+opts.ControlServer)
	}
	if opts.AuthKey != "" {
		args = append(args, "--authkey="+opts.AuthKey)
	}

	output, err := c.run(ctx, args...)
	if err != nil {
		outStr := string(output)
		if strings.Contains(outStr, "To authenticate") || strings.Contains(outStr, "visit:") {
			for _, line := range strings.Split(outStr, "\n") {
				if strings.Contains(line, "https://") {
					for _, part := range strings.Fields(line) {
						if strings.HasPrefix(part, "https://") {
							return &UpResult{AuthURL: part}, nil
						}
					}
				}
			}
		}
		return nil, err
	}

	return &UpResult{}, nil
}

func (c *CLI) Down(ctx context.Context) error {
	_, err := c.run(ctx, "down")
	return err
}

func (c *CLI) Logout(ctx context.Context) error {
	_, err := c.run(ctx, "logout")
	return err
}

func (c *CLI) Status(ctx context.Context) (*StatusResponse, error) {
	output, err := c.run(ctx, "status", "--json")
	if err != nil {
		return nil, err
	}

	var status StatusResponse
	if err := json.Unmarshal(output, &status); err != nil {
		return nil, fmt.Errorf("failed to parse status: %w", err)
	}

	if len(status.TailscaleIPs) == 0 && len(status.Self.TailscaleIPs) > 0 {
		status.TailscaleIPs = status.Self.TailscaleIPs
	}

	return &status, nil
}

func (c *CLI) Version(ctx context.Context) (string, error) {
	output, err := c.run(ctx, "version")
	if err != nil {
		return "", err
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) > 0 {
		return strings.TrimSpace(lines[0]), nil
	}

	return "", nil
}

func (c *CLI) ListExitNodes(ctx context.Context) ([]meshvpn.ExitNode, error) {
	output, err := c.run(ctx, "exit-node", "list", "--json")
	if err == nil {
		var items []ExitNodeListItem
		if err := json.Unmarshal(output, &items); err != nil {
			var itemMap map[string]ExitNodeListItem
			if err2 := json.Unmarshal(output, &itemMap); err2 == nil {
				logger.Debug().Msg("exit node list parsed as map format")
				for _, item := range itemMap {
					items = append(items, item)
				}
			} else {
				logger.Debug().
					AnErr("arrayErr", err).
					AnErr("mapErr", err2).
					Msg("failed to parse exit node list JSON")
			}
		}

		if len(items) > 0 {
			nodes := make([]meshvpn.ExitNode, 0, len(items))
			for _, item := range items {
				nodes = append(nodes, meshvpn.ExitNode{
					ID:       item.ID,
					Name:     item.Name,
					HostName: item.Name,
					Online:   item.Online,
					Country:  item.Location.Country,
					City:     item.Location.City,
				})
			}
			return nodes, nil
		}
	}

	// Fallback: extract exit nodes from status (works with all versions)
	status, err := c.Status(ctx)
	if err != nil {
		return nil, err
	}

	nodes := make([]meshvpn.ExitNode, 0)
	for _, peer := range status.Peer {
		if peer.ExitNodeOption {
			ip := ""
			if len(peer.TailscaleIPs) > 0 {
				ip = peer.TailscaleIPs[0]
			}
			nodes = append(nodes, meshvpn.ExitNode{
				ID:       peer.ID,
				Name:     peer.HostName,
				HostName: peer.HostName,
				IP:       ip,
				Online:   peer.Online,
			})
		}
	}

	return nodes, nil
}

func (c *CLI) SetExitNode(ctx context.Context, hostname string, allowLAN bool) error {
	args := []string{"set", "--exit-node=" + hostname}
	if allowLAN {
		args = append(args, "--exit-node-allow-lan-access")
	}
	_, err := c.run(ctx, args...)
	return err
}

func (c *CLI) ClearExitNode(ctx context.Context) error {
	_, err := c.run(ctx, "set", "--exit-node=")
	return err
}

func (c *CLI) SetAdvertiseExitNode(ctx context.Context, advertise bool) error {
	if advertise {
		_, err := c.run(ctx, "set", "--advertise-exit-node")
		return err
	}
	_, err := c.run(ctx, "set", "--advertise-exit-node=false")
	return err
}
