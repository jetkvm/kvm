package wireguard

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
)

// ImportWarning describes a line of an imported .conf that was not used. The
// UI shows it in the user's language, keyed by Code.
type ImportWarning struct {
	// Code is one of "dns_ignored", "option_ignored" or "default_route_removed".
	Code string `json:"code"`
	// Detail is the option name or the removed allowed IP.
	Detail string `json:"detail"`
}

// ImportResult is a parsed .conf.
type ImportResult struct {
	Config   Config          `json:"config"`
	Warnings []ImportWarning `json:"warnings"`
}

// options that wg-quick understands but this device does not use
var ignoredInterfaceOptions = map[string]bool{
	"mtu": true, "table": true, "fwmark": true, "saveconfig": true,
	"preup": true, "postup": true, "predown": true, "postdown": true,
}

// ParseConf parses a WireGuard config file in the wg-quick format. Options the
// device does not use are skipped with a warning, as are allowed IPs that
// would send all traffic through the VPN. The result is not validated.
func ParseConf(text string) (*ImportResult, error) {
	res := &ImportResult{Config: Config{Addresses: []string{}, Peers: []Peer{}}, Warnings: []ImportWarning{}}
	cfg := &res.Config
	warn := func(code, detail string) {
		res.Warnings = append(res.Warnings, ImportWarning{Code: code, Detail: detail})
	}

	section := ""
	var peer *Peer
	sawInterface := false
	scanner := bufio.NewScanner(strings.NewReader(text))
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := scanner.Text()
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			switch section {
			case "interface":
				sawInterface = true
			case "peer":
				cfg.Peers = append(cfg.Peers, Peer{AllowedIPs: []string{}})
				peer = &cfg.Peers[len(cfg.Peers)-1]
			default:
				return nil, fmt.Errorf("line %d: unknown section [%s]", lineNo, section)
			}
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("line %d: expected key = value", lineNo)
		}
		name := strings.TrimSpace(key)
		key = strings.ToLower(name)
		value = strings.TrimSpace(value)

		switch section {
		case "interface":
			switch {
			case key == "privatekey":
				cfg.PrivateKey = value
			case key == "address":
				cfg.Addresses = append(cfg.Addresses, splitList(value)...)
			case key == "listenport":
				port, err := strconv.Atoi(value)
				if err != nil {
					return nil, fmt.Errorf("line %d: ListenPort must be a number", lineNo)
				}
				cfg.ListenPort = port
			case key == "dns":
				warn("dns_ignored", value)
			case ignoredInterfaceOptions[key]:
				warn("option_ignored", name)
			default:
				return nil, fmt.Errorf("line %d: unknown option %s", lineNo, name)
			}
		case "peer":
			switch key {
			case "publickey":
				peer.PublicKey = value
			case "presharedkey":
				peer.PresharedKey = value
			case "endpoint":
				peer.Endpoint = value
			case "allowedips":
				for _, a := range splitList(value) {
					if p, err := parseAddress(a); err == nil && p.Bits() == 0 {
						warn("default_route_removed", a)
						continue
					}
					peer.AllowedIPs = append(peer.AllowedIPs, a)
				}
			case "persistentkeepalive":
				if strings.EqualFold(value, "off") {
					peer.PersistentKeepalive = 0
					continue
				}
				sec, err := strconv.Atoi(value)
				if err != nil {
					return nil, fmt.Errorf("line %d: PersistentKeepalive must be a number", lineNo)
				}
				peer.PersistentKeepalive = sec
			default:
				return nil, fmt.Errorf("line %d: unknown option %s", lineNo, name)
			}
		default:
			return nil, fmt.Errorf("line %d: option outside of an [Interface] or [Peer] section", lineNo)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if !sawInterface && len(cfg.Peers) == 0 {
		return nil, fmt.Errorf("no [Interface] or [Peer] section found")
	}
	return res, nil
}

func splitList(s string) []string {
	var out []string
	for _, item := range strings.Split(s, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}
