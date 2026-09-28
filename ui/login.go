//go:build windows

package ui

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows/managers"
)

const cloudHostname = "https://app.pangolin.net"

// Steps of the add-account sheet in Preferences > Accounts, as in the macOS
// app's AddAccountSheet.
const (
	loginStepServer   = "server"
	loginStepStarting = "starting"
	// loginStepLoading shows while renewing, before the code arrives.
	loginStepLoading = "loading"
	loginStepCode    = "code"
	loginStepSuccess = "success"
)

// LoginView is what the add-account sheet renders.
type LoginView struct {
	// Session identifies the sheet's session, for Close.
	Session int    `json:"session"`
	Step    string `json:"step"`
	// Host is the server's host name, for "Contacting …" and the code prompt.
	Host string `json:"host"`
	Code string `json:"code"`
	// Email is shown once logged in.
	Email string `json:"email"`
	// Error is shown on the server step after a failed login.
	Error string `json:"error"`
	// Renewing is true when logging in again to an existing account.
	Renewing bool `json:"renewing"`
	// FixedHost hides the server choice and Back: renewing, or a server set by
	// the administrator.
	FixedHost bool `json:"fixedHost"`
	// SelfHosted and ServerURL preset the server step.
	SelfHosted bool   `json:"selfHosted"`
	ServerURL  string `json:"serverUrl"`
}

// loginSession is one opening of the add-account sheet.
type loginSession struct {
	id         int
	renewing   bool
	fixedHost  bool
	renewEmail string
	selfHosted bool
	serverURL  string

	// hostname is the server being logged into.
	hostname      string
	loggingIn     bool
	succeeded     bool
	openedBrowser bool
	err           string
	email         string
	cancel        context.CancelFunc
	closed        bool
}

var (
	loginStateMu sync.Mutex
	login        *loginSession
	loginLastID  int
)

// view must be called with loginStateMu held.
func (s *loginSession) view() LoginView {
	v := LoginView{
		Session:    s.id,
		Host:       displayHost(s.hostname),
		Error:      s.err,
		Email:      s.email,
		Renewing:   s.renewing,
		FixedHost:  s.fixedHost,
		SelfHosted: s.selfHosted,
		ServerURL:  s.serverURL,
	}
	var code *string
	if authManager != nil {
		code = authManager.DeviceAuthCode()
	}
	switch {
	case s.succeeded:
		v.Step = loginStepSuccess
	case !s.loggingIn:
		v.Step = loginStepServer
	case code != nil:
		v.Step = loginStepCode
		v.Code = *code
	case s.renewing:
		v.Step = loginStepLoading
	default:
		v.Step = loginStepStarting
	}
	return v
}

// displayHost is the host of a server URL, or "the server" without one.
func displayHost(hostname string) string {
	if u, err := url.Parse(hostname); err == nil && u.Host != "" {
		return u.Host
	}
	return "the server"
}

func withAuthPath(u string) string {
	if configManager == nil {
		return u
	}
	return appendAuthPathToURL(u, configManager.GetAuthPath())
}

func emitLoginState() {
	app.Event.Emit(eventLoginState, currentLoginView())
}

func currentLoginView() LoginView {
	loginStateMu.Lock()
	defer loginStateMu.Unlock()
	if login == nil {
		return LoginView{Step: loginStepServer}
	}
	return login.view()
}

// withSession runs fn on s if it is still the open session, then emits the
// new state.
func withSession(s *loginSession, fn func()) bool {
	loginStateMu.Lock()
	if login != s || s.closed {
		loginStateMu.Unlock()
		return false
	}
	fn()
	loginStateMu.Unlock()
	emitLoginState()
	return true
}

func activeSession() *loginSession {
	loginStateMu.Lock()
	defer loginStateMu.Unlock()
	if login == nil || login.closed {
		return nil
	}
	return login
}

// loginOpen starts a session for a newly shown sheet. renewHostname is the
// server of an account to log in to again, or "" to add an account. Renewing,
// or a server set by the administrator, starts the login right away.
func loginOpen(renewHostname string) LoginView {
	loginEnd()

	s := &loginSession{}
	if accountManager != nil {
		if active, _ := accountManager.ActiveAccount(); active != nil && active.Hostname != cloudHostname {
			// Offer the current server for another self-hosted account.
			s.serverURL = active.Hostname
		}
	}
	autoStart := ""
	if h := normalizeURL(renewHostname); h != "" {
		s.renewing = true
		s.fixedHost = true
		s.renewEmail = accountEmailForHost(h)
		autoStart = h
	} else if configManager != nil {
		if h := normalizeURL(configManager.GetDefaultServerURL()); h != "" {
			s.fixedHost = true
			autoStart = h
		}
	}
	if autoStart != "" {
		s.selfHosted = autoStart != cloudHostname
		if s.selfHosted {
			s.serverURL = autoStart
		}
	}

	loginStateMu.Lock()
	loginLastID++
	s.id = loginLastID
	login = s
	loginStateMu.Unlock()

	if autoStart != "" {
		loginStart(autoStart)
	}
	return currentLoginView()
}

