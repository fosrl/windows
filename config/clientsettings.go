//go:build windows

package config

// ClientSettings is the server's GET /org/:orgId/my-client-settings response. A
// nil/empty field isn't managed by the server.
type ClientSettings struct {
	OverrideDNS             *bool    `json:"overrideDns,omitempty"`
	MTU                     *int     `json:"mtu,omitempty"`
	TunnelDNS               *bool    `json:"tunnelDns,omitempty"`
	PrimaryDNS              string   `json:"primaryDns,omitempty"`
	SecondaryDNS            string   `json:"secondaryDns,omitempty"`
	ExitNodeTakesPrecedence *bool    `json:"exitNodeTakesPrecedence,omitempty"`
	ExitNodeSiteResourceID  int      `json:"exitNodeSiteResourceId,omitempty"`
	ExitNodeLocked          bool     `json:"exitNodeLocked,omitempty"`
	MatchDomains            []string `json:"matchDomains,omitempty"`
	PreferLocalRoutes       *bool    `json:"preferLocalRoutes,omitempty"`
}

// LockedExitNode returns the exit node the server forces, or 0 if it forces
// none.
func (s *ClientSettings) LockedExitNode() int {
	if s == nil || !s.ExitNodeLocked {
		return 0
	}
	return s.ExitNodeSiteResourceID
}

// ManagesUpstreamDNS reports whether the server sets either upstream
// DNS server. They are managed together, since they form one list.
func (s *ClientSettings) ManagesUpstreamDNS() bool {
	return s != nil && (s.PrimaryDNS != "" || s.SecondaryDNS != "")
}

// OrgConfig is the "orgs" section of pangolin.json: the settings of Config that
// can be set per org, plus ExitNodeResourceID. An exit node only means something
// within one org, so it has no global equivalent.
type OrgConfig struct {
	DNSOverride             *bool    `json:"dnsOverride,omitempty"`
	DNSTunnel               *bool    `json:"dnsTunnel,omitempty"`
	PrimaryDNS              *string  `json:"primaryDNS,omitempty"`
	SecondaryDNS            *string  `json:"secondaryDNS,omitempty"`
	MatchDomains            []string `json:"dnsMatchDomains,omitempty"`
	MTU                     *int     `json:"mtu,omitempty"`
	PreferLocalRoutes       *bool    `json:"preferLocalRoutes,omitempty"`
	ExitNodeTakesPrecedence *bool    `json:"exitNodeTakesPrecedence,omitempty"`
	ExitNodeResourceID      *int     `json:"exitNodeResourceId,omitempty"`
}

// IsEmpty reports whether the org overrides nothing.
func (o OrgConfig) IsEmpty() bool {
	return o.DNSOverride == nil && o.DNSTunnel == nil && o.PrimaryDNS == nil &&
		o.SecondaryDNS == nil && len(o.MatchDomains) == 0 && o.MTU == nil &&
		o.PreferLocalRoutes == nil && o.ExitNodeTakesPrecedence == nil &&
		o.ExitNodeResourceID == nil
}

// mergeOrgConfig overlays the fields set in override onto base.
func mergeOrgConfig(base, override OrgConfig) OrgConfig {
	if override.DNSOverride != nil {
		base.DNSOverride = override.DNSOverride
	}
	if override.DNSTunnel != nil {
		base.DNSTunnel = override.DNSTunnel
	}
	if override.PrimaryDNS != nil {
		base.PrimaryDNS = override.PrimaryDNS
	}
	if override.SecondaryDNS != nil {
		base.SecondaryDNS = override.SecondaryDNS
	}
	if len(override.MatchDomains) > 0 {
		base.MatchDomains = override.MatchDomains
	}
	if override.MTU != nil {
		base.MTU = override.MTU
	}
	if override.PreferLocalRoutes != nil {
		base.PreferLocalRoutes = override.PreferLocalRoutes
	}
	if override.ExitNodeTakesPrecedence != nil {
		base.ExitNodeTakesPrecedence = override.ExitNodeTakesPrecedence
	}
	if override.ExitNodeResourceID != nil {
		base.ExitNodeResourceID = override.ExitNodeResourceID
	}
	return base
}

// TunnelSettings are the settings a tunnel to an org runs with, after every
// layer is applied.
type TunnelSettings struct {
	DNSOverride             bool
	DNSTunnel               bool
	PrimaryDNS              string
	SecondaryDNS            string
	MatchDomains            []string
	MTU                     int
	PreferLocalRoutes       bool
	ExitNodeTakesPrecedence bool
}

