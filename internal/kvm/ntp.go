package kvm

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"time"

	"github.com/beevik/ntp"
)

var timeSynced = false

func TimeSyncLoop() {
	for {
		fmt.Println("Syncing system time")
		start := time.Now()
		err := SyncSystemTime()
		if err != nil {
			log.Printf("Failed to sync system time: %v", err)
			continue
		}
		log.Printf("Time sync successful, now is: %v, time taken: %v", time.Now(), time.Since(start))
		timeSynced = true
		time.Sleep(1 * time.Hour) //once the first sync is done, sync every hour
	}
}

func SyncSystemTime() (err error) {
	now, err := queryNetworkTime()
	if err != nil {
		return fmt.Errorf("failed to query network time: %w", err)
	}
	err = setSystemTime(*now)
	if err != nil {
		return fmt.Errorf("failed to set system time: %w", err)
	}
	return nil
}

func queryNetworkTime() (*time.Time, error) {
	ntpServers := []string{
		"time.cloudflare.com",
		"time.apple.com",
	}
	for _, server := range ntpServers {
		now, err := queryNtpServer(server, 2*time.Second)
		if err == nil {
			return now, nil
		}
	}
	httpUrls := []string{
		"http://apple.com",
		"http://cloudflare.com",
	}
	for _, url := range httpUrls {
		now, err := queryHttpTime(url, 2*time.Second)
		if err == nil {
			return now, nil
		}
	}
	return nil, errors.New("failed to query network time")
}

func queryNtpServer(server string, timeout time.Duration) (now *time.Time, err error) {
	resp, err := ntp.QueryWithOptions(server, ntp.QueryOptions{Timeout: timeout})
	if err != nil {
		return nil, err
	}
	return &resp.Time, nil
}

func queryHttpTime(url string, timeout time.Duration) (*time.Time, error) {
	client := http.Client{
		Timeout: timeout,
	}
	resp, err := client.Head(url)
	if err != nil {
		return nil, err
	}
	dateStr := resp.Header.Get("Date")
	now, err := time.Parse(time.RFC1123, dateStr)
	if err != nil {
		return nil, err
	}
	return &now, nil
}

func setSystemTime(now time.Time) error {
	nowStr := now.Format("2006-01-02 15:04:05")
	output, err := exec.Command("date", "-s", nowStr).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to run date -s: %w, %s", err, string(output))
	}
	return nil
}