// accountEmailForHost is the email of the account being renewed: the active
// account when it is on that server, otherwise any account on it.
func accountEmailForHost(hostname string) string {
	if accountManager == nil {
		return ""
	}
	if active, _ := accountManager.ActiveAccount(); active != nil && normalizeURL(active.Hostname) == hostname {
		return active.Email
	}
	for _, a := range accountManager.Accounts {
		if normalizeURL(a.Hostname) == hostname {
			return a.Email
		}
	}
	return ""
}

// loginStart logs in to hostname, replacing any login in progress.
func loginStart(hostname string) {
	hostname = normalizeURL(hostname)
	s := activeSession()
	if s == nil || hostname == "" || authManager == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	ok := withSession(s, func() {
		if s.cancel != nil {
			s.cancel()
		}
		s.cancel = cancel
		s.hostname = hostname
		s.err = ""
		s.openedBrowser = false
		s.loggingIn = true
	})
	if !ok {
		cancel()
		return
	}
	go watchDeviceCode(s, ctx)
	go performLogin(s, ctx, hostname)
}

// watchDeviceCode shows the code once the server returns it and opens the
// browser for it, once per login.
func watchDeviceCode(s *loginSession, ctx context.Context) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var shown string
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		code := ""
		if c := authManager.DeviceAuthCode(); c != nil {
			code = *c
		}
		if code == shown {
			continue
		}
		shown = code
		var open string
		withSession(s, func() {
			if code == "" || s.openedBrowser {
				return
			}
			s.openedBrowser = true
			u := fmt.Sprintf("%s/auth/login/device?code=%s", s.hostname, strings.ReplaceAll(code, "-", ""))
			if s.renewing && s.renewEmail != "" {
				u += "&user=" + url.QueryEscape(s.renewEmail)
			}
			open = withAuthPath(u)
		})
		if open != "" {
			openURL(open)
		}
	}
}

func performLogin(s *loginSession, ctx context.Context, hostname string) {
	err := authManager.LoginWithDeviceAuth(ctx, &hostname)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		logger.Error("Login failed: %v", err)
		// Back to the server choice, with the error shown there.
		withSession(s, func() {
			s.loggingIn = false
			s.err = err.Error()
		})
		return
	}

	// Always stop any running tunnel after login.
	logger.Info("Stopping tunnel after successful login")
	if err := managers.IPCClientStopTunnel(); err != nil {
		logger.Error("Failed to stop tunnel after login: %v", err)
	}
	withSession(s, func() {
		s.loggingIn = false
		s.succeeded = true
		if user := authManager.CurrentUser(); user != nil {
			s.email = user.Email
		}
	})
	publish()
}

// loginBack cancels the login and returns to the server choice.
func loginBack() {
	s := activeSession()
	if s == nil {
		return
	}
	withSession(s, func() {
		if s.cancel != nil {
			s.cancel()
			s.cancel = nil
		}
		s.loggingIn = false
		s.err = ""
	})
	if authManager != nil {
		authManager.ClearDeviceAuth()
	}
}

// loginClose ends the sheet's session, if it is still the current one.
func loginClose(session int) {
	loginStateMu.Lock()
	current := login != nil && login.id == session
	loginStateMu.Unlock()
	if current {
		loginEnd()
	}
}

// loginEnd closes the session, cancelling a login that hasn't finished.
func loginEnd() {
	loginStateMu.Lock()
	s := login
	if s == nil || s.closed {
		loginStateMu.Unlock()
		return
	}
	s.closed = true
	if s.cancel != nil {
		s.cancel()
	}
	succeeded := s.succeeded
	loginStateMu.Unlock()

	if !succeeded && authManager != nil {
		authManager.ClearDeviceAuth()
	}
	publish()
}

func loginCopyCode() {
	if authManager == nil {
		return
	}
	if code := authManager.DeviceAuthCode(); code != nil {
		app.Clipboard.SetText(*code)
	}
}

// loginOpenBrowser opens the device login page again, without the code.
func loginOpenBrowser() {
	if authManager != nil {
		if u := authManager.DeviceAuthLoginURL(); u != nil {
			openURL(withAuthPath(*u))
			return
		}
	}
	loginStateMu.Lock()
	var hostname string
	if login != nil && !login.closed {
		hostname = login.hostname
	}
	loginStateMu.Unlock()
	if hostname != "" {
		openURL(withAuthPath(hostname + "/auth/login/device"))
	}
}
