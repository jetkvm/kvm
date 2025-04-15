package timesync

import (
	"math/rand/v2"
	"strconv"
	"time"

	"github.com/beevik/ntp"
)

var defaultNTPServers = []string{
	"time.apple.com",
	"time.aws.com",
	"time.windows.com",
	"time.google.com",
	"162.159.200.123", // time.cloudflare.com
	"0.pool.ntp.org",
	"1.pool.ntp.org",
	"2.pool.ntp.org",
	"3.pool.ntp.org",
}

func (t *TimeSync) queryNetworkTime() (now *time.Time, offset *time.Duration) {
	chunkSize := 4
	ntpServers := t.ntpServers

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

			// set the last RTT
			metricNtpServerLastRTT.WithLabelValues(
				server,
			).Set(float64(response.RTT.Milliseconds()))

			// set the RTT histogram
			metricNtpServerRttHistogram.WithLabelValues(
				server,
			).Observe(float64(response.RTT.Milliseconds()))

			// set the server info
			metricNtpServerInfo.WithLabelValues(
				server,
				response.ReferenceString(),
				strconv.Itoa(int(response.Stratum)),
				strconv.Itoa(int(response.Precision)),
			).Set(1)

			if err == nil {
				// increase success count
				metricNtpTotalSuccessCount.Inc()
				metricNtpSuccessCount.WithLabelValues(server).Inc()

				scopedLogger.Info().
					Str("time", now.Format(time.RFC3339)).
					Str("reference", response.ReferenceString()).
					Str("rtt", response.RTT.String()).
					Str("clockOffset", response.ClockOffset.String()).
					Uint8("stratum", response.Stratum).
					Msg("NTP server returned time")
				results <- &ntpResult{
					now:    now,
					offset: &response.ClockOffset,
				}
			} else {
				scopedLogger.Warn().
					Str("error", err.Error()).
					Msg("failed to query NTP server")
			}
		}(server)
	}

	result := <-results
	return result.now, result.offset
}

func queryNtpServer(server string, timeout time.Duration) (now *time.Time, response *ntp.Response, err error) {
	resp, err := ntp.QueryWithOptions(server, ntp.QueryOptions{Timeout: timeout})
	if err != nil {
		return nil, nil, err
	}
	return &resp.Time, resp, nil
}
