package morphoroutes

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

/*
	The auth modules contain a collection of routes and methods to:
	1. Initiate and validate 2FA Login attempts to issue encrypted JWT tokens
	2. Encrypt and Decrypt tokens issued by the system
	3. Check the ACL level of a token
	4. Initiate and validate Password Resets

	The system generates and encrypts JWT tokens.
	There are different secret keys used:
		1. A secret key for hashing passwords (this needs to remain constant) and
		2. A secret key for encrypting tokens.

	These secrets are acquired from the AWS Parameter Store, to avoid storing them locally.
*/

// Middleware for allowing only authenticated users on certain routes.
//
// next is the http handler method to be called if an authenticated user has the right permissions to operate on the data.
//
// permissionFlags is a combination of Permission Flags (see auth_methods.go).
//
// Returns a http handler method wrapped in the authentication middleware.
func AuthenticatedMiddleware(permissionFlags PermissionFlags) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		authState.Init()
		return func(w http.ResponseWriter, r *http.Request) {
			token := r.Header.Get("Authorization")
			if strings.Index(token, "Bearer") == 0 && len(token) > 11 {
				token = token[7:]
			} else {
				w.Header().Add("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(APIMessage{"Not authenticated."})
				return
			}

			authToken, err := VerifyToken[AuthToken](authState.secrets, []byte(token))

			if authToken.Valid() && authToken.Permissions.HasPermission(permissionFlags) && err == nil {
				next(w, r)
			} else {
				w.Header().Add("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(APIMessage{"Not authenticated."})
				return
			}
		}
	}
}

type AuthState struct {
	ResetSessionTokens map[string]string // A map of reset session tokens to the email that it is associated with.
	IsInitialized      bool
	secrets            Secrets
}

var authState AuthState

func (a *AuthState) Init() {
	if a.IsInitialized {
		return
	}
	a.IsInitialized = true
	a.ResetSessionTokens = make(map[string]string)
	a.secrets.Init()
}

/*
Route for a login process. Takes an email and password and returns an encrypted AuthToken.

The token should expire in 30 days.
*/
func (service Service) LoginEndpoint() *Endpoint {
	return NewEndpoint(func(writer http.ResponseWriter, request *http.Request) error {
		authState.Init()
		request.ParseForm()

		email, ok := request.Form["email"]
		if !ok || len(email) != 1 {
			err := fmt.Errorf("An email was not provided.")
			return APIError{http.StatusBadRequest, err.Error(), NewServerError(err)}
		}

		password, ok := request.Form["password"]
		if !ok || len(password) != 1 {
			err := fmt.Errorf("A password was not provided.")
			return APIError{http.StatusBadRequest, err.Error(), NewServerError(err)}
		}

		// check if password hash matches
		db, err := service.GetDB()
		user, err := GetUser(db, email[0])
		if err != nil {
			err := fmt.Errorf("Email or Password provided wasn't correct.")
			return APIError{http.StatusBadRequest, err.Error(), NewServerError(err)}
		}

		if !VerifyUser(user, password[0], authState.secrets) {
			err := fmt.Errorf("Email or Password provided wasn't correct.")
			return APIError{http.StatusBadRequest, err.Error(), NewServerError(err)}
		}

		// generate auth token that expires in a month
		payload, err := GenerateToken(
			authState.secrets,
			&AuthToken{&user.Email, &user.Permissions, &ExpirationDetails{}},
			time.Hour*24*30,
		)
		if err != nil {
			err := fmt.Errorf("Could not generate auth token.")
			return APIError{http.StatusServiceUnavailable, err.Error(), NewServerError(err)}
		}

		writer.Header().Add("Content-Type", "text/plain")
		writer.WriteHeader(http.StatusOK)
		writer.Write(payload)

		return nil
	})
}

/*
Route for verifying a login attempt with an otp.
Accepts an encrypted IntermediaryLoginToken in the authorization header section
and an otp in the form section.

Returns an encrypted authorization JWT.
*/
func (service Service) VerifyLoginEndpoint() *Endpoint {
	return NewEndpoint(func(writer http.ResponseWriter, request *http.Request) error {
		authState.Init()
		bytes, err := io.ReadAll(request.Body)
		if err != nil {
			return APIError{http.StatusBadRequest, "Could not read auth token.", NewServerError(err)}
		}

		token, err := VerifyToken[AuthToken](authState.secrets, bytes)
		if err != nil {
			return APIError{http.StatusBadRequest, "Invalid token.", NewServerError(err)}
		}

		if false {
			log.Println("token:", token, token.Valid())
		}

		resp := []byte{}
		writer.Header().Add("Content-Type", "text/plain")
		writer.WriteHeader(http.StatusOK)
		writer.Write(resp)

		return nil
	})
}

/*
Route for starting a password reset session.
Takes an email, and sends a reset request to the email if it exists.

Rate limit this route, but not transparently.
*/
func (service Service) InitiateResetPasswordEndpoint() *Endpoint {
	return NewEndpoint(func(writer http.ResponseWriter, request *http.Request) error {
		// TODO implement this after acquiring a domain.
		authState.Init()
		err := request.ParseForm()
		if err != nil {
			return APIError{http.StatusBadRequest, "Could not read form data.", NewServerError(err)}
		}

		return nil
	})
}

/*
Route for resetting a password.
Takes a reset session token in the URL parameter section, and a password in the form section.

Once the password is reset, invalidate the password reset session token.
*/
func (service Service) ResetPasswordEndpoint() *Endpoint {
	return NewEndpoint(func(writer http.ResponseWriter, request *http.Request) error {
		var err error
		authState.Init()

		tokenString := request.Header.Get("Authorization")
		if strings.Index(tokenString, "Bearer ") == 0 {
			tokenString = tokenString[7:]
		}

		token, err := VerifyToken[ResetSessionToken](authState.secrets, []byte(tokenString))
		if err != nil {
			err := fmt.Errorf("No reset session.")
			return APIError{http.StatusBadRequest, err.Error(), NewServerError(err)}
		}

		if !token.Valid() {
			err := fmt.Errorf("Invalid reset session.")
			return APIError{http.StatusBadRequest, err.Error(), NewServerError(err)}
		}

		err = request.ParseForm()
		if err != nil {
			return APIError{http.StatusBadRequest, "Could not parse form data.", NewServerError(err)}
		}

		db, err := service.GetDB()
		if err != nil {
			return APIError{http.StatusServiceUnavailable, OPEN_DB_ERROR, NewServerError(err)}
		}

		password, ok := request.Form["password"]
		if !ok || len(password) != 1 {
			err := fmt.Errorf("A password was not provided.")
			return APIError{http.StatusBadRequest, err.Error(), NewServerError(err)}
		}

		err = ReplacePassword(db, *token.Email, password[0], authState.secrets)
		if err != nil {
			return APIError{http.StatusServiceUnavailable, WRITE_DB_ERROR, NewServerError(err)}
		}

		if err = SuccessfulResponseJson(writer, request, APIMessage{"Password was reset."}); err != nil {
			return APIError{http.StatusServiceUnavailable, JSON_MARSHAL_ERROR, NewServerError(err)}
		}

		return nil
	})
}
