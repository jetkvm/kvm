package lldp

import "time"

type Neighbor struct {
	Mac               string            `json:"mac"`
	Source            string            `json:"source"`
	ChassisID         string            `json:"chassis_id"`
	PortID            string            `json:"port_id"`
	PortDescription   string            `json:"port_description"`
	SystemName        string            `json:"system_name"`
	SystemDescription string            `json:"system_description"`
	TTL               uint16            `json:"ttl"`
	ManagementAddress string            `json:"management_address"`
	Values            map[string]string `json:"values"`
}

func (l *LLDP) addNeighbor(mac string, neighbor Neighbor, ttl time.Duration) {
	logger := l.l.With().
		Str("mac", mac).
		Interface("neighbor", neighbor).
		Logger()

	current_neigh := l.neighbors.Get(mac)
	if current_neigh != nil {
		current_source := current_neigh.Value().Source
		if current_source == "lldp" && neighbor.Source != "lldp" {
			logger.Info().Msg("skip updating neighbor, as LLDP has higher priority")
			return
		}
	}

	logger.Info().Msg("adding neighbor")
	l.neighbors.Set(mac, neighbor, ttl)
}

func (l *LLDP) deleteNeighbor(mac string) {
	logger := l.l.With().
		Str("mac", mac).
		Logger()

	logger.Info().Msg("deleting neighbor")
	l.neighbors.Delete(mac)
}

func (l *LLDP) GetNeighbors() []Neighbor {
	items := l.neighbors.Items()
	neighbors := make([]Neighbor, 0, len(items))

	for _, item := range items {
		neighbors = append(neighbors, item.Value())
	}

	l.l.Info().Interface("neighbors", neighbors).Msg("neighbors")

	return neighbors
}
