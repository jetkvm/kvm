package timesync

import (
	"context"
	"math/rand/v2"
	"strconv"
	"time"

	"github.com/beevik/ntp"
)

var defaultNTPServerIPs = []string{
	// These servers are known by static IP and as such don't need DNS lookups
	// These are from Google and Cloudflare since if they're down, the internet
	// is broken anyway
	"162.159.200.1",      // time.cloudflare.com IPv4
	"162.159.200.123",    // time.cloudflare.com IPv4
	"2606:4700:f1::1",    // time.cloudflare.com IPv6
	"2606:4700:f1::123",  // time.cloudflare.com IPv6
	"216.239.35.0",       // time.google.com IPv4
	"216.239.35.4",       // time.google.com IPv4
	"216.239.35.8",       // time.google.com IPv4
	"216.239.35.12",      // time.google.com IPv4
	"2001:4860:4806::",   // time.google.com IPv6
	"2001:4860:4806:4::", // time.google.com IPv6
	"2001:4860:4806:8::", // time.google.com IPv6
	"2001:4860:4806:c::", // time.google.com IPv6
}

var defaultNTPServerHostnames = []string{
	// should use something from https://github.com/jauderho/public-ntp-servers
	"time.apple.com",
	"time.aws.com",
	"time.windows.com",
	"time.google.com",
	"time.cloudflare.com",
	"pool.ntp.org",
}

func (t *TimeSync) queryNetworkTime(ntpServers []string) (now *time.Time, offset *time.Duration) {
	chunkSize := int(t.networkConfig.TimeSyncParallel.ValueOr(4))
	t.l.Info().Strs("servers", ntpServers).Int("chunkSize", chunkSize).Msg("querying NTP servers")

	// shuffle the ntp servers to avoid always querying the same servers
	rand.Shuffle(len(ntpServers), func(i, j int) { ntpServers[i], ntpServers[j] = ntpServers[j], ntpServers[i] })

	for i := 0; i < len(ntpServers); i += chunkSize {
		chunk := ntpServers[i:min(i+chunkSize, len(ntpServers))]
		now, offset := t.queryMultipleNTP(chunk, timeSyncTimeout)
		if now != nil {
			return now, offset
		}
	}

	return nil, nil
}

type ntpResult struct {
	now    *time.Time
	offset *time.Duration
}

func (t *TimeSync) queryMultipleNTP(servers []string, timeout time.Duration) (now *time.Time, offset *time.Duration) {
	results := make(chan *ntpResult, len(servers))

	_, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	for _, server := range servers {
		go func(server string) {
			scopedLogger := t.l.With().
				Str("server", server).
				Logger()

			// increase request count
			metricNtpTotalRequestCount.Inc()
			metricNtpRequestCount.WithLabelValues(server).Inc()

			// query the server
			now, response, err := queryNtpServer(server, timeout)
			if err != nil {
				scopedLogger.Warn().
					Str("error", err.Error()).
					Msg("failed to query NTP server")
				results <- nil
				return
			}

			if response.IsKissOfDeath() {
				scopedLogger.Warn().
					Str("kiss_code", response.KissCode).
					Msg("ignoring NTP server kiss of death")
				results <- nil
				return
			}

			rtt := float64(response.RTT.Milliseconds())

			// set the last RTT
			metricNtpServerLastRTT.WithLabelValues(
				server,
			).Set(rtt)

			// set the RTT histogram
			metricNtpServerRttHistogram.WithLabelValues(
				server,
			).Observe(rtt)

			// set the server info
			metricNtpServerInfo.WithLabelValues(
				server,
				response.ReferenceString(),
				strconv.Itoa(int(response.Stratum)),
				strconv.Itoa(int(response.Precision)),
			).Set(1)

			// increase success count
			metricNtpTotalSuccessCount.Inc()
			metricNtpSuccessCount.WithLabelValues(server).Inc()

			scopedLogger.Info().
				Str("time", now.Format(time.RFC3339)).
				Str("reference", response.ReferenceString()).
				Float64("rtt", rtt).
				Str("clockOffset", response.ClockOffset.String()).
				Uint8("stratum", response.Stratum).
				Msg("NTP server returned time")

			cancel()

			results <- &ntpResult{
				now:    now,
				offset: &response.ClockOffset,
			}
		}(server)
	}

	for range servers {
		result := <-results
		if result == nil {
			continue
		}
		now, offset = result.now, result.offset
		return
	}
	return
}

func queryNtpServer(server string, timeout time.Duration) (now *time.Time, response *ntp.Response, err error) {
	resp, err := ntp.QueryWithOptions(server, ntp.QueryOptions{Timeout: timeout})
	if err != nil {
		return nil, nil, err
	}
	return &resp.Time, resp, nil
}
