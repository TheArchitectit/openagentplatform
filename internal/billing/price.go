package billing

import "github.com/openagentplatform/openagentplatform/internal/license"

// priceIDForTier resolves a Stripe price ID from the environment. The
// Community tier is free and has no price ID.
func priceIDForTier(tier license.Tier) (string, error) {
	switch tier {
	case license.TierCommunity:
		return "", nil // free tier
	case license.TierProfessional:
		pro, enterprise, err := PriceIDs()
		if err != nil {
			return "", err
		}
		_ = enterprise
		return pro, nil
	case license.TierEnterprise:
		_, enterprise, err := PriceIDs()
		if err != nil {
			return "", err
		}
		return enterprise, nil
	default:
		return "", ErrUnknownTier
	}
}
