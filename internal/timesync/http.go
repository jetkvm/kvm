package timesync

import (
	"context"
	"errors"
	"math/rand"
	"net/http"
	"time"
)

func (t *TimeSync) queryAllHttpTime() (now *time.Time) {
	chunkSize := 4
	httpUrls := t.httpUrls

	// shuffle the http urls to avoid always querying the same servers
	rand.Shuffle(len(httpUrls), func(i, j int) { httpUrls[i], httpUrls[j] = httpUrls[j], httpUrls[i] })

	for i := 0; i < len(httpUrls); i += chunkSize {
		chunk := httpUrls[i:min(i+chunkSize, len(httpUrls))]
		results := t.queryMultipleHttp(chunk, timeSyncTimeout)
		if results != nil {
			return results
		}
	}

	return nil
}

func (t *TimeSync) queryMultipleHttp(urls []string, timeout time.Duration) (now *time.Time) {
	results := make(chan *time.Time, len(urls))

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	for _, url := range urls {
		go func(url string) {
			scopedLogger := t.l.With().
				Str("http_url", url).
				Logger()

			startTime := time.Now()
			now, err, response := queryHttpTime(
				ctx,
				url,
				timeout,
			)
			duration := time.Since(startTime)

			var status int
			if response != nil {
				status = response.StatusCode
			}

			if err == nil {
				requestId := response.Header.Get("X-Request-Id")
				if requestId != "" {
					requestId = response.Header.Get("X-Msedge-Ref")
				}
				if requestId == "" {
					requestId = response.Header.Get("Cf-Ray")
				}
				scopedLogger.Info().
					Str("time", now.Format(time.RFC3339)).
					Int("status", status).
					Str("request_id", requestId).
					Str("time_taken", duration.String()).
					Msg("HTTP server returned time")

				cancel()
				results <- now
			} else if !errors.Is(err, context.Canceled) {
				scopedLogger.Warn().
					Str("error", err.Error()).
					Int("status", status).
					Msg("failed to query HTTP server")
			}
		}(url)
	}

	return <-results
}

func queryHttpTime(
	ctx context.Context,
	url string,
	timeout time.Duration,
) (now *time.Time, err error, response *http.Response) {
	client := http.Client{
		Timeout: timeout,
	}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err, nil
	}
	resp, err := client.Do(req)
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