// EffectiveSettings resolves the settings for an org. Precedence: what the
// server manages, then the org's section of pangolin.json, then the global
// settings of pangolin.json.
func (cm *ConfigManager) EffectiveSettings(orgID string, server *ClientSettings) TunnelSettings {
	settings := TunnelSettings{
		DNSOverride:             cm.GetDNSOverride(),
		DNSTunnel:               cm.GetDNSTunnel(),
		PrimaryDNS:              cm.GetPrimaryDNS(),
		SecondaryDNS:            cm.GetSecondaryDNS(),
		MatchDomains:            cm.GetMatchDomains(),
		MTU:                     cm.GetMTU(),
		PreferLocalRoutes:       cm.GetPreferLocalRoutes(),
		ExitNodeTakesPrecedence: cm.GetExitNodeTakesPrecedence(),
	}

	org := cm.GetOrgConfig(orgID)
	if org.DNSOverride != nil {
		settings.DNSOverride = *org.DNSOverride
	}
	if org.DNSTunnel != nil {
		settings.DNSTunnel = *org.DNSTunnel
	}
	if org.PrimaryDNS != nil {
		settings.PrimaryDNS = *org.PrimaryDNS
	}
	if org.SecondaryDNS != nil {
		settings.SecondaryDNS = *org.SecondaryDNS
	}
	if len(org.MatchDomains) > 0 {
		settings.MatchDomains = org.MatchDomains
	}
	if org.MTU != nil {
		settings.MTU = *org.MTU
	}
	if org.PreferLocalRoutes != nil {
		settings.PreferLocalRoutes = *org.PreferLocalRoutes
	}
	if org.ExitNodeTakesPrecedence != nil {
		settings.ExitNodeTakesPrecedence = *org.ExitNodeTakesPrecedence
	}

	if server == nil {
		return settings
	}
	if server.OverrideDNS != nil {
		settings.DNSOverride = *server.OverrideDNS
	}
	if server.TunnelDNS != nil {
		settings.DNSTunnel = *server.TunnelDNS
	}
	if server.ManagesUpstreamDNS() {
		settings.PrimaryDNS = server.PrimaryDNS
		settings.SecondaryDNS = server.SecondaryDNS
	}
	if len(server.MatchDomains) > 0 {
		settings.MatchDomains = server.MatchDomains
	}
	if server.MTU != nil {
		settings.MTU = *server.MTU
	}
	if server.PreferLocalRoutes != nil {
		settings.PreferLocalRoutes = *server.PreferLocalRoutes
	}
	if server.ExitNodeTakesPrecedence != nil {
		settings.ExitNodeTakesPrecedence = *server.ExitNodeTakesPrecedence
	}
	return settings
}

// EffectiveExitNode picks the exit node to use for an org: one the server locks
// always wins; next the user's own selection (savedResourceID), then the org's
// section of pangolin.json, and last the server's default.
func EffectiveExitNode(server *ClientSettings, savedResourceID int, org OrgConfig) int {
	if locked := server.LockedExitNode(); locked != 0 {
		return locked
	}
	if savedResourceID != 0 {
		return savedResourceID
	}
	if org.ExitNodeResourceID != nil {
		return *org.ExitNodeResourceID
	}
	if server != nil {
		return server.ExitNodeSiteResourceID
	}
	return 0
}

// GetOrgConfig returns the org's section of pangolin.json, or an empty one.
func (cm *ConfigManager) GetOrgConfig(orgID string) OrgConfig {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.config == nil {
		return OrgConfig{}
	}
	return cm.config.Orgs[orgID]
}

// UpdateOrgConfig overlays the fields set in changes onto the org's section of
// pangolin.json.
func (cm *ConfigManager) UpdateOrgConfig(orgID string, changes OrgConfig) bool {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cfg := cm.getConfigCopy()
	if cfg.Orgs == nil {
		cfg.Orgs = map[string]OrgConfig{}
	}
	cfg.Orgs[orgID] = mergeOrgConfig(cfg.Orgs[orgID], changes)
	return cm.save(cfg)
}

// ResetOrgConfig removes the org's section of pangolin.json, so it uses the
// global settings again.
func (cm *ConfigManager) ResetOrgConfig(orgID string) bool {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cfg := cm.getConfigCopy()
	delete(cfg.Orgs, orgID)
	return cm.save(cfg)
}
