package api

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// newPublicRateLimiter returns a rate limiter for unauthenticated endpoints
// that are exposed to the internet (register, login, invite redeem).
//
// Settings: 5 requests/second per IP, burst of 10, stale entries expire
// after 5 minutes.  These values are deliberately conservative for a
// public API — adjust if legitimate traffic patterns require it.
func newPublicRateLimiter() echo.MiddlewareFunc {
	return middleware.RateLimiterWithConfig(middleware.RateLimiterConfig{
		Skipper: middleware.DefaultSkipper,
		Store: middleware.NewRateLimiterMemoryStoreWithConfig(
			middleware.RateLimiterMemoryStoreConfig{
				Rate:      5,
				Burst:     10,
				ExpiresIn: 5 * time.Minute,
			},
		),
		IdentifierExtractor: func(c echo.Context) (string, error) {
			return c.RealIP(), nil
		},
		ErrorHandler: func(c echo.Context, err error) error {
			return writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "could not determine client identity")
		},
		DenyHandler: func(c echo.Context, identifier string, err error) error {
			return writeError(c, http.StatusTooManyRequests, "RATE_LIMITED", "too many requests — please try again later")
		},
	})
}

// newAuthRateLimiter returns a stricter rate limiter for authenticated
// endpoints that issue or rotate credentials (renew key, create tokens).
//
// Settings: 3 requests/second per IP, burst of 5.
func newAuthRateLimiter() echo.MiddlewareFunc {
	return middleware.RateLimiterWithConfig(middleware.RateLimiterConfig{
		Skipper: middleware.DefaultSkipper,
		Store: middleware.NewRateLimiterMemoryStoreWithConfig(
			middleware.RateLimiterMemoryStoreConfig{
				Rate:      3,
				Burst:     5,
				ExpiresIn: 5 * time.Minute,
			},
		),
		IdentifierExtractor: func(c echo.Context) (string, error) {
			return c.RealIP(), nil
		},
		ErrorHandler: func(c echo.Context, err error) error {
			return writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "could not determine client identity")
		},
		DenyHandler: func(c echo.Context, identifier string, err error) error {
			return writeError(c, http.StatusTooManyRequests, "RATE_LIMITED", "too many requests — please try again later")
		},
	})
}
