package udhcpc

func (u *DHCPClient) GetNtpServers() []string {
	if u.lease == nil {
		return nil
	}
	servers := make([]string, len(u.lease.NTPServers))
	for i, server := range u.lease.NTPServers {
		servers[i] = server.String()
	}
	return servers
}
