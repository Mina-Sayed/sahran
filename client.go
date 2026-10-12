package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

var httpc = &http.Client{Timeout: 4 * time.Second}

func baseURL() string { return fmt.Sprintf("http://127.0.0.1:%d", loadConfig().Port) }

// api talks to the running daemon.
func api(method, path string, body, out any) error {
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, baseURL()+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("X-Sahran", "1")
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpc.Do(req)
	if err != nil {
		return fmt.Errorf("sahran is not running (start it with: systemctl --user start sahran)")
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&e)
		if e.Error == "" {
			e.Error = resp.Status // non-JSON rejection (e.g. from a proxy)
		}
		return fmt.Errorf("daemon said: %s", e.Error)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func fetchStatus() (Status, error) {
	var s Status
	err := api("GET", "/api/status", nil, &s)
	return s, err
}
