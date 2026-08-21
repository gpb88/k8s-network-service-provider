package network

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"

	"github.com/dcm-project/k8s-network-service-provider/internal/dcm"
)

var reservedNetworkIDs = map[string]bool{
	"health": true,
}

var aep122IDPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

func validateNetworkID(id string) error {
	if reservedNetworkIDs[id] {
		return fmt.Errorf("network ID %q is reserved and cannot be used", id)
	}
	if !aep122IDPattern.MatchString(id) {
		return fmt.Errorf("network ID %q must match AEP-122 pattern", id)
	}
	return nil
}

func validateUserLabels(labels *map[string]string) error {
	if labels == nil {
		return nil
	}
	for k := range *labels {
		if dcm.ReservedLabelKeys[k] {
			return fmt.Errorf("label %q is reserved by DCM and cannot be set by the user", k)
		}
	}
	return nil
}

func generateNetworkID() (string, error) {
	b := make([]byte, 13)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate random ID: %w", err)
	}
	return hex.EncodeToString(b), nil
}
