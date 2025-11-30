//go:build windows

package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/fosrl/newt/logger"
)

// Login authenticates a user with email and password
func (c *APIClient) Login(email, password string, code *string) (*LoginResponse, string, error) {
	requestBody := LoginRequest{
		Email:    email,
		Password: password,
		Code:     code,
	}

	bodyData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, "", &APIError{Type: ErrorTypeDecodingError, Err: err}
	}

	data, resp, err := c.makeRequest("POST", "/auth/login", bodyData)
	if err != nil {
		return nil, "", err
	}

	var loginResponse LoginResponse
	if err := c.parseResponse(data, resp, &loginResponse); err != nil {
		return nil, "", err
	}

	// Extract session token from cookie
	sessionToken := extractCookie(resp, c.sessionCookieName)
	if sessionToken == "" {
		sessionToken = extractCookie(resp, "p_session")
	}

	if sessionToken == "" {
		return nil, "", &APIError{Type: ErrorTypeInvalidResponse, Message: "No session token in response"}
	}

	return &loginResponse, sessionToken, nil
}

// StartDeviceAuth starts a device authentication flow
func (c *APIClient) StartDeviceAuth(applicationName string, deviceName *string) (*DeviceAuthStartResponse, error) {
	requestBody := DeviceAuthStartRequest{
		ApplicationName: applicationName,
		DeviceName:      deviceName,
	}

	bodyData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, &APIError{Type: ErrorTypeDecodingError, Err: err}
	}

	data, resp, err := c.makeRequest("POST", "/auth/device-web-auth/start", bodyData)
	if err != nil {
		return nil, err
	}

	var response DeviceAuthStartResponse
	if err := c.parseResponse(data, resp, &response); err != nil {
		return nil, err
	}

	return &response, nil
}

// PollDeviceAuth polls for device authentication status
func (c *APIClient) PollDeviceAuth(code string) (*DeviceAuthPollResponse, *string, error) {
	path := fmt.Sprintf("/auth/device-web-auth/poll/%s", code)
	data, resp, err := c.makeRequest("GET", path, nil)
	if err != nil {
		return nil, nil, err
	}

	var pollResponse DeviceAuthPollResponse
	if err := c.parseResponse(data, resp, &pollResponse); err != nil {
		return nil, nil, err
	}

	// Extract token if verified
	var sessionToken *string
	if pollResponse.Verified && pollResponse.Token != nil {
		sessionToken = pollResponse.Token
	} else {
		// Also try to extract from cookie
		token := extractCookie(resp, c.sessionCookieName)
		if token != "" {
			sessionToken = &token
		}
	}

	return &pollResponse, sessionToken, nil
}

// Logout logs out the current user
func (c *APIClient) Logout() error {
	data, resp, err := c.makeRequest("POST", "/auth/logout", []byte("{}"))
	if err != nil {
		return err
	}

	var emptyResponse EmptyResponse
	return c.parseResponse(data, resp, &emptyResponse)
}

// GetUser gets the current user information
func (c *APIClient) GetUser() (*User, error) {
	data, resp, err := c.makeRequest("GET", "/user", nil)
	if err != nil {
		return nil, err
	}

	var user User
	if err := c.parseResponse(data, resp, &user); err != nil {
		return nil, err
	}

	return &user, nil
}

// ListUserOrgs lists organizations for a user
func (c *APIClient) ListUserOrgs(userId string) (*ListUserOrgsResponse, error) {
	path := fmt.Sprintf("/user/%s/orgs", userId)
	data, resp, err := c.makeRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}

	var response ListUserOrgsResponse
	if err := c.parseResponse(data, resp, &response); err != nil {
		return nil, err
	}

	return &response, nil
}

// CreateOlm creates an OLM for a user
func (c *APIClient) CreateOlm(userId, name string) (*CreateOlmResponse, error) {
	requestBody := CreateOlmRequest{
		Name: name,
	}

	bodyData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, &APIError{Type: ErrorTypeDecodingError, Err: err}
	}

	path := fmt.Sprintf("/user/%s/olm", userId)
	data, resp, err := c.makeRequest("PUT", path, bodyData)
	if err != nil {
		return nil, err
	}

	var response CreateOlmResponse
	if err := c.parseResponse(data, resp, &response); err != nil {
		return nil, err
	}

	return &response, nil
}

// GetUserOlm gets an OLM for a user by userId and olmId
func (c *APIClient) GetUserOlm(userId, olmId string) (*Olm, error) {
	path := fmt.Sprintf("/user/%s/olm/%s", userId, olmId)
	data, resp, err := c.makeRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}

	var olm Olm
	if err := c.parseResponse(data, resp, &olm); err != nil {
		return nil, err
	}

	return &olm, nil
}

// GetOrg gets an organization by ID
func (c *APIClient) GetOrg(orgId string) (*GetOrgResponse, error) {
	path := fmt.Sprintf("/org/%s", orgId)
	data, resp, err := c.makeRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}

	var response GetOrgResponse
	if err := c.parseResponse(data, resp, &response); err != nil {
		return nil, err
	}

	return &response, nil
}

// CheckOrgUserAccess checks if a user has access to an organization
func (c *APIClient) CheckOrgUserAccess(orgId, userId string) (*CheckOrgUserAccessResponse, error) {
	path := fmt.Sprintf("/org/%s/user/%s/check", orgId, userId)
	data, resp, err := c.makeRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}

	var response CheckOrgUserAccessResponse
	if err := c.parseResponse(data, resp, &response); err != nil {
		return nil, err
	}

	return &response, nil
}

// GetClient gets a client by ID
func (c *APIClient) GetClient(clientId int) (*GetClientResponse, error) {
	path := fmt.Sprintf("/client/%d", clientId)
	data, resp, err := c.makeRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}

	var response GetClientResponse
	if err := c.parseResponse(data, resp, &response); err != nil {
		return nil, err
	}

	return &response, nil
}

// GetMyDevice gets the current device information including user, organizations, and OLM
func (c *APIClient) GetMyDevice(olmId string) (*MyDeviceResponse, error) {
	// Build query parameters
	params := url.Values{}
	params.Set("olmId", olmId)
	path := fmt.Sprintf("/my-device?%s", params.Encode())

	data, resp, err := c.makeRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}

	var response MyDeviceResponse
	if err := c.parseResponse(data, resp, &response); err != nil {
		return nil, err
	}

	return &response, nil
}

// TestConnection tests the connection to the API server
func (c *APIClient) TestConnection() (bool, error) {
	// Create a temporary client with shorter timeout for connection test
	testClient := &http.Client{
		Timeout: 10 * time.Second,
	}

	// Use HEAD request to test connection
	fullURL := c.baseURL
	req, err := http.NewRequest("HEAD", fullURL, nil)
	if err != nil {
		return false, &APIError{Type: ErrorTypeInvalidURL, Err: err}
	}

	req.Header.Set("User-Agent", c.agentName)

	resp, err := testClient.Do(req)
	if err != nil {
		return false, nil // Return false (not an error) if connection fails
	}
	defer resp.Body.Close()

	// Consider 200-299 and 404 as successful connection
	return (resp.StatusCode >= 200 && resp.StatusCode < 300) || resp.StatusCode == 404, nil
}
