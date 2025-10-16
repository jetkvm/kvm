package jetdhcpc

import (
	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv4/nclient4"
	"github.com/insomniacslk/dhcp/dhcpv6/nclient6"
	"github.com/rs/zerolog"
)

type dhcpLogger struct {
	// Printfer is used for actual output of the logger
	nclient4.Printfer

	l *zerolog.Logger
}

// Printf prints a log message as-is via predefined Printfer
func (s dhcpLogger) Printf(format string, v ...interface{}) {
	s.l.Info().Msgf(format, v...)
}

// PrintMessage prints a DHCP message in the short format via predefined Printfer
func (s dhcpLogger) PrintMessage(prefix string, message *dhcpv4.DHCPv4) {
	s.l.Info().Msgf("%s: %s", prefix, message.String())
}

func summaryStructured(d *dhcpv4.DHCPv4, l *zerolog.Logger) *zerolog.Logger {
	logger := l.With().
		Str("opCode", d.OpCode.String()).
		Str("hwType", d.HWType.String()).
		Int("hopCount", int(d.HopCount)).
		Str("transactionID", d.TransactionID.String()).
		Int("numSeconds", int(d.NumSeconds)).
		Str("flagsString", d.FlagsToString()).
		Int("flags", int(d.Flags)).
		Str("clientIP", d.ClientIPAddr.String()).
		Str("yourIP", d.YourIPAddr.String()).
		Str("serverIP", d.ServerIPAddr.String()).
		Str("gatewayIP", d.GatewayIPAddr.String()).
		Str("clientMAC", d.ClientHWAddr.String()).
		Str("serverHostname", d.ServerHostName).
		Str("bootFileName", d.BootFileName).
		Str("options", d.Options.Summary(nil)).
		Logger()
	return &logger
}

func (c *Client) getDHCP4Logger(ifname string) nclient4.ClientOpt {
	logger := c.l.With().
		Str("interface", ifname).
		Str("source", "dhcp4").
		Logger()

	return nclient4.WithLogger(dhcpLogger{
		l: &logger,
	})
}

// TODO: nclient6 doesn't implement the WithLogger option,
// we might need to open a PR to add it

func (c *Client) getDHCP6Logger() nclient6.ClientOpt {
	return nclient6.WithSummaryLogger()
}
