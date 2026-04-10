package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
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

	state := generateRandomString(32)
	verifier := generateRandomString(64)
	challenge := generateChallenge(verifier)

	// Store verifier and state in cookies (short-lived)
	c.SetCookie("pkce_verifier", verifier, 600, "/", "", h.cfg.IsProd(), true)
	c.SetCookie("pkce_state", state, 600, "/", "", h.cfg.IsProd(), true)

	authURL := h.cfg.CNSAuthURL + "/login"
	redirectURI := h.cfg.BaseURL + "/auth/callback"

	q := []string{
		"client_id=" + h.cfg.CNSAuthClientID,
		"redirect_uri=" + redirectURI,
		"response_type=code",
		"code_challenge=" + challenge,
		"code_challenge_method=S256",
		"state=" + state,
	}

	c.Redirect(http.StatusFound, authURL+"?"+strings.Join(q, "&"))
}

func (h *AuthHandler) Callback(c *gin.Context) {
	code := c.Query("code")
	state := c.Query("state")

	savedState, err := c.Cookie("pkce_state")
	if err != nil || savedState != state {
		c.String(http.StatusBadRequest, "Invalid state")
		return
	}

	verifier, err := c.Cookie("pkce_verifier")
	if err != nil {
		c.String(http.StatusBadRequest, "Missing verifier")
		return
	}

	// Clear PKCE cookies
	c.SetCookie("pkce_verifier", "", -1, "/", "", h.cfg.IsProd(), true)
	c.SetCookie("pkce_state", "", -1, "/", "", h.cfg.IsProd(), true)

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
		c.String(http.StatusInternalServerError, "Failed to exchange code")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.String(http.StatusInternalServerError, "Token exchange failed")
		return
	}

	var result struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		c.String(http.StatusInternalServerError, "Failed to decode token response")
		return
	}

	// Set auth_token cookie
	c.SetCookie("auth_token", result.AccessToken, 3600*24*7, "/", "", h.cfg.IsProd(), true)

	c.Redirect(http.StatusFound, "/")
}

func (h *AuthHandler) Logout(c *gin.Context) {
	c.SetCookie("auth_token", "", -1, "/", "", h.cfg.IsProd(), true)
	c.Redirect(http.StatusFound, "/")
}

func generateRandomString(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func generateChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}
