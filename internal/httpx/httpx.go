package httpx

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/xingshanghe/x-phone/internal/auth"
	"github.com/xingshanghe/x-phone/internal/service"
)

const requestIDKey = "requestId"
const accountIDKey = "accountId"
const hostIDKey = "hostId"

type Response struct {
	Success bool   `json:"success"`
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
	Meta    *Meta  `json:"meta,omitempty"`
}

type Meta struct {
	RequestID string `json:"requestId,omitempty"`
	Page      int    `json:"page,omitempty"`
	PageSize  int    `json:"pageSize,omitempty"`
	Total     int64  `json:"total,omitempty"`
}

func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		rid := strings.TrimSpace(c.GetHeader("X-Request-ID"))
		if rid == "" {
			buf := make([]byte, 8)
			_, _ = rand.Read(buf)
			rid = hex.EncodeToString(buf)
		}
		c.Set(requestIDKey, rid)
		c.Header("X-Request-ID", rid)
		c.Next()
	}
}

func ID(c *gin.Context) string {
	value, _ := c.Get(requestIDKey)
	rid, _ := value.(string)
	return rid
}

func OK(c *gin.Context, data any, meta *Meta) {
	if meta == nil {
		meta = &Meta{}
	}
	meta.RequestID = ID(c)
	c.JSON(http.StatusOK, Response{Success: true, Code: 0, Message: "ok", Data: data, Meta: meta})
}

func Fail(c *gin.Context, err error) {
	status, code, message := mapError(err)
	c.JSON(status, Response{
		Success: false,
		Code:    code,
		Message: message,
		Meta:    &Meta{RequestID: ID(c)},
	})
}

func mapError(err error) (int, int, string) {
	switch {
	case errors.Is(err, service.ErrUnauthorized):
		return http.StatusUnauthorized, 40101, "unauthorized"
	case errors.Is(err, service.ErrNotFound):
		return http.StatusNotFound, 40401, "not found"
	case errors.Is(err, service.ErrValidation):
		return http.StatusBadRequest, 40001, err.Error()
	case errors.Is(err, service.ErrBinderfs):
		return http.StatusConflict, 40901, err.Error()
	case errors.Is(err, service.ErrConflict):
		return http.StatusConflict, 40902, err.Error()
	case errors.Is(err, service.ErrLocked):
		return http.StatusConflict, 40903, err.Error()
	case errors.Is(err, service.ErrNotImplemented):
		return http.StatusNotImplemented, 50101, "script tasks are not implemented"
	default:
		return http.StatusInternalServerError, 50001, "internal error"
	}
}

func Page(c *gin.Context) (int, int) {
	page := atoi(c.Query("page"), 1)
	size := atoi(c.Query("pageSize"), 20)
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 200 {
		size = 200
	}
	return page, size
}

func atoi(value string, fallback int) int {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func UserAuth(signer *auth.Signer) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, err := signer.Parse(bearer(c))
		if err != nil {
			Fail(c, service.ErrUnauthorized)
			c.Abort()
			return
		}
		c.Set(accountIDKey, accountID)
		c.Next()
	}
}

func AccountID(c *gin.Context) string {
	value, _ := c.Get(accountIDKey)
	id, _ := value.(string)
	return id
}

func HostID(c *gin.Context) string {
	value, _ := c.Get(hostIDKey)
	id, _ := value.(string)
	return id
}

func SetHostID(c *gin.Context, id string) {
	c.Set(hostIDKey, id)
}

func bearer(c *gin.Context) string {
	header := c.GetHeader("Authorization")
	if strings.HasPrefix(strings.ToLower(header), "bearer ") {
		return strings.TrimSpace(header[7:])
	}
	return strings.TrimSpace(c.Query("token"))
}

func CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-ID")
		c.Header("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
