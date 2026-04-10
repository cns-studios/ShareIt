package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"shareit/internal/config"
	"strings"

	"github.com/gin-gonic/gin"
)

type AuthHandler struct {
	cfg *config.Config
}

func NewAuthHandler(cfg *config.Config) *AuthHandler {
	return &AuthHandler{cfg: cfg}
}

func (h *AuthHandler) Login(c *gin.Context) {
	if h.cfg.CNSAuthURL == "" || h.cfg.CNSAuthClientID == "" {
		c.String(http.StatusInternalServerError, "CNS Auth is not configured")
		return
	}

	// Use Hex for state and verifier to avoid any Base64 encoding/decoding issues in URLs or cookies
	state := generateRandomHex(16)    // 32 chars
	verifier := generateRandomHex(32) // 64 chars
	challenge := generateChallenge(verifier)

	isSecure := strings.HasPrefix(h.cfg.BaseURL, "https")

	// Store verifier and state in cookies (short-lived)
	// We use empty domain string to set a Host-Only cookie for the current host.
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("pkce_verifier", verifier, 600, "/", "", isSecure, true)
	c.SetCookie("pkce_state", state, 600, "/", "", isSecure, true)

	authURL := h.cfg.CNSAuthURL + "/login"
	redirectURI := h.cfg.BaseURL + "/auth/callback"

	val := url.Values{}
	val.Add("client_id", h.cfg.CNSAuthClientID)
	val.Add("redirect_uri", redirectURI)
	val.Add("response_type", "code")
	val.Add("code_challenge", challenge)
	val.Add("code_challenge_method", "S256")
	val.Add("state", state)
	val.Add("scope", "openid profile")

	c.Redirect(http.StatusFound, authURL+"?"+val.Encode())
}

func (h *AuthHandler) Callback(c *gin.Context) {
	code := c.Query("code")
	state := c.Query("state")

	savedState, err := c.Cookie("pkce_state")
	if err != nil {
		c.String(http.StatusBadRequest, "Invalid state: missing pkce_state cookie. (Error: %v)", err)
		return
	}
	if savedState != state {
		fmt.Printf("State Mismatch: saved_cookie=%s, got_url=%s\n", savedState, state)
		c.String(http.StatusBadRequest, "Invalid state: mismatch. Cookie had '%s' but URL had '%s'.", savedState, state)
		return
	}

	verifier, err := c.Cookie("pkce_verifier")
	if err != nil {
		c.String(http.StatusBadRequest, "Missing verifier cookie. Your session may have expired.")
		return
	}

	isSecure := strings.HasPrefix(h.cfg.BaseURL, "https")

	// Clear PKCE cookies
	c.SetCookie("pkce_verifier", "", -1, "/", "", isSecure, true)
	c.SetCookie("pkce_state", "", -1, "/", "", isSecure, true)

	// Exchange code for tokens
	tokenURL := h.cfg.CNSAuthURL + "/v2/token"
	redirectURI := h.cfg.BaseURL + "/auth/callback"

	payload := map[string]string{
		"code":          code,
		"code_verifier": verifier,
		"client_id":     h.cfg.CNSAuthClientID,
		"redirect_uri":  redirectURI,
	}

	body, _ := json.Marshal(payload)
	resp, err := http.Post(tokenURL, "application/json", strings.NewReader(string(body)))
	if err != nil {
		c.String(http.StatusInternalServerError, "Failed to connect to auth server: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.String(http.StatusInternalServerError, "Token exchange failed: %s", resp.Status)
		return
	}

	var result struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		c.String(http.StatusInternalServerError, "Failed to parse token response")
		return
	}

	// Set auth_token as a host-only cookie
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("auth_token", result.AccessToken, 3600*24*7, "/", "", isSecure, true)

	c.Redirect(http.StatusFound, "/")
}

func (h *AuthHandler) Logout(c *gin.Context) {
	isSecure := strings.HasPrefix(h.cfg.BaseURL, "https")
	c.SetCookie("auth_token", "", -1, "/", "", isSecure, true)
	c.Redirect(http.StatusFound, "/")
}

func generateRandomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func generateChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}
