package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/Gentleman-Programming/brain-context/internal/auth"
	"github.com/Gentleman-Programming/brain-context/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v4"
)

func AuthMiddleware(st *store.Store) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			header := strings.TrimSpace(c.Request().Header.Get(echo.HeaderAuthorization))
			if !strings.HasPrefix(header, "Bearer ") {
				return writeError(c, http.StatusUnauthorized, "UNAUTHORIZED", "missing bearer token")
			}

			token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
			record, err := st.AuthenticateAPIKey(c.Request().Context(), token)
			if err != nil {
				if errors.Is(err, store.ErrUnauthorized) {
					return writeError(c, http.StatusUnauthorized, "UNAUTHORIZED", "invalid or expired credentials")
				}
				return writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
			}

			tx, err := st.Pool().BeginTx(c.Request().Context(), pgx.TxOptions{})
			if err != nil {
				return writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
			}

			ctx := store.ContextWithTx(c.Request().Context(), tx)
			if err := store.SetTenantContext(ctx, tx, record.TenantID); err != nil {
				_ = tx.Rollback(ctx)
				return writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to set tenant context")
			}

			c.SetRequest(c.Request().WithContext(ctx))
			setAuthContext(c, record)

			if err := next(c); err != nil {
				_ = tx.Rollback(ctx)
				return err
			}

			if err := tx.Commit(ctx); err != nil {
				return writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to commit request transaction")
			}

			return nil
		}
	}
}

func RequireScopes(allowed ...auth.TokenScope) echo.MiddlewareFunc {
	allowedSet := make(map[auth.TokenScope]struct{}, len(allowed))
	for _, scope := range allowed {
		allowedSet[scope] = struct{}{}
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			scope, ok := scopeFromContext(c)
			if !ok {
				return writeError(c, http.StatusUnauthorized, "UNAUTHORIZED", "missing auth context")
			}
			if _, ok := allowedSet[scope]; !ok {
				return writeError(c, http.StatusForbidden, "FORBIDDEN", "token does not have the required scope")
			}
			return next(c)
		}
	}
}
