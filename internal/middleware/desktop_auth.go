package middleware

import (
	"net/http"

	"shareit/internal/models"
	"shareit/internal/storage"

	"github.com/gin-gonic/gin"
)

const DesktopAPIKeyCtx = "desktop_api_key"

func DesktopAuthMiddleware(db *storage.Postgres) gin.HandlerFunc {
	return func(c *gin.Context) {
		keyValue := c.GetHeader("X-API-KEY")
		if keyValue == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, models.ErrorResponse{
				Error: "Missing API key",
				Code:  "MISSING_API_KEY",
			})
			return
		}

		key, err := db.GetDesktopAPIKey(c.Request.Context(), keyValue)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, models.ErrorResponse{
				Error: models.ErrAPIKeyNotFound.Message,
				Code:  models.ErrAPIKeyNotFound.Code,
			})
			return
		}

		c.Set(DesktopAPIKeyCtx, key)
		c.Next()
	}
}

func GetDesktopAPIKey(c *gin.Context) *models.DesktopAPIKey {
	val, _ := c.Get(DesktopAPIKeyCtx)
	key, _ := val.(*models.DesktopAPIKey)
	return key
}