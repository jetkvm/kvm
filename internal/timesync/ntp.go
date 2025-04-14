package timesync

import (
	"math/rand/v2"
	"time"

	"github.com/beevik/ntp"
)

func (t *TimeSync) queryNetworkTime() (now *time.Time) {
	chunkSize := 4
	ntpServers := t.ntpServers

	// shuffle the ntp servers to avoid always querying the same servers
	rand.Shuffle(len(ntpServers), func(i, j int) { ntpServers[i], ntpServers[j] = ntpServers[j], ntpServers[i] })

	for i := 0; i < len(ntpServers); i += chunkSize {
		chunk := ntpServers[i:min(i+chunkSize, len(ntpServers))]
		results := t.queryMultipleNTP(chunk, timeSyncTimeout)
		if results != nil {
			return results
		}
	}

	return nil
}

func (t *TimeSync) queryMultipleNTP(servers []string, timeout time.Duration) (now *time.Time) {
	results := make(chan *time.Time, len(servers))

	for _, server := range servers {
		go func(server string) {
			scopedLogger := t.l.With().
				Str("server", server).
				Logger()

			now, err, response := queryNtpServer(server, timeout)

			if err == nil {
				scopedLogger.Info().
					Str("time", now.Format(time.RFC3339)).
					Str("reference", response.ReferenceString()).
					Str("rtt", response.RTT.String()).
					Str("clockOffset", response.ClockOffset.String()).
					Uint8("stratum", response.Stratum).
					Msg("NTP server returned time")
				results <- now
			} else {
				scopedLogger.Warn().
					Str("error", err.Error()).
					Msg("failed to query NTP server")
			}
		}(server)
	}

	return <-results
}

func queryNtpServer(server string, timeout time.Duration) (now *time.Time, err error, response *ntp.Response) {
	resp, err := ntp.QueryWithOptions(server, ntp.QueryOptions{Timeout: timeout})
	if err != nil {
		return nil, err, nil
	}
	return &resp.Time, nil, resp
}
