package morphoroutes

import (
	"encoding/json"
	"net/http"

	jwt "github.com/golang-jwt/jwt/v5"
)

// Middleware for allowing only authenticated users on certain routes.
func AuthenticatedMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// access cookie
		// validate jwt within cookie
		// if jwt is an auth token, allow access
		// else deny access
		isValidated := false
		if isValidated {
			next.ServeHTTP(w, r)
		} else {
			w.Header().Add("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(ErrorMessage{"Not authenticated."})
		}
	})
}

func RefreshSecret(writer http.ResponseWriter, request *http.Request) {
}

type InitLoginRequest struct {
	username, password string
}

type IntermediaryToken struct {
	username string
	jwt.RegisteredClaims
}

func InitLogin(writer http.ResponseWriter, request *http.Request) {
	// check if password hash matches
	// generate intermediary jwt with short expiration
	// token := jwt.NewWithClaims(jwt.SigningMethodHS256, IntermediaryToken{username: "hi!"})
	// signed_token := token.SignedString("somekey") // TODO: get a signing key from the environment
}

type VerifyLoginRequest struct {
	otp string
	jwt.RegisteredClaims
}

type AuthToken struct {
	username string
}

func VerifyLogin(writer http.ResponseWriter, request *http.Request) {
}

func InitiateResetPassword(writer http.ResponseWriter, request *http.Request) {
}

func ResetPassword(writer http.ResponseWriter, request *http.Request) {
}
