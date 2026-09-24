package auth

const (
	// tokenType is the scheme clients put before the token: "Authorization: Bearer <token>".
	tokenType = "Bearer"

	// dummyHash is compared against when the email is unknown, so both failures take the same time.
	dummyHash = "$2a$10$CHItcZdGTaN.9ZYG1ip71uSOWV2Y37Q.UnoDB8ztGxT5fC17mKYCy"

	// Why a login failed, for the log only: the caller gets the same answer either way.
	reasonUnknownEmail  = "unknown_email"
	reasonWrongPassword = "wrong_password"
)
