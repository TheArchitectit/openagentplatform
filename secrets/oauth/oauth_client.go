package oauth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"net/url"
	"time"
	"github.com/openagentplatform/openagentplatform/internal/audit"
)

func (a *AuthorizationServer) RegisterClient(ctx context.Context, metadata ClientMetadata) (*ClientRegistrationResponse, error) {
	if len(metadata.RedirectURIs) == 0 {
		return nil, fmt.Errorf("%w: at least one redirect_uri is required", ErrInvalidRedirectURI)
	}
	for _, uri := range metadata.RedirectURIs {
		if _, err := url.Parse(uri); err != nil {
			return nil, fmt.Errorf("%w: %s", ErrInvalidRedirectURI, uri)
		}
	}

	clientID, err := generateToken("client_")
	if err != nil {
		return nil, fmt.Errorf("oauth: generate client ID: %w", err)
	}
	clientSecret, err := generateToken("secret_")
	if err != nil {
		return nil, fmt.Errorf("oauth: generate client secret: %w", err)
	}
	regAccessToken, err := generateToken("reg_")
	if err != nil {
		return nil, fmt.Errorf("oauth: generate registration access token: %w", err)
	}

	secretHash := sha256.Sum256([]byte(clientSecret))
	prefix := ""
	if len(clientSecret) >= 4 {
		prefix = clientSecret[:4]
	}

	now := time.Now().UTC()
	client := &RegisteredClient{
		ClientID:                clientID,
		ClientSecretHash:        secretHash[:],
		ClientSecretPrefix:      prefix,
		Metadata:                metadata,
		CreatedAt:               now,
		RegistrationAccessToken: regAccessToken,
	}

	a.mu.Lock()
	a.clients[clientID] = client
	a.mu.Unlock()

	a.emitAudit(ctx, "oauth.client.register", clientID, audit.OutcomeSuccess,
		fmt.Sprintf("redirect_uris=%d name=%s", len(metadata.RedirectURIs), metadata.ClientName))

	authMethod := metadata.TokenEndpointAuthMethod
	if authMethod == "" {
		authMethod = "client_secret_basic"
	}

	return &ClientRegistrationResponse{
		ClientID:                clientID,
		ClientSecret:            clientSecret,
		ClientIDIssuedAt:        now.Unix(),
		ClientSecretExpiresAt:   now.Add(RegistrationAccessTokenTTL).Unix(),
		RegistrationAccessToken: regAccessToken,
		RegistrationClientURI:   a.registrationEndpoint + "/" + clientID,
		RedirectURIs:            metadata.RedirectURIs,
		TokenEndpointAuthMethod: authMethod,
		GrantTypes:              metadata.GrantTypes,
		ResponseTypes:           metadata.ResponseTypes,
		ClientName:              metadata.ClientName,
		ClientURI:               metadata.ClientURI,
		Scope:                   metadata.Scope,
	}, nil
}

// RotateClientSecret generates a new client secret for the given client ID.
// The old secret is invalidated immediately.
func (a *AuthorizationServer) RotateClientSecret(ctx context.Context, clientID string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	client, ok := a.clients[clientID]
	if !ok {
		return "", ErrClientNotFound
	}

	newSecret, err := generateToken("secret_")
	if err != nil {
		return "", fmt.Errorf("oauth: generate secret: %w", err)
	}

	hash := sha256.Sum256([]byte(newSecret))
	client.ClientSecretHash = hash[:]
	if len(newSecret) >= 4 {
		client.ClientSecretPrefix = newSecret[:4]
	}

	a.emitAudit(ctx, "oauth.client.rotate_secret", clientID, audit.OutcomeSuccess, "")
	return newSecret, nil
}

// DeleteClient removes a client and revokes all its tokens and codes.
func (a *AuthorizationServer) DeleteClient(ctx context.Context, clientID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	client, ok := a.clients[clientID]
	if !ok {
		return ErrClientNotFound
	}

	// Revoke all tokens and codes belonging to this client.
	for _, tok := range a.tokens {
		if tok.ClientID == clientID {
			tok.Revoked = true
		}
	}
	for _, rt := range a.refresh {
		if rt.ClientID == clientID {
			rt.Revoked = true
		}
	}
	for _, code := range a.codes {
		if code.ClientID == clientID {
			code.Used = true
		}
	}

	delete(a.clients, clientID)
	a.emitAudit(ctx, "oauth.client.delete", clientID, audit.OutcomeSuccess, "")
	_ = client
	return nil
}

// GetClient retrieves a registered client by ID.
func (a *AuthorizationServer) GetClient(clientID string) (*RegisteredClient, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	client, ok := a.clients[clientID]
	if !ok {
		return nil, ErrClientNotFound
	}
	return client, nil
}

// VerifyClientSecret checks the provided client secret against the stored hash.
func (a *AuthorizationServer) VerifyClientSecret(clientID, clientSecret string) error {
	a.mu.RLock()
	defer a.mu.RUnlock()

	client, ok := a.clients[clientID]
	if !ok {
		return ErrClientNotFound
	}
	hash := sha256.Sum256([]byte(clientSecret))
	if subtle.ConstantTimeCompare(hash[:], client.ClientSecretHash) != 1 {
		return ErrInvalidClientSecret
	}
	return nil
}

// --- Protected Resource Metadata (RFC 9728) ---

// ProtectedResourceMetadata returns the RFC 9728 metadata document.
func (a *AuthorizationServer) ProtectedResourceMetadata() *ProtectedResourceMetadata {
	return &ProtectedResourceMetadata{
		Resource:               a.resource,
		AuthorizationServers:   []string{a.issuer},
		BearerMethodsSupported: []string{"header", "body"},
		DPoPSupported:          true,
		ScopesSupported:        []string{"read", "write", "admin", "agent:execute"},
	}
}

// --- Authorization Code Grant with PKCE (RFC 7636) ---

// AuthorizeCodeRequest is the parsed authorization request.
