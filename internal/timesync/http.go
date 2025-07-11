package timesync

import (
	"context"
	"errors"
	"math/rand"
	"net/http"
	"strconv"
	"time"
)

var defaultHTTPUrls = []string{
	"http://www.gstatic.com/generate_204",
	"http://cp.cloudflare.com/",
	"http://edge-http.microsoft.com/captiveportal/generate_204",
	// Firefox, Apple, and Microsoft have inconsistent results, so we don't use it
	// "http://detectportal.firefox.com/",
	// "http://www.apple.com/library/test/success.html",
	// "http://www.msftconnecttest.com/connecttest.txt",
}

func (t *TimeSync) queryAllHttpTime(httpUrls []string) (now *time.Time) {
	chunkSize := int(t.networkConfig.TimeSyncParallel.ValueOr(4))
	t.l.Info().Strs("httpUrls", httpUrls).Int("chunkSize", chunkSize).Msg("querying HTTP URLs")

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

			metricHttpRequestCount.WithLabelValues(url).Inc()
			metricHttpTotalRequestCount.Inc()

			startTime := time.Now()
			now, response, err := queryHttpTime(
				ctx,
				url,
				timeout,
			)
			duration := time.Since(startTime)

			metricHttpServerLastRTT.WithLabelValues(url).Set(float64(duration.Milliseconds()))
			metricHttpServerRttHistogram.WithLabelValues(url).Observe(float64(duration.Milliseconds()))

			status := 0
			if response != nil {
				status = response.StatusCode
			}
			metricHttpServerInfo.WithLabelValues(
				url,
				strconv.Itoa(status),
			).Set(1)

			if err == nil {
				metricHttpTotalSuccessCount.Inc()
				metricHttpSuccessCount.WithLabelValues(url).Inc()

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
			} else if errors.Is(err, context.Canceled) {
				metricHttpCancelCount.WithLabelValues(url).Inc()
				metricHttpTotalCancelCount.Inc()
				results <- nil
			} else {
				scopedLogger.Warn().
					Str("error", err.Error()).
					Int("status", status).
					Msg("failed to query HTTP server")
				results <- nil
			}
		}(url)
	}

	for range urls {
		result := <-results
		if result == nil {
			continue
		}
		now = result
		return
	}

	return
}

func queryHttpTime(
	ctx context.Context,
	url string,
	timeout time.Duration,
) (now *time.Time, response *http.Response, err error) {
	client := http.Client{
		Timeout: timeout,
	}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	dateStr := resp.Header.Get("Date")
	parsedTime, err := time.Parse(time.RFC1123, dateStr)
	if err != nil {
		return nil, nil, err
	}
	return &parsedTime, resp, nil
}
