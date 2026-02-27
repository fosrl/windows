//go:build windows

package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows/version"
)

// APIError represents an error from the API client
type APIError struct {
	Type    ErrorType
	Status  int
	Message string
	Err     error
}

type ErrorType int

const (
	ErrorTypeInvalidURL ErrorType = iota
	ErrorTypeInvalidResponse
	ErrorTypeHTTPError
	ErrorTypeNetworkError
	ErrorTypeDecodingError
)

func (e *APIError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	switch e.Type {
	case ErrorTypeInvalidURL:
		return "Invalid URL"
	case ErrorTypeInvalidResponse:
		return "Invalid response from server"
	case ErrorTypeHTTPError:
		if e.Status != 0 {
			return fmt.Sprintf("HTTP error %d", e.Status)
		}
		return "HTTP error"
	case ErrorTypeNetworkError:
		if e.Err != nil {
			return e.Err.Error()
		}
		return "Network error"
	case ErrorTypeDecodingError:
		if e.Err != nil {
			return fmt.Sprintf("Failed to decode response: %v", e.Err)
		}
		return "Failed to decode response"
	default:
		return "Unknown error"
	}
}

func (e *APIError) Unwrap() error {
	return e.Err
}

// APIClient handles HTTP requests to the Pangolin API
type APIClient struct {
	baseURL           string
	sessionToken      string
	sessionCookieName string
	csrfToken         string
	client            *http.Client
	onUnauthorized    func()
}

// NewAPIClient creates a new API client instance
func NewAPIClient(baseURL string, sessionToken string) *APIClient {
	normalizedURL := normalizeBaseURL(baseURL)

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	apiClient := &APIClient{
		baseURL:           normalizedURL,
		sessionToken:      sessionToken,
		sessionCookieName: "p_session_token",
		csrfToken:         "x-csrf-protection",
		client:            client,
	}

	logger.Info("APIClient initialized with baseURL: %s", apiClient.baseURL)
	return apiClient
}

// UpdateBaseURL updates the base URL for the API client
func (c *APIClient) UpdateBaseURL(newBaseURL string) {
	c.baseURL = normalizeBaseURL(newBaseURL)
}

// UpdateSessionToken updates the session token
func (c *APIClient) UpdateSessionToken(token string) {
	c.sessionToken = token
}

// CurrentBaseURL returns the current base URL
func (c *APIClient) CurrentBaseURL() string {
	return c.baseURL
}

// SetOnUnauthorized sets the callback invoked when a request sent with a session token returns 401 or 403.
func (c *APIClient) SetOnUnauthorized(fn func()) {
	c.onUnauthorized = fn
}

// normalizeBaseURL normalizes a base URL string
func normalizeBaseURL(urlStr string) string {
	normalized := strings.TrimSpace(urlStr)

	// If empty, return default
	if normalized == "" {
		return "https://app.pangolin.net"
	}

	// Add https:// if no scheme
	if !strings.HasPrefix(normalized, "http://") && !strings.HasPrefix(normalized, "https://") {
		normalized = "https://" + normalized
	}

	// Remove trailing slashes
	normalized = strings.TrimRight(normalized, "/")

	return normalized
}

// apiURL constructs the full API URL for a given path
func (c *APIClient) apiURL(path string) (string, error) {
	// Ensure path starts with /
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	// Add /api/v1 prefix
	apiPath := "/api/v1" + path

	// Construct full URL
	fullURL := c.baseURL + apiPath

	// Validate URL
	_, err := url.Parse(fullURL)
	if err != nil {
		logger.Error("Error: Invalid URL constructed: %s (baseURL: %s, path: %s)", fullURL, c.baseURL, path)
		return "", &APIError{Type: ErrorTypeInvalidURL}
	}

	return fullURL, nil
}

// makeRequest makes an HTTP request and returns the response data and status
func (c *APIClient) makeRequest(method, path string, body []byte) ([]byte, *http.Response, error) {
	fullURL, err := c.apiURL(path)
	if err != nil {
		return nil, nil, err
	}

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequest(method, fullURL, bodyReader)
	if err != nil {
		return nil, nil, &APIError{Type: ErrorTypeInvalidURL, Err: err}
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", version.UserAgent())
	req.Header.Set("X-CSRF-Token", c.csrfToken)

	// Add session cookie if available
	if c.sessionToken != "" {
		req.Header.Set("Cookie", fmt.Sprintf("%s=%s", c.sessionCookieName, c.sessionToken))
	}

	logger.Debug("Making request to: %s", fullURL)

	resp, err := c.client.Do(req)
	if err != nil {
		// Handle network errors with more specific messages
		if urlErr, ok := err.(*url.Error); ok {
			if urlErr.Timeout() {
				msg := fmt.Sprintf("Connection to %s timed out. Please check your network connection.", c.baseURL)
				logger.Error("%s", msg)
				return nil, nil, &APIError{Type: ErrorTypeHTTPError, Message: msg, Err: err}
			}
			if urlErr.Temporary() {
				msg := fmt.Sprintf("Temporary network error connecting to %s: %v", c.baseURL, urlErr)
				logger.Error("%s", msg)
				return nil, nil, &APIError{Type: ErrorTypeNetworkError, Message: msg, Err: err}
			}
		}

		// Generic network error
		logger.Error("Network error: %v", err)
		return nil, nil, &APIError{Type: ErrorTypeNetworkError, Err: err}
	}
	defer resp.Body.Close()

	// Read response body
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Error("Error reading response body: %v", err)
		return nil, resp, &APIError{Type: ErrorTypeInvalidResponse, Err: err}
	}

	// Notify when an authenticated request gets 401/403 so session-expired state can be set
	if (resp.StatusCode == 401 || resp.StatusCode == 403) && c.sessionToken != "" && c.onUnauthorized != nil {
		c.onUnauthorized()
	}

	return data, resp, nil
}

