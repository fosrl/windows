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
	"github.com/fosrl/windows/config"
	"github.com/fosrl/windows/managers"
)

const cloudHostname = "https://app.pangolin.net"

type hostingOption int

const (
	hostingNone hostingOption = iota
	hostingCloud
	hostingSelfHosted
)

// Login window stages.
const (
	loginStageHosting = "hosting"
	loginStageURL     = "url"
	loginStageCode    = "code"
	loginStageSuccess = "success"
)

// LoginView is what the login window renders.
type LoginView struct {
	Stage         string `json:"stage"`
	SelfHostedURL string `json:"selfHostedUrl"`
	Code          string `json:"code"`
	ManualURL     string `json:"manualUrl"`
	ShowBack      bool   `json:"showBack"`
	BackEnabled   bool   `json:"backEnabled"`
	ShowLogin     bool   `json:"showLogin"`
	LoginEnabled  bool   `json:"loginEnabled"`
}

// loginSession is the state of one opening of the login window.
type loginSession struct {
	stage              string
	hosting            hostingOption
	selfHostedURL      string
	loggingIn          bool
	autoOpenedBrowser  bool
	succeeded          bool
	includeUserInURL   bool // only when entering from re-auth
	temporaryHostname  string
	activeAccountEmail string
	activeAccountHost  string
	cancelPoll         context.CancelFunc
	loginCtx           context.Context
	cancelLogin        context.CancelFunc
	closed             bool
}

var (
	loginStateMu sync.Mutex
	login        *loginSession
)

func (s *loginSession) readyToLogin() bool {
	switch s.hosting {
	case hostingCloud:
		return true
	case hostingSelfHosted:
		return strings.TrimSpace(s.selfHostedURL) != ""
	}
	return false
}

// view must be called with loginStateMu held.
func (s *loginSession) view() LoginView {
	v := LoginView{
		Stage:         s.stage,
		SelfHostedURL: s.selfHostedURL,
		ShowBack:      s.stage != loginStageHosting && s.stage != loginStageSuccess,
		BackEnabled:   !s.loggingIn,
		ShowLogin:     s.stage == loginStageURL,
		LoginEnabled:  !s.loggingIn && s.readyToLogin(),
	}
	if s.stage == loginStageCode && authManager != nil {
		if code := authManager.DeviceAuthCode(); code != nil {
			// PIN style: spaces between characters.
			v.Code = strings.Join(strings.Split(*code, ""), " ")
		}
		if s.temporaryHostname != "" {
			v.ManualURL = s.withAuthPath(fmt.Sprintf("%s/auth/login/device", s.temporaryHostname))
		}
	}
	return v
}

func (s *loginSession) withAuthPath(u string) string {
	if configManager == nil {
		return u
	}
	return appendAuthPathToURL(u, configManager.GetAuthPath())
}

func emitLoginState() {
	loginStateMu.Lock()
	s := login
	var v LoginView
	if s != nil {
		v = s.view()
	} else {
		v = LoginView{Stage: loginStageHosting}
	}
	loginStateMu.Unlock()
	app.Event.Emit(eventLoginState, v)
}

func currentLoginView() LoginView {
	loginStateMu.Lock()
	defer loginStateMu.Unlock()
	if login == nil {
		return LoginView{Stage: loginStageHosting}
	}
	return login.view()
}

// startLoginFlow begins a new login session for a freshly opened window.
func startLoginFlow() {
	loginStateMu.Lock()
	if login != nil && !login.closed {
		loginStateMu.Unlock()
		return
	}
	s := &loginSession{stage: loginStageHosting, temporaryHostname: config.DefaultHostname}
	if accountManager != nil {
		if active, _ := accountManager.ActiveAccount(); active != nil {
			s.temporaryHostname = active.Hostname
			s.activeAccountHost = active.Hostname
			s.activeAccountEmail = active.Email
		}
	}
	pollCtx, cancelPoll := context.WithCancel(context.Background())
	s.cancelPoll = cancelPoll
	s.loginCtx, s.cancelLogin = context.WithCancel(context.Background())
	login = s
	loginStateMu.Unlock()

	emitLoginState()
	go pollDeviceCode(s, pollCtx)
	go autoStartLogin(s)
}

// endLoginFlow cancels the login and clears device auth unless it succeeded.
func endLoginFlow() {
	loginStateMu.Lock()
	s := login
	if s == nil || s.closed {
		loginStateMu.Unlock()
		return
	}
	s.closed = true
	s.cancelLogin()
	s.cancelPoll()
	succeeded := s.succeeded
	loginStateMu.Unlock()

	if !succeeded && authManager != nil {
		authManager.ClearDeviceAuth()
		authManager.ClearStartDeviceAuthImmediately()
		logger.Info("Cleared device auth state after dialog close")
	}
	logger.Info("Login dialog closed")
}

// withSession runs fn on the active session, ignoring stale ones.
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

// autoStartLogin skips hosting selection when opened from re-auth or when a
// default server URL is configured.
func autoStartLogin(s *loginSession) {
	time.Sleep(150 * time.Millisecond) // let the window become visible
	start := false
	withSession(s, func() {
		if authManager != nil && authManager.StartDeviceAuthImmediately() {
			authManager.ClearStartDeviceAuthImmediately()
			if s.activeAccountHost != "" {
				s.temporaryHostname = s.activeAccountHost
				s.selfHostedURL = s.activeAccountHost
			}
			s.includeUserInURL = true
			s.hosting = hostingSelfHosted
			s.stage = loginStageCode
			s.loggingIn = true
			start = true
			return
		}
		if configManager != nil {
			if u := normalizeURL(configManager.GetDefaultServerURL()); u != "" {
				s.temporaryHostname = u
				s.selfHostedURL = u
				s.hosting = hostingSelfHosted
				s.stage = loginStageCode
				s.loggingIn = true
				start = true
			}
		}
	})
	if start {
		go performLogin(s)
	}
}

