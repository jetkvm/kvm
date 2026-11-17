package mdns

import "strconv"

const JetKVMServiceType = "_jetkvm._tcp"

type DeviceState struct {
	Hostname     string
	FQDN         string
	Mode         string
	TLSEnabled   bool
	LoopbackOnly bool
	SetupDone    bool
	DeviceID     string
	Version      string
}

func DeviceOptions(d DeviceState) *MDNSOptions {
	listen := &MDNSListenOptions{}
	switch d.Mode {
	case "auto":
		listen.IPv4, listen.IPv6 = true, true
	case "ipv4_only":
		listen.IPv4 = true
	case "ipv6_only":
		listen.IPv6 = true
	}

	return &MDNSOptions{
		LocalNames:    []string{d.Hostname, d.FQDN},
		ListenOptions: listen,
		Service:       deviceService(d),
	}
}

func deviceService(d DeviceState) *MDNSService {
	if d.LoopbackOnly {
		return nil
	}

	port := 80
	if d.TLSEnabled {
		port = 443
	}

	return &MDNSService{
		Type:     JetKVMServiceType,
		Instance: d.Hostname,
		Port:     port,
		TXT: []TXTEntry{
			NewTXTString("id", d.DeviceID),
			NewTXTString("setup", strconv.FormatBool(d.SetupDone)),
			NewTXTString("version", d.Version),
		},
	}
}