// parseResponse parses the API response and returns the data
func (c *APIClient) parseResponse(data []byte, resp *http.Response, result interface{}) error {
	// Check HTTP status first
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Try to parse error message from response
		var errorResponse APIResponse[EmptyResponse]
		if err := json.Unmarshal(data, &errorResponse); err == nil {
			message := errorResponse.Message
			if message == "" {
				message = getDefaultHTTPErrorMessage(resp.StatusCode)
			}
			return &APIError{
				Type:    ErrorTypeHTTPError,
				Status:  resp.StatusCode,
				Message: message,
			}
		}

		// Fallback to default error message
		return &APIError{
			Type:    ErrorTypeHTTPError,
			Status:  resp.StatusCode,
			Message: getDefaultHTTPErrorMessage(resp.StatusCode),
		}
	}

	// Handle empty responses
	if len(data) == 0 || (len(data) == 2 && string(data) == "{}") {
		// Check if result is EmptyResponse
		if emptyResp, ok := result.(*EmptyResponse); ok {
			*emptyResp = EmptyResponse{}
			return nil
		}
		return &APIError{Type: ErrorTypeInvalidResponse, Message: "Empty response"}
	}

	// Parse API response wrapper
	var apiResponse APIResponse[json.RawMessage]
	if err := json.Unmarshal(data, &apiResponse); err != nil {
		logger.Error("Error parsing API response: %v", err)
		return &APIError{Type: ErrorTypeDecodingError, Err: err}
	}

	// Check API-level success/error flags
	if apiResponse.Success != nil && !*apiResponse.Success {
		message := apiResponse.Message
		if message == "" {
			message = "Request failed"
		}
		status := apiResponse.Status
		if status == 0 {
			status = resp.StatusCode
		}
		return &APIError{
			Type:    ErrorTypeHTTPError,
			Status:  status,
			Message: message,
		}
	}

	if apiResponse.Error != nil && *apiResponse.Error {
		message := apiResponse.Message
		if message == "" {
			message = "Request failed"
		}
		status := apiResponse.Status
		if status == 0 {
			status = resp.StatusCode
		}
		return &APIError{
			Type:    ErrorTypeHTTPError,
			Status:  status,
			Message: message,
		}
	}

	// Extract data from response
	if len(apiResponse.Data) > 0 {
		if err := json.Unmarshal(apiResponse.Data, result); err != nil {
			logger.Error("Error decoding response data: %v", err)
			return &APIError{Type: ErrorTypeDecodingError, Err: err}
		}
	} else {
		// If data is nil but response was successful, try to return EmptyResponse
		if emptyResp, ok := result.(*EmptyResponse); ok {
			*emptyResp = EmptyResponse{}
			return nil
		}
		return &APIError{Type: ErrorTypeInvalidResponse, Message: "No data in response"}
	}

	return nil
}

// getDefaultHTTPErrorMessage returns a default error message for HTTP status codes
func getDefaultHTTPErrorMessage(statusCode int) string {
	switch statusCode {
	case 401, 403:
		return "Unauthorized"
	case 404:
		return "Not found"
	case 429:
		return "Rate limit exceeded"
	case 500:
		return "Internal server error"
	default:
		return fmt.Sprintf("HTTP error %d", statusCode)
	}
}

// extractCookie extracts a cookie value from HTTP response headers
func extractCookie(resp *http.Response, name string) string {
	// Check Set-Cookie headers
	for _, cookie := range resp.Cookies() {
		if cookie.Name == name {
			return cookie.Value
		}
	}

	// Also check Set-Cookie header directly (for cases where cookies aren't parsed)
	setCookie := resp.Header.Get("Set-Cookie")
	if setCookie != "" {
		// Parse cookie string (format: "name=value; Path=/; ...")
		parts := strings.Split(setCookie, ";")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if strings.HasPrefix(part, name+"=") {
				value := strings.TrimPrefix(part, name+"=")
				return value
			}
		}
	}

	// Check for multiple Set-Cookie headers
	for key, values := range resp.Header {
		if strings.ToLower(key) == "set-cookie" {
			for _, value := range values {
				parts := strings.Split(value, ";")
				for _, part := range parts {
					part = strings.TrimSpace(part)
					if strings.HasPrefix(part, name+"=") {
						cookieValue := strings.TrimPrefix(part, name+"=")
						return cookieValue
					}
				}
			}
		}
	}

	return ""
}
