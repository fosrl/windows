package fingerprint

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"runtime"
	"strings"
)

type Fingerprint struct {
	Username            string `json:"username"`
	Hostname            string `json:"hostname"`
	Platform            string `json:"platform"`
	OSVersion           string `json:"osVersion"`
	KernelVersion       string `json:"kernelVersion"`
	Architecture        string `json:"arch"`
	DeviceModel         string `json:"deviceModel"`
	SerialNumber        string `json:"serialNumber"`
	PlatformFingerprint string `json:"platformFingerprint"`
}

func GatherFingerprintInfo() *Fingerprint {
	fp, _ := gatherDevicePosture()
	return fp
}

func GatherPostureChecks() *PostureChecks {
	_, postures := gatherDevicePosture()
	return postures
}

func (p *Fingerprint) ToMap() map[string]any {
	b, err := json.Marshal(p)
	if err != nil {
		return map[string]any{}
	}

	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return map[string]any{}
	}

	return m
}

func (p *PostureChecks) ToMap() map[string]any {
	b, err := json.Marshal(p)
	if err != nil {
		return map[string]any{}
	}

	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return map[string]any{}
	}

	return m
}

func computePlatformFingerprint(serialNumber string) string {
	parts := []string{
		runtime.GOOS,
		runtime.GOARCH,
		cpuFingerprint(),
		dmiFingerprint(),
	}

	if serialNumber != "" {
		parts = append(parts, "serial="+normalize(serialNumber))
	}

	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}

	raw := strings.Join(out, "|")
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

func isUsefulSerial(serial string) bool {
	serial = strings.TrimSpace(serial)
	if serial == "" {
		return false
	}

	switch strings.ToLower(serial) {
	case "to be filled by o.e.m.",
		"default string",
		"none",
		"not specified",
		"system serial number",
		"0123456789",
		"123456789",
		"00000000":
		return false
	}

	return true
}

func normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.Join(strings.Fields(s), " ")
}
