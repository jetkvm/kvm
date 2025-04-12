package timesync

import (
	"time"

	"github.com/beevik/ntp"
)

func (t *TimeSync) queryNetworkTime() (now *time.Time) {
	for _, server := range t.ntpServers {
		now, err, response := queryNtpServer(server, timeSyncTimeout)

		scopedLogger := t.l.With().
			Str("server", server).
			Logger()

		if err == nil {
			scopedLogger.Info().
				Str("time", now.Format(time.RFC3339)).
				Str("reference", response.ReferenceString()).
				Str("rtt", response.RTT.String()).
				Str("clockOffset", response.ClockOffset.String()).
				Uint8("stratum", response.Stratum).
				Msg("NTP server returned time")
			return now
		} else {
			scopedLogger.Error().
				Str("error", err.Error()).
				Msg("failed to query NTP server")
		}
	}

	return nil
}

func queryNtpServer(server string, timeout time.Duration) (now *time.Time, err error, response *ntp.Response) {
	resp, err := ntp.QueryWithOptions(server, ntp.QueryOptions{Timeout: timeout})
	if err != nil {
		return nil, err, nil
	}
	return &resp.Time, nil, resp
}
