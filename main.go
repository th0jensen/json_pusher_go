package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
)

type Config struct {
	Method      string
	TokenFile   string
	EndpointURL string
	InputFile   string
}

func main() {
	config, err := parseFlags()
	if err != nil {
		fmt.Println(err)
		flag.Usage()
		os.Exit(1)
	}

	bearerToken, err := readTokenFile(config.TokenFile)
	if err != nil {
		fmt.Printf("Error reading bearer token file: %v\n", err)
		return
	}

	file, err := os.Open(config.InputFile)
	if err != nil {
		fmt.Printf("Error opening file: %v\n", err)
		return
	}
	defer file.Close()

	decoder := json.NewDecoder(file)

	// Read opening bracket
	_, err = decoder.Token()
	if err != nil {
		fmt.Printf("Error reading opening bracket: %v\n", err)
		return
	}

	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 10) // Limit concurrent requests
	var successCount, failCount int64

	for decoder.More() {
		var item json.RawMessage
		if err := decoder.Decode(&item); err != nil {
			fmt.Printf("Error decoding item: %v\n", err)
			continue
		}

		wg.Add(1)
		semaphore <- struct{}{}
		go func(data json.RawMessage) {
			defer wg.Done()
			defer func() { <-semaphore }()
			if sendRequest(data, bearerToken, config) {
				atomic.AddInt64(&successCount, 1)
			} else {
				atomic.AddInt64(&failCount, 1)
			}
		}(item)
	}

	wg.Wait()

	fmt.Printf("\nExecution Summary:\n")
	fmt.Printf("Successful requests: %d\n", successCount)
	fmt.Printf("Failed requests: %d\n", failCount)
}

func parseFlags() (Config, error) {
	method := flag.String("method", "", "HTTP method (POST or PUT)")
	tokenFile := flag.String("token", "", "Path to the bearer token file")
	endpointURL := flag.String("url", "", "Endpoint URL")
	inputFile := flag.String("input", "", "Path to the JSON input file")

	flag.Parse()

	config := Config{
		Method:      *method,
		TokenFile:   *tokenFile,
		EndpointURL: *endpointURL,
		InputFile:   *inputFile,
	}

	var missingParams []string

	if config.Method == "" {
		missingParams = append(missingParams, "method")
	} else if config.Method != "POST" && config.Method != "PUT" {
		return Config{}, fmt.Errorf("invalid method: %s. Must be POST or PUT", config.Method)
	}

	if config.TokenFile == "" {
		missingParams = append(missingParams, "token")
	}

	if config.EndpointURL == "" {
		missingParams = append(missingParams, "url")
	}

	if config.InputFile == "" {
		missingParams = append(missingParams, "input")
	}

	if len(missingParams) > 0 {
		return Config{}, fmt.Errorf("missing required parameters: %s", strings.Join(missingParams, ", "))
	}

	return config, nil
}

func readTokenFile(tokenFile string) (string, error) {
	tokenBytes, err := os.ReadFile(tokenFile)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(tokenBytes)), nil
}

func sendRequest(data json.RawMessage, bearerToken string, config Config) bool {
	client := &http.Client{}
	req, err := http.NewRequest(config.Method, config.EndpointURL, bytes.NewReader(data))
	if err != nil {
		fmt.Printf("Error creating request: %v\n", err)
		return false
	}

	req.Header.Set("Content-Type", "application/json")
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error sending request: %v\n", err)
		return false
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("Error reading response: %v\n", err)
		return false
	}

	fmt.Printf("Response: %s\n", body)
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}
