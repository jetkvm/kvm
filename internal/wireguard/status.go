package wireguard

// Status is the state of the tunnel for the UI.
type Status struct {
	Supported  bool         `json:"supported"`
	Enabled    bool         `json:"enabled"`
	Up         bool         `json:"up"`
	PublicKey  string       `json:"publicKey,omitempty"`
	ListenPort int          `json:"listenPort,omitempty"`
	Error      string       `json:"error,omitempty"`
	Peers      []PeerStatus `json:"peers"`
}

// PeerStatus is the state of one peer.
type PeerStatus struct {
	Name      string `json:"name,omitempty"`
	PublicKey string `json:"publicKey"`
	// Endpoint is the address the peer was last seen at, or the configured
	// one before that.
	Endpoint string `json:"endpoint,omitempty"`
	// SecondsSinceHandshake is omitted when there has been no handshake.
	SecondsSinceHandshake *int64 `json:"secondsSinceHandshake,omitempty"`
	RxBytes               int64  `json:"rxBytes"`
	TxBytes               int64  `json:"txBytes"`
}
