package udhcpc

import (
	"testing"
	"time"
)

func TestUnmarshalDHCPCLease(t *testing.T) {
	lease := &Lease{}
	err := UnmarshalDHCPCLease(lease, `
# generated @ Mon Jan  4 19:31:53 UTC 2021
#  19:31:53 up 0 min,  0 users,  load average: 0.72, 0.14, 0.04
# the date might be inaccurate if the clock is not set
ip=192.168.0.240
siaddr=192.168.0.1
sname=
boot_file=
subnet=255.255.255.0
timezone=
router=192.168.0.1
timesvr=
namesvr=
dns=172.19.53.2
logsvr=
cookiesvr=
lprsvr=
hostname=
bootsize=
domain=
swapsvr=
rootpath=
ipttl=
mtu=
broadcast=
ntpsrv=162.159.200.123
wins=
lease=172800
dhcptype=
serverid=192.168.0.1
message=
tftp=
bootfile=
	`)
	if lease.IPAddress.String() != "192.168.0.240" {
		t.Fatalf("expected ip to be 192.168.0.240, got %s", lease.IPAddress.String())
	}
	if lease.Netmask.String() != "255.255.255.0" {
		t.Fatalf("expected netmask to be 255.255.255.0, got %s", lease.Netmask.String())
	}
	if len(lease.Routers) != 1 {
		t.Fatalf("expected 1 router, got %d", len(lease.Routers))
	}
	if lease.Routers[0].String() != "192.168.0.1" {
		t.Fatalf("expected router to be 192.168.0.1, got %s", lease.Routers[0].String())
	}
	if len(lease.NTPServers) != 1 {
		t.Fatalf("expected 1 timeserver, got %d", len(lease.NTPServers))
	}
	if lease.NTPServers[0].String() != "162.159.200.123" {
		t.Fatalf("expected timeserver to be 162.159.200.123, got %s", lease.NTPServers[0].String())
	}
	if len(lease.DNS) != 1 {
		t.Fatalf("expected 1 dns, got %d", len(lease.DNS))
	}
	if lease.DNS[0].String() != "172.19.53.2" {
		t.Fatalf("expected dns to be 172.19.53.2, got %s", lease.DNS[0].String())
	}
	if lease.LeaseTime != 172800*time.Second {
		t.Fatalf("expected lease time to be 172800 seconds, got %d", lease.LeaseTime)
	}
	if err != nil {
		t.Fatal(err)
	}
}
