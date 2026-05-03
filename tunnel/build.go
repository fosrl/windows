//go:build windows

package tunnel

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/fosrl/newt/logger"

	olmpkg "github.com/fosrl/olm/olm"
	configpkg "github.com/fosrl/windows/config"
	"github.com/fosrl/windows/fingerprint"
	"github.com/fosrl/windows/version"
)

// buildTunnel builds the tunnel
func (s *tunnelService) buildTunnel(config Config) error {
	logger.Debug("Build tunnel called: config: %+v", config)

	// Create context for OLM
	olmContext := context.Background()

	// Create OLM GlobalConfig with values derived from system config
	olmInitConfig := olmpkg.OlmConfig{
		LogLevel:   configpkg.GetSystemLogLevel(),
		EnableAPI:  true,
		SocketPath: OLMNamedPipePath,
		Version:    version.Number,
		Agent:      "Pangolin Windows",
		OnConnected: func() {
			logger.Info("Tunnel: OLM connected")
		},
		OnRegistered: func() {
			logger.Info("Tunnel: OLM registered")
		},
		OnTerminated: func() {
			logger.Info("Tunnel: OLM terminated")
		},
	}

	// Initialize OLM with context and GlobalConfig
	var err error
	s.olm, err = olmpkg.Init(olmContext, olmInitConfig)
	if err != nil {
		return err
	}

	var fp map[string]any
	var postures map[string]any
	cacheOK := false
	if len(config.InitialFingerprint) > 0 && len(config.InitialPostures) > 0 {
		if err := json.Unmarshal(config.InitialFingerprint, &fp); err == nil {
			if err := json.Unmarshal(config.InitialPostures, &postures); err == nil {
				cacheOK = len(fp) > 0 && len(postures) > 0
			}
		}
	}
	if !cacheOK {
		var gatherWg sync.WaitGroup
		gatherWg.Add(2)
		go func() {
			defer gatherWg.Done()
			fp = fingerprint.GatherFingerprintInfo().ToMap()
		}()
		go func() {
			defer gatherWg.Done()
			postures = fingerprint.GatherPostureChecks().ToMap()
		}()
		gatherWg.Wait()
	}

	olmConfig := olmpkg.TunnelConfig{
		Endpoint:             config.Endpoint,
		ID:                   config.ID,
		Secret:               config.Secret,
		MTU:                  config.MTU,
		DNS:                  config.DNS,
		Holepunch:            config.Holepunch,
		PingIntervalDuration: time.Duration(config.PingIntervalSeconds) * time.Second,
		PingTimeoutDuration:  time.Duration(config.PingTimeoutSeconds) * time.Second,
		UserToken:            config.UserToken,
		OrgID:                config.OrgID,
		InterfaceName:        config.InterfaceName,
		UpstreamDNS:          config.UpstreamDNS,
		OverrideDNS:          config.OverrideDNS,
		TunnelDNS:            config.TunnelDNS,
		InitialFingerprint:   fp,
		InitialPostures:      postures,
	}

	s.fingerprintCtx, s.fingerprintCancel = context.WithCancel(context.Background())

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		calcFingerprint := func() {
			fp := fingerprint.GatherFingerprintInfo().ToMap()
			postures := fingerprint.GatherPostureChecks().ToMap()

			s.olm.SetFingerprint(fp)
			s.olm.SetPostures(postures)
		}

		calcFingerprint()

		for {
			select {
			case <-s.fingerprintCtx.Done():
				return
			case <-ticker.C:
				calcFingerprint()
			}
		}
	}()

	s.olm.StartApi()

	logger.Info("Starting OLM tunnel...")
	go func() {
		s.olm.StartTunnel(olmConfig)
		logger.Info("OLM tunnel stopped")
	}()

	logger.Debug("Build tunnel completed successfully")
	return nil
}
