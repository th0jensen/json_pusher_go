package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
)

type Config struct {
	Method      string
	Email       string
	Password    string
	EndpointURL string
	InputFile   string
}

type LoginResponse struct {
	Token string `json:"token"`
}

func main() {
	config, err := parseFlags()
	if err != nil {
		fmt.Println(err)
		flag.Usage()
		os.Exit(1)
	}

	bearerToken, err := login(config.Email, config.Password, config.EndpointURL)
	if err != nil {
		fmt.Printf("Error logging in: %v\n", err)
		return
	}

	file, err := os.Open(config.InputFile)
	if err != nil {
		fmt.Printf("Error opening file: %v\n", err)
		return
	}
	defer file.Close()

	decoder := json.NewDecoder(file)

	_, err = decoder.Token()
	if err != nil {
		fmt.Printf("Error reading opening bracket: %v\n", err)
		return
	}

	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 10)
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
	email := flag.String("email", "", "Email for login")
	password := flag.String("password", "", "Password for login")
	endpointURL := flag.String("url", "", "Endpoint URL")
	inputFile := flag.String("input", "", "Path to the JSON input file")

	flag.Parse()

	config := Config{
		Method:      *method,
		Email:       *email,
		Password:    *password,
		EndpointURL: *endpointURL,
		InputFile:   *inputFile,
	}

	var missingParams []string

	if config.Method == "" {
		missingParams = append(missingParams, "method")
	} else if config.Method != "POST" && config.Method != "PUT" {
		return Config{}, fmt.Errorf("invalid method: %s. Must be POST or PUT", config.Method)
	}

	if config.Email == "" {
		missingParams = append(missingParams, "email")
	}

	if config.Password == "" {
		missingParams = append(missingParams, "password")
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

func login(email, password string, endpointURL string) (string, error) {
	baseURL, err := url.Parse(endpointURL)
	if err != nil {
		return "", fmt.Errorf("error parsing endpoint URL: %v", err)
	}

	loginURL := baseURL.Scheme + "://" + baseURL.Host + "/users/login"

	loginData := map[string]string{
		"email":    email,
		"password": password,
	}
	jsonData, err := json.Marshal(loginData)
	if err != nil {
		return "", fmt.Errorf("error marshaling login data: %v", err)
	}

	resp, err := http.Post(loginURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("error sending login request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("login failed with status code: %d", resp.StatusCode)
	}

	var loginResp LoginResponse
	err = json.NewDecoder(resp.Body).Decode(&loginResp)
	if err != nil {
		return "", fmt.Errorf("error decoding login response: %v", err)
	}

	return loginResp.Token, nil
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
