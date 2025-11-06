package lldp

import (
	"fmt"
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
	Mac               string             `json:"mac"`
	Source            string             `json:"source"`
	ChassisID         string             `json:"chassis_id"`
	PortID            string             `json:"port_id"`
	PortDescription   string             `json:"port_description"`
	SystemName        string             `json:"system_name"`
	SystemDescription string             `json:"system_description"`
	TTL               uint16             `json:"ttl"`
	ManagementAddress *ManagementAddress `json:"management_address,omitempty"`
	Capabilities      []string           `json:"capabilities"`
	Values            map[string]string  `json:"values"`
}

func (n *Neighbor) cacheKey() string {
	return fmt.Sprintf("%s-%s", n.Mac, n.Source)
}

func (l *LLDP) addNeighbor(neighbor *Neighbor, ttl time.Duration) {
	logger := l.l.With().
		Str("mac", neighbor.Mac).
		Interface("neighbor", neighbor).
		Logger()

	key := neighbor.cacheKey()

	current_neigh := l.neighbors.Get(key)
	if current_neigh != nil {
		current_source := current_neigh.Value().Source
		if current_source == "lldp" && neighbor.Source != "lldp" {
			logger.Info().Msg("skip updating neighbor, as LLDP has higher priority")
			return
		}
	}

	logger.Trace().Msg("adding neighbor")
	l.neighbors.Set(key, *neighbor, ttl)

	l.onChange(l.GetNeighbors())
}

func (l *LLDP) deleteNeighbor(neighbor *Neighbor) {
	logger := l.l.With().
		Str("mac", neighbor.Mac).
		Logger()

	logger.Info().Msg("deleting neighbor")
	l.neighbors.Delete(neighbor.cacheKey())

	l.onChange(l.GetNeighbors())
}

func (l *LLDP) GetNeighbors() []Neighbor {
	items := l.neighbors.Items()
	neighbors := make([]Neighbor, 0, len(items))

	for _, item := range items {
		neighbors = append(neighbors, item.Value())
	}

	return neighbors
}
