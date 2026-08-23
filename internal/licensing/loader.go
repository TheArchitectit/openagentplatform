package licensing

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// LicenseFile represents the on-disk license file format.
type LicenseFile struct {
	// Version is the license file format version.
	Version int `json:"version"`
	// License is the license data.
	License License `json:"license"`
	// Signature is the hex-encoded Ed25519 signature.
	Signature string `json:"signature"`
}

// Loader loads and validates license files from disk.
type Loader struct {
	// validator is the license validator.
	validator *Validator
	// licensePath is the path to the license file.
	licensePath string
}

// NewLoader creates a new license loader.
func NewLoader(validator *Validator, licensePath string) *Loader {
	return &Loader{
		validator:   validator,
		licensePath: licensePath,
	}
}

// Load loads and validates the license file from disk.
func (l *Loader) Load() (*ValidationResult, error) {
	// Read license file
	data, err := os.ReadFile(l.licensePath)
	if err != nil {
		if os.IsNotExist(err) {
			// No license file = community license
			return &ValidationResult{
				Valid:   true,
				License: DefaultCommunityLicense(),
			}, nil
		}
		return nil, fmt.Errorf("failed to read license file: %w", err)
	}

	// Parse license file
	var licenseFile LicenseFile
	if err := json.Unmarshal(data, &licenseFile); err != nil {
		return nil, fmt.Errorf("failed to parse license file: %w", err)
	}

	// Validate version
	if licenseFile.Version != 1 {
		return nil, fmt.Errorf("unsupported license file version: %d", licenseFile.Version)
	}

	// Decode signature from hex
	sigBytes, err := hexDecode(licenseFile.Signature)
	if err != nil {
		return nil, fmt.Errorf("failed to decode signature: %w", err)
	}

	// Create signed license
	signed := &SignedLicense{
		License:   licenseFile.License,
		Signature: sigBytes,
	}

	// Validate
	return l.validator.Validate(signed), nil
}

// LoadFromBytes loads and validates a license from JSON bytes.
func (l *Loader) LoadFromBytes(data []byte) (*ValidationResult, error) {
	var licenseFile LicenseFile
	if err := json.Unmarshal(data, &licenseFile); err != nil {
		return nil, fmt.Errorf("failed to parse license file: %w", err)
	}

	if licenseFile.Version != 1 {
		return nil, fmt.Errorf("unsupported license file version: %d", licenseFile.Version)
	}

	sigBytes, err := hexDecode(licenseFile.Signature)
	if err != nil {
		return nil, fmt.Errorf("failed to decode signature: %w", err)
	}

	signed := &SignedLicense{
		License:   licenseFile.License,
		Signature: sigBytes,
	}

	return l.validator.Validate(signed), nil
}

// LicensePath returns the configured license file path.
func (l *Loader) LicensePath() string {
	return l.licensePath
}

// DefaultLicensePath returns the default license file path.
func DefaultLicensePath() string {
	// Check environment variable first
	if path := os.Getenv("OAP_LICENSE_FILE"); path != "" {
		return path
	}

	// Check current directory
	if _, err := os.Stat("license.json"); err == nil {
		return "license.json"
	}

	// Check config directory
	homeDir, err := os.UserHomeDir()
	if err == nil {
		configPath := filepath.Join(homeDir, ".openagentplatform", "license.json")
		if _, err := os.Stat(configPath); err == nil {
			return configPath
		}
	}

	// Default to current directory
	return "license.json"
}

// hexDecode decodes a hex string to bytes.
func hexDecode(s string) ([]byte, error) {
	// Simple hex decode without importing encoding/hex
	// This avoids an import for a simple operation
	if len(s)%2 != 0 {
		return nil, fmt.Errorf("invalid hex string length")
	}

	result := make([]byte, len(s)/2)
	for i := 0; i < len(s); i += 2 {
		high, err := hexCharToByte(s[i])
		if err != nil {
			return nil, err
		}
		low, err := hexCharToByte(s[i+1])
		if err != nil {
			return nil, err
		}
		result[i/2] = (high << 4) | low
	}
	return result, nil
}

func hexCharToByte(c byte) (byte, error) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', nil
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, nil
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, nil
	default:
		return 0, fmt.Errorf("invalid hex character: %c", c)
	}
}
