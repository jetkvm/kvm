package usbgadget

import (
	"fmt"
	"os/exec"

	"github.com/google/nftables"
	"github.com/google/nftables/expr"
	"golang.org/x/sys/unix"
)

const (
	ncmFirewallTableName = "jetkvm"
	ncmFirewallChainName = "input_usb0"
)

// applyNcmFirewall installs (or replaces) an nftables table that drops all
// inbound TCP and UDP arriving on usb0. ICMP/ICMPv6 are intentionally not
// touched so NDP and ping continue to work — host isolation, not full
// blackhole. Idempotent: deletes any pre-existing table of the same name
// first so a stale ruleset from a previous run can't accumulate.
//
// Loads nf_tables.ko on first call via modprobe; the rv1106 rootfs ships
// the module but does not auto-load it.
func (u *UsbGadget) applyNcmFirewall() error {
	if out, err := exec.Command("modprobe", "nf_tables").CombinedOutput(); err != nil {
		return fmt.Errorf("modprobe nf_tables: %w: %s", err, out)
	}

	conn, err := nftables.New()
	if err != nil {
		return fmt.Errorf("open nftables conn: %w", err)
	}

	// Wipe any stale table from a previous run before building fresh.
	table := &nftables.Table{Name: ncmFirewallTableName, Family: nftables.TableFamilyINet}
	conn.DelTable(table)
	// Ignore error — table may not exist, which is fine.
	_ = conn.Flush()

	table = conn.AddTable(table)
	policy := nftables.ChainPolicyAccept
	chain := conn.AddChain(&nftables.Chain{
		Name:     ncmFirewallChainName,
		Table:    table,
		Hooknum:  nftables.ChainHookInput,
		Priority: nftables.ChainPriorityFilter,
		Type:     nftables.ChainTypeFilter,
		Policy:   &policy,
	})

	// One drop rule per L4 protocol. Each rule matches:
	//   iifname == usb0 AND l4proto == <tcp|udp>  =>  drop
	for _, proto := range []byte{unix.IPPROTO_TCP, unix.IPPROTO_UDP} {
		conn.AddRule(&nftables.Rule{
			Table: table,
			Chain: chain,
			Exprs: []expr.Any{
				&expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1},
				&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: ifnameBytes(ncmInterfaceName)},
				&expr.Meta{Key: expr.MetaKeyL4PROTO, Register: 1},
				&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{proto}},
				&expr.Verdict{Kind: expr.VerdictDrop},
			},
		})
	}

	if err := conn.Flush(); err != nil {
		return fmt.Errorf("commit nftables ruleset: %w", err)
	}
	return nil
}

// removeNcmFirewall deletes our nftables table. Best-effort: a missing table
// is not an error (we may be called during teardown after a crash or during
// rapid toggle off/on cycles).
func (u *UsbGadget) removeNcmFirewall() {
	conn, err := nftables.New()
	if err != nil {
		u.log.Warn().Err(err).Msg("nftables open failed during teardown")
		return
	}
	conn.DelTable(&nftables.Table{Name: ncmFirewallTableName, Family: nftables.TableFamilyINet})
	if err := conn.Flush(); err != nil {
		// Most likely cause is "table not found", which we don't care about.
		u.log.Debug().Err(err).Msg("nftables flush during teardown")
	}
}

// ifnameBytes pads or truncates name to IFNAMSIZ (16 bytes), the form nft
// expects when comparing against the iifname meta key. A shorter slice
// silently fails to match (the kernel memcmps the full register width).
func ifnameBytes(name string) []byte {
	b := make([]byte, unix.IFNAMSIZ)
	copy(b, name)
	return b
}
