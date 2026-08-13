package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"omnirelay/internal/hub"
)

var currentHub *hub.Hub

func SetHub(h *hub.Hub) {
	currentHub = h
}

func WebSocketUpgrader(jwtSecret string, allowedOrigins []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if !isWSOriginAllowed(origin, c.Request.Host, allowedOrigins) {
			c.JSON(http.StatusForbidden, gin.H{"error": "origin not allowed"})
			c.Abort()
			return
		}

		tokenString := c.Query("token")
		if tokenString == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing token query parameter"})
			c.Abort()
			return
		}

		userID, err := parseWSUserID(tokenString, jwtSecret)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			c.Abort()
			return
		}

		wsConn, err := hub.Upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to upgrade connection"})
			c.Abort()
			return
		}

		hubConn := currentHub.Register(userID)
		go hubConn.WritePump(wsConn)
		hubConn.ReadPump(wsConn)
	}
}

// parseWSUserID validates a JWT and extracts the user_id claim.
func parseWSUserID(tokenString, jwtSecret string) (int64, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(jwtSecret), nil
	})
	if err != nil || !token.Valid {
		return 0, errors.New("invalid or expired token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return 0, errors.New("invalid token claims")
	}

	userIDFloat, ok := claims["user_id"].(float64)
	if !ok {
		return 0, errors.New("invalid token claims")
	}
	return int64(userIDFloat), nil
}

// isWSOriginAllowed checks a WebSocket handshake Origin against the request
// host and an allow list. Same-origin requests (the dashboard's own WS
// connection) are always permitted. An empty allow list preserves the legacy
// allow-any behavior; when origins are configured, only exact matches are
// accepted and requests without an Origin header are rejected.
func isWSOriginAllowed(origin, host string, allowedOrigins []string) bool {
	if origin == "" {
		return len(allowedOrigins) == 0
	}
	if len(allowedOrigins) == 0 {
		return true
	}
	if origin == "http://"+host || origin == "https://"+host {
		return true
	}
	for _, allowed := range allowedOrigins {
		if origin == strings.TrimRight(allowed, "/") {
			return true
		}
	}
	return false
}
