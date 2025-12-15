package jetdhcpc

import (
	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv4/nclient4"
	"github.com/insomniacslk/dhcp/dhcpv6/nclient6"
	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

type dhcpLogger struct {
	// Printfer is used for actual output of the logger
	nclient4.Printfer
	logger *zerolog.Logger
}

// Printf prints a log message as-is via predefined Printfer
func (s dhcpLogger) Printf(format string, v ...interface{}) {
	s.logger.Info().Msgf(format, v...)
}

// PrintMessage prints a DHCP message in the short format via predefined Printfer
func (s dhcpLogger) PrintMessage(prefix string, message *dhcpv4.DHCPv4) {
	s.logger.Info().Msgf("%s: %s", prefix, message.String())
}

func summaryStructured(d *dhcpv4.DHCPv4, l *zerolog.Logger) *zerolog.Logger {
	if !logging.IsTraceLevel(l) {
		return l
	}

	logger := l.
		With().
		Stringer("opCode", d.OpCode).
		Stringer("hwType", d.HWType).
		Int("hopCount", int(d.HopCount)).
		Stringer("transactionID", d.TransactionID).
		Int("numSeconds", int(d.NumSeconds)).
		Str("flagsString", d.FlagsToString()).
		Int("flags", int(d.Flags)).
		IPAddr("clientIP", d.ClientIPAddr).
		IPAddr("yourIP", d.YourIPAddr).
		IPAddr("serverIP", d.ServerIPAddr).
		IPAddr("gatewayIP", d.GatewayIPAddr).
		MACAddr("clientMAC", d.ClientHWAddr).
		Str("serverHostname", d.ServerHostName).
		Str("bootFileName", d.BootFileName).
		Str("options", d.Options.Summary(nil)).
		Logger()
	return &logger
}

func (c *Client) getDHCP4Logger(ifname string) nclient4.ClientOpt {
	logger := c.getLogger().
		With().
		Str("interface", ifname).
		Str("source", "dhcp4").
		Logger()

	return nclient4.WithLogger(dhcpLogger{
		logger: &logger,
	})
}

// TODO: nclient6 doesn't implement the WithLogger option,
// we might need to open a PR to add it

func (c *Client) getDHCP6Logger() nclient6.ClientOpt {
	return nclient6.WithSummaryLogger()
}
