package api

import (
	"errors"
	"net/http"

	"github.com/Gentleman-Programming/brain-context/internal/store"
	"github.com/labstack/echo/v4"
)

type errorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

func writeError(c echo.Context, status int, code, message string) error {
	return c.JSON(status, errorResponse{Error: message, Code: code})
}

func handleStoreError(c echo.Context, err error) error {
	switch {
	case errors.Is(err, store.ErrConflict):
		return writeError(c, http.StatusConflict, "CONFLICT", "resource already exists")
	case errors.Is(err, store.ErrNotFound):
		return writeError(c, http.StatusNotFound, "NOT_FOUND", "resource not found")
	case errors.Is(err, store.ErrUnauthorized):
		return writeError(c, http.StatusUnauthorized, "UNAUTHORIZED", "invalid or expired credentials")
	default:
		return writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
	}
}