// pollDeviceCode refreshes the code display and returns to the previous stage
// when the code is cleared.
func pollDeviceCode(s *loginSession, ctx context.Context) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		var openURLNow string
		withSession(s, func() {
			if s.stage != loginStageCode || authManager == nil {
				return
			}
			code := authManager.DeviceAuthCode()
			if code == nil {
				if !s.loggingIn {
					s.resetAfterFailure()
				}
				return
			}
			// Open the browser once when the code is first generated.
			if !s.autoOpenedBrowser && s.temporaryHostname != "" {
				s.autoOpenedBrowser = true
				u := fmt.Sprintf("%s/auth/login/device?code=%s", s.temporaryHostname, strings.ReplaceAll(*code, "-", ""))
				if s.includeUserInURL {
					if user := authManager.CurrentUser(); user != nil && user.Email != "" {
						u += "&user=" + url.QueryEscape(user.Email)
					} else if s.activeAccountEmail != "" {
						u += "&user=" + url.QueryEscape(s.activeAccountEmail)
					}
				}
				openURLNow = s.withAuthPath(u)
			}
		})
		if openURLNow != "" {
			openURL(openURLNow)
		}
	}
}

// resetAfterFailure returns to hosting selection for cloud, or to URL entry
// for self-hosted so the user can try again. Call with loginStateMu held.
func (s *loginSession) resetAfterFailure() {
	s.autoOpenedBrowser = false
	s.includeUserInURL = false
	if s.hosting == hostingSelfHosted {
		s.stage = loginStageURL
		return
	}
	s.stage = loginStageHosting
	s.hosting = hostingNone
}

func performLogin(s *loginSession) {
	var hostname string
	var ctx context.Context
	emptyURL := false
	withSession(s, func() {
		switch s.hosting {
		case hostingSelfHosted:
			u := normalizeURL(s.selfHostedURL)
			if u == "" {
				emptyURL = true
				s.loggingIn = false
				s.stage = loginStageURL
				return
			}
			s.temporaryHostname = u
		case hostingCloud:
			s.temporaryHostname = cloudHostname
		}
		hostname = s.temporaryHostname
		ctx = s.loginCtx
	})
	if emptyURL {
		showError(loginWindowOrNil(), "Error", "Please enter a server URL.")
		return
	}
	if ctx == nil || authManager == nil {
		return
	}

	err := authManager.LoginWithDeviceAuth(ctx, &hostname)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			withSession(s, func() { s.loggingIn = false })
			return
		}
		withSession(s, func() { s.loggingIn = false })
		if activeSession() == s {
			showError(loginWindowOrNil(), "Login Error", err.Error())
		}
		withSession(s, s.resetAfterFailure)
		return
	}

	// Always stop any running tunnel after login, then close.
	logger.Info("Stopping tunnel after successful login")
	if err := managers.IPCClientStopTunnel(); err != nil {
		logger.Error("Failed to stop tunnel after login: %v", err)
	}
	ok := withSession(s, func() {
		s.loggingIn = false
		s.succeeded = true
		s.stage = loginStageSuccess
	})
	if ok {
		// Show the success screen briefly before closing, like the macOS app.
		time.Sleep(500 * time.Millisecond)
		if activeSession() == s {
			closeLoginWindow()
		}
	}
}

// Login window actions.

func loginChooseCloud() {
	s := activeSession()
	if s == nil {
		return
	}
	if withSession(s, func() {
		s.hosting = hostingCloud
		s.temporaryHostname = cloudHostname
		s.stage = loginStageCode
		s.loggingIn = true
	}) {
		go performLogin(s)
	}
}

func loginChooseSelfHosted() {
	if s := activeSession(); s != nil {
		withSession(s, func() {
			s.hosting = hostingSelfHosted
			s.stage = loginStageURL
		})
	}
}

func loginSetURL(u string) {
	if s := activeSession(); s != nil {
		withSession(s, func() {
			s.selfHostedURL = u
			s.temporaryHostname = normalizeURL(u)
		})
	}
}

func loginSubmit() {
	s := activeSession()
	if s == nil {
		return
	}
	start := false
	withSession(s, func() {
		if s.stage != loginStageURL || s.loggingIn || !s.readyToLogin() {
			return
		}
		s.stage = loginStageCode
		s.loggingIn = true
		start = true
	})
	if start {
		go performLogin(s)
	}
}

func loginBack() {
	s := activeSession()
	if s == nil {
		return
	}
	withSession(s, func() {
		if s.stage == loginStageCode {
			// Cancel the auth flow.
			s.autoOpenedBrowser = false
		} else {
			s.selfHostedURL = ""
		}
		s.stage = loginStageHosting
		s.hosting = hostingNone
	})
}

func loginCopyCode() {
	if authManager == nil {
		return
	}
	if code := authManager.DeviceAuthCode(); code != nil {
		app.Clipboard.SetText(*code)
	}
}

func loginOpenBrowser() {
	if authManager == nil {
		return
	}
	if u := authManager.DeviceAuthLoginURL(); u != nil {
		s := activeSession()
		if s != nil {
			openURL(s.withAuthPath(*u))
		} else {
			openURL(*u)
		}
	}
}
