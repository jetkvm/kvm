package timesync

import (
	"net/http"
	"time"
)

func queryHttpTime(
	url string,
	timeout time.Duration,
) (now *time.Time, err error, response *http.Response) {
	client := http.Client{
		Timeout: timeout,
	}
	resp, err := client.Head(url)
	if err != nil {
		return nil, err, nil
	}
	dateStr := resp.Header.Get("Date")
	parsedTime, err := time.Parse(time.RFC1123, dateStr)
	if err != nil {
		return nil, err, resp
	}
	return &parsedTime, nil, resp
}

func (t *TimeSync) queryAllHttpTime() (now *time.Time) {
	for _, url := range t.httpUrls {
		now, err, response := queryHttpTime(url, timeSyncTimeout)

		var status string
		if response != nil {
			status = response.Status
		}

		scopedLogger := t.l.With().
			Str("http_url", url).
			Str("status", status).
			Logger()

		if err == nil {
			scopedLogger.Info().
				Str("time", now.Format(time.RFC3339)).
				Msg("HTTP server returned time")
			return now
		} else {
			scopedLogger.Error().
				Str("error", err.Error()).
				Msg("failed to query HTTP server")
		}
	}

	return nil
}
