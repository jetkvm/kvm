package lldp

import (
	"time"
)

type ManagementAddress struct {
	AddressFamily    string `json:"address_family"`
	Address          string `json:"address"`
	InterfaceSubtype string `json:"interface_subtype"`
	InterfaceNumber  uint32 `json:"interface_number"`
	OID              string `json:"oid,omitempty"`
}

type Neighbor struct {
	Mac                 string              `json:"mac"`
	Source              string              `json:"source"`
	ChassisID           string              `json:"chassis_id"`
	PortID              string              `json:"port_id"`
	PortDescription     string              `json:"port_description"`
	SystemName          string              `json:"system_name"`
	SystemDescription   string              `json:"system_description"`
	TTL                 uint16              `json:"ttl"`
	ManagementAddresses []ManagementAddress `json:"management_addresses"`
	Capabilities        []string            `json:"capabilities"`
	Values              map[string]string   `json:"values"`
	cacheTTL            time.Time
	cacheKey            neighborCacheKey
}

const (
	NeighborSourceLLDP uint8 = 0x1
	NeighborSourceCDP  uint8 = 0x2
)

var (
	NeighborSourceMap = map[uint8]string{
		NeighborSourceLLDP: "lldp",
		NeighborSourceCDP:  "cdp",
	}
)

type neighborCacheKey struct {
	Mac    string
	Source uint8
}

func newNeighbor(mac string, source uint8) *Neighbor {
	return &Neighbor{
		Mac:    mac,
		Source: NeighborSourceMap[source],
		Values: make(map[string]string),
		cacheKey: neighborCacheKey{
			Mac:    mac,
			Source: source,
		},
	}
}

func (l *LLDP) addNeighbor(neighbor *Neighbor, ttl time.Duration) {
	logger := l.l.With().
		Str("source", neighbor.Source).
		Str("mac", neighbor.Mac).
		Interface("neighbor", neighbor).
		Logger()

	l.neighborsMu.RLock()

	_, ok := l.neighbors[neighbor.cacheKey]
	if ok {
		logger.Trace().Msg("neighbor already exists, updating it")
	}

	logger.Trace().Msg("adding neighbor")
	neighbor.cacheTTL = time.Now().Add(ttl)
	l.neighbors[neighbor.cacheKey] = *neighbor

	l.neighborsMu.RUnlock()

	l.onChange(l.GetNeighbors())
}

func (l *LLDP) deleteNeighbor(neighbor *Neighbor) {
	logger := l.l.With().
		Str("source", neighbor.Source).
		Str("mac", neighbor.Mac).
		Logger()

	logger.Info().Msg("deleting neighbor")

	l.neighborsMu.Lock()
	delete(l.neighbors, neighbor.cacheKey)
	l.neighborsMu.Unlock()

	l.onChange(l.GetNeighbors())
}

func (l *LLDP) flushNeighbors() {
	l.neighborsMu.Lock()
	defer l.neighborsMu.Unlock()

	l.neighbors = make(map[neighborCacheKey]Neighbor)
}

func (l *LLDP) GetNeighbors() []Neighbor {
	l.neighborsMu.Lock()
	defer l.neighborsMu.Unlock()

	neighbors := make([]Neighbor, 0)

	for key, neighbor := range l.neighbors {
		if time.Now().After(neighbor.cacheTTL) {
			delete(l.neighbors, key)
			continue
		}
		neighbors = append(neighbors, neighbor)
	}

	return neighbors
}
